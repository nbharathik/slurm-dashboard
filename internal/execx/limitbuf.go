package execx

import (
	"bytes"
	"sync"
)

// capBuffer keeps up to limit bytes, then discards writes (so the child never blocks) and calls onOverflow once.
type capBuffer struct {
	mu         sync.Mutex
	buf        bytes.Buffer
	limit      int
	overflow   bool
	onOverflow func()
	// squeeze keeps one space of each run, for padded squeue -O output:
	// the limit then counts the data, not the padding.
	squeeze bool
	space   bool // the last byte kept was a space
}

func (c *capBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := len(p)
	if c.overflow {
		return n, nil
	}
	if c.squeeze {
		p = c.squeezed(p)
	}
	if room := c.limit - c.buf.Len(); len(p) > room {
		c.buf.Write(p[:max(room, 0)])
		c.overflow = true
		if c.onOverflow != nil {
			c.onOverflow()
		}
		return n, nil
	}
	c.buf.Write(p)
	return n, nil
}

// squeezed drops spaces that follow a space, across writes.
func (c *capBuffer) squeezed(p []byte) []byte {
	out := make([]byte, 0, len(p))
	for _, b := range p {
		if b == ' ' {
			if c.space {
				continue
			}
			c.space = true
		} else {
			c.space = false
		}
		out = append(out, b)
	}
	return out
}

// Bytes returns the collected bytes without copying them: it is called once
// the command has exited and nothing writes to the buffer any more. Large
// outputs (everyone's jobs on a big cluster) are not held twice.
func (c *capBuffer) Bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Bytes()
}

// Overflowed reports whether writes went past the limit.
func (c *capBuffer) Overflowed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.overflow
}
