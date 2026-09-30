package execx

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Class says whether a command can change state.
type Class int

const (
	// Mutating commands change cluster or filesystem state. They need an
	// authorisation from WithMutation. Unknown commands are Mutating.
	Mutating Class = iota
	// ReadOnly commands only report state.
	ReadOnly
)

func (c Class) String() string {
	if c == ReadOnly {
		return "read-only"
	}
	return "mutating"
}

// clusterTools are the executables sdash may run by default. Each is
// classified by Classify. salloc, sacctmgr, sattach and every admin tool are
// deliberately absent.
var clusterTools = map[string]bool{
	// Slurm
	"squeue": true, "sinfo": true, "scontrol": true, "sacct": true, "sstat": true,
	"sshare": true, "sprio": true, "scancel": true, "sbatch": true, "srun": true,
	// storage quotas
	"lfs": true, "mmlsquota": true, "beegfs-ctl": true, "quota": true,
	// identity fallback when $USER is unset
	"id": true,
}

// localTools may run in any context: the disk-usage analyser's
// "nice -n 19 ionice -c3 du ..." chain. Classify treats the chain as
// read-only only when it ends in du, so nice and ionice cannot wrap
// anything else.
var localTools = map[string]bool{"nice": true, "ionice": true, "du": true}

// verifyTools check the signature of a downloaded release ("sdash update").
// Classify accepts them only as "cosign verify-blob ..." and
// "gh attestation verify ...", which read files and change nothing.
var verifyTools = map[string]bool{"cosign": true, "gh": true}

// slurmTools is the subset of clusterTools that talk to Slurm daemons.
var slurmTools = map[string]bool{
	"squeue": true, "sinfo": true, "scontrol": true, "sacct": true, "sstat": true,
	"sshare": true, "sprio": true, "scancel": true, "sbatch": true, "srun": true,
	"salloc": true,
}

// IsSlurmTool reports whether name (a basename or path) is a Slurm client
// command.
func IsSlurmTool(name string) bool {
	return slurmTools[filepath.Base(name)]
}

// Classify reports whether argv is read-only. It fails closed: anything it
// does not positively recognise as read-only is Mutating.
//
// Read-only forms:
//
//	squeue, sinfo, sacct, sstat, sshare, sprio, mmlsquota, quota, id  (any args)
//	scontrol show ...
//	scontrol --version | -V
//	scontrol write batch_script <jobid> -     (stdout only; without "-" it writes a file)
//	sbatch --test-only ...                    (--test-only must be the first argument)
//	lfs quota ...
//	beegfs-ctl --getquota ...
//	cosign verify-blob ...
//	gh attestation verify ...
//
// scancel, srun, salloc, every other scontrol verb (hold, release, requeue,
// update, top, suspend, ...) and every other sbatch form are Mutating.
func Classify(argv []string) Class {
	if len(argv) == 0 {
		return Mutating
	}
	arg := func(i int) string {
		if i < len(argv) {
			return argv[i]
		}
		return ""
	}
	switch filepath.Base(argv[0]) {
	case "squeue", "sinfo", "sacct", "sstat", "sshare", "sprio", "mmlsquota", "quota", "id":
		return ReadOnly
	case "scontrol":
		switch arg(1) {
		case "show", "--version", "-V":
			return ReadOnly
		case "write":
			if len(argv) == 5 && arg(2) == "batch_script" && arg(4) == "-" {
				return ReadOnly
			}
		}
		return Mutating
	case "sbatch":
		if arg(1) == "--test-only" {
			return ReadOnly
		}
		return Mutating
	case "lfs":
		if arg(1) == "quota" {
			return ReadOnly
		}
		return Mutating
	case "beegfs-ctl":
		if arg(1) == "--getquota" {
			return ReadOnly
		}
		return Mutating
	case "nice", "ionice", "du":
		return classifyDu(argv)
	case "cosign":
		if arg(1) == "verify-blob" {
			return ReadOnly
		}
		return Mutating
	case "gh":
		if arg(1) == "attestation" && arg(2) == "verify" {
			return ReadOnly
		}
		return Mutating
	default:
		return Mutating
	}
}

// classifyDu accepts "[nice -n N] [ionice -c C [-n N]] du ...": read-only
// only when the chain ends in du (which never writes).
func classifyDu(argv []string) Class {
	i := 0
	num := func(s string) bool {
		if s == "" {
			return false
		}
		for _, r := range s {
			if r < '0' || r > '9' {
				return false
			}
		}
		return true
	}
	if i < len(argv) && filepath.Base(argv[i]) == "nice" {
		i++
		switch {
		case i+1 < len(argv) && argv[i] == "-n" && num(argv[i+1]):
			i += 2
		case i < len(argv) && strings.HasPrefix(argv[i], "-n") && num(argv[i][2:]):
			i++
		}
	}
	if i < len(argv) && filepath.Base(argv[i]) == "ionice" {
		i++
		for i < len(argv) {
			switch {
			case i+1 < len(argv) && (argv[i] == "-c" || argv[i] == "-n") && num(argv[i+1]):
				i += 2
				continue
			case (strings.HasPrefix(argv[i], "-c") || strings.HasPrefix(argv[i], "-n")) && num(argv[i][2:]):
				i++
				continue
			}
			break
		}
	}
	if i < len(argv) && filepath.Base(argv[i]) == "du" {
		return ReadOnly
	}
	return Mutating
}

