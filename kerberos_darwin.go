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
	"time"
	"unsafe"
)

// negotiateAuthenticator implements proxyAuthenticator using SPNEGO
// over GSS.framework on macOS. It does NOT enforce a host allowlist
// itself — that's the picker's job (see *authChain.allowedHost), which
// applies uniformly to Basic, NTLM, and Negotiate. The only per-method
// applicability check Negotiate enforces is "do we currently have a
// Kerberos ticket?" — re-checked on every 407 so a ticket that
// arrives mid-session (Apple SSO completing, kinit, etc.) is honoured
// automatically without an alpaca restart.
type negotiateAuthenticator struct {
	// hasTicket is the ticket-availability check used by applicableTo
	// at picker time. Defaults to checkKerberosTicket; tests inject
	// their own to avoid depending on the developer's real Kerberos
	// state.
	hasTicket func() bool
}

// newNegotiateAuthenticator returns a negotiateAuthenticator that will
// be consulted on every 407 response. It does NOT require a Kerberos
// ticket to exist at the moment alpaca starts: applicableTo() re-checks
// ticket availability per-request, so a ticket that arrives later (e.g.
// because Apple SSO finishes after alpaca, or the user runs kinit
// mid-session) starts being honoured at the next 407 without a
// restart.
//
// waitSeconds is the optional startup wait: if > 0 and no ticket is
// present, alpaca will block here for up to waitSeconds polling for one
// to arrive. This is purely cosmetic — it makes the startup log line
// say "ticket found" rather than "no ticket yet". A value of 0 means
// "don't wait at startup, just use whatever is in the cache when each
// request comes through".
func newNegotiateAuthenticator(waitSeconds int) proxyAuthenticator {
	auth := &negotiateAuthenticator{hasTicket: checkKerberosTicket}
	switch {
	case checkKerberosTicket():
		log.Println("Kerberos ticket found")
	case waitSeconds <= 0:
		log.Println("No Kerberos ticket at startup; will check again " +
			"on each 407 response so a ticket that arrives later " +
			"(e.g. via kinit or Apple SSO) is honoured automatically")
	default:
		log.Printf("No Kerberos ticket found, waiting up to %d seconds...",
			waitSeconds)
		if waitForKerberosTicket(waitSeconds) {
			log.Println("Kerberos ticket found")
		} else {
			log.Println("No Kerberos ticket found after waiting; " +
				"will continue to check on each 407 response")
		}
	}
	return auth
}

// checkKerberosTicket returns true if valid Kerberos credentials exist.
// Uses GSS.framework to check the system credential store, which includes
// tickets managed by Apple SSO and the Ticket Viewer app.
func checkKerberosTicket() bool {
	return C.hasCredential() == 1
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

// applicableTo enforces two policies at picker time:
//
//  1. The proxy host must be non-empty (we cannot generate an SPN
//     without it).
//  2. A valid Kerberos ticket must currently be available. We re-check
//     on every 407 because the user's ticket may have expired or been
//     revoked since alpaca started; if it has, returning false here
//     causes the picker to omit Negotiate and fall through to NTLM /
//     Basic instead of failing the chain on a stale-ticket error.
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
