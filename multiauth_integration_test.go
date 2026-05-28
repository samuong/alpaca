// Copyright 2026 The Alpaca Authors
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
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedProxy is an httptest.Server that advertises a chosen set of
// schemes via Proxy-Authenticate and accepts a chosen Proxy-Authorization
// value.
type scriptedProxy struct {
	*httptest.Server
	advertise []string
	accept    string
	mu        sync.Mutex
	observed  []string
}

func newScriptedProxy(advertise []string, accept string) *scriptedProxy {
	p := &scriptedProxy{advertise: advertise, accept: accept}
	p.Server = httptest.NewServer(http.HandlerFunc(p.handle))
	return p
}

func (p *scriptedProxy) handle(w http.ResponseWriter, req *http.Request) {
	p.mu.Lock()
	p.observed = append(p.observed, req.Header.Get("Proxy-Authorization"))
	p.mu.Unlock()
	if req.Header.Get("Proxy-Authorization") == p.accept {
		w.WriteHeader(http.StatusOK)
		return
	}
	for _, s := range p.advertise {
		w.Header().Add("Proxy-Authenticate", s)
	}
	w.WriteHeader(http.StatusProxyAuthRequired)
}

// runProxyRequest exercises the same code path as ProxyHandler.proxyRequest
// against the given proxy + auth chain and returns the final response.
// We invoke retryProxyRequestWithAuth directly so the test doesn't need
// to model the full middleware stack. The body argument lets tests
// verify that a non-empty request body survives across retries.
func runProxyRequest(t *testing.T, proxy *scriptedProxy, chain *authChain,
	body []byte) (*http.Response, error) {
	t.Helper()
	proxyURL, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer tr.CloseIdleConnections()
	rd := bytes.NewReader(body)
	req, err := http.NewRequest(http.MethodPost, "http://example.com",
		io.NopCloser(rd))
	require.NoError(t, err)
	// Mirror the ProxyHandler middleware: stash the proxy URL on the
	// context so applicableTo / negotiateAuthenticator can find it.
	req = req.WithContext(context.WithValue(req.Context(),
		contextKeyProxy, proxyURL))
	resp, err := tr.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusProxyAuthRequired {
		return resp, nil
	}
	schemes := parseProxyAuthenticateSchemes(resp.Header)
	_ = resp.Body.Close()
	return retryProxyRequestWithAuth(req, tr, chain, schemes, rd)
}

