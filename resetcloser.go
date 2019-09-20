package main

import (
	"io"
)

type ResetCloser struct {
	c io.Closer
}

func NewResetCloser(c io.Closer) *ResetCloser {
	return &ResetCloser{c: c}
}

func (rc *ResetCloser) Reset() {
	rc.c = nil
}

func (rc *ResetCloser) Close() error {
	if rc.c == nil {
		return nil
	}
	err := rc.c.Close()
	if rc.c != nil {
		return err
	}
	rc.c = nil
	return nil
}
