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

//go:build integration && windows

// Integration test for the Windows SSPI Negotiate backend.
//
// Unlike the macOS fixture, this test cannot create its own environment:
// SSPI requires the host to be domain-joined with a real Kerberos credential,
// which can't be containerised or run on the CI matrix. The test therefore
// reads its connection details from the environment and self-skips when they
// (or a real ticket) are absent, so it never fails on a developer machine or
// in CI. testdata/kerberos-windows-integration/README.md documents the
// domain-joined environment used to exercise it (a Samba AD DC and a
// Negotiate-advertising Squid) and how to reproduce it locally.
//
//	ALPACA_IT_PROXY          proxy host:port, where host matches the SPN
//	                         (e.g. proxy.example.test:3128)
//	ALPACA_IT_UPSTREAM       URL the proxy fetches on success
//	                         (e.g. http://web.example.test/)
//	ALPACA_IT_UPSTREAM_BODY  expected upstream body (default "ok\n")
//	ALPACA_IT_BASIC          login:password for the Basic-fallback assertions
//
// Run with:
//
//	go test -tags=integration -run TestKerberosWindowsIntegration -v .

package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// windowsITConfig holds the harness-supplied connection details for the run.
type windowsITConfig struct {
	proxy        string // host:port; host must match the proxy's SPN
	upstream     string // URL squid fetches once auth succeeds
	upstreamBody string // body the upstream returns (proves the request was forwarded)
	basic        string // login:password for Basic, empty if not provisioned
}

// loadWindowsITConfig reads the harness environment and skips the test when
// the infrastructure (or a real Kerberos credential) is absent.
func loadWindowsITConfig(t *testing.T) windowsITConfig {
	t.Helper()
	proxy := os.Getenv("ALPACA_IT_PROXY")
	if proxy == "" {
		t.Skip("integration: ALPACA_IT_PROXY not set; run via the " +
			"testdata/kerberos-windows-integration harness on a domain-joined host")
	}
	if !checkKerberosTicket() {
		t.Skip("integration: no Kerberos credential available; this host is " +
			"not domain-joined or has no TGT")
	}
	upstream := os.Getenv("ALPACA_IT_UPSTREAM")
	require.NotEmpty(t, upstream,
		"ALPACA_IT_UPSTREAM must be set alongside ALPACA_IT_PROXY")
	body := os.Getenv("ALPACA_IT_UPSTREAM_BODY")
	if body == "" {
		body = "ok\n"
	}
	return windowsITConfig{
		proxy:        proxy,
		upstream:     upstream,
		upstreamBody: body,
		basic:        os.Getenv("ALPACA_IT_BASIC"),
	}
}

func TestKerberosWindowsIntegration(t *testing.T) {
	cfg := loadWindowsITConfig(t)

	// Negotiate succeeds when a real ticket is present.
	t.Run("Negotiate succeeds when ticket is present", func(t *testing.T) {
		neg := newNegotiateAuthenticator()
		require.NotNil(t, neg, "expected a Kerberos credential on a domain-joined host")
		chain := newAuthChain(neg)
		require.NotNil(t, chain)

		resp, err := cfg.roundTrip(t, chain)
		require.NoError(t, err)
		cfg.assertForwarded200(t, resp)
	})

	// With every method configured, Negotiate is tried first and Basic is
	// never invoked.
	t.Run("Multi-method chain prefers Negotiate", func(t *testing.T) {
		basic := cfg.requireBasic(t)
		neg := newNegotiateAuthenticator()
		require.NotNil(t, neg)
		instrumented := newInstrumentedBasic(basic)
		chain := newAuthChain(neg, instrumented)

		resp, err := cfg.roundTrip(t, chain)
		require.NoError(t, err)
		cfg.assertForwarded200(t, resp)
		assert.EqualValues(t, 0, instrumented.calls.Load(),
			"Basic must not be invoked when Negotiate succeeded first")
	})

	// When the ticket is lost between picker time and request time,
	// applicableTo excludes Negotiate and the chain falls through to Basic.
	t.Run("Falls through to Basic when Negotiate ticket is gone", func(t *testing.T) {
		basic := cfg.requireBasic(t)
		negotiator := newWindowsNegotiatorWithoutTicket(t)
		chain := newAuthChain(negotiator, newBasicAuthenticator(basic))

		resp, err := cfg.roundTrip(t, chain)
		require.NoError(t, err)
		cfg.assertForwarded200(t, resp)
	})

	// Only Negotiate configured but ineligible: the picker yields zero
	// candidates and the loop returns errNoMatchingAuthMethod rather than
	// silently sending Basic.
	t.Run("Refuses downgrade when only Negotiate is configured but ineligible", func(t *testing.T) {
		negotiator := newWindowsNegotiatorWithoutTicket(t)
		chain := newAuthChain(negotiator)

		resp, err := cfg.roundTrip(t, chain)
		if err == nil {
			_ = resp.Body.Close()
		}
		require.Error(t, err)
		assert.ErrorIs(t, err, errNoMatchingAuthMethod)
	})

	// The chain-level allowlist excludes the proxy host uniformly, so no
	// method is attempted.
	t.Run("proxy-auth allowlist excludes proxy", func(t *testing.T) {
		neg := newNegotiateAuthenticator()
		require.NotNil(t, neg)
		chain := newAuthChain(neg)
		require.NotNil(t, chain)
		chain.hostAllowlist = parseAuthAllowlist(".unrelated.test")

		resp, err := cfg.roundTrip(t, chain)
		if err == nil {
			_ = resp.Body.Close()
		}
		require.Error(t, err,
			"chain-level allowlist must exclude the proxy host before any method runs")
	})
}

