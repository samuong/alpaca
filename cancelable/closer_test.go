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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testCloser struct {
	closed bool
	err    error
}

func (tc *testCloser) Close() error {
	tc.closed = true
	return tc.err
}

func TestUncanceledCloser(t *testing.T) {
	var tc testCloser
	require.False(t, tc.closed)
	c := NewCloser(&tc)
	assert.Nil(t, c.Close())
	assert.True(t, tc.closed)
}

func TestCanceledCloser(t *testing.T) {
	var tc testCloser
	require.False(t, tc.closed)
	c := NewCloser(&tc)
	c.Cancel()
	assert.Nil(t, c.Close())
	assert.False(t, tc.closed)
}

func TestCloserPropagatesError(t *testing.T) {
	var tc testCloser
	require.False(t, tc.closed)
	tc.err = errors.New("super serious error")
	c := NewCloser(&tc)
	// The first call to c.Close() propagates the error from the testCloser.
	assert.Equal(t, "super serious error", c.Close().Error())
	assert.True(t, tc.closed)
	// The second call doesn't go to the testCloser, so we don't get a second error.
	assert.Nil(t, c.Close())
}
