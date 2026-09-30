package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// anonymizer consistently replaces user names, accounts, job names, the
// cluster name and the home directory in recorded fixtures.
type anonymizer struct {
	home     string
	users    []string
	accounts []string
	jobs     []string
	cluster  string
	reserved map[string]bool
}

// mappingFile is written next to the fixtures and must never be committed
// (".gitignore" excludes *.mapping.json).
type mappingFile struct {
	Note     string            `json:"note"`
	Users    map[string]string `json:"users"`
	Accounts map[string]string `json:"accounts"`
	Jobs     map[string]string `json:"jobs"`
	Cluster  map[string]string `json:"cluster"`
	Home     map[string]string `json:"home"`
	Kept     []string          `json:"kept_unchanged"`
}

func newAnonymizer(me, home string) *anonymizer {
	an := &anonymizer{home: home, reserved: map[string]bool{}}
	for _, w := range []string{
		"batch", "extern", "interactive", "gpu", "gpus", "cpu", "cpus", "root", "slurm", "none", "null",
		"normal", "debug", "main", "default", "mem", "node", "nodes", "billing", "energy", "pages", "vmem",
		"fs", "disk", "job", "jobs", "user", "users", "account", "partition", "unknown", "all", "wrap",
		"running", "pending", "completed", "failed", "cancelled", "timeout", "high", "low", "long", "short",
		"test", "bash", "sh", "python", "sleep", "echo",
	} {
		an.reserved[w] = true
	}
	an.addUser(me)
	return an
}

// reserve protects names that must stay recognisable (partitions, nodes,
// QOS, states).
func (an *anonymizer) reserve(names ...string) {
	for _, n := range names {
		an.reserved[strings.ToLower(n)] = true
	}
}

func (an *anonymizer) usable(s string) bool {
	s = strings.TrimSpace(s)
	if len([]rune(s)) < 3 || an.reserved[strings.ToLower(s)] {
		return false
	}
	if _, err := strconv.Atoi(s); err == nil {
		return false
	}
	return true
}

func addUnique(list *[]string, s string) {
	if s = strings.TrimSpace(s); s != "" && !slices.Contains(*list, s) {
		*list = append(*list, s)
	}
}

func (an *anonymizer) addUser(u string)    { addUnique(&an.users, u) }
func (an *anonymizer) addAccount(a string) { addUnique(&an.accounts, a) }
func (an *anonymizer) addJob(j string)     { addUnique(&an.jobs, j) }

// pairs returns every replacement, longest original first.
func (an *anonymizer) pairs() ([][2]string, mappingFile) {
	m := mappingFile{
		Note:     "Maps real names to the placeholders used in the recorded fixtures. Do not commit or share this file.",
		Users:    map[string]string{},
		Accounts: map[string]string{},
		Jobs:     map[string]string{},
		Cluster:  map[string]string{},
		Home:     map[string]string{},
		Kept:     []string{},
	}
	var pairs [][2]string
	add := func(kind map[string]string, orig, repl string) {
		kind[orig] = repl
		pairs = append(pairs, [2]string{orig, repl})
	}
	if an.home != "" && an.home != "/" {
		add(m.Home, an.home, "/home/user1")
	}
	if an.cluster != "" && an.usable(an.cluster) {
		add(m.Cluster, an.cluster, "cluster1")
	}
	for i, u := range an.users {
		if an.usable(u) {
			add(m.Users, u, fmt.Sprintf("user%d", i+1))
		} else {
			m.Kept = append(m.Kept, u)
		}
	}
	for i, a := range an.accounts {
		if an.usable(a) && m.Users[a] == "" {
			add(m.Accounts, a, fmt.Sprintf("acct%d", i+1))
		} else if m.Users[a] == "" {
			m.Kept = append(m.Kept, a)
		}
	}
	n := 0
	for _, j := range an.jobs {
		if !an.usable(j) || m.Users[j] != "" || m.Accounts[j] != "" {
			if !an.usable(j) {
				m.Kept = append(m.Kept, j)
			}
			continue
		}
		n++
		add(m.Jobs, j, fmt.Sprintf("job%d", n))
	}
	slices.SortStableFunc(pairs, func(a, b [2]string) int { return len(b[0]) - len(a[0]) })
	return pairs, m
}

// replace substitutes whole-word occurrences: a match must not be glued to
// letters or digits on either side.
func replace(text string, pairs [][2]string) string {
	for _, p := range pairs {
		text = replaceWord(text, p[0], p[1])
	}
	return text
}

func replaceWord(text, orig, repl string) string {
	if orig == "" {
		return text
	}
	var b strings.Builder
	i := 0
	for {
		j := strings.Index(text[i:], orig)
		if j < 0 {
			b.WriteString(text[i:])
			return b.String()
		}
		j += i
		end := j + len(orig)
		if isWordRune(lastRune(text[:j])) || isWordRune(firstRune(text[end:])) {
			b.WriteString(text[i : j+1])
			i = j + 1
			continue
		}
		b.WriteString(text[i:j])
		b.WriteString(repl)
		i = end
	}
}

func isWordRune(r rune) bool { return r != 0 && (unicode.IsLetter(r) || unicode.IsDigit(r)) }

func lastRune(s string) rune {
	r := []rune(s)
	if len(r) == 0 {
		return 0
	}
	return r[len(r)-1]
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

// apply rewrites every fixture file in dir and writes the mapping file.
func (an *anonymizer) apply(dir string) (string, error) {
	pairs, mapping := an.pairs()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		name := e.Name()
		fixture := strings.HasSuffix(name, ".txt") || strings.HasSuffix(name, ".json")
		if e.IsDir() || strings.HasSuffix(name, ".mapping.json") || !fixture {
			continue
		}
		if !e.Type().IsRegular() {
			return "", fmt.Errorf("refusing to anonymize non-regular file %s", name)
		}
		path := filepath.Join(dir, name)
		b, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		//nolint:gosec // a file sdash itself wrote under the --out directory the user chose
		if err := os.WriteFile(path, []byte(replace(string(b), pairs)), 0o600); err != nil {
			return "", err
		}
	}
	out, err := json.MarshalIndent(mapping, "", "  ")
	if err != nil {
		return "", err
	}
	mapPath := filepath.Join(dir, "anonymize.mapping.json")
	return mapPath, os.WriteFile(mapPath, append(out, '\n'), 0o600)
}
