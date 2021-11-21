// Copyright 2019, 2021, 2022 The Alpaca Authors
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

package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testServer struct {
	requests chan<- string
}

func (ts testServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ts.requests <- fmt.Sprintf("%s to server", req.Method)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "Hello, client")
}

type testProxy struct {
	requests chan<- string
	name     string
	delegate http.Handler
}

func (tp testProxy) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	tp.requests <- fmt.Sprintf("%s to %s", req.Method, tp.name)
	tp.delegate.ServeHTTP(w, req)
}

func newDirectProxy() ProxyHandler {
	return NewProxyHandler(nil, http.ProxyURL(nil), func(string) {})
}

func newChildProxy(parent *httptest.Server) http.Handler {
	parentURL := &url.URL{Host: parent.Listener.Addr().String()}
	childProxy := NewProxyHandler(nil, getProxyFromContext, func(string) {})
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := context.WithValue(req.Context(), contextKeyProxy, parentURL)
		reqWithProxy := req.WithContext(ctx)
		childProxy.ServeHTTP(w, reqWithProxy)
	})
}

func proxyServer(t *testing.T, proxy *httptest.Server) proxyFunc {
	u, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	return http.ProxyURL(u)
}

func tlsConfig(server *httptest.Server) *tls.Config {
	cp := x509.NewCertPool()
	cp.AddCert(server.Certificate())
	return &tls.Config{RootCAs: cp}
}

func testGetRequest(t *testing.T, tr *http.Transport, serverURL string) {
	client := http.Client{Transport: tr}
	resp, err := client.Get(serverURL)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	buf, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "Hello, client\n", string(buf))
}

func TestGetViaProxy(t *testing.T) {
	requests := make(chan string, 2)
	server := httptest.NewServer(testServer{requests})
	defer server.Close()
	// Proxy request should not go to the mux. The empty mux will always return 404.
	mux := http.NewServeMux()
	proxy := httptest.NewServer(testProxy{requests, "proxy", newDirectProxy().WrapHandler(mux)})
	defer proxy.Close()
	tr := &http.Transport{Proxy: proxyServer(t, proxy)}
	testGetRequest(t, tr, server.URL)
	require.Len(t, requests, 2)
	assert.Equal(t, "GET to proxy", <-requests)
	assert.Equal(t, "GET to server", <-requests)
}

func TestGetOverTlsViaProxy(t *testing.T) {
	requests := make(chan string, 2)
	server := httptest.NewTLSServer(testServer{requests})
	defer server.Close()
	// Proxy request should not go to the mux. The empty mux will always return 404.
	mux := http.NewServeMux()
	proxy := httptest.NewServer(testProxy{requests, "proxy", newDirectProxy().WrapHandler(mux)})
	defer proxy.Close()
	tr := &http.Transport{Proxy: proxyServer(t, proxy), TLSClientConfig: tlsConfig(server)}
	testGetRequest(t, tr, server.URL)
	require.Len(t, requests, 2)
	assert.Equal(t, "CONNECT to proxy", <-requests)
	assert.Equal(t, "GET to server", <-requests)
}

func TestGetOriginURLsNotProxied(t *testing.T) {
	requests := make(chan string, 2)
	mux := http.NewServeMux()
	mux.HandleFunc("/origin", func(w http.ResponseWriter, req *http.Request) {
		_, err := w.Write([]byte("Hello, client\n"))
		require.NoError(t, err)
	})
	proxy := httptest.NewServer(testProxy{requests, "proxy", newDirectProxy().WrapHandler(mux)})
	defer proxy.Close()
	testGetRequest(t, &http.Transport{}, proxy.URL+"/origin")
	require.Len(t, requests, 1)
	assert.Equal(t, "GET to proxy", <-requests)
}

