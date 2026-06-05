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

//go:build windows

// Windows backend for Kerberos/Negotiate proxy authentication. The
// platform-agnostic authenticator lives in kerberos_common.go; this file
// supplies the two functions it depends on, implemented with Microsoft's
// SSPI Negotiate package (github.com/alexbrainman/sspi/negotiate, a pure-Go
// wrapper over secur32.dll, no cgo):
//
//   - checkKerberosTicket()     reports whether the current logon session
//                               has a usable Negotiate credential (Kerberos,
//                               or NTLM as SSPI's fallback).
//   - generateSPNEGOToken(host) produces the initial SPNEGO token for the
//                               proxy's HTTP service principal.
//
// See RFC 4559 for the Negotiate HTTP scheme and RFC 4178 for SPNEGO.

package main

import (
	"fmt"
	"log"

	"github.com/alexbrainman/sspi/negotiate"
)

// newNegotiateAuthenticator returns a negotiateAuthenticator that will be
// consulted on every 407 response. It does NOT require a Kerberos
// credential to exist when alpaca starts: applicableTo() re-checks
// availability per request, so a credential that becomes available later
// is honoured at the next 407 without restarting alpaca.
func newNegotiateAuthenticator() proxyAuthenticator {
	if checkKerberosTicket() {
		log.Println("Kerberos ticket found")
	} else {
		log.Println("No Kerberos ticket at startup; will check again " +
			"on each 407 response so a ticket that arrives later is " +
			"honoured automatically")
	}
	return &negotiateAuthenticator{hasTicket: checkKerberosTicket}
}

// checkKerberosTicket reports whether the current user's logon session has a
// usable Negotiate credential. It probes by acquiring the current-user
// Negotiate credential handle and releasing it immediately; we only need to
// know whether the acquire would succeed.
//
// Two caveats the name understates: SSPI's Negotiate package selects Kerberos
// when it can and falls back to NTLM otherwise, so a true result does not
// guarantee a Kerberos ticket specifically; and the probe does not assert a
// particular unexpired TGT lifetime (unlike the macOS backend's lifetime
// check). It reports only whether Negotiate could be attempted; which proxy
// hosts may actually receive a token is decided solely by the chain-level
// ALPACA_PROXY_AUTH_ALLOWLIST gate in multiauth.go. Returns
// false (never panics) when no credential is available, e.g. on a workgroup
// machine.
func checkKerberosTicket() bool {
	cred, err := negotiate.AcquireCurrentUserCredentials()
	if err != nil {
		return false
	}
	_ = cred.Release()
	return true
}

// generateSPNEGOToken produces the initial SPNEGO token for the proxy's
// HTTP/<host> service principal using the current user's Kerberos
// credential. Only the first leg is sent: the proxy validates the token
// against its keytab and either accepts it or re-challenges. RFC 4559 §5
// permits further legs for mutual authentication, which alpaca does not
// request, matching the macOS backend.
//
// The SPN form is "HTTP/<host>", which Active Directory registers and SSPI
// expects; the macOS GSS backend names the same principal as "HTTP@<host>"
// (the GSS host-based service form, RFC 2743 §4.1).
//
// The credential and context handles are owned by the OS and are released
// explicitly via defer; Go's garbage collector will not reclaim them.
func generateSPNEGOToken(proxyHost string) ([]byte, error) {
	cred, err := negotiate.AcquireCurrentUserCredentials()
	if err != nil {
		return nil, fmt.Errorf("AcquireCurrentUserCredentials: %w", err)
	}
	defer func() { _ = cred.Release() }()

	target := "HTTP/" + proxyHost
	cc, token, err := negotiate.NewClientContext(cred, target)
	if err != nil {
		return nil, fmt.Errorf("negotiate.NewClientContext(%q): %w", target, err)
	}
	defer func() { _ = cc.Release() }()

	if len(token) == 0 {
		return nil, fmt.Errorf("negotiate: empty initial SPNEGO token for %q", target)
	}
	return token, nil
}
