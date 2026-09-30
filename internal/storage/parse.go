package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// usage is a parsed quota line; sizes in bytes.
type usage struct {
	usedB, softB, hardB int64
	usedF, softF, hardF int64
	grace               string
}

func (u usage) into(q *model.Quota) {
	q.UsedBytes, q.SoftBytes, q.HardBytes = u.usedB, u.softB, u.hardB
	q.UsedFiles, q.SoftFiles, q.HardFiles = u.usedF, u.softF, u.hardF
	q.Grace = u.grace
}

// num parses a quota number: "123", "123*" (over the soft limit), "-" or
// "none" (no limit, 0).
func num(s string) (int64, error) {
	s = strings.TrimSuffix(strings.TrimSpace(s), "*")
	switch strings.ToLower(s) {
	case "", "-", "none", "unlimited":
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}

func isNum(s string) bool {
	_, err := num(s)
	return err == nil && s != "-" && !strings.EqualFold(s, "none")
}

// cleanGrace hides the placeholders tools print when no grace applies.
func cleanGrace(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "-", "none", "0", "[-]", "no":
		return ""
	}
	return s
}

// parseLustre parses "lfs quota -q -u USER MOUNT"; a long filesystem name wraps onto its own line.
func parseLustre(out []byte) (usage, error) {
	f := strings.Fields(string(out))
	// The first field is the filesystem; the rest are the eight values,
	// joined across the wrapped line.
	if len(f) < 9 {
		return usage{}, fmt.Errorf("lfs quota: expected 9 fields, got %d: %q", len(f), strings.TrimSpace(string(out)))
	}
	v := f[1:9]
	var u usage
	var errs [6]error
	u.usedB, errs[0] = num(v[0])
	u.softB, errs[1] = num(v[1])
	u.hardB, errs[2] = num(v[2])
	u.usedF, errs[3] = num(v[4])
	u.softF, errs[4] = num(v[5])
	u.hardF, errs[5] = num(v[6])
	if err := errors.Join(errs[:]...); err != nil {
		return usage{}, fmt.Errorf("lfs quota: %w", err)
	}
	u.usedB, u.softB, u.hardB = u.usedB*1024, u.softB*1024, u.hardB*1024
	u.grace = cleanGrace(v[3])
	if g := cleanGrace(v[7]); g != "" && u.grace == "" {
		u.grace = g
	}
	return u, nil
}

// parseGPFS parses "mmlsquota -u USER -Y --block-size 1K", keeping the first USR row matching names.
func parseGPFS(out []byte, names ...string) (usage, error) {
	var header []string
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(strings.TrimSpace(line), ":")
		if len(f) < 4 {
			continue
		}
		if f[2] == "HEADER" {
			header = f
			continue
		}
		if header == nil {
			continue
		}
		col := func(name string) string {
			for i, h := range header {
				if h == name && i < len(f) {
					return f[i]
				}
			}
			return ""
		}
		if t := col("quotaType"); t != "" && t != "USR" {
			continue
		}
		if len(names) > 0 && !matchesAny(names, col("filesystemName"), col("filesetname")) {
			continue
		}
		var u usage
		var errs [6]error
		u.usedB, errs[0] = num(col("blockUsage"))
		u.softB, errs[1] = num(col("blockQuota"))
		u.hardB, errs[2] = num(col("blockLimit"))
		u.usedF, errs[3] = num(col("filesUsage"))
		u.softF, errs[4] = num(col("filesQuota"))
		u.hardF, errs[5] = num(col("filesLimit"))
		if err := errors.Join(errs[:]...); err != nil {
			return usage{}, fmt.Errorf("mmlsquota: %w", err)
		}
		u.usedB, u.softB, u.hardB = u.usedB*1024, u.softB*1024, u.hardB*1024
		u.grace = cleanGrace(col("blockGrace"))
		if u.grace == "" {
			u.grace = cleanGrace(col("filesGrace"))
		}
		return u, nil
	}
	if header == nil {
		return usage{}, errors.New("mmlsquota: no HEADER row in -Y output")
	}
	return usage{}, fmt.Errorf("mmlsquota: no quota for %s", strings.Join(names, " or "))
}

