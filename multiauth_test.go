// Copyright 2025 The Alpaca Authors
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
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAuth is a test authenticator that succeeds when the proxy accepts its scheme.
type fakeAuth struct {
	name    string
	header  string // value sent in Proxy-Authorization
	calls   int
}

func (f *fakeAuth) scheme() string { return f.name }

func (f *fakeAuth) do(req *http.Request, rt http.RoundTripper) (*http.Response, error) {
	f.calls++
	req.Header.Set("Proxy-Authorization", f.header)
	return rt.RoundTrip(req)
}

// multiAuthProxy simulates a proxy that advertises specific auth schemes and
// accepts a specific Proxy-Authorization value.
type multiAuthProxy struct {
	schemes []string // schemes to advertise in Proxy-Authenticate
	accept  string   // accepted Proxy-Authorization value
}

func (p *multiAuthProxy) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	auth := req.Header.Get("Proxy-Authorization")
	if auth == p.accept {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
		return
	}
	for _, s := range p.schemes {
		w.Header().Add("Proxy-Authenticate", s)
	}
	w.WriteHeader(http.StatusProxyAuthRequired)
	fmt.Fprint(w, "auth required")
}

func TestNewMultiAuthenticatorNil(t *testing.T) {
	assert.Nil(t, newMultiAuthenticator())
	assert.Nil(t, newMultiAuthenticator(nil, nil))
}

func TestNewMultiAuthenticatorSingle(t *testing.T) {
	b := newBasicAuthenticator("user:pass")
	auth := newMultiAuthenticator(nil, b, nil)
	// With a single method, newMultiAuthenticator returns it directly.
	assert.Equal(t, b, auth)
}

func TestMultiAuthSelectsByProxyAuthenticate(t *testing.T) {
	proxy := &multiAuthProxy{
		schemes: []string{"Basic realm=\"test\""},
		accept:  "Basic ok",
	}
	server := httptest.NewServer(proxy)
	defer server.Close()
	proxyURL, _ := url.Parse("http://" + server.Listener.Addr().String())

	ntlmAuth := &fakeAuth{name: "NTLM", header: "NTLM token"}
	basicAuth := &fakeAuth{name: "Basic", header: "Basic ok"}
	auth := newMultiAuthenticator(ntlmAuth, basicAuth)

	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)
	ctx := context.WithValue(req.Context(), contextKeyProxy, proxyURL)
	req = req.WithContext(ctx)

	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	resp, err := auth.do(req, tr)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	// NTLM should not have been tried because the proxy only advertises Basic.
	assert.Equal(t, 0, ntlmAuth.calls)
	assert.Equal(t, 1, basicAuth.calls)
}

func TestMultiAuthCachesMethod(t *testing.T) {
	proxy := &multiAuthProxy{
		schemes: []string{"NTLM"},
		accept:  "NTLM token",
	}
	server := httptest.NewServer(proxy)
	defer server.Close()
	proxyURL, _ := url.Parse("http://" + server.Listener.Addr().String())

	ntlmAuth := &fakeAuth{name: "NTLM", header: "NTLM token"}
	basicAuth := &fakeAuth{name: "Basic", header: "Basic creds"}
	auth := newMultiAuthenticator(ntlmAuth, basicAuth)

	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL)}

	// First request — discovers NTLM via Proxy-Authenticate.
	req1, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	ctx := context.WithValue(req1.Context(), contextKeyProxy, proxyURL)
	req1 = req1.WithContext(ctx)
	resp1, err := auth.do(req1, tr)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, http.StatusOK, resp1.StatusCode)
	assert.Equal(t, 1, ntlmAuth.calls)

	// Second request — should use cached method directly, no probe.
	req2, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	req2 = req2.WithContext(ctx)
	resp2, err := auth.do(req2, tr)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	assert.Equal(t, 2, ntlmAuth.calls)
	assert.Equal(t, 0, basicAuth.calls)
}

func TestMultiAuthFallsBackWithoutProxyAuthenticate(t *testing.T) {
	// Proxy that returns 407 without Proxy-Authenticate headers, then accepts Basic.
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls++
		auth := req.Header.Get("Proxy-Authorization")
		if auth == "Basic ok" {
			w.WriteHeader(http.StatusOK)
			return
		}
		// No Proxy-Authenticate header.
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	defer server.Close()
	proxyURL, _ := url.Parse("http://" + server.Listener.Addr().String())

	ntlmAuth := &fakeAuth{name: "NTLM", header: "NTLM wrong"}
	basicAuth := &fakeAuth{name: "Basic", header: "Basic ok"}
	auth := newMultiAuthenticator(ntlmAuth, basicAuth)

	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	ctx := context.WithValue(req.Context(), contextKeyProxy, proxyURL)
	req = req.WithContext(ctx)

	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	resp, err := auth.do(req, tr)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	// Both should be tried since there's no Proxy-Authenticate to filter on.
	assert.Equal(t, 1, ntlmAuth.calls)
	assert.Equal(t, 1, basicAuth.calls)
}

func TestParseProxyAuthenticateSchemes(t *testing.T) {
	tests := []struct {
		name    string
		headers []string
		want    map[string]bool
	}{
		{
			name:    "single scheme",
			headers: []string{"Basic realm=\"proxy\""},
			want:    map[string]bool{"basic": true},
		},
		{
			name:    "multiple headers",
			headers: []string{"Negotiate", "NTLM", "Basic realm=\"proxy\""},
			want:    map[string]bool{"negotiate": true, "ntlm": true, "basic": true},
		},
		{
			name:    "no headers",
			headers: nil,
			want:    map[string]bool{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := make(http.Header)
			for _, h := range tt.headers {
				header.Add("Proxy-Authenticate", h)
			}
			got := parseProxyAuthenticateSchemes(header)
			assert.Equal(t, tt.want, got)
		})
	}
}