func TestRetryProxyRequest_FirstMethodWins(t *testing.T) {
	proxy := newScriptedProxy(
		[]string{"Negotiate", "NTLM", "Basic"},
		"Negotiate token-A")
	defer proxy.Close()

	neg := realisticFake("Negotiate", "Negotiate token-A")
	ntlm := realisticFake("NTLM", "NTLM should-not-be-sent")
	basic := newBasicAuthenticator("u:p")
	chain := newAuthChain(neg, ntlm, basic)

	resp, err := runProxyRequest(t, proxy, chain, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.EqualValues(t, 1, neg.calls.Load())
	assert.EqualValues(t, 0, ntlm.calls.Load(),
		"NTLM must not be tried after Negotiate succeeded")
}

func TestRetryProxyRequest_FallsThroughOn407(t *testing.T) {
	proxy := newScriptedProxy(
		[]string{"Negotiate", "NTLM"},
		"NTLM final-token")
	defer proxy.Close()

	neg := realisticFake("Negotiate", "Negotiate bad-token")
	ntlm := realisticFake("NTLM", "NTLM final-token")
	chain := newAuthChain(neg, ntlm)

	resp, err := runProxyRequest(t, proxy, chain, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.EqualValues(t, 1, neg.calls.Load())
	assert.EqualValues(t, 1, ntlm.calls.Load())
}

func TestRetryProxyRequest_All407ReturnsLast407(t *testing.T) {
	proxy := newScriptedProxy(
		[]string{"Negotiate", "NTLM"},
		"never-accepted")
	defer proxy.Close()

	neg := realisticFake("Negotiate", "Negotiate wrong")
	ntlm := realisticFake("NTLM", "NTLM also-wrong")
	chain := newAuthChain(neg, ntlm)

	resp, err := runProxyRequest(t, proxy, chain, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusProxyAuthRequired, resp.StatusCode)
	assert.EqualValues(t, 1, neg.calls.Load())
	assert.EqualValues(t, 1, ntlm.calls.Load())
}

func TestRetryProxyRequest_BasicAdvertisedAndSent(t *testing.T) {
	// Positive Basic case: when the proxy explicitly advertises Basic
	// AND the chain has Basic configured, Basic must be tried.
	encoded := "dTpw" // base64("u:p")
	proxy := newScriptedProxy(
		[]string{`Basic realm="proxy"`},
		"Basic "+encoded)
	defer proxy.Close()

	basic := newBasicAuthenticator("u:p")
	chain := newAuthChain(basic)

	resp, err := runProxyRequest(t, proxy, chain, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestRetryProxyRequest_NTLMOnlyChainAgainstBasicProxy_Refuses(t *testing.T) {
	// Negative case: NTLM-only chain against a Basic-only proxy must
	// surface errNoMatchingAuthMethod, not silently fall back.
	proxy := newScriptedProxy(
		[]string{`Basic realm="proxy"`},
		"never-accepted")
	defer proxy.Close()

	ntlm := realisticFake("NTLM", "NTLM tok")
	chain := newAuthChain(ntlm)

	resp, err := runProxyRequest(t, proxy, chain, nil)
	if err == nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	assert.ErrorIs(t, err, errNoMatchingAuthMethod)
}

func TestRetryProxyRequest_RefusesUnadvertisedDowngrade(t *testing.T) {
	// Proxy returns 407 with no Proxy-Authenticate. With only Basic
	// configured the chain must refuse (downgrade defence).
	proxy := newScriptedProxy(nil, "never-accepted")
	defer proxy.Close()

	basic := newBasicAuthenticator("u:p")
	chain := newAuthChain(basic)

	resp, err := runProxyRequest(t, proxy, chain, nil)
	if err == nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	assert.ErrorIs(t, err, errNoMatchingAuthMethod)
}

func TestRetryProxyRequest_ClearsProxyAuthorizationBetweenAttempts(t *testing.T) {
	proxy := newScriptedProxy(
		[]string{"Negotiate", "NTLM"},
		"NTLM accept-me")
	defer proxy.Close()

	neg := realisticFake("Negotiate", "Negotiate sentinel-N")
	ntlm := realisticFake("NTLM", "NTLM accept-me")
	chain := newAuthChain(neg, ntlm)

	resp, err := runProxyRequest(t, proxy, chain, nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// The fakeAuth records what Proxy-Authorization it observed at
	// entry. Both methods must enter with an EMPTY Proxy-Authorization
	// — the loop clears the header before each method.
	require.Len(t, neg.seenHeaders, 1)
	require.Len(t, ntlm.seenHeaders, 1)
	assert.Empty(t, neg.seenHeaders[0],
		"Negotiate must enter with cleared Proxy-Authorization")
	assert.Empty(t, ntlm.seenHeaders[0],
		"NTLM must enter with cleared Proxy-Authorization (no leftover from Negotiate)")
}

func TestRetryProxyRequest_AbortsChainOnError(t *testing.T) {
	// N-5 invariant: any error from a method aborts the chain. This
	// is the test that pins it: make method 1 return an error after
	// setting Proxy-Authorization, and assert method 2 is never
	// invoked.
	proxy := newScriptedProxy(
		[]string{"Negotiate", "NTLM"},
		"never-accepted")
	defer proxy.Close()

	failing := realisticFake("Negotiate", "Negotiate-but-failing")
	failing.err = errors.New("synthetic boom")
	ntlm := realisticFake("NTLM", "NTLM tok")
	chain := newAuthChain(failing, ntlm)

	resp, err := runProxyRequest(t, proxy, chain, nil)
	if err == nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	assert.Contains(t, err.Error(), "synthetic boom")
	assert.EqualValues(t, 1, failing.calls.Load())
	assert.EqualValues(t, 0, ntlm.calls.Load(),
		"NTLM must not be tried after a method returned an error")
}

func TestRetryProxyRequest_BodyPreservedAcrossAttempts(t *testing.T) {
	// Confirm that a non-empty request body is replayed verbatim on
	// each method retry. Pre-fix, retryProxyRequestWithAuth had a
	// seek-on-retry path that could regress silently.
	const payload = "the quick brown fox"

	var got [][]byte
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		mu.Lock()
		got = append(got, body)
		mu.Unlock()
		auth := req.Header.Get("Proxy-Authorization")
		if auth == "Basic accept" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Add("Proxy-Authenticate", "Negotiate")
		w.Header().Add("Proxy-Authenticate", `Basic realm="proxy"`)
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	defer server.Close()

	proxyURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer tr.CloseIdleConnections()
	rd := bytes.NewReader([]byte(payload))
	req, err := http.NewRequest(http.MethodPost, "http://example.com",
		io.NopCloser(rd))
	require.NoError(t, err)
	req = req.WithContext(context.WithValue(req.Context(),
		contextKeyProxy, proxyURL))

	// Build the chain so Negotiate is tried first (and 407s) then
	// Basic succeeds. This forces TWO body-replays.
	neg := realisticFake("Negotiate", "Negotiate sentinel")
	basic := &fakeAuth{name: "Basic", header: "Basic accept"}
	chain := newAuthChain(neg, basic)

	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusProxyAuthRequired, resp.StatusCode)
	got = got[:0] // discard the initial 407 probe
	schemes := parseProxyAuthenticateSchemes(resp.Header)
	_ = resp.Body.Close()

	resp, err = retryProxyRequestWithAuth(req, tr, chain, schemes, rd)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(got), 2, "expected at least 2 retry attempts")
	for i, b := range got {
		assert.Equal(t, payload, string(b),
			"attempt %d: body bytes diverged from original payload", i)
	}
}

// --- CONNECT path integration ---

// connectProxy is a raw TCP listener that scripts CONNECT responses. It
// reads the CONNECT line, optionally sends a 407 with configured
// Proxy-Authenticate, then on a subsequent CONNECT either accepts (when
// the Proxy-Authorization matches) or 407s again.
type connectProxy struct {
	t         *testing.T
	listener  net.Listener
	advertise []string
	accept    string
	mu        sync.Mutex
	observed  []string
}

func newConnectProxy(t *testing.T, advertise []string, accept string) *connectProxy {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	p := &connectProxy{t: t, listener: l, advertise: advertise, accept: accept}
	go p.serve()
	return p
}

func (p *connectProxy) URL() *url.URL {
	return &url.URL{Scheme: "http", Host: p.listener.Addr().String()}
}

func (p *connectProxy) Close() { _ = p.listener.Close() }

func (p *connectProxy) serve() {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			return
		}
		go p.handle(conn)
	}
}

func (p *connectProxy) handle(conn net.Conn) {
	defer conn.Close() //nolint:errcheck
	br := bufio.NewReader(conn)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	hdr := req.Header.Get("Proxy-Authorization")
	p.mu.Lock()
	p.observed = append(p.observed, hdr)
	p.mu.Unlock()
	if hdr == p.accept {
		_, _ = fmt.Fprint(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		return
	}
	resp := "HTTP/1.1 407 Proxy Authentication Required\r\n"
	for _, s := range p.advertise {
		resp += "Proxy-Authenticate: " + s + "\r\n"
	}
	resp += "Content-Length: 0\r\nConnection: close\r\n\r\n"
	_, _ = io.WriteString(conn, resp)
}

func (p *connectProxy) requests() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.observed))
	copy(out, p.observed)
	return out
}

