package actions

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
)

// RerunArgv0 starts every rerun command; options follow as --name=value.
// The script always arrives on stdin, so no file path can be smuggled in.
var RerunArgv0 = []string{"sbatch", "--parsable"}

// Value patterns for the sbatch options a rerun may pass. Anything else
// from the original command line is shown as "not carried over".
var (
	reName   = regexp.MustCompile(`^[A-Za-z0-9_.][A-Za-z0-9_.,-]*$`)
	reCount  = regexp.MustCompile(`^[1-9][0-9]{0,5}$`)
	reNodes  = regexp.MustCompile(`^[1-9][0-9]{0,5}(-[1-9][0-9]{0,5})?$`)
	reTime   = regexp.MustCompile(`^([0-9]{1,4}-)?[0-9]{1,6}(:[0-9]{1,2}){0,2}$|^(?i:unlimited|infinite)$`)
	reMem    = regexp.MustCompile(`^[0-9]{1,9}(?i:[KMGT]B?)?$`)
	reGres   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.:,=+-]*$`)
	reFeat   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.:,&|!*()\[\]-]*$`)
	reArray  = regexp.MustCompile(`^[0-9][0-9,:%-]*$`)
	reMailTy = regexp.MustCompile(`^(?i:[A-Z_0-9]+)(,(?i:[A-Z_0-9]+))*$`)
	reMail   = regexp.MustCompile(`^[A-Za-z0-9._%+-]+(@[A-Za-z0-9.-]+)?$`)
)

// rerunOptions are the options a rerun may pass, with their value check.
var rerunOptions = map[string]func(string) bool{
	"partition":       reName.MatchString,
	"account":         reName.MatchString,
	"qos":             reName.MatchString,
	"job-name":        jobName,
	"time":            reTime.MatchString,
	"cpus-per-task":   reCount.MatchString,
	"ntasks":          reCount.MatchString,
	"ntasks-per-node": reCount.MatchString,
	"nodes":           reNodes.MatchString,
	"mem":             reMem.MatchString,
	"mem-per-cpu":     reMem.MatchString,
	"gres":            reGres.MatchString,
	"gpus":            reGres.MatchString,
	"gpus-per-node":   reGres.MatchString,
	"constraint":      reFeat.MatchString,
	"array":           reArray.MatchString,
	"output":          filePattern,
	"error":           filePattern,
	"mail-type":       reMailTy.MatchString,
	"mail-user":       reMail.MatchString,
}

func plainText(s string, maxLen int) bool {
	if s == "" || len(s) > maxLen || strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return false
		}
	}
	return true
}

func jobName(s string) bool     { return plainText(s, 200) }
func filePattern(s string) bool { return plainText(s, 4096) }

// Opt is one sbatch option a rerun passes as --name=value.
type Opt struct{ Name, Value string }

// CheckOpt reports whether an option may be passed to a rerun.
func CheckOpt(o Opt) error {
	ok, known := rerunOptions[o.Name]
	switch {
	case !known:
		return fmt.Errorf("--%s cannot be passed on a rerun; edit the script instead", o.Name)
	case !ok(o.Value):
		return fmt.Errorf("--%s: %q is not a valid value", o.Name, o.Value)
	}
	return nil
}

// RerunArgv builds "sbatch --parsable --name=value ..." after checking
// every option.
func RerunArgv(opts []Opt) ([]string, error) {
	argv := slices.Clone(RerunArgv0)
	for _, o := range opts {
		if err := CheckOpt(o); err != nil {
			return nil, err
		}
		argv = append(argv, "--"+o.Name+"="+o.Value)
	}
	return argv, nil
}

// checkRerunArgv re-checks an argv built by RerunArgv before it runs.
func checkRerunArgv(argv []string) error {
	if len(argv) < len(RerunArgv0) || !slices.Equal(argv[:len(RerunArgv0)], RerunArgv0) {
		return errors.New("a rerun must start with sbatch --parsable")
	}
	for _, a := range argv[len(RerunArgv0):] {
		name, val, ok := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		if !ok || !strings.HasPrefix(a, "--") {
			return fmt.Errorf("unexpected argument %q", a)
		}
		if err := CheckOpt(Opt{name, val}); err != nil {
			return err
		}
	}
	return nil
}

// Submitted is the result of a submission.
type Submitted struct {
	JobID   string
	Cluster string
}

func checkScript(script, dir string) error {
	switch {
	case strings.TrimSpace(script) == "":
		return errors.New("the script is empty")
	case !strings.HasPrefix(script, "#!"):
		return errors.New("the script must start with #! (for example #!/bin/bash)")
	case dir == "" || !strings.HasPrefix(dir, "/"):
		return errors.New("the job needs an absolute working directory")
	}
	return nil
}

// Rerun pipes a confirmed script to argv (built by RerunArgv) from dir.
// The grant covers exactly that argv.
func Rerun(ctx context.Context, r execx.Runner, argv []string, script, dir string) (Submitted, error) {
	if err := checkScript(script, dir); err != nil {
		return Submitted{}, err
	}
	if err := checkRerunArgv(argv); err != nil {
		return Submitted{}, err
	}
	ctx = execx.WithLabel(execx.WithMutation(ctx, "rerun", argv), "action-rerun")
	res, err := execx.RunWith(ctx, r, execx.RunOpts{Stdin: []byte(script), Dir: dir}, argv...)
	if err != nil {
		if msg := execx.FirstLine(res.Stderr); msg != "" {
			return Submitted{}, fmt.Errorf("sbatch: %s", msg)
		}
		return Submitted{}, err
	}
	id, cluster, err := parse.Parsable(res.Stdout)
	if err != nil {
		return Submitted{}, err
	}
	return Submitted{JobID: id, Cluster: cluster}, nil
}

// Estimate asks "sbatch --test-only" when the script would start with the
// given options, without submitting it (read-only).
func Estimate(ctx context.Context, r execx.Runner, script, dir string, opts []Opt) (parse.Estimate, error) {
	if err := checkScript(script, dir); err != nil {
		return parse.Estimate{}, err
	}
	argv := []string{"sbatch", "--test-only"}
	for _, o := range opts {
		if err := CheckOpt(o); err != nil {
			return parse.Estimate{}, err
		}
		argv = append(argv, "--"+o.Name+"="+o.Value)
	}
	res, err := execx.RunWith(execx.WithLabel(ctx, "estimate"), r, execx.RunOpts{Stdin: []byte(script), Dir: dir}, argv...)
	if est, perr := parse.TestOnly(res.Stderr); perr == nil {
		return est, nil
	}
	if err != nil {
		if msg := execx.FirstLine(res.Stderr); msg != "" {
			return parse.Estimate{}, fmt.Errorf("sbatch --test-only: %s", msg)
		}
		return parse.Estimate{}, err
	}
	return parse.TestOnly(res.Stderr)
}
