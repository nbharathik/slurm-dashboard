package actions

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
)

// Args are action arguments. None of the current actions take any; the
// type stays so an action can be added without changing callers.
type Args map[string]string

// Action is one thing sdash can do to jobs.
type Action struct {
	ID          string
	Title       string // "Cancel"
	Done        string // past tense for the flash line: "Cancelled"
	Destructive bool   // needs a confirmation that shows the exact command
	AppliesTo   func(model.Job) bool
	Build       func(jobs []model.Job, args Args) ([][]string, error)
}

// Registry holds the actions.
type Registry struct {
	list []Action
}

// Get returns an action by ID.
func (r *Registry) Get(id string) (Action, bool) {
	for _, a := range r.list {
		if a.ID == id {
			return a, true
		}
	}
	return Action{}, false
}

// MaxJobs is the most jobs one action may touch without typing the count
// to confirm.
const MaxJobs = 10

func active(j model.Job) bool { return j.State.IsActive() }

func held(j model.Job) bool {
	return j.State == model.StatePending && (strings.HasPrefix(j.Reason, "JobHeld") || strings.Contains(strings.ToLower(j.Reason), "held"))
}

func pendingNotHeld(j model.Job) bool { return j.State == model.StatePending && !held(j) }

// Default returns the standard registry.
func Default() *Registry {
	return &Registry{list: []Action{
		{
			ID: "cancel", Title: "Cancel", Done: "Cancelled", Destructive: true, AppliesTo: active,
			Build: func(jobs []model.Job, _ Args) ([][]string, error) {
				ids, err := jobIDs(jobs)
				if err != nil {
					return nil, err
				}
				return [][]string{append([]string{"scancel"}, ids...)}, nil
			},
		},
		{
			ID: "hold", Title: "Hold", Done: "Held", AppliesTo: pendingNotHeld,
			Build: scontrolList("hold"),
		},
		{
			ID: "release", Title: "Release", Done: "Released", AppliesTo: held,
			Build: scontrolList("release"),
		},
		{
			ID: "requeue", Title: "Requeue", Done: "Requeued", Destructive: true,
			AppliesTo: func(j model.Job) bool {
				return j.State == model.StateRunning || j.State == model.StateSuspended || j.State == model.StateCompleting
			},
			Build: scontrolList("requeue"),
		},
	}}
}

// scontrolList builds "scontrol <verb> <id,id,...>".
func scontrolList(verb string) func([]model.Job, Args) ([][]string, error) {
	return func(jobs []model.Job, _ Args) ([][]string, error) {
		ids, err := jobIDs(jobs)
		if err != nil {
			return nil, err
		}
		return [][]string{{"scontrol", verb, strings.Join(ids, ",")}}, nil
	}
}

// jobIDs validates and returns the job IDs, rejecting anything that is not
// a plain Slurm job reference.
func jobIDs(jobs []model.Job) ([]string, error) {
	if len(jobs) == 0 {
		return nil, errors.New("no jobs selected")
	}
	ids := make([]string, 0, len(jobs))
	for _, j := range jobs {
		if err := ValidateJobID(j.ID.Raw); err != nil {
			return nil, err
		}
		ids = append(ids, j.ID.Raw)
	}
	return ids, nil
}

// ValidateJobID rejects anything but 123, 123_4, 123_[1-5], 123+0.
func ValidateJobID(id string) error {
	if !units.ValidJobRef.MatchString(id) {
		return fmt.Errorf("%q is not a valid job ID", id)
	}
	return nil
}

// Result is the outcome of running an action.
type Result struct {
	Action Action
	Argvs  [][]string
	Output string // combined stdout of all commands
	Err    error  // first failure, with Slurm's first stderr line
}

// Summary is the flash-line text for a result.
func (r Result) Summary(jobs int) string {
	if r.Err != nil {
		return r.Err.Error()
	}
	noun := "job"
	if jobs != 1 {
		noun = "jobs"
	}
	return fmt.Sprintf("%s %d %s", r.Action.Done, jobs, noun)
}

// Run executes the confirmed command lines of an action, granting
// permission for exactly those argv lists.
func Run(ctx context.Context, r execx.Runner, a Action, argvs [][]string) Result {
	res := Result{Action: a, Argvs: argvs}
	ctx = execx.WithMutation(ctx, a.ID, argvs...)
	var out strings.Builder
	for _, argv := range argvs {
		cr, err := r.Run(execx.WithLabel(ctx, "action-"+a.ID), argv...)
		out.Write(cr.Stdout)
		if err != nil {
			msg := execx.FirstLine(cr.Stderr)
			if msg == "" {
				msg = err.Error()
			}
			res.Err = fmt.Errorf("%s: %s", argv[0], msg)
			break
		}
	}
	res.Output = out.String()
	return res
}

// Command renders argv lists for display ("scancel 812 813").
func Command(argvs [][]string) string {
	lines := make([]string, len(argvs))
	for i, a := range argvs {
		lines[i] = execx.Key(a)
	}
	return strings.Join(lines, "\n")
}
