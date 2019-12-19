// Copyright 2019 The Alpaca Authors
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
			pw := NewPACWrapper(PACData{Port: 1})
			pf := NewProxyFinder(server.URL, pw)
			req := httptest.NewRequest(http.MethodGet, "https://www.test", nil)
			req = req.WithContext(context.WithValue(req.Context(), "id", i))
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

func TestFallbackToDirectWhenNoPACURL(t *testing.T) {
	url := ""
	pw := NewPACWrapper(PACData{Port: 1})
	pf := NewProxyFinder(url, pw)
	req := httptest.NewRequest(http.MethodGet, "http://www.test", nil)
	proxy, err := pf.findProxyForRequest(req)
	require.NoError(t, err)
	assert.Nil(t, proxy)
}