func matchesAny(names []string, values ...string) bool {
	for _, v := range values {
		v = strings.TrimPrefix(v, "/dev/")
		for _, n := range names {
			if v != "" && strings.TrimPrefix(n, "/dev/") == v {
				return true
			}
		}
	}
	return false
}

// parseBeeGFS parses "beegfs-ctl --getquota --uid USER --csv" (not yet verified on a live BeeGFS).
func parseBeeGFS(out []byte) (usage, error) {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		f := strings.Split(strings.TrimSpace(line), ",")
		if len(f) < 6 || !isNum(f[2]) {
			continue
		}
		var u usage
		var errs [4]error
		u.usedB, errs[0] = num(f[2])
		u.hardB, errs[1] = num(f[3])
		u.usedF, errs[2] = num(f[4])
		u.hardF, errs[3] = num(f[5])
		if err := errors.Join(errs[:]...); err != nil {
			return usage{}, fmt.Errorf("beegfs-ctl: %w", err)
		}
		return u, nil
	}
	return usage{}, fmt.Errorf("beegfs-ctl: no quota row in %q", strings.TrimSpace(string(out)))
}

// errNoQuota means the tool reported no quota for the user.
var errNoQuota = errors.New("no quota set")

// parseQuota parses "quota -w -u USER" (1K blocks). Grace columns are
// blank unless a soft limit is exceeded, so they are detected by content.
// device picks the row when there are several.
func parseQuota(out []byte, device string) (usage, error) {
	text := string(out)
	if strings.Contains(text, ": none") {
		return usage{}, errNoQuota
	}
	var rows [][]string
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 7 || f[0] == "Filesystem" || strings.HasPrefix(line, "Disk quotas") {
			continue
		}
		rows = append(rows, f)
	}
	if len(rows) == 0 {
		return usage{}, errNoQuota
	}
	row := rows[0]
	for _, r := range rows {
		if r[0] == device {
			row = r
		}
	}
	vals := row[1:]
	take := func() string {
		if len(vals) == 0 {
			return ""
		}
		v := vals[0]
		vals = vals[1:]
		return v
	}
	var u usage
	var errs [6]error
	u.usedB, errs[0] = num(take())
	u.softB, errs[1] = num(take())
	u.hardB, errs[2] = num(take())
	if len(vals) > 0 && !isNum(vals[0]) {
		u.grace = cleanGrace(take())
	}
	u.usedF, errs[3] = num(take())
	u.softF, errs[4] = num(take())
	u.hardF, errs[5] = num(take())
	if len(vals) > 0 && u.grace == "" {
		u.grace = cleanGrace(take())
	}
	if err := errors.Join(errs[:]...); err != nil {
		return usage{}, fmt.Errorf("quota: %w", err)
	}
	u.usedB, u.softB, u.hardB = u.usedB*1024, u.softB*1024, u.hardB*1024
	return u, nil
}

// parseJSON parses the "command" backend's JSON format.
func parseJSON(out []byte) (usage, error) {
	var v struct {
		UsedBytes int64  `json:"used_bytes"`
		SoftBytes int64  `json:"soft_bytes"`
		HardBytes int64  `json:"hard_bytes"`
		UsedFiles int64  `json:"used_files"`
		SoftFiles int64  `json:"soft_files"`
		HardFiles int64  `json:"hard_files"`
		Grace     string `json:"grace"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return usage{}, fmt.Errorf("quota command JSON: %w", err)
	}
	return usage{v.UsedBytes, v.SoftBytes, v.HardBytes, v.UsedFiles, v.SoftFiles, v.HardFiles, cleanGrace(v.Grace)}, nil
}
