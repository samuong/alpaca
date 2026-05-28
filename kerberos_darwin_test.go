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

//go:build darwin

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNegotiateApplicableTo(t *testing.T) {
	withTicket := func() bool { return true }
	withoutTicket := func() bool { return false }

	t.Run("ticket present permits any non-empty host", func(t *testing.T) {
		// The picker (*authChain.allowedHost) enforces host policy
		// across all auth methods; Negotiate's applicableTo only
		// checks runtime preconditions (ticket presence + host
		// resolvability). Both cross-realm and home-realm hosts pass
		// applicableTo as long as a ticket exists.
		n := &negotiateAuthenticator{hasTicket: withTicket}
		assert.True(t, n.applicableTo("proxy.corp.example"))
		assert.True(t, n.applicableTo("proxy.any-other.example.net"))
	})

	t.Run("blank host is never applicable", func(t *testing.T) {
		n := &negotiateAuthenticator{hasTicket: withTicket}
		assert.False(t, n.applicableTo(""))
	})

	t.Run("ticket missing causes silent fall-through", func(t *testing.T) {
		// Re-check on every 407 means an expired or revoked ticket
		// causes Negotiate to opt out of the picker, falling through
		// to NTLM/Basic instead of failing the chain on a stale
		// ticket error.
		n := &negotiateAuthenticator{hasTicket: withoutTicket}
		assert.False(t, n.applicableTo("proxy.example"))
	})
}
