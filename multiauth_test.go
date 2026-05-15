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
	"bytes"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAuth is a test authenticator that records calls and returns the
// configured response code or error. It sets a unique Proxy-Authorization
// header so tests can verify which method was invoked at the wire level.
type fakeAuth struct {
	name        string
	header      string // value to set in Proxy-Authorization
	status      int    // status to return without hitting rt; 0 means call rt
	err         error  // if non-nil, return (nil, err) instead of round-tripping
	safe        bool   // value returned by safeWithoutChallenge()
	hostFilter  func(string) bool
	calls       atomic.Int32
	mu          sync.Mutex
	seenHeaders []string // observed Proxy-Authorization on entry to do()
}

func (f *fakeAuth) scheme() string             { return f.name }
func (f *fakeAuth) safeWithoutChallenge() bool { return f.safe }

func (f *fakeAuth) applicableTo(host string) bool {
	if f.hostFilter == nil {
		return true
	}
	return f.hostFilter(host)
}

func (f *fakeAuth) do(req *http.Request, rt http.RoundTripper) (*http.Response, error) {
	f.mu.Lock()
	f.seenHeaders = append(f.seenHeaders, req.Header.Get("Proxy-Authorization"))
	f.mu.Unlock()
	f.calls.Add(1)
	if f.err != nil {
		req.Header.Set("Proxy-Authorization", f.header)
		return nil, f.err
	}
	req.Header.Set("Proxy-Authorization", f.header)
	if f.status != 0 {
		return &http.Response{
			StatusCode: f.status,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(nil)),
		}, nil
	}
	return rt.RoundTrip(req)
}

// realisticFake builds a fakeAuth that mimics one of the real
// authenticators' safety properties: NTLM/Negotiate are
// safe-without-challenge, Basic is not.
func realisticFake(name, header string) *fakeAuth {
	f := &fakeAuth{name: name, header: header}
	switch name {
	case "Negotiate", "NTLM":
		f.safe = true
	}
	return f
}

func TestNewAuthChain(t *testing.T) {
	t.Run("nil when no methods", func(t *testing.T) {
		assert.Nil(t, newAuthChain())
		assert.Nil(t, newAuthChain(nil, nil))
	})
	t.Run("filters nil entries", func(t *testing.T) {
		basic := newBasicAuthenticator("u:p")
		chain := newAuthChain(nil, basic, nil)
		require.NotNil(t, chain)
		assert.Equal(t, []proxyAuthenticator{basic}, chain.methods)
	})
	t.Run("preserves caller order", func(t *testing.T) {
		neg := realisticFake("Negotiate", "Negotiate t")
		ntlm := realisticFake("NTLM", "NTLM t")
		basic := realisticFake("Basic", "Basic t")
		chain := newAuthChain(neg, ntlm, basic)
		require.NotNil(t, chain)
		assert.Equal(t,
			[]proxyAuthenticator{neg, ntlm, basic},
			chain.methods)
	})
}

