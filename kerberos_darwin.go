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

//go:build darwin

package main

/*
#cgo LDFLAGS: -framework GSS
#include <GSS/GSS.h>
#include <stdlib.h>

// hasCredential checks if the current user has a valid Kerberos credential
// by attempting to acquire the default credential via GSS.framework.
// Returns 1 if a credential is available, 0 otherwise.
static int hasCredential() {
    OM_uint32 major, minor;
    gss_cred_id_t cred = GSS_C_NO_CREDENTIAL;
    OM_uint32 lifetime = 0;

    major = gss_acquire_cred(
        &minor,
        GSS_C_NO_NAME,       // default principal (current user)
        0,                   // default lifetime
        GSS_C_NO_OID_SET,    // default mechanism set
        GSS_C_INITIATE,      // we are the initiator
        &cred,
        NULL,
        &lifetime
    );

    if (cred != GSS_C_NO_CREDENTIAL) {
        gss_release_cred(&minor, &cred);
    }

    return (major == GSS_S_COMPLETE && lifetime > 0) ? 1 : 0;
}

// defaultPrincipal acquires the default Kerberos credential and writes its
// printable principal name (e.g. "alice@CORP.EXAMPLE.COM") into the
// caller-supplied buffer. Returns the number of bytes written, or 0 on
// failure / no credential. The Go side parses the realm from this string.
static size_t defaultPrincipal(char *buf, size_t buflen) {
    OM_uint32 major, minor;
    gss_cred_id_t cred = GSS_C_NO_CREDENTIAL;
    gss_name_t name = GSS_C_NO_NAME;
    gss_buffer_desc display = GSS_C_EMPTY_BUFFER;
    size_t written = 0;

    major = gss_acquire_cred(
        &minor,
        GSS_C_NO_NAME,
        0,
        GSS_C_NO_OID_SET,
        GSS_C_INITIATE,
        &cred,
        NULL,
        NULL
    );
    if (major != GSS_S_COMPLETE || cred == GSS_C_NO_CREDENTIAL) {
        if (cred != GSS_C_NO_CREDENTIAL) {
            gss_release_cred(&minor, &cred);
        }
        return 0;
    }

    major = gss_inquire_cred(&minor, cred, &name, NULL, NULL, NULL);
    gss_release_cred(&minor, &cred);
    if (major != GSS_S_COMPLETE || name == GSS_C_NO_NAME) {
        if (name != GSS_C_NO_NAME) {
            gss_release_name(&minor, &name);
        }
        return 0;
    }

    major = gss_display_name(&minor, name, &display, NULL);
    gss_release_name(&minor, &name);
    if (major != GSS_S_COMPLETE) {
        gss_release_buffer(&minor, &display);
        return 0;
    }

    if (display.length > 0 && display.length < buflen) {
        memcpy(buf, display.value, display.length);
        buf[display.length] = '\0';
        written = display.length;
    }
    gss_release_buffer(&minor, &display);
    return written;
}

// generateToken generates a SPNEGO token for the given service principal name.
// The caller must free the returned token with free().
// Returns 0 on success, non-zero GSS major status on failure.
static OM_uint32 generateToken(const char *spn, void **tokenData, size_t *tokenLen, OM_uint32 *minorStatus) {
    OM_uint32 major, minor;
    gss_buffer_desc nameBuffer;
    gss_name_t targetName = GSS_C_NO_NAME;
    gss_ctx_id_t ctx = GSS_C_NO_CONTEXT;
    gss_buffer_desc outputToken = GSS_C_EMPTY_BUFFER;

    *tokenData = NULL;
    *tokenLen = 0;
    *minorStatus = 0;

    // Import the target name (HTTP@proxyhost)
    nameBuffer.value = (void *)spn;
    nameBuffer.length = strlen(spn);
    major = gss_import_name(&minor, &nameBuffer, GSS_C_NT_HOSTBASED_SERVICE, &targetName);
    if (major != GSS_S_COMPLETE) {
        *minorStatus = minor;
        return major;
    }

    // Initialize security context to get the SPNEGO token
    major = gss_init_sec_context(
        &minor,
        GSS_C_NO_CREDENTIAL,   // use default credential (current user's TGT)
        &ctx,
        targetName,
        GSS_SPNEGO_MECHANISM,  // SPNEGO mechanism
        0,                     // no special flags
        0,                     // default lifetime
        GSS_C_NO_CHANNEL_BINDINGS,
        GSS_C_NO_BUFFER,       // no input token (first call)
        NULL,                  // actual mechanism (not needed)
        &outputToken,
        NULL,                  // ret_flags
        NULL                   // time_rec
    );

    gss_release_name(&minor, &targetName);

    if (major != GSS_S_COMPLETE && major != GSS_S_CONTINUE_NEEDED) {
        *minorStatus = minor;
        if (ctx != GSS_C_NO_CONTEXT) {
            gss_delete_sec_context(&minor, &ctx, GSS_C_NO_BUFFER);
        }
        return major;
    }

    // Copy the output token
    if (outputToken.length > 0) {
        *tokenData = malloc(outputToken.length);
        if (*tokenData != NULL) {
            memcpy(*tokenData, outputToken.value, outputToken.length);
            *tokenLen = outputToken.length;
        }
    }

    gss_release_buffer(&minor, &outputToken);
    if (ctx != GSS_C_NO_CONTEXT) {
        gss_delete_sec_context(&minor, &ctx, GSS_C_NO_BUFFER);
    }

    return GSS_S_COMPLETE;
}
*/
import "C"

