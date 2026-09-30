package units

import (
	"fmt"
	"strings"
	"time"
)

// TimeLayout is the format SLURM_TIME_FORMAT=standard produces.
const TimeLayout = "2006-01-02T15:04:05"

// ParseTimestamp parses a Slurm timestamp in loc (nil means time.Local).
// N/A, Unknown, None, (null) and empty yield the zero time.
func ParseTimestamp(s string, loc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "", "n/a", "unknown", "none", "(null)", "0":
		return time.Time{}, nil
	}
	if loc == nil {
		loc = time.Local
	}
	t, err := time.ParseInLocation(TimeLayout, s, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("timestamp %q: expected YYYY-MM-DDTHH:MM:SS", s)
	}
	return t, nil
}
