package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// It is possible, and probably more accurate, to test this using a sub-process
// test (see https://talks.golang.org/2014/testing.slide#23). This would
// involve building three separate binaries for the test client, proxy and test
// server. But doing everything inside the test process means that we'll
// collect coverage data. It's also a bit simpler to implement.

func serverHandlerFunc(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "Hello, client")
}

func testClient(t *testing.T, client *http.Client, serverUrl string) {
	resp, err := client.Get(serverUrl)
	require.Nil(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	buf, err := ioutil.ReadAll(resp.Body)
	require.Nil(t, err)
	assert.Equal(t, "Hello, client\n", string(buf))
}

func TestProxyDirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(serverHandlerFunc))
	defer server.Close()
	ph, err := NewDirectProxyHandler()
	require.Nil(t, err)
	proxy := httptest.NewServer(ph)
	defer proxy.Close()
	proxyUrl, err := url.Parse(proxy.URL)
	require.Nil(t, err)
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyUrl)}}
	testClient(t, client, server.URL)
}

func TestProxyDirectTls(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(serverHandlerFunc))
	defer server.Close()
	ph, err := NewDirectProxyHandler()
	require.Nil(t, err)
	proxy := httptest.NewServer(ph)
	defer proxy.Close()
	proxyUrl, err := url.Parse(proxy.URL)
	require.Nil(t, err)
	cp := x509.NewCertPool()
	cp.AddCert(server.Certificate())
	client := &http.Client{Transport: &http.Transport{
		Proxy:           http.ProxyURL(proxyUrl),
		TLSClientConfig: &tls.Config{RootCAs: cp}}}
	testClient(t, client, server.URL)
}
