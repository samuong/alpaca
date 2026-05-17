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
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type basicAuthServer struct {
	t        *testing.T
	expected string // expected base64-encoded credentials
}

func (s basicAuthServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	hdr := req.Header.Get("Proxy-Authorization")
	if !strings.HasPrefix(hdr, "Basic ") {
		w.Header().Set("Proxy-Authenticate", "Basic realm=\"proxy\"")
		w.WriteHeader(http.StatusProxyAuthRequired)
		fmt.Fprint(w, "Proxy authentication required")
		return
	}
	if strings.TrimPrefix(hdr, "Basic ") != s.expected {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "Invalid credentials")
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "Access granted")
}

func TestBasicAuth(t *testing.T) {
	creds := "alice:s3cret"
	encoded := base64.StdEncoding.EncodeToString([]byte(creds))
	server := httptest.NewServer(basicAuthServer{t, encoded})
	defer server.Close()
	serverAddr := server.Listener.Addr().String()
	tr := &http.Transport{Proxy: http.ProxyURL(&url.URL{Host: serverAddr})}
	req, err := http.NewRequest(http.MethodGet, "http://"+serverAddr, nil)
	require.NoError(t, err)
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusProxyAuthRequired, resp.StatusCode)
	auth := newBasicAuthenticator(creds)
	resp, err = auth.do(req, tr)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestBasicAuthScheme(t *testing.T) {
	auth := newBasicAuthenticator("user:pass")
	assert.Equal(t, "Basic", auth.scheme())
}
