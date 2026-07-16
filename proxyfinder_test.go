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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindProxyForRequest(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		enableSocks bool
		expectError bool
		expected    string
	}{
		{"JavaScriptError", "throw 'error'", false, true, ""},
		{"MultipleBlocks", "return 'PROXY proxy.test:1; DIRECT'", false, false, "proxy.test:1"},
		{"Direct", "return 'DIRECT'", false, false, ""},
		{"Proxy", "return 'PROXY proxy.test:2'", false, false, "proxy.test:2"},
		{"ProxyWithoutPort", "return 'PROXY proxy.test'", false, false, "proxy.test:80"},
		{"Socks", "return 'SOCKS socksproxy.test:3'", false, true, "socksproxy.test:3"},
		{"Socks5Disabled", "return 'SOCKS5 socksproxy.test:1080'", false, true, ""},
		{"Socks5Enabled", "return 'SOCKS5 socksproxy.test:1080'", true, false, "socksproxy.test:1080"},
		{"Http", "return 'HTTP http.test:4'", false, false, "http.test:4"},
		{"HttpWithoutPort", "return 'HTTP http.test'", false, false, "http.test:80"},
		{"Https", "return 'HTTPS https.test:5'", false, false, "https.test:5"},
		{"HttpsWithoutPort", "return 'HTTPS https.test'", false, false, "https.test:443"},
		{"InvalidReturnValue", "return 'INVALID RETURN VALUE'", false, true, ""},
	}
	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			js := fmt.Sprintf("function FindProxyForURL(url, host) { %s }", test.body)
			server := httptest.NewServer(http.HandlerFunc(pacjsHandler(js)))
			defer server.Close()
			pw := NewPACWrapper(PACData{Port: 1})
			pf := NewProxyFinder(server.URL, pw, test.enableSocks)
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
	pf := NewProxyFinder(url, pw, false)
	req := httptest.NewRequest(http.MethodGet, "http://www.test", nil)
	proxy, err := pf.findProxyForRequest(req)
	require.NoError(t, err)
	assert.Nil(t, proxy)
}

// Removed TestFallbackToDirectWhenNoPACURL. Behaviour is fallback to system default when no
// PACURL; see test case TestFallbackToDefaultWhenNoPACUrl.

func TestSkipBadProxies(t *testing.T) {
	js := `function FindProxyForURL(url, host) { return "PROXY primary:80; PROXY backup:80" }`
	server := httptest.NewServer(http.HandlerFunc(pacjsHandler(js)))
	defer server.Close()
	pw := NewPACWrapper(PACData{Port: 1})
	pf := NewProxyFinder(server.URL, pw, false)
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

func TestBlockProxySuppressedDuringGracePeriod(t *testing.T) {
	// Enable grace period for this test (default is 0/disabled).
	oldGracePeriod := gracePeriod
	gracePeriod = 5 * time.Second
	defer func() { gracePeriod = oldGracePeriod }()

	js := `function FindProxyForURL(url, host) { return "PROXY proxy.test:80; DIRECT" }`
	server := httptest.NewServer(http.HandlerFunc(pacjsHandler(js)))
	defer server.Close()
	pw := NewPACWrapper(PACData{Port: 1})
	pf := NewProxyFinder(server.URL, pw, false)

	req := httptest.NewRequest(http.MethodGet, "https://www.test", nil)
	ctx := context.WithValue(req.Context(), contextKeyID, 0)
	req = req.WithContext(ctx)

	// Simulate a network change: checkForUpdates resets the blocklist and sets graceUntil.
	// The grace period is already active from NewProxyFinder calling checkForUpdates().
	// Verify that blockProxy is suppressed during the grace period.
	pf.blockProxy("proxy.test:80")
	assert.False(t, pf.blocked.contains("proxy.test:80"),
		"blockProxy should be suppressed during grace period")

	// Proxy should still be returned (not blocked).
	proxy, err := pf.findProxyForRequest(req)
	require.NoError(t, err)
	require.NotNil(t, proxy)
	assert.Equal(t, "proxy.test:80", proxy.Host)

	// After grace period expires, blockProxy should work again.
	pf.Lock()
	pf.graceUntil = pf.now().Add(-1 * time.Second) // expire the grace period
	pf.Unlock()

	pf.blockProxy("proxy.test:80")
	assert.True(t, pf.blocked.contains("proxy.test:80"),
		"blockProxy should work after grace period expires")

	// Proxy should now be skipped (blocked), falling through to DIRECT.
	proxy, err = pf.findProxyForRequest(req)
	require.NoError(t, err)
	assert.Nil(t, proxy) // DIRECT
}