func TestConnectViaProxy_FallsThroughOn407(t *testing.T) {
	proxy := newConnectProxy(t,
		[]string{"Negotiate", "NTLM"},
		"NTLM final-token")
	defer proxy.Close()

	neg := realisticFake("Negotiate", "Negotiate bad")
	ntlm := realisticFake("NTLM", "NTLM final-token")
	chain := newAuthChain(neg, ntlm)

	req, err := http.NewRequest(http.MethodConnect, "https://example.com:443", nil)
	require.NoError(t, err)
	req.Host = "example.com:443"

	conn, err := connectViaProxy(req, proxy.URL(), chain)
	require.NoError(t, err)
	defer conn.Close() //nolint:errcheck //nolint:errcheck

	// Three CONNECT attempts: probe (no header), Negotiate, NTLM.
	requests := proxy.requests()
	require.Len(t, requests, 3, "expected probe + Negotiate + NTLM")
	assert.Empty(t, requests[0], "initial probe must have no Proxy-Authorization")
	assert.Equal(t, "Negotiate bad", requests[1])
	assert.Equal(t, "NTLM final-token", requests[2])
}

func TestConnectViaProxy_RefusesBasicDowngrade(t *testing.T) {
	// Basic-only proxy with NTLM-only chain → errNoMatchingAuthMethod
	// surfaced as 502 to the client. We invoke connectViaProxy
	// directly and assert the error.
	proxy := newConnectProxy(t,
		[]string{`Basic realm="proxy"`},
		"never-accepted")
	defer proxy.Close()

	ntlm := realisticFake("NTLM", "NTLM tok")
	chain := newAuthChain(ntlm)

	req, err := http.NewRequest(http.MethodConnect, "https://example.com:443", nil)
	require.NoError(t, err)
	req.Host = "example.com:443"

	_, err = connectViaProxy(req, proxy.URL(), chain)
	require.Error(t, err)
	assert.ErrorIs(t, err, errNoMatchingAuthMethod)
}
