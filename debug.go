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

package main

import "log"

// debugEnabled is set from main.go's --debug flag. When true, debugf
// emits "DEBUG: " prefixed log lines that explain alpaca's
// decision-making in detail — which auth methods the picker
// considered for a 407, what the resolved SPN allowlist is, which
// SPN alpaca asked GSS for, and so on. When false, debugf is a
// no-op.
//
// Always-on log lines (`log.Printf` / `log.Println` directly) are
// reserved for events a user troubleshooting a misconfiguration
// needs to see without re-launching alpaca — e.g. "Kerberos SPN
// allowlist excludes …" tells the user exactly why Negotiate
// declined and how to fix it. Anything noisier than that goes
// behind --debug to keep steady-state logs scannable.
var debugEnabled bool

// debugf logs a "DEBUG: " prefixed line iff --debug is set. Uses
// log.Printf under the hood so the line goes through the same sink
// as all other alpaca logs (which means -q correctly suppresses it
// alongside everything else).
func debugf(format string, args ...any) {
	if !debugEnabled {
		return
	}
	log.Printf("DEBUG: "+format, args...)
}
