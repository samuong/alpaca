package main

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

type testCloser struct {
	closed bool
}

func (tc *testCloser) Close() error {
	tc.closed = true
	return nil
}

func TestResetCloser(t *testing.T) {
	var tc testCloser
	assert.False(t, tc.closed)
	func() {
		mc := NewResetCloser(&tc)
		defer mc.Close()
	}()
	assert.True(t, tc.closed)
}

func TestResetCloserReset(t *testing.T) {
	var tc testCloser
	assert.False(t, tc.closed)
	func() {
		mc := NewResetCloser(&tc)
		mc.Reset()
		defer mc.Close()
	}()
	assert.False(t, tc.closed)
}

func TestTwoResetClosers(t *testing.T) {
	var tc1, tc2 testCloser
	assert.False(t, tc1.closed)
	assert.False(t, tc2.closed)
	func() {
		mc := NewResetCloser(&tc1)
		defer mc.Close()
		mc = NewResetCloser(&tc2)
		defer mc.Close()
		mc.Reset()
	}()
	assert.True(t, tc1.closed)
	assert.False(t, tc2.closed)
}