import (
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unsafe"
)

type negotiateAuthenticator struct {
	// allowedSuffixes restricts SPN generation to proxy hostnames whose
	// FQDN ends with one of these (case-insensitive, dot-prefixed)
	// suffixes. Sourced from the KERBEROS_SPN_ALLOWLIST env var. An
	// empty slice means "permissive" — any proxy host is allowed.
	allowedSuffixes []string
	// hasTicket is the ticket-availability check used by applicableTo
	// at picker time. Defaults to checkKerberosTicket; tests inject
	// their own to avoid depending on the developer's real Kerberos
	// state.
	hasTicket func() bool
}

// newNegotiateAuthenticator checks for a Kerberos ticket and returns a
// negotiateAuthenticator if one is available. If waitSeconds > 0 and no ticket
// is found immediately, it polls every second up to the given timeout.
// Returns nil if no ticket is available.
//
// SPN allowlist: when KERBEROS_SPN_ALLOWLIST is set (comma-separated list
// of DNS suffixes, e.g. ".corp.example.com,.example.test"), Negotiate
// will only request a Kerberos service ticket for proxy hostnames whose
// FQDN matches one of the suffixes. This defends against a hostile PAC
// coercing the user's KDC to issue tickets for an attacker-named SPN.
// Default empty = permissive (no behavioural change for trusted networks).
func newNegotiateAuthenticator(waitSeconds int) proxyAuthenticator {
	allowed := parseSPNAllowlist(os.Getenv("KERBEROS_SPN_ALLOWLIST"))
	if len(allowed) > 0 {
		log.Printf("Kerberos SPN allowlist active: %v", allowed)
	}
	if checkKerberosTicket() {
		log.Println("Kerberos ticket found")
		return &negotiateAuthenticator{
			allowedSuffixes: allowed,
			hasTicket:       checkKerberosTicket,
		}
	}
	if waitSeconds <= 0 {
		return nil
	}
	log.Printf("No Kerberos ticket found, waiting up to %d seconds...", waitSeconds)
	if waitForKerberosTicket(waitSeconds) {
		log.Println("Kerberos ticket found")
		return &negotiateAuthenticator{
			allowedSuffixes: allowed,
			hasTicket:       checkKerberosTicket,
		}
	}
	log.Println("No Kerberos ticket found after waiting")
	return nil
}

// parseSPNAllowlist parses a comma-separated list of DNS suffixes from
// KERBEROS_SPN_ALLOWLIST. Each entry is normalised to lower-case and
// dot-prefixed canonical form (".corp.example") so allowedHost can do a
// single suffix match. A literal "*" entry means "allow any host" and
// is translated to a nil allowlist (preserving backward-compatible
// permissive behaviour for that explicit opt-out). Malformed entries
// are dropped with a warning.
func parseSPNAllowlist(value string) []string {
	if value == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(strings.ToLower(part))
		if part == "" {
			continue
		}
		// "*" means "any host". Surface explicit override as nil so
		// allowedHost short-circuits.
		if part == "*" {
			return nil
		}
		if !isAllowlistEntry(part) {
			log.Printf("Ignoring malformed KERBEROS_SPN_ALLOWLIST entry %q", part)
			continue
		}
		// Normalise to dot-prefixed canonical form so allowedHost is
		// a single suffix match. Bare "corp.example" is recorded as
		// ".corp.example" which matches "*.corp.example" hosts only;
		// to also match the bare "corp.example" host, the user
		// should add the bare form too.
		if !strings.HasPrefix(part, ".") {
			out = append(out, "."+part)
		} else {
			out = append(out, part)
		}
	}
	return out
}

// isAllowlistEntry reports whether s looks like a plausible DNS
// suffix entry. We allow lower-case alphanumeric, hyphen, and dot, with
// an optional leading dot. We reject anything containing whitespace,
// shell wildcards (other than the literal "*" handled above), or
// other unexpected characters.
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

