package logs

import (
	"regexp"
	"strings"
)

// Level is a line's highlight level.
type Level int8

// Highlight levels.
const (
	Plain Level = iota
	Warning
	Error
)

// DefaultErrors and DefaultWarnings are the built-in patterns.
var (
	DefaultErrors = []string{
		`Traceback \(most recent call last\)`,
		`CUDA out of memory|OutOfMemoryError`,
		`oom[-_ ]kill|\bKilled\b`,
		`Segmentation fault|core dumped`,
		`slurmstepd: error|DUE TO TIME LIMIT|CANCELLED AT`,
		`NCCL (WARN|ERROR)`,
		`\bERROR\b|\bError:`,
	}
	DefaultWarnings = []string{`\bWARN(ING)?\b`}
)

// Highlighter classifies lines by case-insensitive patterns.
type Highlighter struct {
	errs, warns []*regexp.Regexp
}

// NewHighlighter compiles the default patterns plus extra ones. It returns
// the patterns that did not compile, which are skipped.
func NewHighlighter(extraErrors, extraWarnings []string) (*Highlighter, []string) {
	h := &Highlighter{}
	var bad []string
	compile := func(list []string) []*regexp.Regexp {
		var out []*regexp.Regexp
		for _, p := range list {
			re, err := regexp.Compile("(?i)" + p)
			if err != nil {
				bad = append(bad, p)
				continue
			}
			out = append(out, re)
		}
		return out
	}
	h.errs = compile(append(append([]string{}, DefaultErrors...), extraErrors...))
	h.warns = compile(append(append([]string{}, DefaultWarnings...), extraWarnings...))
	return h, bad
}

// Level classifies a line.
func (h *Highlighter) Level(line string) Level {
	for _, re := range h.errs {
		if re.MatchString(line) {
			return Error
		}
	}
	for _, re := range h.warns {
		if re.MatchString(line) {
			return Warning
		}
	}
	return Plain
}

// Matcher finds a search query in lines.
type Matcher struct {
	re *regexp.Regexp
}

// regexChars mark a query as a regular expression.
const regexChars = `.*+?()[]{}|^$\`

// NewMatcher builds a smart-case matcher: a literal search unless the
// query contains regex characters, case-insensitive unless it contains an
// upper-case letter.
func NewMatcher(query string) (*Matcher, error) {
	if query == "" {
		return nil, nil
	}
	pattern := query
	if !strings.ContainsAny(query, regexChars) {
		pattern = regexp.QuoteMeta(query)
	}
	if strings.ToLower(query) == query {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return &Matcher{re: re}, nil
}

// Match reports whether line contains the query.
func (m *Matcher) Match(line string) bool { return m != nil && m.re.MatchString(line) }

// Spans returns the byte ranges of matches in line.
func (m *Matcher) Spans(line string) [][]int {
	if m == nil {
		return nil
	}
	return m.re.FindAllStringIndex(line, 20)
}
