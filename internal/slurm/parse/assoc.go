package parse

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/textsafe"
)

// assocRecord is one "Association Records" or "QOS Records" entry.
type assocRecord struct {
	kind, account, user, parent string
	name                        string // QOS name
	tokens                      map[string]string
}

var (
	assocHead = regexp.MustCompile(`^ClusterName=\S+ Account=(\S*) UserName=(\S*) `)
	qosHead   = regexp.MustCompile(`^QOS=([^\s(]+)`)
	parentRe  = regexp.MustCompile(`ParentAccount=([^\s(]*)`)
)

// limitKeys are the "Name=value" tokens of a record that carry limits, in
// the order they are shown, with the unit of their numbers.
var limitKeys = []struct{ key, unit string }{
	{"GrpJobs", ""},
	{"GrpSubmitJobs", ""},
	{"GrpWall", "min"},
	{"GrpTRES", ""},
	{"GrpTRESMins", "min"},
	{"GrpTRESRunMins", "min"},
	{"MaxJobs", ""},
	{"MaxSubmitJobs", ""},
	{"MaxWallPJ", "min"},
	{"MaxTRESPJ", ""},
	{"MaxTRESPN", ""},
	{"MaxTRESMinsPJ", "min"},
	{"MaxTRESPU", ""},
	{"MaxJobsPU", ""},
	{"MaxSubmitJobsPU", ""},
}

// AssocMgr parses "scontrol show assoc_mgr users=USER flags=assoc,qos": the
// limits of user's own associations, of the accounts above them, and of the
// QOS the output lists. Records without a limit are left out. A site that
// hides the data, or has no accounting, yields no scopes and no warning.
func AssocMgr(raw []byte, user string) (scopes []model.LimitScope, warns []model.ParseWarning) {
	w := &warnings{source: "assocmgr"}
	defer func() { warns = w.list }()
	defer w.recover()

	var recs []*assocRecord
	var cur *assocRecord
	lines(raw, w, func(_ int, line string) {
		if !strings.HasPrefix(line, " ") { // a record starts at column 0
			cur = nil
			if m := assocHead.FindStringSubmatch(line); m != nil {
				u, _, _ := strings.Cut(m[2], "(")
				cur = &assocRecord{account: textsafe.Field(m[1]), user: textsafe.Field(u), tokens: map[string]string{}}
				cur.kind = "account"
				if u != "" {
					cur.kind = "user"
				}
				if p := parentRe.FindStringSubmatch(line); p != nil {
					cur.parent = p[1]
				}
				recs = append(recs, cur)
			} else if m := qosHead.FindStringSubmatch(line); m != nil {
				cur = &assocRecord{kind: "qos", name: textsafe.Field(m[1]), tokens: map[string]string{}}
				recs = append(recs, cur)
			}
			return
		}
		if cur == nil {
			return
		}
		for _, tok := range strings.Fields(line) {
			if k, v, ok := strings.Cut(tok, "="); ok {
				if _, seen := cur.tokens[k]; !seen {
					cur.tokens[k] = v
				}
				if k == "ParentAccount" && cur.parent == "" {
					cur.parent, _, _ = strings.Cut(v, "(")
				}
			}
		}
	})

	// The accounts that matter are those of the user's own associations
	// and their parents.
	mine := map[string]bool{}
	for _, r := range recs {
		if r.kind == "user" && r.user == user {
			mine[r.account] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for _, r := range recs {
			if r.kind == "account" && mine[r.account] && r.parent != "" && !mine[r.parent] {
				mine[r.parent], changed = true, true
			}
		}
	}
	for _, r := range recs {
		var name string
		switch {
		case r.kind == "user" && r.user == user:
			name = r.account
		case r.kind == "account" && mine[r.account]:
			name = r.account
		case r.kind == "qos":
			name = r.name
		default:
			continue
		}
		if ls := r.limits(); len(ls) > 0 {
			scopes = append(scopes, model.LimitScope{Kind: r.kind, Name: name, Limits: ls})
		}
	}
	return scopes, w.list
}

// limits lists the limits a record sets.
func (r *assocRecord) limits() []model.Limit {
	var out []model.Limit
	for _, lk := range limitKeys {
		v, ok := r.tokens[lk.key]
		if !ok || v == "" {
			continue
		}
		if strings.HasPrefix(lk.key, "Grp") && strings.Contains(lk.key, "TRES") || strings.HasPrefix(lk.key, "Max") && strings.Contains(lk.key, "TRES") {
			for _, item := range strings.Split(v, ",") {
				res, val, ok := strings.Cut(item, "=")
				if !ok {
					continue
				}
				if l, ok := parseLimit(lk.key+" "+res, lk.unit, res, val); ok {
					out = append(out, l)
				}
			}
			continue
		}
		if l, ok := parseLimit(displayLimitName(lk.key), lk.unit, "", v); ok {
			out = append(out, l)
		}
	}
	return out
}

// displayLimitName drops the per-job suffix Slurm prints: MaxWallPJ is
// "MaxWall".
func displayLimitName(k string) string {
	return strings.TrimSuffix(k, "PJ")
}

// parseLimit reads "64(12)", "64", "N(0)" (no limit) into a Limit. res is
// the TRES name, when there is one: memory is in MB.
func parseLimit(name, unit, res, val string) (model.Limit, bool) {
	maxs, used, hasUsed := strings.Cut(val, "(")
	used = strings.TrimSuffix(used, ")")
	if maxs == "" || maxs == "N" || maxs == "-1" {
		return model.Limit{}, false
	}
	m, err := parseAmount(maxs, res)
	if err != nil {
		return model.Limit{}, false
	}
	l := model.Limit{Name: name, Unit: unit, Max: m, Used: -1}
	if res == "mem" {
		l.Unit = "MB"
	}
	if hasUsed {
		if u, err := parseAmount(used, res); err == nil {
			l.Used = u
		}
	}
	return l, true
}

// parseAmount reads a number, or a memory size such as "64G" for mem.
func parseAmount(s, res string) (float64, error) {
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f, nil
	}
	if res == "mem" {
		mb, err := units.MemMB(s, 0)
		return float64(mb), err
	}
	return 0, fmt.Errorf("not a number: %q", s)
}
