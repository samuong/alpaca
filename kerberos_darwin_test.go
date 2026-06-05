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

// Unit tests for the macOS backend. These do not exercise GSS.framework
// against a real KDC; that needs a Kerberos credential and is covered by the
// integration fixture (kerberos_darwin_integration_test.go). They confirm the
// backend wires the shared authenticator correctly and that the credential
// probe is safe to call in any environment, mirroring the Windows backend's
// unit tests.

//go:build darwin

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewNegotiateAuthenticatorWiring(t *testing.T) {
	auth := newNegotiateAuthenticator()
	require.NotNil(t, auth,
		"newNegotiateAuthenticator must return a value even when no ticket "+
			"is present at startup, so a credential that arrives later is honoured")

	na, ok := auth.(*negotiateAuthenticator)
	require.True(t, ok, "expected *negotiateAuthenticator, got %T", auth)
	assert.Equal(t, "Negotiate", na.scheme())
	assert.NotNil(t, na.hasTicket, "hasTicket must be wired to the GSS presence check")
}

func TestCheckKerberosTicketIsSafeToCall(t *testing.T) {
	// The result depends on the host: true when the user has a usable
	// credential in the system cache (Apple SSO, Ticket Viewer, kinit),
	// false otherwise. The unit suite only asserts the probe runs without
	// panicking; real ticket validation is covered by the integration
	// fixture.
	_ = checkKerberosTicket()
}
