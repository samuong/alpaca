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
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ntlmServer struct {
	t        *testing.T
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
		_, err := w.Write([]byte("Access granted"))
		require.NoError(s.t, err)
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
	server := httptest.NewServer(ntlmServer{t})
	defer server.Close()
	serverAddr := server.Listener.Addr().String()
	tr := &http.Transport{Proxy: http.ProxyURL(&url.URL{Host: serverAddr})}
	req, err := http.NewRequest(http.MethodGet, "http://" + serverAddr, nil)
	require.NoError(t, err)
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusProxyAuthRequired, resp.StatusCode)
	auth := &authenticator{"isis", "malory", getNtlmHash([]byte("guest"))}
	resp, err = auth.do(req, tr)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "Access granted", string(body))
}

func TestGetNtlmHash(t *testing.T) {
	assert.Equal(t, "823893adfad2cda6e1a414f3ebdf58f7", getNtlmHash([]byte("guest")))
}