type mutationKey struct{}

// mutationGrant is the authorisation carried in a context.
type mutationGrant struct {
	action string
	argvs  [][]string
}

// WithMutation authorises exactly the given command lines to run in ctx.
// action names the user action (e.g. "cancel") for the debug log.
//
// Only internal/actions may call this, after the user has seen and
// confirmed the exact argv (golangci-lint enforces the restriction). A
// runner compares argv element by element, so a grant for "scancel 812"
// cannot run "scancel 813".
func WithMutation(ctx context.Context, action string, argvs ...[]string) context.Context {
	grant := mutationGrant{action: action}
	for _, a := range argvs {
		grant.argvs = append(grant.argvs, slices.Clone(a))
	}
	return context.WithValue(ctx, mutationKey{}, grant)
}

// MutationFrom returns the action name of the grant in ctx that authorises
// argv, and whether one does.
func MutationFrom(ctx context.Context, argv []string) (string, bool) {
	grant, ok := ctx.Value(mutationKey{}).(mutationGrant)
	if !ok {
		return "", false
	}
	for _, a := range grant.argvs {
		if slices.Equal(a, argv) {
			return grant.action, true
		}
	}
	return "", false
}

// Policy decides whether a command may run. The zero value allows only
// the default cluster tools and applies the test guard.
type Policy struct {
	// ExtraReadOnly allows more executables, by basename, and treats them
	// as read-only. Use it only for commands that cannot change state, such
	// as a site quota command. It cannot reclassify a default cluster tool.
	ExtraReadOnly []string
	// AllowClusterToolsInTests lets a test binary run real cluster tools.
	// Only the Docker integration tests set it.
	AllowClusterToolsInTests bool
	// NoTestGuard disables the test-binary guard entirely. FakeRunner sets
	// it because it never executes anything.
	NoTestGuard bool
}

// Check validates argv against the policy and returns its class. It checks,
// in order: argv shape, allowlist, test guard, and mutation authorisation.
func (p Policy) Check(ctx context.Context, argv []string) (Class, error) {
	if err := validateArgv(argv); err != nil {
		return Mutating, err
	}
	name := argv[0]
	base := filepath.Base(name)

	var class Class
	switch {
	case clusterTools[base]:
		class = Classify(argv)
		if !p.NoTestGuard && testing.Testing() && !p.AllowClusterToolsInTests {
			return class, fmt.Errorf("%s: %w", base, ErrClusterToolInTests)
		}
	case localTools[base]:
		// Only the read-only du chain; no grant can make nice or ionice
		// run anything else.
		if class = Classify(argv); class != ReadOnly {
			return class, fmt.Errorf("%s: %w (only as a wrapper around du)", Key(argv), ErrNotAllowed)
		}
	case verifyTools[base]:
		// Only to verify a download; no grant can make them do more.
		if class = Classify(argv); class != ReadOnly {
			return class, fmt.Errorf("%s: %w (only to verify a release)", Key(argv), ErrNotAllowed)
		}
	case slices.Contains(p.ExtraReadOnly, base) && !slurmTools[base]:
		class = ReadOnly
	default:
		return Mutating, fmt.Errorf("%s: %w", base, ErrNotAllowed)
	}

	if class == Mutating {
		if _, ok := MutationFrom(ctx, argv); !ok {
			return class, fmt.Errorf("%s: %w", Key(argv), ErrMutationNotAuthorized)
		}
	}
	return class, nil
}

// validateArgv rejects empty command lines, relative paths with a directory
// part (which would run whatever is in the current directory), and bytes
// that have no business in a command line.
func validateArgv(argv []string) error {
	if len(argv) == 0 || argv[0] == "" {
		return fmt.Errorf("%w: empty", ErrInvalidArgv)
	}
	if strings.ContainsRune(argv[0], '/') && !filepath.IsAbs(argv[0]) {
		return fmt.Errorf("%w: %q must be a bare name or an absolute path", ErrInvalidArgv, argv[0])
	}
	if filepath.IsAbs(argv[0]) && filepath.Clean(argv[0]) != argv[0] {
		return fmt.Errorf("%w: %q is not a clean path", ErrInvalidArgv, argv[0])
	}
	for i, a := range argv {
		if strings.ContainsAny(a, "\x00\n\r") {
			return fmt.Errorf("%w: argument %d contains a NUL or newline", ErrInvalidArgv, i)
		}
	}
	return nil
}
