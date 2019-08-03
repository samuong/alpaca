package main

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFindProxyForRequest(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		expectError bool
		expected    string
	}{
		{"javascript error", "throw 'error'", true, ""},
		{"multiple blocks", "return 'PROXY proxy.test:1; DIRECT'", false, "proxy.test:1"},
		{"direct", "return 'DIRECT'", false, ""},
		{"proxy", "return 'PROXY proxy.test:2'", false, "proxy.test:2"},
		{"proxy without port", "return 'PROXY proxy.test'", false, "proxy.test:80"},
		{"socks", "return 'SOCKS socksproxy.test:3'", true, ""},
		{"invalid return value", "return 'INVALID RETURN VALUE'", true, ""},
	}
	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			js := fmt.Sprintf("function FindProxyForURL(url, host) { %s }", test.body)
			server := httptest.NewServer(http.HandlerFunc(pacjsHandler(js)))
			defer server.Close()
			pf := NewProxyFinder(server.URL)
			req := httptest.NewRequest(http.MethodGet, "https://www.test", nil)
			req = req.WithContext(context.WithValue(req.Context(), "id", i))
			proxy, err := pf.findProxyForRequest(req)
			if test.expectError {
				assert.NotNil(t, err)
				return
			}
			require.Nil(t, err)
			if test.expected == "" {
				assert.Nil(t, proxy)
				return
			}
			require.NotNil(t, proxy)
			assert.Equal(t, test.expected, proxy.Host)
		})
	}
}

func TestFallbackToDirectWhenNotConnected(t *testing.T) {
	pf := NewProxyFinder("http://pacserver.invalid/nonexistent.pac")
	req := httptest.NewRequest(http.MethodGet, "http://www.test", nil)
	proxy, err := pf.findProxyForRequest(req)
	require.Nil(t, err)
	assert.Nil(t, proxy)
}
