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
//
// hostAllowlist (sourced from --proxy-auth-allowlist or
// ALPACA_PROXY_AUTH_ALLOWLIST) is an optional list of DNS suffixes that
// restricts which proxy hostnames may receive ANY proxy credential —
// Basic, NTLM, or Negotiate. A nil allowlist is the default and permits
// every host: alpaca will offer whatever credentials the proxy advertises.
// Set the field to a non-empty slice from parseAuthAllowlist to enforce
// the restriction.
type authChain struct {
	methods       []proxyAuthenticator
	hostAllowlist []string // nil = permit any host (the default)
}

// newAuthChain builds an authChain from the given methods, skipping nil
// entries. Callers should pass methods in their preferred order
// (most-preferred first). Chrome's hierarchy is Negotiate → NTLM → Basic
// — Basic last because it transmits credentials unencrypted. Returns nil
// when no usable methods remain so the proxy code path can short-circuit.
//
// The returned chain's hostAllowlist defaults to nil (permit any host).
// Callers that need to restrict credential exposure should set it before
// the chain is consulted, e.g. via parseAuthAllowlist on the user's
// --proxy-auth-allowlist / ALPACA_PROXY_AUTH_ALLOWLIST value.
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
// given proxy host. Filtering is the union of four policies, in order:
//
//  1. Host allowlist (chain-level). If hostAllowlist is non-empty and
//     proxyHost doesn't match any suffix, ZERO methods are returned —
//     alpaca refuses to send any credentials to a non-allowlisted host.
//     This is the same gate for Basic, NTLM, and Negotiate.
//  2. Per-authenticator runtime applicability via applicableTo(proxyHost).
//     An authenticator may opt out for reasons unrelated to the host
//     allowlist (e.g. Negotiate has no Kerberos ticket) so the chain
//     falls through to the next method instead of failing.
//  3. Scheme advertisement: only methods whose scheme() is in the
//     advertised schemes set are considered.
//  4. Empty-schemes (unadvertised) fallback: when the proxy did not
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
	// Host allowlist gate runs first so a non-permitted host receives
	// no credentials of any kind. INFO-level log (not behind --debug)
	// because excluding a proxy from auth is the most common cause of
	// "alpaca didn't authenticate against my proxy" and the user
	// needs to self-diagnose.
	if !c.allowedHost(proxyHost) {
		log.Printf("Proxy %q not in proxy-auth allowlist (allowed: %v); "+
			"update --proxy-auth-allowlist or ALPACA_PROXY_AUTH_ALLOWLIST "+
			"to include this host, or unset to permit any host", proxyHost,
			c.hostAllowlist)
		return nil
	}
	// Per-authenticator runtime policy (e.g. Negotiate's ticket-presence
	// check). Methods that opt out are completely invisible to the
	// rest of the picker.
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

// allowedHost reports whether the given proxy hostname is permitted to
// receive credentials under the configured allowlist. A nil or empty
// allowlist permits everything (the default). Entries are dot-prefixed
// lower-case DNS suffixes; matching prepends "." to the host so
// ".corp.example" matches both "corp.example" and "proxy.corp.example"
// but not "evilcorp.example".
//
// A trailing dot on the host (e.g. "proxy.corp.example.", which
// url.URL.Hostname() does not strip) is removed before matching, so a
// hostile PAC cannot bypass the allowlist by appending a dot. The same
// normalisation is applied to allowlist entries at parse time.
func (c *authChain) allowedHost(host string) bool {
	if len(c.hostAllowlist) == 0 {
		return true
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	host = "." + host
	for _, suffix := range c.hostAllowlist {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// parseAuthAllowlist parses a comma-separated list of DNS suffixes from
// the user-supplied allowlist (either the --proxy-auth-allowlist flag or
// the ALPACA_PROXY_AUTH_ALLOWLIST env var). Each entry is normalised to
// lower-case and dot-prefixed canonical form (".corp.example") so
// allowedHost can do a single suffix match. The literal value "*" is
// translated to a nil slice — the same shape as the default — so callers
// can treat "permissive" uniformly. Malformed entries are dropped with a
// warning rather than failing parsing.
//
// Returns nil for empty input, for "*", or when every entry was
// malformed. A nil return therefore always means "no restriction".
func parseAuthAllowlist(value string) []string {
	if value == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(strings.ToLower(part))
		// Strip a trailing dot — DNS FQDNs are sometimes written
		// "corp.example." and we want that to compare equal to
		// "corp.example". allowedHost strips trailing dots from the
		// host side too, so both sides are normalised the same way.
		part = strings.TrimSuffix(part, ".")
		if part == "" {
			continue
		}
		// "*" means "any host". Returning nil surfaces this as the
		// same shape as "unset / default permissive".
		if part == "*" {
			return nil
		}
		if !isAllowlistEntry(part) {
			log.Printf("Ignoring malformed proxy-auth allowlist entry %q", part)
			continue
		}
		// Normalise to dot-prefixed canonical form so allowedHost is
		// a single suffix match. Bare "corp.example" is recorded as
		// ".corp.example"; allowedHost prepends "." to the host at
		// match time, so both "corp.example" and any subdomain
		// satisfy the entry.
		if !strings.HasPrefix(part, ".") {
			out = append(out, "."+part)
		} else {
			out = append(out, part)
		}
	}
	return out
}

// isAllowlistEntry reports whether s looks like a plausible DNS suffix
// entry. Lower-case alphanumeric, hyphen, and dot are accepted, with an
// optional leading dot. Anything else — whitespace, shell wildcards
// (other than the bare "*" handled above), path separators, etc. — is
// rejected so a typo doesn't silently match something unintended.
func isAllowlistEntry(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r == '-', r == '.':
			continue
		}
		return false
	}
	return true
}
