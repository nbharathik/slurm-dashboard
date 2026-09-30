package units

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// ParseJobState parses a job state. "CANCELLED by 1234" becomes CANCELLED
// with canceller UID "1234". Unknown states are returned unchanged (upper
// case) so they can be shown neutrally.
func ParseJobState(s string) (state model.JobState, cancelledBy string) {
	s = strings.TrimSpace(s)
	if base, uid, ok := strings.Cut(s, " by "); ok {
		s, cancelledBy = base, strings.TrimSpace(uid)
	}
	s = strings.TrimSuffix(strings.ToUpper(s), "+") // truncated sacct output
	return model.JobState(s), cancelledBy
}

// nodeSuffixFlags maps sinfo's one-character state suffixes to flags.
var nodeSuffixFlags = map[byte]string{
	'*': "NOT_RESPONDING",
	'~': "POWERED_DOWN",
	'#': "POWERING_UP",
	'%': "POWERING_DOWN",
	'$': "MAINTENANCE",
	'@': "REBOOT_REQUESTED",
	'^': "REBOOT_ISSUED",
	'!': "POWER_DOWN",
	'-': "PLANNED",
}

// ParseNodeState splits "MIXED+DRAIN", "DOWN+NOT_RESPONDING" or "IDLE*"
// into a base state and flags. Unknown flags are kept.
func ParseNodeState(s string) (base string, flags []string) {
	s = strings.ToUpper(strings.TrimSpace(s))
	for len(s) > 0 {
		f, ok := nodeSuffixFlags[s[len(s)-1]]
		if !ok {
			break
		}
		flags = append(flags, f)
		s = s[:len(s)-1]
	}
	parts := strings.Split(s, "+")
	base = parts[0]
	for _, f := range parts[1:] {
		if f != "" {
			flags = append(flags, f)
		}
	}
	return base, flags
}

// ParseExitCode parses sacct's "code:signal".
func ParseExitCode(s string) (code, signal int, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, nil
	}
	c, sig, ok := strings.Cut(s, ":")
	code, err1 := strconv.Atoi(c)
	if !ok {
		if err1 != nil {
			return 0, 0, fmt.Errorf("exit code %q: bad number", s)
		}
		return code, 0, nil
	}
	signal, err2 := strconv.Atoi(sig)
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("exit code %q: expected code:signal", s)
	}
	return code, signal, nil
}
