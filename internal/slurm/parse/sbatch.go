package parse

import (
	"errors"
	"path"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/textsafe"
)

// SbatchOpt is one sbatch option: Name is the long name without "--"
// ("partition" for -p), Value is empty for a flag.
type SbatchOpt struct {
	Name  string
	Value string
	Flag  bool
}

// String renders the option the way sdash passes it: --name=value.
func (o SbatchOpt) String() string {
	if o.Flag {
		return "--" + o.Name
	}
	return "--" + o.Name + "=" + o.Value
}

// SbatchLine is an sbatch command line read without a shell: the options
// read with certainty, the words that could not be (Unread), the script
// path ("" when the script came on stdin or from --wrap) and the script's
// own arguments.
type SbatchLine struct {
	Opts       []SbatchOpt
	Unread     []string
	Script     string
	ScriptArgs []string
}

// sbatchShort maps short options that take a value to their long names.
var sbatchShort = map[byte]string{
	'A': "account", 'a': "array", 'b': "begin", 'C': "constraint", 'c': "cpus-per-task",
	'D': "chdir", 'd': "dependency", 'e': "error", 'F': "nodefile", 'G': "gpus",
	'i': "input", 'J': "job-name", 'L': "licenses", 'M': "clusters", 'm': "distribution",
	'N': "nodes", 'n': "ntasks", 'o': "output", 'p': "partition", 'q': "qos",
	'S': "core-spec", 't': "time", 'w': "nodelist", 'x': "exclude",
}

// sbatchShortFlags are short options without a value.
var sbatchShortFlags = map[byte]string{
	'H': "hold", 'h': "help", 'I': "immediate", 'k': "no-kill", 'O': "overcommit",
	'Q': "quiet", 's': "oversubscribe", 'v': "verbose", 'V': "version", 'W': "wait",
}

// sbatchLongFlags are long options that take no value, or take one only
// as --name=value. Every other long option takes a value.
var sbatchLongFlags = map[string]bool{
	"hold": true, "help": true, "immediate": true, "no-kill": true, "overcommit": true,
	"quiet": true, "oversubscribe": true, "verbose": true, "version": true, "wait": true,
	"requeue": true, "no-requeue": true, "parsable": true, "test-only": true,
	"contiguous": true, "exclusive": true, "spread-job": true, "use-min-nodes": true,
	"ignore-pbs": true, "reboot": true, "get-user-env": true, "usage": true, "x11": true,
	"nice": true,
}

// ErrNotSbatch means a submit line does not start with sbatch.
var ErrNotSbatch = errors.New("not an sbatch command line")

// freeText are options whose value may contain spaces.
var freeText = map[string]bool{
	"job-name": true, "output": true, "error": true, "input": true, "comment": true,
	"wrap": true, "chdir": true, "workdir": true, "export": true, "export-file": true,
	"nodefile": true, "container": true, "extra": true, "bb": true, "bbf": true,
}

// SubmitLine reads sacct's SubmitLine field (Slurm 23.02 and later), for
// example `sbatch -p gpu --mem 32G train.sh --epochs 3`.
//
// Slurm stores the words joined by spaces without their quotes, so a
// value that held a space looks like several words. Options whose value
// is one word by nature (partition, time, memory, ...) are always read.
// A free-text value (job name, output, comment, ...) is trusted only when
// the next word is another option or the last word, or, for the job name,
// when it equals jobName, the name Slurm recorded; otherwise that option
// and every word after it go to Unread. --wrap ends the line: the stored
// script holds the wrapped command.
func SubmitLine(line, jobName string) (SbatchLine, error) {
	words, err := SplitWords(line)
	if err != nil {
		return SbatchLine{}, err
	}
	if len(words) == 0 || path.Base(words[0]) != "sbatch" {
		return SbatchLine{}, ErrNotSbatch
	}
	words = words[1:]
	toks, rest := sbatchToks(words)
	var out SbatchLine
	for _, t := range toks {
		if t.opt.Name == "wrap" {
			return out, nil
		}
		if freeText[t.opt.Name] && !t.opt.Flag {
			sure := t.end >= len(words)-1 || strings.HasPrefix(words[t.end], "-") ||
				(t.opt.Name == "job-name" && jobName != "" && t.opt.Value == jobName)
			if !sure {
				out.Unread = words[t.start:]
				return out, nil
			}
		}
		out.Opts = append(out.Opts, t.opt)
	}
	if len(rest) > 0 {
		out.Script, out.ScriptArgs = rest[0], rest[1:]
	}
	return out, nil
}

