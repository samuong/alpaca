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

// Unit tests for the Windows backend. These do not exercise SSPI against a
// real domain; that needs a domain-joined host and is covered by the
// manual smoke test and the integration harness. They confirm the backend
// wires the shared authenticator correctly and that the credential probe
// is safe to call in any environment.

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
	assert.NotNil(t, na.hasTicket, "hasTicket must be wired to the SSPI presence check")
}

func TestCheckKerberosTicketIsSafeToCall(t *testing.T) {
	// The result depends on the host: true on a domain-joined machine
	// with a usable credential, and it may be either on a workgroup CI
	// runner. The unit suite only asserts the probe runs without
	// panicking; real ticket validation is covered by the integration
	// harness and the manual smoke test.
	_ = checkKerberosTicket()
}
