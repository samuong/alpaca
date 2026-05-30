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

package main

import "log"

// debugEnabled is currently unwired. The original `--debug` CLI flag was
// withdrawn before merge to avoid shipping a flag that would have to be
// removed when alpaca migrates its logging to log/slog and gains a proper
// log-level surface (see samuong/alpaca#178). The scaffold is retained
// so the migration can re-wire it to an `ALPACA_LOG_LEVEL` env var
// without re-introducing every debugf callsite.
//
//nolint:unused // reserved for the slog migration; see PR #178 discussion
var debugEnabled bool

// debugf logs a "DEBUG: " prefixed line iff debugEnabled is set. Uses
// log.Printf under the hood so the line goes through the same sink as
// all other alpaca logs (which means -q correctly suppresses it
// alongside everything else).
//
//nolint:unused // reserved for the slog migration; see PR #178 discussion
func debugf(format string, args ...any) {
	if !debugEnabled {
		return
	}
	log.Printf("DEBUG: "+format, args...)
}