// ScriptDirectives reads the #SBATCH lines of a batch script. Like sbatch,
// it stops at the first line that is neither blank nor a comment.
func ScriptDirectives(script string) []SbatchOpt {
	var out []SbatchOpt
	for i, line := range strings.Split(script, "\n") {
		t := strings.TrimSpace(line)
		if i == 0 && strings.HasPrefix(t, "#!") {
			continue
		}
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, "#") {
			break
		}
		rest, ok := strings.CutPrefix(t, "#SBATCH")
		if !ok || (rest != "" && rest[0] != ' ' && rest[0] != '\t') {
			continue
		}
		if c := strings.Index(rest, " #"); c >= 0 {
			rest = rest[:c] // a trailing comment
		}
		words, err := SplitWords(rest)
		if err != nil {
			continue
		}
		opts, _ := sbatchOpts(words)
		out = append(out, opts...)
	}
	return out
}

// Last returns the value of the last option called name, as sbatch lets a
// later option win.
func Last(opts []SbatchOpt, name string) (string, bool) {
	val, ok := "", false
	for _, o := range opts {
		if o.Name == name {
			val, ok = o.Value, true
		}
	}
	return val, ok
}

// sbatchTok is one option and the words it spans, [start, end).
type sbatchTok struct {
	opt        SbatchOpt
	start, end int
}

// sbatchOpts reads options up to the first word that is not one.
func sbatchOpts(words []string) (opts []SbatchOpt, rest []string) {
	toks, rest := sbatchToks(words)
	for _, t := range toks {
		opts = append(opts, t.opt)
	}
	return opts, rest
}

func sbatchToks(words []string) (toks []sbatchTok, rest []string) {
	for i := 0; i < len(words); i++ {
		w, start := words[i], i
		add := func(o SbatchOpt) { toks = append(toks, sbatchTok{o, start, i + 1}) }
		next := func() string {
			if i+1 < len(words) {
				i++
				return words[i]
			}
			return ""
		}
		switch {
		case w == "--":
			return toks, words[i+1:]
		case strings.HasPrefix(w, "--"):
			name, val, hasVal := strings.Cut(w[2:], "=")
			switch {
			case hasVal:
				add(SbatchOpt{Name: name, Value: val})
			case sbatchLongFlags[name]:
				add(SbatchOpt{Name: name, Flag: true})
			default:
				v := next()
				add(SbatchOpt{Name: name, Value: v})
			}
		case len(w) >= 2 && w[0] == '-':
			if allShortFlags(w[1:]) {
				for j := 1; j < len(w); j++ {
					add(SbatchOpt{Name: sbatchShortFlags[w[j]], Flag: true})
				}
				continue
			}
			name := sbatchShort[w[1]]
			if name == "" {
				name = "-" + w[1:2] // unknown: keep the letter so it is shown, never carried
			}
			v := w[2:]
			if v == "" {
				v = next()
			}
			add(SbatchOpt{Name: name, Value: strings.TrimPrefix(v, "=")})
		default:
			return toks, words[i:]
		}
	}
	return toks, nil
}

func allShortFlags(s string) bool {
	for i := 0; i < len(s); i++ {
		if sbatchShortFlags[s[i]] == "" {
			return false
		}
	}
	return s != ""
}

// SplitWords splits a command line into words the way a POSIX shell would
// without expanding anything: blanks separate words, single quotes are
// literal, double quotes allow \" \\ \$ and \`, and a backslash outside
// quotes escapes the next character.
func SplitWords(s string) ([]string, error) {
	var (
		out   []string
		cur   strings.Builder
		inTok bool
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n':
			if inTok {
				out = append(out, cur.String())
				cur.Reset()
				inTok = false
			}
		case c == '\'':
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				return nil, errors.New("unterminated single quote")
			}
			cur.WriteString(s[i+1 : i+1+end])
			i += end + 1
			inTok = true
		case c == '"':
			j := i + 1
			for ; j < len(s) && s[j] != '"'; j++ {
				if s[j] == '\\' && j+1 < len(s) && strings.IndexByte("\"\\$`", s[j+1]) >= 0 {
					j++
				}
				cur.WriteByte(s[j])
			}
			if j >= len(s) {
				return nil, errors.New("unterminated double quote")
			}
			i = j
			inTok = true
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			inTok = true
		default:
			cur.WriteByte(c)
			inTok = true
		}
	}
	if inTok {
		out = append(out, cur.String())
	}
	return out, nil
}

// SubmitRecord reads "sacct -n -P -X -o SubmitLine,WorkDir" for one job.
// Both fields are cleaned for display; an empty line means unknown.
func SubmitRecord(raw []byte) (line, workDir string) {
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		f := strings.Split(strings.TrimRight(l, "\r"), Sep)
		line = textsafe.Field(f[0])
		if len(f) > 1 {
			workDir = textsafe.Field(f[1])
		}
		return line, workDir
	}
	return "", ""
}
