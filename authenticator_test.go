// Copyright 2019, 2021 The Alpaca Authors
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
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ntlmServer struct {
	t        *testing.T
	delegate http.Handler
}

func (s ntlmServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	hdr := req.Header.Get("Proxy-Authorization")
	if !strings.HasPrefix(hdr, "NTLM ") {
		sendProxyAuthRequired(w)
		return
	}
	msg, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(hdr, "NTLM "))
	require.NoError(s.t, err)
	require.True(s.t, bytes.Equal(msg[0:8], []byte("NTLMSSP\x00")), "Missing NTLMSSP signature")
	msgType := binary.LittleEndian.Uint32(msg[8:12])
	switch msgType {
	case 1:
		sendChallengeResponse(w)
	case 3:
		req.Header.Del("Proxy-Authenticate")
		s.delegate.ServeHTTP(w, req)
	default:
		s.t.Fatalf("Unexpected NTLM message type: %x", msgType)
	}
}

func sendProxyAuthRequired(w http.ResponseWriter) {
	w.Header().Set("Proxy-Authenticate", "NTLM")
	w.Header().Set("Connection", "close")
	w.WriteHeader(http.StatusProxyAuthRequired)
	fmt.Fprintf(w, "<html><body>oh noes!</body></html>")
}

func sendChallengeResponse(w http.ResponseWriter) {
	w.Header().Set("Proxy-Authenticate", "NTLM TlRMTVNTUAACAAAADAAMADgAAAAFgomi+Rp9UDbAycMAAAAAAAAAAKIAogBEAAAABgEAAAAAAA9HAEwATwBCAEEATAACAAwARwBMAE8AQgBBAEwAAQAeAFAAWABZAEEAVQAwADAAMgBNAEUATAAwADEAMAAzAAQAHABnAGwAbwBiAGEAbAAuAGEAbgB6AC4AYwBvAG0AAwA8AHAAeAB5AGEAdQAwADAAMgBtAGUAbAAwADEAMAAzAC4AZwBsAG8AYgBhAGwALgBhAG4AegAuAGMAbwBtAAcACABQ7ZOkOQbVAQAAAAA=")
	w.WriteHeader(http.StatusProxyAuthRequired)
}

func TestNtlmAuth(t *testing.T) {
	requests := make(chan string, 3)
	server := httptest.NewServer(testServer{requests})
	defer server.Close()
	parent := httptest.NewServer(
		ntlmServer{t, testProxy{requests, "parent proxy", newDirectProxy()}})
	defer parent.Close()
	handler := newChildProxy(parent)
	handler.auth = &authenticator{"isis", "malory", getNtlmHash([]byte("guest"))}
	child := httptest.NewServer(testProxy{requests, "child proxy", handler})
	defer child.Close()
	tr := &http.Transport{Proxy: proxyServer(t, child)}
	testGetRequest(t, tr, server.URL)
	require.Len(t, requests, 3)
	assert.Equal(t, "GET to child proxy", <-requests)
	assert.Equal(t, "GET to parent proxy", <-requests)
	assert.Equal(t, "GET to server", <-requests)
}

func TestNtlmAuthOverTls(t *testing.T) {
	requests := make(chan string, 3)
	server := httptest.NewTLSServer(testServer{requests})
	defer server.Close()
	parent := httptest.NewServer(
		ntlmServer{t, testProxy{requests, "parent proxy", newDirectProxy()}})
	defer parent.Close()
	handler := newChildProxy(parent)
	handler.auth = &authenticator{"isis", "malory", getNtlmHash([]byte("guest"))}
	child := httptest.NewServer(testProxy{requests, "child proxy", handler})
	defer child.Close()
	tr := &http.Transport{Proxy: proxyServer(t, child), TLSClientConfig: tlsConfig(server)}
	testGetRequest(t, tr, server.URL)
	require.Len(t, requests, 3)
	assert.Equal(t, "CONNECT to child proxy", <-requests)
	assert.Equal(t, "CONNECT to parent proxy", <-requests)
	assert.Equal(t, "GET to server", <-requests)
}
