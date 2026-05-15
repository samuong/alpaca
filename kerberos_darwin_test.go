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

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseSPNAllowlist(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "empty input is permissive (nil)",
			in:   "",
			want: nil,
		},
		{
			name: "single bare suffix is normalised to dot-prefix",
			in:   "corp.example",
			want: []string{".corp.example"},
		},
		{
			name: "single dot-prefixed suffix passed through",
			in:   ".corp.example",
			want: []string{".corp.example"},
		},
		{
			name: "multiple entries with whitespace tolerated",
			in:   " .corp.example , example.test ",
			want: []string{".corp.example", ".example.test"},
		},
		{
			name: "case-folded to lower",
			in:   ".CORP.Example",
			want: []string{".corp.example"},
		},
		{
			name: "literal asterisk means permissive (nil)",
			in:   "*",
			want: nil,
		},
		{
			name: "asterisk among other entries also disables",
			in:   ".corp.example,*",
			want: nil,
		},
		{
			name: "malformed entries dropped (whitespace inside name)",
			in:   "corp .example,.good.example",
			want: []string{".good.example"},
		},
		{
			name: "malformed entries dropped (slashes, wildcards)",
			in:   "*.corp.example,/etc/passwd,.good.example",
			want: []string{".good.example"},
		},
		{
			name: "trailing comma yields no entry",
			in:   ".corp.example,",
			want: []string{".corp.example"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, parseSPNAllowlist(tc.in))
		})
	}
}

func TestAllowedHost(t *testing.T) {
	mk := func(allow string) *negotiateAuthenticator {
		return &negotiateAuthenticator{allowedSuffixes: parseSPNAllowlist(allow)}
	}

	t.Run("empty allowlist permits everything", func(t *testing.T) {
		n := mk("")
		assert.True(t, n.allowedHost("anything.example"))
		assert.True(t, n.allowedHost("evil.example.test"))
	})

	t.Run("dot-prefixed suffix matches subdomain", func(t *testing.T) {
		n := mk(".corp.example")
		assert.True(t, n.allowedHost("proxy.corp.example"))
		assert.True(t, n.allowedHost("a.b.corp.example"))
	})

	t.Run("dot-prefixed suffix matches the bare suffix host as well", func(t *testing.T) {
		// Implementation detail: parseSPNAllowlist normalises both
		// bare and dot-prefixed entries to dot-prefixed form, then
		// allowedHost prepends "." to the host before suffix match.
		// Result: ".corp.example" matches both "corp.example" and
		// "proxy.corp.example" — most users intuit this.
		n := mk(".corp.example")
		assert.True(t, n.allowedHost("corp.example"))
	})

	t.Run("bare suffix entry matches subdomain after normalisation", func(t *testing.T) {
		n := mk("corp.example")
		assert.True(t, n.allowedHost("proxy.corp.example"))
		assert.True(t, n.allowedHost("a.b.corp.example"))
	})

	t.Run("look-alike domains are rejected", func(t *testing.T) {
		n := mk(".corp.example")
		// Critical: must NOT match attacker-controlled look-alikes.
		assert.False(t, n.allowedHost("evil-corp.example"))
		assert.False(t, n.allowedHost("notcorp.example"))
		assert.False(t, n.allowedHost("evilcorp.example"))
	})

	t.Run("case-insensitive host comparison", func(t *testing.T) {
		n := mk(".corp.example")
		assert.True(t, n.allowedHost("PROXY.CORP.EXAMPLE"))
	})

	t.Run("multiple suffixes - any match permits", func(t *testing.T) {
		n := mk(".corp.example,.example.test")
		assert.True(t, n.allowedHost("proxy.corp.example"))
		assert.True(t, n.allowedHost("proxy.example.test"))
		assert.False(t, n.allowedHost("proxy.evil.example"))
	})

	t.Run("explicit asterisk permits everything", func(t *testing.T) {
		n := mk("*")
		assert.True(t, n.allowedHost("anything.example"))
	})
}

func TestApplicableTo(t *testing.T) {
	withTicket := func() bool { return true }
	withoutTicket := func() bool { return false }

	t.Run("explicit permissive allowlist permits any host", func(t *testing.T) {
		// allowedSuffixes==nil + implicitDefault==false is the
		// shape produced by KERBEROS_SPN_ALLOWLIST=*: the user has
		// explicitly opted in to permissive mode.
		n := &negotiateAuthenticator{hasTicket: withTicket}
		assert.True(t, n.applicableTo("any.example"))
	})

	t.Run("blank host is never applicable", func(t *testing.T) {
		n := &negotiateAuthenticator{hasTicket: withTicket}
		assert.False(t, n.applicableTo(""))
	})

	t.Run("allowlist enforced", func(t *testing.T) {
		n := &negotiateAuthenticator{
			allowedSuffixes: parseSPNAllowlist(".corp.example"),
			hasTicket:       withTicket,
		}
		assert.True(t, n.applicableTo("proxy.corp.example"))
		assert.False(t, n.applicableTo("evil.example"))
	})

	t.Run("ticket missing causes silent fall-through", func(t *testing.T) {
		// Re-check on every 407 means an expired/revoked ticket
		// causes Negotiate to opt out of the picker, falling through
		// to NTLM/Basic instead of failing with a stale-ticket error.
		n := &negotiateAuthenticator{hasTicket: withoutTicket}
		assert.False(t, n.applicableTo("proxy.example"))
	})
}

