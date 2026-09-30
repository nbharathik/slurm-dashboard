// Package units parses Slurm's small value formats (durations, times, memory, job IDs, hosts, GRES, states) without panicking.
package units

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DurKind says what a duration string meant.
type DurKind int

const (
	// DurValid is an ordinary duration.
	DurValid DurKind = iota
	// DurUnlimited is UNLIMITED, INFINITE or Partition_Limit.
	DurUnlimited
	// DurUnknown is INVALID, NOT_SET, N/A, NONE or empty.
	DurUnknown
)

// ParseDuration parses Slurm durations (MM:SS, HH:MM:SS, D-HH[:MM[:SS]], optional .mmm, bare minutes).
func ParseDuration(s string) (time.Duration, DurKind, error) {
	s = strings.TrimSpace(s)
	switch strings.ToUpper(s) {
	case "UNLIMITED", "INFINITE", "PARTITION_LIMIT":
		return 0, DurUnlimited, nil
	case "", "INVALID", "NOT_SET", "N/A", "NONE", "(NULL)":
		return 0, DurUnknown, nil
	}

	var days int64
	rest := s
	if d, r, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.ParseInt(d, 10, 64)
		if err != nil || n < 0 {
			return 0, DurUnknown, fmt.Errorf("duration %q: bad day count", s)
		}
		days, rest = n, r
	}

	var frac time.Duration
	if main, f, ok := strings.Cut(rest, "."); ok {
		if f == "" || len(f) > 9 {
			return 0, DurUnknown, fmt.Errorf("duration %q: bad fraction", s)
		}
		n, err := strconv.ParseInt(f, 10, 64)
		if err != nil || n < 0 {
			return 0, DurUnknown, fmt.Errorf("duration %q: bad fraction", s)
		}
		for i := len(f); i < 9; i++ {
			n *= 10
		}
		frac, rest = time.Duration(n), main
	}

	parts := strings.Split(rest, ":")
	nums := make([]int64, len(parts))
	for i, p := range parts {
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil || n < 0 {
			return 0, DurUnknown, fmt.Errorf("duration %q: bad number %q", s, p)
		}
		nums[i] = n
	}

	var h, m, sec int64
	hasDays := days > 0 || strings.Contains(s, "-")
	switch {
	case hasDays && len(nums) == 1: // D-HH
		h = nums[0]
	case hasDays && len(nums) == 2: // D-HH:MM
		h, m = nums[0], nums[1]
	case len(nums) == 3: // [D-]HH:MM:SS
		h, m, sec = nums[0], nums[1], nums[2]
	case len(nums) == 2: // MM:SS
		m, sec = nums[0], nums[1]
	case len(nums) == 1 && frac == 0: // minutes
		m = nums[0]
	case len(nums) == 1: // SS.mmm
		sec = nums[0]
	default:
		return 0, DurUnknown, fmt.Errorf("duration %q: unrecognised form", s)
	}
	total := time.Duration(days)*24*time.Hour + time.Duration(h)*time.Hour +
		time.Duration(m)*time.Minute + time.Duration(sec)*time.Second + frac
	return total, DurValid, nil
}

// ParseLimit parses a time limit. It returns nil for unlimited or unknown
// limits.
func ParseLimit(s string) (*time.Duration, error) {
	d, kind, err := ParseDuration(s)
	if err != nil || kind != DurValid {
		return nil, err
	}
	return &d, nil
}

// ParseSeconds parses a whole number of seconds (ElapsedRaw).
func ParseSeconds(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("seconds %q: not a non-negative integer", s)
	}
	return time.Duration(n) * time.Second, nil
}

// ParseMinutesLimit parses a limit in whole minutes (TimelimitRaw). Empty,
// UNLIMITED and similar return nil.
func ParseMinutesLimit(s string) (*time.Duration, error) {
	s = strings.TrimSpace(s)
	if _, kind, _ := ParseDuration(s); kind != DurValid || s == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return nil, fmt.Errorf("minutes %q: not a non-negative integer", s)
	}
	d := time.Duration(n) * time.Minute
	return &d, nil
}

// FormatDuration renders d the way Slurm does: [D-]HH:MM:SS, or MM:SS
// under an hour.
func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	secs := int64(d / time.Second)
	days := secs / 86400
	h := secs % 86400 / 3600
	m := secs % 3600 / 60
	s := secs % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%d-%02d:%02d:%02d", days, h, m, s)
	case h > 0:
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

// FormatLimit renders a limit compactly, dropping zero parts: 24h, 2d,
// 30m, 1h30m.
func FormatLimit(d time.Duration) string {
	switch {
	case d > 0 && d%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	case d >= time.Hour && d < 100*time.Hour && d%time.Hour == 0:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	case d >= time.Minute && d < time.Hour && d%time.Minute == 0:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return FormatShort(d)
}

// FormatShort renders d compactly for narrow columns: 45s, 12m, 3h05m,
// 12h, 2d03h, 2d.
func FormatShort(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		if m := int(d % time.Hour / time.Minute); m > 0 {
			return fmt.Sprintf("%dh%02dm", int(d/time.Hour), m)
		}
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	if h := int(d % (24 * time.Hour) / time.Hour); h > 0 {
		return fmt.Sprintf("%dd%02dh", int(d/(24*time.Hour)), h)
	}
	return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
}
