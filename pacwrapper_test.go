package main

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWrapPAC(t *testing.T) {
	pw := NewPACWrapper(PACData{1234, "http://pacserver/proxy.pac", "domain", "username"})
	pac := `function FindProxyForURL(url, host) { return "DIRECT" }`
	pw.Wrap([]byte(pac))
	assert.Contains(t, pw.alpacaPAC, pac)
	assert.Contains(t, pw.alpacaPAC, `"DIRECT" : "PROXY localhost:1234"`)
}

func TestWrapEmptyPAC(t *testing.T) {
	pw := NewPACWrapper(PACData{1234, "http://pacserver/proxy.pac", "domain", "username"})
	pw.Wrap(nil)
	assert.Contains(t, pw.alpacaPAC, `return "DIRECT"`)
}

func TestProxyPACServe(t *testing.T) {
	pw := NewPACWrapper(PACData{1234, "http://pacserver/proxy.pac", "domain", "username"})
	pac := `function FindProxyForURL(url, host) { return "DIRECT" }`
	pw.Wrap([]byte(pac))
	mux := http.NewServeMux()
	pw.SetupHandlers(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/proxy.pac")
	assert.NoError(t, err)

	assert.Equal(t, resp.StatusCode, http.StatusOK)
	assert.Equal(t, "application/x-ns-proxy-autoconfig", resp.Header.Get("Content-Type"))
	body, err := ioutil.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.Equal(t, string(body), pac)
	resp.Body.Close()
}

func TestAlpacaPACServe(t *testing.T) {
	pw := NewPACWrapper(PACData{1234, "http://pacserver/proxy.pac", "domain", "username"})
	pac := `function FindProxyForURL(url, host) { return "DIRECT" }`
	pw.Wrap([]byte(pac))
	mux := http.NewServeMux()
	pw.SetupHandlers(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/alpaca.pac")
	assert.NoError(t, err)

	assert.Equal(t, resp.StatusCode, http.StatusOK)
	assert.Equal(t, "application/x-ns-proxy-autoconfig", resp.Header.Get("Content-Type"))
	b, err := ioutil.ReadAll(resp.Body)
	body := string(b)
	assert.NoError(t, err)
	assert.Contains(t, body, pac)
	assert.Contains(t, body, `"DIRECT" : "PROXY localhost:1234"`)
	resp.Body.Close()
}