// allowedHost reports whether the given proxy hostname is permitted to
// receive a SPNEGO token under the configured allowlist. An empty
// allowlist permits everything. Allowlist entries are normalised by
// parseSPNAllowlist to dot-prefixed lower-case form so a single suffix
// match is sufficient: ".corp.example" matches "proxy.corp.example".
func (n *negotiateAuthenticator) allowedHost(host string) bool {
	if len(n.allowedSuffixes) == 0 {
		return true
	}
	host = "." + strings.ToLower(host)
	for _, suffix := range n.allowedSuffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// checkKerberosTicket returns true if valid Kerberos credentials exist.
// Uses GSS.framework to check the system credential store, which includes
// tickets managed by Apple SSO and the Ticket Viewer app.
func checkKerberosTicket() bool {
	return C.hasCredential() == 1
}

// defaultKerberosRealm returns the lower-cased realm of the user's
// default Kerberos credential (e.g. "corp.example.com" for a principal
// alice@CORP.EXAMPLE.COM), or the empty string if no credential is
// available or the principal name is malformed.
//
// Used by main.go to derive a sensible default for KERBEROS_SPN_ALLOWLIST
// when the user hasn't set one explicitly: requesting Kerberos service
// tickets within the user's own home realm is the security boundary
// that actually matters for SPN coercion. Tickets for SPNs OUTSIDE the
// home realm would have to come from a cross-realm trust, which is not
// implicitly granted just because alpaca asked.
func defaultKerberosRealm() string {
	const buflen = 256
	buf := make([]byte, buflen)
	n := C.defaultPrincipal(
		(*C.char)(unsafe.Pointer(&buf[0])),
		C.size_t(buflen),
	)
	if n == 0 {
		return ""
	}
	principal := string(buf[:n])
	at := strings.LastIndex(principal, "@")
	if at < 0 || at == len(principal)-1 {
		return ""
	}
	return strings.ToLower(principal[at+1:])
}

// waitForKerberosTicket polls for a Kerberos ticket every second up to
// the given timeout, returning true as soon as one becomes available.
// The poll interval is deliberately short so that a `-w 1` invocation
// performs at least one check before giving up.
func waitForKerberosTicket(timeoutSeconds int) bool {
	if timeoutSeconds <= 0 {
		return false
	}
	deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	const pollInterval = time.Second
	for {
		if checkKerberosTicket() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(pollInterval)
	}
}

// generateSPNEGOToken creates a SPNEGO token for the given proxy host using
// the macOS GSS.framework and the current user's Kerberos TGT.
func generateSPNEGOToken(proxyHost string) ([]byte, error) {
	spn := "HTTP@" + proxyHost
	cSPN := C.CString(spn)
	defer C.free(unsafe.Pointer(cSPN))

	var tokenData unsafe.Pointer
	var tokenLen C.size_t
	var minorStatus C.OM_uint32

	major := C.generateToken(cSPN, &tokenData, &tokenLen, &minorStatus)
	if tokenData != nil {
		defer C.free(tokenData)
	}

	if major != C.GSS_S_COMPLETE {
		return nil, fmt.Errorf("gss_init_sec_context failed: major=%d minor=%d", major, minorStatus)
	}
	if tokenLen == 0 {
		return nil, fmt.Errorf("gss_init_sec_context returned empty token")
	}

	return C.GoBytes(tokenData, C.int(tokenLen)), nil
}

func (n *negotiateAuthenticator) scheme() string { return "Negotiate" }

// safeWithoutChallenge reports true: the SPNEGO initial token contains
// no password material (it's a Kerberos service ticket request), so it
// is safe to send before the proxy has explicitly advertised Negotiate.
func (n *negotiateAuthenticator) safeWithoutChallenge() bool { return true }

// applicableTo enforces three policies at picker time:
//
//  1. The proxy host must be non-empty (we cannot generate an SPN
//     without it).
//  2. The proxy host must satisfy KERBEROS_SPN_ALLOWLIST.
//  3. A valid Kerberos ticket must currently be available. We re-check
//     on every 407 because the user's ticket may have expired or been
//     revoked since alpaca started; if it has, returning false here
//     causes the picker to omit Negotiate and fall through to NTLM /
//     Basic instead of failing the chain on a stale-ticket error.
//
// Returning false is silent fall-through; the chain proceeds to the
// next configured authenticator.
func (n *negotiateAuthenticator) applicableTo(proxyHost string) bool {
	if proxyHost == "" {
		return false
	}
	if !n.allowedHost(proxyHost) {
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
	// Defence-in-depth: applicableTo is the picker-time gate, but
	// re-check here in case do() is invoked directly (e.g. by tests
	// or future callers that bypass the picker).
	if !n.allowedHost(proxyHost) {
		log.Printf("Proxy host %q not on KERBEROS_SPN_ALLOWLIST; refusing Negotiate",
			proxyHost)
		return nil, fmt.Errorf(
			"proxy host %q not in KERBEROS_SPN_ALLOWLIST", proxyHost)
	}

	token, err := generateSPNEGOToken(proxyHost)
	if err != nil {
		log.Printf("Error generating SPNEGO token for %s: %v", proxyHost, err)
		return nil, err
	}

	req.Header.Set("Proxy-Authorization", "Negotiate "+base64.StdEncoding.EncodeToString(token))
	return rt.RoundTrip(req)
}