func TestApplicableToLateTicketDoesNotBypassAllowlist(t *testing.T) {
	// Regression test: when alpaca starts with no Kerberos ticket,
	// resolveSPNAllowlist returns implicit=true with allowedSuffixes==nil.
	// If a ticket arrives later, applicableTo MUST NOT treat the empty
	// allowlist as "permissive — any host". It must lazily derive the
	// home-realm allowlist (or refuse) before agreeing to act.

	// hasTicket starts false (no ticket at startup) and flips true
	// later (simulating Apple SSO completing).
	var ticketArrived atomic.Bool
	hasTicket := func() bool { return ticketArrived.Load() }

	// realmFn returns "" until the ticket arrives, then "corp.example".
	realmFn := func() string {
		if !ticketArrived.Load() {
			return ""
		}
		return "corp.example"
	}

	t.Run("no ticket → no Negotiate", func(t *testing.T) {
		n := &negotiateAuthenticator{
			implicitDefault: true,
			hasTicket:       hasTicket,
			realmFn:         realmFn,
		}
		assert.False(t, n.applicableTo("attacker.example"),
			"with no ticket, ANY proxy host must be refused")
	})

	t.Run("ticket arrives → only home realm matches", func(t *testing.T) {
		n := &negotiateAuthenticator{
			implicitDefault: true,
			hasTicket:       hasTicket,
			realmFn:         realmFn,
		}
		ticketArrived.Store(true)

		// Hostile PAC pointing alpaca at attacker.example must be refused
		// even though a ticket now exists, because attacker.example is
		// not in the user's home realm.
		assert.False(t, n.applicableTo("attacker.example"),
			"late-arriving ticket must not unlock arbitrary proxies")

		// Home-realm proxy is now allowed.
		assert.True(t, n.applicableTo("proxy.corp.example"),
			"late-arriving ticket should permit the home-realm proxy "+
				"once the implicit allowlist resolves")

		// implicitDefault should now be false; subsequent calls
		// short-circuit the lazy resolve.
		assert.False(t, n.implicitDefault,
			"implicitDefault should be cleared once allowlist resolves")
		assert.Equal(t, []string{".corp.example"}, n.allowedSuffixes)
	})

	t.Run("ticket arrives but realm not derivable → refuse", func(t *testing.T) {
		// Pathological case: a ticket exists but its principal name
		// isn't in user@REALM form. Refuse rather than fall through
		// to permissive.
		n := &negotiateAuthenticator{
			implicitDefault: true,
			hasTicket:       func() bool { return true },
			realmFn:         func() string { return "" },
		}
		assert.False(t, n.applicableTo("anything.example"))
		assert.True(t, n.implicitDefault, "implicitDefault must remain "+
			"set when realm derivation fails so a future ticket-refresh "+
			"can retry")
	})
}

func TestResolveSPNAllowlist(t *testing.T) {
	t.Run("explicit value", func(t *testing.T) {
		t.Setenv("KERBEROS_SPN_ALLOWLIST", ".corp.example")
		got, implicit, lazy := resolveSPNAllowlist()
		assert.Equal(t, []string{".corp.example"}, got)
		assert.False(t, implicit)
		assert.Nil(t, lazy)
	})

	t.Run("explicit asterisk yields permissive", func(t *testing.T) {
		t.Setenv("KERBEROS_SPN_ALLOWLIST", "*")
		got, implicit, lazy := resolveSPNAllowlist()
		assert.Nil(t, got)
		assert.False(t, implicit,
			"explicit '*' is a deliberate user opt-in, not an implicit default")
		assert.Nil(t, lazy)
	})

	t.Run("unset and no realm yields implicit-pending", func(t *testing.T) {
		t.Setenv("KERBEROS_SPN_ALLOWLIST", "")
		// On a developer machine running this test, defaultKerberosRealm
		// may or may not return a realm depending on whether the
		// developer has a TGT. The deterministic case to assert is:
		// we never return implicit=false with an empty allowlist (i.e.
		// "permissive" without an explicit asterisk).
		got, implicit, _ := resolveSPNAllowlist()
		if len(got) == 0 {
			assert.True(t, implicit,
				"an empty allowlist with no explicit '*' must be "+
					"flagged as implicit so applicableTo() can retry "+
					"the realm derivation later")
		}
	})
}
