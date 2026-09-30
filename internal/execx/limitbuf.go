package execx

import (
	"bytes"
	"sync"
)

// capBuffer collects up to limit bytes. Once the limit is passed it keeps
// accepting (and discarding) writes so the child never blocks on a full
// pipe, records the overflow, and calls onOverflow once.
type capBuffer struct {
	mu         sync.Mutex
	buf        bytes.Buffer
	limit      int
	overflow   bool
	onOverflow func()
}

func (c *capBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.overflow {
		return len(p), nil
	}
	if room := c.limit - c.buf.Len(); len(p) > room {
		c.buf.Write(p[:max(room, 0)])
		c.overflow = true
		if c.onOverflow != nil {
			c.onOverflow()
		}
		return len(p), nil
	}
	return c.buf.Write(p)
}

// Bytes returns a copy of the collected bytes.
func (c *capBuffer) Bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return bytes.Clone(c.buf.Bytes())
}

// Overflowed reports whether writes went past the limit.
func (c *capBuffer) Overflowed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.overflow
}
