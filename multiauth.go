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
	"errors"
	"log"
	"strings"
)

// authChain is the picker that proxy.go drives. It owns the ordered list
// of available authentication methods and decides — given the schemes the
// proxy advertised in its 407 Proxy-Authenticate header and the proxy
// hostname — which methods to attempt and in what order. Iteration (and
// connection re-dialling between attempts) is the caller's responsibility:
// each method's do() must run on its own freshly-dialled connection, both
// because some proxies close the TCP socket after a 407 and because
// schemes like NTLM and Negotiate are connection-bound (RFC 4559) and must
// not share a socket with a different scheme's state machine.
type authChain struct {
	methods []proxyAuthenticator
}

// newAuthChain builds an authChain from the given methods, skipping nil
// entries. Callers should pass methods in their preferred order
// (most-preferred first). Chrome's hierarchy is Negotiate → NTLM → Basic
// — Basic last because it transmits credentials unencrypted. Returns nil
// when no usable methods remain so the proxy code path can short-circuit.
func newAuthChain(methods ...proxyAuthenticator) *authChain {
	var filtered []proxyAuthenticator
	for _, m := range methods {
		if m != nil {
			filtered = append(filtered, m)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return &authChain{methods: filtered}
}

// errNoMatchingAuthMethod is returned when none of the configured
// authenticators are willing to act for the schemes the proxy advertised
// against the given proxy host. proxy.go translates this into a 502
// Bad Gateway to the client.
var errNoMatchingAuthMethod = errors.New("no matching authentication method for proxy")

// pick returns the authenticators (in chain order) that should be tried
// against a 407 response that advertised the given schemes from the
// given proxy host. Filtering is the union of three policies:
//
//  1. Per-authenticator host policy via applicableTo(proxyHost). An
//     authenticator may opt out (e.g. KERBEROS_SPN_ALLOWLIST) so the
//     chain falls through to the next method instead of failing.
//  2. Scheme advertisement: only methods whose scheme() is in the
//     advertised schemes set are considered.
//  3. Empty-schemes (unadvertised) fallback: when the proxy did not
//     advertise any parseable scheme, only methods that opt in via
//     safeWithoutChallenge() are considered. This is the F-1/F-4
//     downgrade defence: Basic's first message IS the credential, so
//     it must NEVER be tried unless the proxy explicitly said so.
func (c *authChain) pick(schemes []string, proxyHost string) []proxyAuthenticator {
	if c == nil || len(c.methods) == 0 {
		return nil
	}
	debugf("Picker: %d configured method(s) for proxy=%q advertised-schemes=%v",
		len(c.methods), proxyHost, schemes)
	// Apply host policy first so methods that opt out are completely
	// invisible to the rest of the picker.
	applicable := make([]proxyAuthenticator, 0, len(c.methods))
	for _, m := range c.methods {
		if !m.applicableTo(proxyHost) {
			log.Printf("Auth method %s declines for proxy host %q",
				m.scheme(), proxyHost)
			continue
		}
		debugf("Picker: %s applicable for proxy=%q", m.scheme(), proxyHost)
		applicable = append(applicable, m)
	}
	if len(applicable) == 0 {
		debugf("Picker: no applicable methods after host-policy filter")
		return nil
	}

	if len(schemes) == 0 {
		// Fallback path: no Proxy-Authenticate parsed. Only allow
		// methods that explicitly opt in via safeWithoutChallenge().
		// Today that's NTLM (schemeNTLM) and Negotiate (schemeNegotiate);
		// schemeBasic is excluded because Basic's first message IS the
		// credential — see proxyAuthenticator interface doc.
		var safe []proxyAuthenticator
		for _, m := range applicable {
			if m.safeWithoutChallenge() {
				safe = append(safe, m)
			}
		}
		if len(safe) == 0 {
			log.Printf("Proxy did not advertise any scheme and no " +
				"safe-without-challenge authenticator is configured; " +
				"refusing to send credentials")
		}
		debugf("Picker: no advertised schemes; safe-without-challenge "+
			"fallback selected %d method(s)", len(safe))
		return safe
	}

	advertised := make(map[string]bool, len(schemes))
	for _, s := range schemes {
		advertised[strings.ToLower(s)] = true
	}
	var matched []proxyAuthenticator
	for _, m := range applicable {
		if advertised[strings.ToLower(m.scheme())] {
			matched = append(matched, m)
		}
	}
	if len(matched) == 0 {
		log.Printf("Proxy advertises %v but no matching authenticator is configured",
			schemes)
	}
	if debugEnabled {
		names := make([]string, 0, len(matched))
		for _, m := range matched {
			names = append(names, m.scheme())
		}
		debugf("Picker: %d candidate(s) in attempt order: %v",
			len(matched), names)
	}
	return matched
}
