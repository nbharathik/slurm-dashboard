package config

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2/unstable"
)

// pos is a 1-based line and column in the config file.
type pos struct{ Line, Col int }

// expr is one top-level TOML expression: a key/value, a [table] or an
// [[array table]] header.
type expr struct {
	Line   int
	Header bool
	Key    string // "refresh", "notify.on", "storage[2]"
}

// layout records where keys and expressions are in a config file, so
// validation errors can name their line and type errors can be isolated to
// one expression.
type layout struct {
	keys  map[string]pos // "refresh.myjobs", "storage[2].path", "storage[2]" (header)
	exprs []expr         // in file order
}

// indexLayout walks the file once with go-toml's parser. It returns
// whatever it indexed before a syntax error; syntax errors themselves are
// reported by the decoder.
func indexLayout(data []byte) layout {
	l := layout{keys: map[string]pos{}}
	var p unstable.Parser
	p.Reset(data)

	var table string
	arrays := map[string]int{}
	for p.NextExpression() {
		e := p.Expression()
		switch e.Kind {
		case unstable.Table, unstable.ArrayTable:
			name, at := keyPath(&p, e.Key())
			if e.Kind == unstable.ArrayTable {
				arrays[name]++
				name += "[" + strconv.Itoa(arrays[name]) + "]"
			}
			table = name
			l.keys[name] = at
			l.exprs = append(l.exprs, expr{Line: at.Line, Header: true, Key: name})
		case unstable.KeyValue:
			name, at := keyPath(&p, e.Key())
			if table != "" {
				name = table + "." + name
			}
			l.keys[name] = at
			l.exprs = append(l.exprs, expr{Line: at.Line, Key: name})
		}
	}
	return l
}

// keyPath joins a dotted key and returns the position of its first part.
func keyPath(p *unstable.Parser, it unstable.Iterator) (string, pos) {
	var parts []string
	var at pos
	for it.Next() {
		n := it.Node()
		if at.Line == 0 {
			s := p.Shape(n.Raw)
			at = pos{Line: s.Start.Line, Col: s.Start.Column}
		}
		parts = append(parts, string(n.Data))
	}
	return strings.Join(parts, "."), at
}

// lookup returns the position of key, falling back to its closest
// recorded parent (for example the [[storage]] header of a missing field).
func (l layout) lookup(key string) pos {
	for k := key; k != ""; {
		if p, ok := l.keys[k]; ok {
			return p
		}
		i := strings.LastIndexByte(k, '.')
		if i < 0 {
			break
		}
		k = k[:i]
	}
	return pos{}
}

// exprAt returns the index of the expression that contains line.
func (l layout) exprAt(line int) (int, bool) {
	idx := -1
	for i, e := range l.exprs {
		if e.Line > line {
			break
		}
		idx = i
	}
	return idx, idx >= 0
}

// blankExpr overwrites expression i, up to the line before the next
// expression, with spaces. Newlines are kept so every other line and
// column stays where it was.
func (l layout) blankExpr(data []byte, i int) {
	start := lineOffset(data, l.exprs[i].Line)
	end := len(data)
	if i+1 < len(l.exprs) {
		end = lineOffset(data, l.exprs[i+1].Line)
	}
	for j := start; j < end; j++ {
		if data[j] != '\n' && data[j] != '\r' {
			data[j] = ' '
		}
	}
}

// lineOffset returns the byte offset where 1-based line starts.
func lineOffset(data []byte, line int) int {
	off := 0
	for l := 1; l < line; l++ {
		i := bytes.IndexByte(data[off:], '\n')
		if i < 0 {
			return len(data)
		}
		off += i + 1
	}
	return off
}
