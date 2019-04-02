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

func (ts testServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ts.requests <- fmt.Sprintf("%s to server", r.Method)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "Hello, client")
}

type testProxy struct {
	requests chan<- string
	name     string
	delegate http.Handler
}

func newDirectProxy(name string, requests chan<- string) testProxy {
	alwaysDirect := func(r *http.Request) (*url.URL, error) { return nil, nil }
	return testProxy{requests, name, ProxyHandler{&http.Transport{Proxy: alwaysDirect}}}
}

func newChildProxy(name string, requests chan<- string, parent *httptest.Server) testProxy {
	alwaysProxy := func(r *http.Request) (*url.URL, error) {
		return &url.URL{Host: parent.Listener.Addr().String()}, nil
	}
	return testProxy{requests, name, ProxyHandler{&http.Transport{Proxy: alwaysProxy}}}
}

func (tp testProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tp.requests <- fmt.Sprintf("%s to %s", r.Method, tp.name)
	tp.delegate.ServeHTTP(w, r)
}

func proxyFunc(t *testing.T, proxy *httptest.Server) func(*http.Request) (*url.URL, error) {
	u, err := url.Parse(proxy.URL)
	require.Nil(t, err)
	return http.ProxyURL(u)
}

func tlsConfig(server *httptest.Server) *tls.Config {
	cp := x509.NewCertPool()
	cp.AddCert(server.Certificate())
	return &tls.Config{RootCAs: cp}
}

func testGetRequest(t *testing.T, tr *http.Transport, serverUrl string) {
	client := http.Client{Transport: tr}
	resp, err := client.Get(serverUrl)
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
	proxy := httptest.NewServer(newDirectProxy("proxy", requests))
	defer proxy.Close()
	tr := &http.Transport{Proxy: proxyFunc(t, proxy)}
	testGetRequest(t, tr, server.URL)
	require.Len(t, requests, 2)
	assert.Equal(t, "GET to proxy", <-requests)
	assert.Equal(t, "GET to server", <-requests)
}

func TestGetOverTlsViaProxy(t *testing.T) {
	requests := make(chan string, 2)
	server := httptest.NewTLSServer(testServer{requests})
	defer server.Close()
	proxy := httptest.NewServer(newDirectProxy("proxy", requests))
	defer proxy.Close()
	tr := &http.Transport{Proxy: proxyFunc(t, proxy), TLSClientConfig: tlsConfig(server)}
	testGetRequest(t, tr, server.URL)
	require.Len(t, requests, 2)
	assert.Equal(t, "CONNECT to proxy", <-requests)
	assert.Equal(t, "GET to server", <-requests)
}

func TestGetViaTwoProxies(t *testing.T) {
	requests := make(chan string, 3)
	server := httptest.NewServer(testServer{requests})
	defer server.Close()
	parent := httptest.NewServer(newDirectProxy("parent proxy", requests))
	defer parent.Close()
	child := httptest.NewServer(newChildProxy("child proxy", requests, parent))
	defer child.Close()
	tr := &http.Transport{Proxy: proxyFunc(t, child)}
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
	parent := httptest.NewServer(newDirectProxy("parent proxy", requests))
	defer parent.Close()
	child := httptest.NewServer(newChildProxy("child proxy", requests, parent))
	defer child.Close()
	tr := &http.Transport{Proxy: proxyFunc(t, child), TLSClientConfig: tlsConfig(server)}
	testGetRequest(t, tr, server.URL)
	require.Len(t, requests, 3)
	assert.Equal(t, "CONNECT to child proxy", <-requests)
	assert.Equal(t, "CONNECT to parent proxy", <-requests)
	assert.Equal(t, "GET to server", <-requests)
}