func TestGetViaTwoProxies(t *testing.T) {
	requests := make(chan string, 3)
	server := httptest.NewServer(testServer{requests})
	defer server.Close()
	parent := httptest.NewServer(testProxy{requests, "parent proxy", newDirectProxy()})
	defer parent.Close()
	child := httptest.NewServer(testProxy{requests, "child proxy", newChildProxy(parent)})
	defer child.Close()
	tr := &http.Transport{Proxy: proxyServer(t, child)}
	testGetRequest(t, tr, server.URL)
	require.Len(t, requests, 3)
	assert.Equal(t, "GET to child proxy", <-requests)
	assert.Equal(t, "GET to parent proxy", <-requests)
	assert.Equal(t, "GET to server", <-requests)
}

func TestGetOverTlsViaTwoProxies(t *testing.T) {
	requests := make(chan string, 3)
	server := httptest.NewTLSServer(testServer{requests})
	defer server.Close()
	parent := httptest.NewServer(testProxy{requests, "parent proxy", newDirectProxy()})
	defer parent.Close()
	child := httptest.NewServer(testProxy{requests, "child proxy", newChildProxy(parent)})
	defer child.Close()
	tr := &http.Transport{Proxy: proxyServer(t, child), TLSClientConfig: tlsConfig(server)}
	testGetRequest(t, tr, server.URL)
	require.Len(t, requests, 3)
	assert.Equal(t, "CONNECT to child proxy", <-requests)
	assert.Equal(t, "CONNECT to parent proxy", <-requests)
	assert.Equal(t, "GET to server", <-requests)
}

type hopByHopTestServer struct {
	t *testing.T
}

func (s hopByHopTestServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	assert.NotContains(s.t, req.Header, "Connection")
	assert.NotContains(s.t, req.Header, "Proxy-Authorization")
	assert.Contains(s.t, req.Header, "Authorization")
	assert.NotContains(s.t, req.Header, "X-Alpaca-Request")
	w.Header().Set("Connection", "X-Alpaca-Response")
	w.Header().Set("X-Alpaca-Response", "this should get dropped")
	w.WriteHeader(http.StatusOK)
}

func testHopByHopHeaders(t *testing.T, method, url string, proxy proxyFunc) {
	req, err := http.NewRequest(method, url, nil)
	require.NoError(t, err)
	req.Header.Set("Connection", "X-Alpaca-Request")
	req.Header.Set("Proxy-Authorization", "Basic bWFsb3J5YXJjaGVyOmd1ZXN0")
	req.Header.Set("Authorization", "Basic bmlrb2xhaWpha292Omd1ZXN0")
	req.Header.Set("X-Alpaca-Request", "this should get dropped")

	tr := &http.Transport{Proxy: proxy}
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotContains(t, resp.Header, "Connection")
	assert.NotContains(t, resp.Header, "X-Alpaca-Response")
}

func TestHopByHopHeaders(t *testing.T) {
	server := httptest.NewServer(hopByHopTestServer{t})
	defer server.Close()
	proxy := httptest.NewServer(newDirectProxy())
	defer proxy.Close()
	testHopByHopHeaders(t, http.MethodGet, server.URL, proxyServer(t, proxy))
}

func TestHopByHopHeadersForConnectRequest(t *testing.T) {
	parent := httptest.NewServer(hopByHopTestServer{t})
	defer parent.Close()
	child := httptest.NewServer(newChildProxy(parent))
	defer child.Close()
	testHopByHopHeaders(t, http.MethodConnect, parent.URL, proxyServer(t, child))
}

func TestDeleteConnectionTokens(t *testing.T) {
	header := make(http.Header)
	header.Add("Connection", "close")
	header.Add("Connection", "x-alpaca-1, x-alpaca-2")
	header.Set("X-Alpaca-1", "this should get dropped")
	header.Set("X-Alpaca-2", "this should get dropped")
	header.Set("X-Alpaca-3", "this should NOT get dropped")
	deleteConnectionTokens(header)
	assert.NotContains(t, header, "X-Alpaca-1")
	assert.NotContains(t, header, "X-Alpaca-2")
	assert.Contains(t, header, "X-Alpaca-3")
}

