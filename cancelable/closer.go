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