// requireBasic returns the Basic credential or skips the sub-test when the
// harness did not provision one.
func (cfg windowsITConfig) requireBasic(t *testing.T) string {
	t.Helper()
	if cfg.basic == "" {
		t.Skip("integration: ALPACA_IT_BASIC not set; skipping Basic-fallback assertion")
	}
	return cfg.basic
}

// newWindowsNegotiatorWithoutTicket builds the real Windows Negotiate
// authenticator, then overrides its ticket check to report "no ticket" so the
// picker treats it as ineligible, exercising fall-through without depending
// on the absence of a real credential.
func newWindowsNegotiatorWithoutTicket(t *testing.T) *negotiateAuthenticator {
	t.Helper()
	neg := newNegotiateAuthenticator()
	require.NotNil(t, neg)
	negotiator, ok := neg.(*negotiateAuthenticator)
	require.True(t, ok)
	negotiator.hasTicket = func() bool { return false }
	return negotiator
}

// roundTrip drives a request through alpaca's auth chain against the
// configured proxy, mirroring what ProxyHandler does without the full
// middleware stack.
func (cfg windowsITConfig) roundTrip(t *testing.T, chain *authChain) (*http.Response, error) {
	t.Helper()
	proxyURL := &url.URL{Scheme: "http", Host: cfg.proxy}
	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer tr.CloseIdleConnections()

	req, err := http.NewRequest(http.MethodGet, cfg.upstream, nil)
	require.NoError(t, err)
	// Decorate the request with the proxy URL so negotiateAuthenticator can
	// derive the SPN host from the context.
	req = req.WithContext(context.WithValue(req.Context(), contextKeyProxy, proxyURL))

	// Empty body reader so the auth retry can replay it across attempts.
	rd := bytes.NewReader(nil)

	resp, err := tr.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusProxyAuthRequired {
		return resp, nil
	}
	if chain == nil {
		return resp, nil
	}
	schemes := parseProxyAuthenticateSchemes(resp.Header)
	_ = resp.Body.Close()
	return retryProxyRequestWithAuth(req, tr, chain, schemes, rd)
}

// assertForwarded200 verifies the response is a 200 carrying the upstream's
// body rather than a squid-synthesised page, proving the auth chain reached
// the "request forwarded" stage. Closes the body.
func (cfg windowsITConfig) assertForwarded200(t *testing.T, resp *http.Response) {
	t.Helper()
	defer resp.Body.Close() //nolint:errcheck
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, cfg.upstreamBody, string(body),
		"expected the upstream test server response, not a squid synthesised page")
}

// instrumentedBasic wraps a basicAuthenticator with a call counter so a test
// can assert Basic was not invoked when Negotiate should have won.
type instrumentedBasic struct {
	*basicAuthenticator
	calls atomic.Int32
}

func (b *instrumentedBasic) do(req *http.Request, rt http.RoundTripper) (*http.Response, error) {
	b.calls.Add(1)
	return b.basicAuthenticator.do(req, rt)
}

func newInstrumentedBasic(creds string) *instrumentedBasic {
	return &instrumentedBasic{basicAuthenticator: newBasicAuthenticator(creds)}
}
