// Copyright 2019 The Alpaca Authors
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

package cancelable

import (
	"io"
)

type Closer struct {
	c io.Closer
}

func NewCloser(c io.Closer) *Closer {
	return &Closer{c: c}
}

func (c *Closer) Cancel() {
	c.c = nil
}

func (c *Closer) Close() error {
	if c.c == nil {
		return nil
	}
	err := c.c.Close()
	c.Cancel()
	return err
}
