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
	return NewProxyHandler(func(r *http.Request) (*url.URL, error) { return nil, nil })
}

func newChildProxy(parent *httptest.Server) ProxyHandler {
	return NewProxyHandler(func(r *http.Request) (*url.URL, error) {
		return &url.URL{Host: parent.Listener.Addr().String()}, nil
	})
}

func proxyServer(t *testing.T, proxy *httptest.Server) proxyFunc {
	u, err := url.Parse(proxy.URL)
	require.Nil(t, err)
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
	require.Nil(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	buf, err := ioutil.ReadAll(resp.Body)
	require.Nil(t, err)
	assert.Equal(t, "Hello, client\n", string(buf))
}

func TestGetViaProxy(t *testing.T) {
	requests := make(chan string, 2)
	server := httptest.NewServer(testServer{requests})
	defer server.Close()
	proxy := httptest.NewServer(testProxy{requests, "proxy", newDirectProxy()})
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
	proxy := httptest.NewServer(testProxy{requests, "proxy", newDirectProxy()})
	defer proxy.Close()
	tr := &http.Transport{Proxy: proxyServer(t, proxy), TLSClientConfig: tlsConfig(server)}
	testGetRequest(t, tr, server.URL)
	require.Len(t, requests, 2)
	assert.Equal(t, "CONNECT to proxy", <-requests)
	assert.Equal(t, "GET to server", <-requests)
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
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Alpaca-Response", "this should get dropped")
	w.WriteHeader(http.StatusOK)
}

func testHopByHopHeaders(t *testing.T, method, url string, proxy proxyFunc) {
	req, err := http.NewRequest(method, url, nil)
	require.Nil(t, err)
	req.Header.Set("Connection", "X-Alpaca-Request")
	req.Header.Set("Proxy-Authorization", "Basic bWFsb3J5YXJjaGVyOmd1ZXN0")
	req.Header.Set("Authorization", "Basic bmlrb2xhaWpha292Omd1ZXN0")
	req.Header.Set("X-Alpaca-Request", "this should get dropped")

	tr := &http.Transport{Proxy: proxy}
	resp, err := tr.RoundTrip(req)
	require.Nil(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotContains(t, resp.Header, "Connection")
	assert.Contains(t, resp.Header, "Cache-Control")
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
