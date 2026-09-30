package execx

import "strings"

// droppedPrefixes are environment variable prefixes that let users change
// the output format of Slurm tools. sdash parses fixed formats, so they are
// removed from every child environment.
var droppedPrefixes = []string{"SQUEUE_", "SINFO_", "SACCT_", "SSTAT_", "SPRIO_", "SSHARE_"}

// droppedKeys are removed and then set to fixed values by SanitizeEnv.
var droppedKeys = map[string]bool{"SLURM_TIME_FORMAT": true, "LC_ALL": true}

// SanitizeEnv returns a copy of environ suitable for non-interactive child
// processes. It removes SQUEUE_*, SINFO_*, SACCT_*, SSTAT_*, SPRIO_*,
// SSHARE_* and SLURM_TIME_FORMAT, keeps everything else (including
// SLURM_CONF) in order, and appends LC_ALL=C and SLURM_TIME_FORMAT=standard
// so numbers and timestamps have one predictable form.
func SanitizeEnv(environ []string) []string {
	out := make([]string, 0, len(environ)+2)
	for _, kv := range environ {
		key, _, _ := strings.Cut(kv, "=")
		if key == "" || droppedKeys[key] || hasDroppedPrefix(key) {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "LC_ALL=C", "SLURM_TIME_FORMAT=standard")
}

func hasDroppedPrefix(key string) bool {
	for _, p := range droppedPrefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}
