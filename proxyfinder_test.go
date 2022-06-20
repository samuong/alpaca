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
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindProxyForRequest(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		expectError bool
		expected    string
	}{
		{"JavaScriptError", "throw 'error'", true, ""},
		{"MultipleBlocks", "return 'PROXY proxy.test:1; DIRECT'", false, "proxy.test:1"},
		{"Direct", "return 'DIRECT'", false, ""},
		{"Proxy", "return 'PROXY proxy.test:2'", false, "proxy.test:2"},
		{"ProxyWithoutPort", "return 'PROXY proxy.test'", false, "proxy.test:80"},
		{"Socks", "return 'SOCKS socksproxy.test:3'", true, "socksproxy.test:3"},
		{"Http", "return 'HTTP http.test:4'", false, "http.test:4"},
		{"HttpWithoutPort", "return 'HTTP http.test'", false, "http.test:80"},
		{"Https", "return 'HTTPS https.test:5'", false, "https.test:5"},
		{"HttpsWithoutPort", "return 'HTTPS https.test'", false, "https.test:443"},
		{"InvalidReturnValue", "return 'INVALID RETURN VALUE'", true, ""},
	}
	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			js := fmt.Sprintf("function FindProxyForURL(url, host) { %s }", test.body)
			server := httptest.NewServer(http.HandlerFunc(pacjsHandler(js)))
			defer server.Close()
			pw := NewPACWrapper(PACData{Port: 1})
			pf := NewProxyFinder(server.URL, pw)
			req := httptest.NewRequest(http.MethodGet, "https://www.test", nil)
			ctx := context.WithValue(req.Context(), contextKeyID, i)
			req = req.WithContext(ctx)
			proxy, err := pf.findProxyForRequest(req)
			if test.expectError {
				assert.NotNil(t, err)
				return
			}
			require.NoError(t, err)
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
	url := "http://pacserver.invalid/nonexistent.pac"
	pw := NewPACWrapper(PACData{Port: 1})
	pf := NewProxyFinder(url, pw)
	req := httptest.NewRequest(http.MethodGet, "http://www.test", nil)
	proxy, err := pf.findProxyForRequest(req)
	require.NoError(t, err)
	assert.Nil(t, proxy)
}

// Removed TestFallbackToDirectWhenNoPACURL - behaviour is fallback to system default when no PACURL, test case TestFallbackToDefaultWhenNoPACUrl

func TestSkipBadProxies(t *testing.T) {
	js := `function FindProxyForURL(url, host) { return "PROXY primary:80; PROXY backup:80" }`
	server := httptest.NewServer(http.HandlerFunc(pacjsHandler(js)))
	defer server.Close()
	pw := NewPACWrapper(PACData{Port: 1})
	pf := NewProxyFinder(server.URL, pw)
	req := httptest.NewRequest(http.MethodGet, "https://www.test", nil)
	ctx := context.WithValue(req.Context(), contextKeyID, 0)
	req = req.WithContext(ctx)
	proxy, err := pf.findProxyForRequest(req)
	require.NoError(t, err)
	assert.Equal(t, "primary:80", proxy.Host)
	pf.blocked.add("primary:80")
	proxy, err = pf.findProxyForRequest(req)
	require.NoError(t, err)
	assert.Equal(t, "backup:80", proxy.Host)
	pf.blocked.add("backup:80")
	proxy, err = pf.findProxyForRequest(req)
	require.NoError(t, err)
	assert.Equal(t, "primary:80", proxy.Host)
}
