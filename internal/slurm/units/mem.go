package units

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParseMemMB parses a Slurm memory size and returns megabytes. Suffixes
// K, M, G, T and P are 1024-based; a bare number is MB; "0" means "all of
// the node's memory". A trailing "n" (per node) or "c" (per CPU) from old
// ReqMem values is reported through perCPU.
func ParseMemMB(s string) (mb float64, perCPU bool, err error) {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "", "n/a", "(null)", "unknown":
		return 0, false, nil
	}
	switch last := s[len(s)-1]; last {
	case 'n':
		s = s[:len(s)-1]
	case 'c':
		s, perCPU = s[:len(s)-1], true
	}
	if s == "" {
		return 0, false, fmt.Errorf("memory %q: missing number", s)
	}
	mult, num := 1.0, s
	switch s[len(s)-1] {
	case 'K', 'k':
		mult, num = 1.0/1024, s[:len(s)-1]
	case 'M', 'm':
		num = s[:len(s)-1]
	case 'G', 'g':
		mult, num = 1024, s[:len(s)-1]
	case 'T', 't':
		mult, num = 1024*1024, s[:len(s)-1]
	case 'P', 'p':
		mult, num = 1024*1024*1024, s[:len(s)-1]
	}
	v, err := strconv.ParseFloat(num, 64)
	if err != nil || v < 0 || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, false, fmt.Errorf("memory %q: bad number", s)
	}
	return v * mult, perCPU, nil
}

// MemMB parses like ParseMemMB and rounds to whole megabytes; a per-CPU
// value is multiplied by cpus.
func MemMB(s string, cpus int) (int64, error) {
	mb, perCPU, err := ParseMemMB(s)
	if err != nil {
		return 0, err
	}
	if perCPU && cpus > 0 {
		mb *= float64(cpus)
	}
	return int64(math.Round(mb)), nil
}

// FormatMB renders megabytes with a 1024-based suffix (M, G, T), matching
// Slurm's style: 900M, 22.7G, 1.2T.
func FormatMB(mb float64) string {
	switch {
	case mb >= 1024*1024:
		return trimZero(mb/(1024*1024)) + "T"
	case mb >= 1024:
		return trimZero(mb/1024) + "G"
	}
	return strconv.FormatFloat(math.Round(mb), 'f', -1, 64) + "M"
}

// FormatBytes renders a byte count with a 1024-based suffix.
func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return strconv.FormatInt(b, 10) + "B"
	}
	v := float64(b)
	for _, s := range []string{"K", "M", "G", "T", "P", "E"} {
		v /= unit
		if v < unit || s == "E" {
			return trimZero(v) + s
		}
	}
	return ""
}

func trimZero(v float64) string {
	if v >= 100 {
		return strconv.FormatFloat(math.Round(v), 'f', 0, 64)
	}
	s := strconv.FormatFloat(v, 'f', 1, 64)
	return strings.TrimSuffix(s, ".0")
}

// FormatCount renders a file count with a 1000-based suffix: 950, 12k,
// 1.5M.
func FormatCount(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return trimZero(float64(n)/1e9) + "G"
	case n >= 1_000_000:
		return trimZero(float64(n)/1e6) + "M"
	case n >= 10_000:
		return trimZero(float64(n)/1e3) + "k"
	}
	return strconv.FormatInt(n, 10)
}
