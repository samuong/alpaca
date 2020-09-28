// Copyright 2019, 2020 The Alpaca Authors
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

// Package cancelable provides a cancelable version of io.Closer.
//
// This is intended to be used by functions where the responsibility for closing
// a resource sometimes belongs to the function itself (e.g. early returns due
// to error handling), and sometimes belongs to the caller.
package cancelable

import (
	"io"
)

// Closer is an io.Closer that can be cancelled.
type Closer struct {
	c io.Closer
}

// NewCloser returns a cancelable.Closer that wraps an io.Closer.
func NewCloser(c io.Closer) *Closer {
	return &Closer{c: c}
}

// Cancel turns any future calls to Close into a no-op.
func (c *Closer) Cancel() {
	c.c = nil
}

// Close calls the Close function on the io.Closer (if it has not been cancelled
// or already closed). It is safe to call this multiple times.
func (c *Closer) Close() error {
	if c.c == nil {
		return nil
	}
	err := c.c.Close()
	c.Cancel()
	return err
}
