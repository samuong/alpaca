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

	t.Run("empty allowlist permits any host when ticket present", func(t *testing.T) {
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
