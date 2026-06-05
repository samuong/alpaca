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

//go:build darwin || windows

package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"net/url"
)

// negotiateAuthenticator implements proxyAuthenticator using SPNEGO
// (Kerberos/Negotiate). The platform backends supply the two functions
// it depends on: checkKerberosTicket and generateSPNEGOToken. macOS supplies
// them via GSS.framework (kerberos_darwin.go), Windows via SSPI
// (kerberos_windows.go).
//
// It does NOT enforce a host allowlist itself; that's the picker's job
// (see *authChain.allowedHost), which applies uniformly to Basic, NTLM,
// and Negotiate. The only per-method applicability check Negotiate
// enforces is "do we currently have a Kerberos ticket?", re-checked on
// every 407 so a ticket that arrives mid-session is honoured
// automatically without an alpaca restart.
type negotiateAuthenticator struct {
	// hasTicket is the ticket-availability check used by applicableTo
	// at picker time. Defaults to checkKerberosTicket; tests inject
	// their own to avoid depending on the developer's real Kerberos
	// state.
	hasTicket func() bool
}

func (n *negotiateAuthenticator) scheme() string { return "Negotiate" }

// applicableTo enforces two policies at picker time:
//
//  1. The proxy host must be non-empty (we cannot generate an SPN
//     without it).
//  2. A usable credential must currently be available, as reported by the
//     platform's checkKerberosTicket. We re-check on every 407 because the
//     credential may have expired or been revoked since alpaca started; if
//     it has, returning false here causes the picker to omit Negotiate and
//     fall through to NTLM / Basic instead of failing the chain.
//
// Host policy (the ALPACA_PROXY_AUTH_ALLOWLIST gate) is enforced at the
// picker level in *authChain.pick, uniformly across Basic, NTLM, and
// Negotiate, so this method intentionally doesn't repeat that check.
//
// Returning false is silent fall-through; the chain proceeds to the
// next configured authenticator.
func (n *negotiateAuthenticator) applicableTo(proxyHost string) bool {
	if proxyHost == "" {
		return false
	}
	check := n.hasTicket
	if check == nil {
		check = checkKerberosTicket
	}
	if !check() {
		log.Printf("Kerberos ticket no longer valid; skipping Negotiate for %s",
			proxyHost)
		return false
	}
	return true
}

// do performs Negotiate/SPNEGO proxy authentication. It generates a SPNEGO
// token for the upstream proxy and sends the request with a
// Proxy-Authorization: Negotiate header.
func (n *negotiateAuthenticator) do(req *http.Request, rt http.RoundTripper) (*http.Response, error) {
	// Get the proxy host from the request context.
	proxyHost := ""
	if value := req.Context().Value(contextKeyProxy); value != nil {
		proxy := value.(*url.URL)
		proxyHost = proxy.Hostname()
	}
	if proxyHost == "" {
		return nil, fmt.Errorf("cannot determine proxy host for Negotiate auth")
	}

	token, err := generateSPNEGOToken(proxyHost)
	if err != nil {
		log.Printf("Error generating SPNEGO token for %s: %v", proxyHost, err)
		return nil, err
	}

	req.Header.Set("Proxy-Authorization", "Negotiate "+base64.StdEncoding.EncodeToString(token))
	return rt.RoundTrip(req)
}