func TestAuthChainPick(t *testing.T) {
	neg := realisticFake("Negotiate", "Negotiate t")
	ntlm := realisticFake("NTLM", "NTLM t")
	basic := realisticFake("Basic", "Basic t")
	chain := newAuthChain(neg, ntlm, basic)

	tests := []struct {
		name    string
		schemes []string
		want    []proxyAuthenticator
	}{
		{
			name:    "all three advertised picks all in chain order",
			schemes: []string{schemeNegotiate, schemeNTLM, schemeBasic},
			want:    []proxyAuthenticator{neg, ntlm, basic},
		},
		{
			name:    "only basic advertised picks only basic",
			schemes: []string{"basic"},
			want:    []proxyAuthenticator{basic},
		},
		{
			name:    "case-insensitive scheme match",
			schemes: []string{"NEGOTIATE", "NTLM"},
			want:    []proxyAuthenticator{neg, ntlm},
		},
		{
			name:    "advertised order does not change chain order",
			schemes: []string{"basic", "negotiate"},
			want:    []proxyAuthenticator{neg, basic},
		},
		{
			name:    "unknown scheme yields no candidates",
			schemes: []string{"digest"},
			want:    nil,
		},
		{
			name:    "empty schemes returns only safeWithoutChallenge methods (no Basic)",
			schemes: nil,
			want:    []proxyAuthenticator{neg, ntlm},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := chain.pick(tc.schemes, "proxy.example")
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestAuthChainPickRefusesBasicWhenUnadvertised(t *testing.T) {
	// F-1 / F-4 fix: Basic-only chain must refuse to act when proxy
	// returned 407 with no parseable Proxy-Authenticate.
	basic := newBasicAuthenticator("user:pass")
	chain := newAuthChain(basic)
	require.NotNil(t, chain)
	assert.Empty(t, chain.pick(nil, "proxy.example"))
	assert.Empty(t, chain.pick([]string{}, "proxy.example"))
}

func TestAuthChainPickHostFilterFallsThrough(t *testing.T) {
	// Gap D fix: applicableTo lets a method opt out for a specific
	// host. The chain falls through to the next method instead of
	// failing the whole chain.
	neg := realisticFake("Negotiate", "Negotiate t")
	neg.hostFilter = func(host string) bool { return host == "trusted.example" }
	ntlm := realisticFake("NTLM", "NTLM t")
	chain := newAuthChain(neg, ntlm)

	t.Run("trusted host: Negotiate selected first", func(t *testing.T) {
		got := chain.pick([]string{"negotiate", "ntlm"}, "trusted.example")
		assert.Equal(t, []proxyAuthenticator{neg, ntlm}, got)
	})
	t.Run("untrusted host: Negotiate omitted, NTLM selected", func(t *testing.T) {
		got := chain.pick([]string{"negotiate", "ntlm"}, "evil.example")
		assert.Equal(t, []proxyAuthenticator{ntlm}, got)
	})
	t.Run("untrusted host with only Negotiate advertised: nothing matches",
		func(t *testing.T) {
			got := chain.pick([]string{"negotiate"}, "evil.example")
			assert.Empty(t, got)
		})
}

func TestAuthChainPickNilReceiverIsSafe(t *testing.T) {
	var chain *authChain
	assert.Nil(t, chain.pick(nil, ""))
	assert.Nil(t, chain.pick([]string{"basic"}, "proxy.example"))
}

func TestAuthChainPickEmptyMethodsIsSafe(t *testing.T) {
	chain := &authChain{methods: nil}
	assert.Nil(t, chain.pick(nil, "proxy.example"))
	assert.Nil(t, chain.pick([]string{"basic"}, "proxy.example"))
}

func TestParseProxyAuthenticateSchemes(t *testing.T) {
	tests := []struct {
		name    string
		headers []string
		want    []string
	}{
		{
			name:    "single scheme with realm",
			headers: []string{`Basic realm="proxy"`},
			want:    []string{"basic"},
		},
		{
			name:    "multiple separate header values",
			headers: []string{"Negotiate", "NTLM", `Basic realm="proxy"`},
			want:    []string{"negotiate", "ntlm", "basic"},
		},
		{
			name:    "comma-joined challenges in one header",
			headers: []string{`Negotiate, NTLM, Basic realm="proxy"`},
			want:    []string{"negotiate", "ntlm", "basic"},
		},
		{
			name:    "quoted realm containing a comma is not split",
			headers: []string{`Basic realm="hello, world"`},
			want:    []string{"basic"},
		},
		{
			name: "unquoted parameter with commas: phantom token tolerated",
			// RFC 7230 §3.2.6 forbids unquoted commas inside parameter
			// values, but some misbehaving proxies emit them. The parser
			// distinguishes "key=value" continuations from new auth-schemes
			// by looking for '=' in the head token, which means a stray
			// bare token (`bar`) inside a parameter list is misclassified
			// as a phantom scheme. This is benign because no real
			// authenticator's scheme() matches such a token; the picker
			// silently ignores it. Asserted here to pin the documented
			// behaviour against future regressions.
			headers: []string{`Negotiate, Basic realm=foo,bar, NTLM`},
			want:    []string{"negotiate", "basic", "bar", "ntlm"},
		},
		{
			name:    "key=value parameter continuation across commas",
			headers: []string{`Basic realm=proxy, charset=UTF-8, NTLM`},
			want:    []string{"basic", "ntlm"},
		},
		{
			name:    "mixed-case scheme names normalised",
			headers: []string{"NeGoTiAtE", "NTLM"},
			want:    []string{"negotiate", "ntlm"},
		},
		{
			name:    "duplicate schemes deduplicated",
			headers: []string{"Negotiate", "negotiate", `Basic realm="a"`, `Basic realm="b"`},
			want:    []string{"negotiate", "basic"},
		},
		{
			name:    "no headers yields nil slice",
			headers: nil,
			want:    nil,
		},
		{
			name:    "empty header value skipped",
			headers: []string{"", "Basic"},
			want:    []string{"basic"},
		},
		{
			name:    "malformed scheme rejected by token grammar",
			headers: []string{`"not-a-token"`, "Basic"},
			want:    []string{"basic"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			header := make(http.Header)
			for _, h := range tc.headers {
				header.Add("Proxy-Authenticate", h)
			}
			got := parseProxyAuthenticateSchemes(header)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestIsToken(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"basic", true},
		{"NTLM", true},
		{"Negotiate", true},
		{"abc-123", true},
		{"a.b!c#d$e", true},
		{"", false},
		{"has space", false},
		{`with"quote`, false},
		{"comma,here", false},
		{"trailing,", false},
		{"\u00e9", false}, // non-ASCII
	}
	for _, tc := range tests {
		t.Run(tc.s, func(t *testing.T) {
			assert.Equal(t, tc.want, isToken(tc.s))
		})
	}
}

func TestSplitChallengeNames(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{
			in:   "Negotiate, NTLM",
			want: []string{"Negotiate", "NTLM"},
		},
		{
			in:   `Basic realm="hello, world", NTLM`,
			want: []string{"Basic", "NTLM"},
		},
		{
			in:   `Basic realm="quote\"inside, still", Negotiate`,
			want: []string{"Basic", "Negotiate"},
		},
		{
			// See TestParseProxyAuthenticateSchemes for the rationale —
			// `bar` is misclassified as a scheme but is benign downstream.
			in:   `Basic realm=foo,bar, NTLM`,
			want: []string{"Basic", "bar", "NTLM"},
		},
		{
			in:   `Basic realm=foo, charset=UTF-8, NTLM`,
			want: []string{"Basic", "NTLM"},
		},
		{
			in:   "  Single  ",
			want: []string{"Single"},
		},
		{
			in:   "",
			want: nil,
		},
		{
			in:   ",,",
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, splitChallengeNames(tc.in))
		})
	}
}
