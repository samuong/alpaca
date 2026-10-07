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

//go:build darwin || linux

package main

/*
#cgo darwin LDFLAGS: -framework GSS
#cgo linux LDFLAGS: -lgssapi_krb5
#ifdef __APPLE__
#include <GSS/GSS.h>
#else
#include <gssapi/gssapi.h>
#endif
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// GSS_SPNEGO_MECHANISM is Apple-only, so spell out the SPNEGO OID
// 1.3.6.1.5.5.2 (RFC 4178).
static gss_OID_desc spnegoMechanism = {6, "\x2b\x06\x01\x05\x05\x02"};

// hasCredential checks if the current user has a valid Kerberos credential
// by attempting to acquire the default credential.
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
        &spnegoMechanism,
        0,                     // no special flags
        0,                     // default lifetime
        GSS_C_NO_CHANNEL_BINDINGS,
        GSS_C_NO_BUFFER,       // no input token (first call)
        NULL,                  // actual mechanism (not needed)
        &outputToken,
        NULL,                  // ret_flags
        NULL                   // time_rec
    );

    *minorStatus = minor;
    gss_release_name(&minor, &targetName);

    if (major != GSS_S_COMPLETE && major != GSS_S_CONTINUE_NEEDED) {
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

// appendStatus appends the human-readable messages for a GSS major
// (GSS_C_GSS_CODE) or mechanism minor (GSS_C_MECH_CODE) status to buf.
static void appendStatus(OM_uint32 code, int type, char *buf, size_t len) {
    OM_uint32 minor, msgCtx = 0;
    gss_buffer_desc msg;
    do {
        if (GSS_ERROR(gss_display_status(&minor, code, type, GSS_C_NO_OID, &msgCtx, &msg))) {
            return;
        }
        size_t used = strlen(buf);
        snprintf(buf + used, len - used, "%s%.*s", used > 0 ? ": " : "",
                 (int)msg.length, (char *)msg.value);
        gss_release_buffer(&minor, &msg);
    } while (msgCtx != 0);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// checkKerberosTicket returns true if valid Kerberos credentials exist in
// the default credential cache.
func checkKerberosTicket() bool {
	return C.hasCredential() == 1
}

// generateSPNEGOToken creates a SPNEGO token for the given proxy host using
// the current user's Kerberos TGT.
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
		return nil, fmt.Errorf("GSS-API error for %s: %s", spn,
			gssStatusString(major, minorStatus))
	}
	if tokenLen == 0 {
		return nil, fmt.Errorf("GSS-API returned an empty token for %s", spn)
	}

	return C.GoBytes(tokenData, C.int(tokenLen)), nil
}

func gssStatusString(major, minor C.OM_uint32) string {
	var buf [1024]C.char
	C.appendStatus(major, C.GSS_C_GSS_CODE, &buf[0], C.size_t(len(buf)))
	if minor != 0 {
		C.appendStatus(minor, C.GSS_C_MECH_CODE, &buf[0], C.size_t(len(buf)))
	}
	return C.GoString(&buf[0])
}