func TestCloseFromOneSideResultsInEOFOnOtherSide(t *testing.T) {
	closeConnection := func(conn net.Conn) {
		conn.Close()
	}
	assertEOF := func(conn net.Conn) {
		_, err := bufio.NewReader(conn).Peek(1)
		assert.Equal(t, io.EOF, err)
	}
	testProxyTunnel(t, closeConnection, assertEOF)
	testProxyTunnel(t, assertEOF, closeConnection)
}

func testProxyTunnel(t *testing.T, onServer, onClient func(conn net.Conn)) {
	// Set up a Listener to act as a server, which we'll connect to via the proxy.
	server, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	defer server.Close()
	proxy := httptest.NewServer(newDirectProxy())
	defer proxy.Close()
	client, err := net.Dial("tcp", proxy.Listener.Addr().String())
	require.NoError(t, err)
	defer client.Close()
	// The server just accepts a connection and calls the callback.
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := server.Accept()
		require.NoError(t, err)
		onServer(conn)
	}()
	// Connect to the server via the proxy, using a CONNECT request.
	serverURL := url.URL{Host: server.Addr().String()}
	req, err := http.NewRequest(http.MethodConnect, serverURL.String(), nil)
	require.NoError(t, err)
	require.NoError(t, req.Write(client))
	resp, err := http.ReadResponse(bufio.NewReader(client), req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	// Call the client callback, and then make sure that the server is done before finishing.
	onClient(client)
	<-done
}

func TestConnectResponseHeadersWithOneProxy(t *testing.T) {
	server, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	defer server.Close()
	proxy := httptest.NewServer(newDirectProxy())
	defer proxy.Close()
	client, err := net.Dial("tcp", proxy.Listener.Addr().String())
	require.NoError(t, err)
	defer client.Close()
	testConnectResponseHeaders(t, server.Addr().String(), client)
}

func TestConnectResponseHeadersWithTwoProxies(t *testing.T) {
	server, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	defer server.Close()
	parent := httptest.NewServer(newDirectProxy())
	defer parent.Close()
	child := httptest.NewServer(newChildProxy(parent))
	defer child.Close()
	client, err := net.Dial("tcp", child.Listener.Addr().String())
	require.NoError(t, err)
	defer client.Close()
	testConnectResponseHeaders(t, server.Addr().String(), client)
}

func testConnectResponseHeaders(t *testing.T, server string, client net.Conn) {
	_, err := fmt.Fprintf(client, "CONNECT %s HTTP/1.1\nHost: %s\n\n", server, server)
	require.NoError(t, err)
	rd := bufio.NewReader(client)
	resp, err := http.ReadResponse(rd, nil)
	require.NoError(t, err)
	// A server MUST NOT send any Transfer-Encoding or Content-Length header fields in a 2xx
	// (Successful) response to CONNECT (see https://tools.ietf.org/html/rfc7231#section-4.3.6).
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, resp.TransferEncoding)
	assert.Equal(t, int64(-1), resp.ContentLength)
}

func TestConnectResponseHasCorrectNewlines(t *testing.T) {
	// See https://github.com/samuong/alpaca/issues/29 for some context behind this test.
	server, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	defer server.Close()
	go func() {
		conn, err := server.Accept()
		require.NoError(t, err)
		conn.Close()
	}()
	proxy := httptest.NewServer(newDirectProxy())
	defer proxy.Close()
	client, err := net.Dial("tcp", proxy.Listener.Addr().String())
	require.NoError(t, err)
	defer client.Close()
	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\n\r\n", server.Addr().String())
	_, err = client.Write([]byte(req))
	require.NoError(t, err)
	buf, err := ioutil.ReadAll(client)
	require.NoError(t, err)
	resp := string(buf)
	// "HTTP/1.1 defines the sequence CR LF as the end-of-line marker"
	// https://www.w3.org/Protocols/rfc2616/rfc2616-sec2.html#sec2.2
	noCRLFs := strings.ReplaceAll(resp, "\r\n", "")
	assert.NotContains(t, noCRLFs, "\r", "response contains unmatched CR")
	assert.NotContains(t, noCRLFs, "\n", "response contains unmatched LF")
}
