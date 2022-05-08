// Copyright 2022 The Alpaca Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Running an integration test requires a squid binary, which isn't available on
// GitHub Actions runners, nor is it likely to be available on random developer
// machines. So the tests in this file are disabled by default using a build
// constraint, and need to be run using `go test ./... -tags=squid`.

// +build squid

package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func findAvailablePort(t *testing.T) string {
	l, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	defer l.Close()
	_, port, err := net.SplitHostPort(l.Addr().String())
	require.NoError(t, err)
	return port
}

func configureSquid(t *testing.T, dir string, cert []byte, key interface{}) string {
	certFile, err := os.Create(filepath.Join(dir, "cert.pem"))
	require.NoError(t, err)
	defer certFile.Close()
	require.NoError(t, pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: cert}))

	keyFile, err := os.Create(filepath.Join(dir, "key.pem"))
	require.NoError(t, err)
	defer keyFile.Close()
	keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	require.NoError(t, pem.Encode(keyFile, &pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes}))

	httpsPort := findAvailablePort(t)

	// TODO: configure squid to require NTLM authentication
	// https://wiki.squid-cache.org/ConfigExamples/Authenticate/Ntlm
	squidConf, err := os.Create(filepath.Join(dir, "squid.conf"))
	require.NoError(t, err)
	defer squidConf.Close()
	fmt.Fprintf(squidConf, "access_log access.log\n")
	fmt.Fprintf(squidConf, "cache_log cache.log\n")
	fmt.Fprintf(squidConf, "pid_filename none\n")
	fmt.Fprintf(squidConf, "http_access allow localhost\n")
	fmt.Fprintf(squidConf, "http_access deny all\n")
	fmt.Fprintf(squidConf, "https_port %s cert=cert.pem key=key.pem\n", httpsPort)

	return httpsPort
}

func writeFileToLog(t *testing.T, path string) error {
	buf, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	t.Logf("%s:\n%s", path, string(buf))
	return nil
}

func waitForServer(address string) error {
	delays := []time.Duration{
		0,
		50 * time.Millisecond,
		500 * time.Millisecond,
		5 * time.Second,
	}
	var err error
	for _, delay := range delays {
		time.Sleep(delay)
		var conn net.Conn
		if conn, err = net.Dial("tcp", address); err != nil {
			continue
		}
		conn.Close()
		return nil
	}
	return err
}

func serveString(s string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(s))
	}
}

func TestWithSquid(t *testing.T) {
	// This is an integration test that sets up the following components:
	//
	//                        +-> pac server
	//                        |
	// http client -> alpaca -+-> squid -> https server
	//
	// The connection between alpaca and squid is TLS-encrypted, as is the
	// connection between the http client and the https server. In both
	// cases, self-signed certs are generated and configured in-test.

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := x509.Certificate{
		SerialNumber: &big.Int{},
		Subject:      pkix.Name{Organization: []string{"Alpaca, Inc."}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	cert, err := x509.CreateCertificate(rand.Reader, &template, &template, key.Public(), key)
	require.NoError(t, err)

	tempDir, err := os.MkdirTemp("", "squid")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	httpsPort := configureSquid(t, tempDir, cert, key)
	defer writeFileToLog(t, filepath.Join(tempDir, "access.log"))
	defer writeFileToLog(t, filepath.Join(tempDir, "cache.log"))

	cmd := exec.Command("squid", "-f", "squid.conf", "-N")
	cmd.Dir = tempDir
	require.NoError(t, cmd.Start())
	defer cmd.Process.Kill()
	waitForServer("localhost:" + httpsPort)

	httpsServer := httptest.NewTLSServer(serveString(fmt.Sprintf("https server")))
	defer httpsServer.Close()

	pacScript := fmt.Sprintf(
		`function FindProxyForURL(url, host) { return "HTTPS localhost:%s"; }`,
		httpsPort,
	)
	pacServer := httptest.NewServer(serveString(pacScript))
	defer pacServer.Close()
	t.Logf("pac server is available on %s", pacServer.URL)

	// Squid is set up to use a self-signed certificate; configure Alpaca
	// to accept it.
	tlsClientConfig = &tls.Config{RootCAs: x509.NewCertPool()}
	c, err := x509.ParseCertificate(cert)
	require.NoError(t, err)
	tlsClientConfig.RootCAs.AddCert(c)

	// Alpaca logs to stderr by default; redirect to a buffer and write to
	// the test log in case it's useful for debugging.
	var logs bytes.Buffer
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.SetOutput(&logs)
	defer func() { t.Logf("alpaca logs:\n%s", logs.String()) }()

	// Run (most of) Alpaca in a goroutine.
	port, err := strconv.Atoi(findAvailablePort(t))
	require.NoError(t, err)
	alpaca := createServer(port, pacServer.URL, nil)
	go alpaca.ListenAndServe()
	defer alpaca.Close()
	waitForServer(alpaca.Addr)
	t.Logf("alpaca is listening on port %d", port)

	tr := &http.Transport{
		Proxy:           http.ProxyURL(&url.URL{Host: alpaca.Addr}),
		TLSClientConfig: &tls.Config{RootCAs: x509.NewCertPool()},
	}
	tr.TLSClientConfig.RootCAs.AddCert(httpsServer.Certificate())

	req, err := http.NewRequest(http.MethodGet, httpsServer.URL, nil)
	require.NoError(t, err)
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "https server", string(body))
}
