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

type negotiateAuthenticator struct{}

// newNegotiateAuthenticator checks for a Kerberos ticket and returns a
// negotiateAuthenticator if one is available. If waitSeconds > 0 and no ticket
// is found immediately, it polls every second up to the given timeout.
// Returns nil if no ticket is available.
func newNegotiateAuthenticator(waitSeconds int) proxyAuthenticator {
	if checkKerberosTicket() {
		log.Println("Kerberos ticket found")
		return &negotiateAuthenticator{}
	}
	if waitSeconds <= 0 {
		return nil
	}
	log.Printf("No Kerberos ticket found, waiting up to %d seconds...", waitSeconds)
	if waitForKerberosTicket(waitSeconds) {
		log.Println("Kerberos ticket found")
		return &negotiateAuthenticator{}
	}
	log.Println("No Kerberos ticket found after waiting")
	return nil
}

// checkKerberosTicket returns true if valid Kerberos credentials exist.
// Uses GSS.framework to check the system credential store, which includes
// tickets managed by Apple SSO and the Ticket Viewer app.
func checkKerberosTicket() bool {
	return C.hasCredential() == 1
}

// waitForKerberosTicket polls for a Kerberos ticket every second up to timeout.
func waitForKerberosTicket(timeoutSeconds int) bool {
	deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		if checkKerberosTicket() {
			return true
		}
	}
	return false
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
