// Copyright 2021 The Alpaca Authors
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

import (
	"fmt"
	"sync"
	"time"
)

// This duration was chosen to match Chrome's behaviour (see "Evaluating proxy lists" in
// https://crsrc.org/net/docs/proxy.md).
const maxAge = 5 * time.Minute

type blocklist struct {
	entries []string             // Slice of entries, ordered by expiry time
	expiry  map[string]time.Time // Map containing the expiry time for each entry
	now     func() time.Time
	mux     sync.Mutex
}

func newBlocklist() *blocklist {
	return &blocklist{
		entries: []string{},
		expiry:  map[string]time.Time{},
		now:     time.Now,
	}
}

func (b *blocklist) add(entry string) {
	b.mux.Lock()
	defer b.mux.Unlock()
	b.sweep()
	if _, ok := b.expiry[entry]; ok {
		// Ignore duplicate entries. An entry can only have a single expiry time, so it
		// shouldn't be added to the `entries` slice twice. Otherwise we'll have problems
		// deleting it later.
		return
	}
	b.expiry[entry] = b.now().Add(maxAge)
	b.entries = append(b.entries, entry)
}

func (b *blocklist) contains(entry string) bool {
	b.mux.Lock()
	defer b.mux.Unlock()
	b.sweep()
	_, ok := b.expiry[entry]
	return ok
}

func (b *blocklist) sweep() {
	// Delete any stale entries from both the slice and the map. This function is *not*
	// reentrant; `mux` should be locked before calling this function!
	count := 0
	for _, entry := range b.entries {
		expiry, ok := b.expiry[entry]
		if !ok {
			// This should never happen.
			panic(fmt.Sprintf("blocklist contains entry %q without expiry time", entry))
		}
		if b.now().Before(expiry) {
			break
		}
		delete(b.expiry, entry)
		count++
	}
	b.entries = b.entries[count:]
}
