package cancelable

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testCloser struct {
	closed bool
}

func (tc *testCloser) Close() error {
	tc.closed = true
	return nil
}

func TestUncanceledCloser(t *testing.T) {
	var tc testCloser
	require.False(t, tc.closed)
	c := NewCloser(&tc)
	c.Close()
	assert.True(t, tc.closed)
}

func TestCanceledCloser(t *testing.T) {
	var tc testCloser
	require.False(t, tc.closed)
	c := NewCloser(&tc)
	c.Cancel()
	c.Close()
	assert.False(t, tc.closed)
}
