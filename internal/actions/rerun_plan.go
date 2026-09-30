package actions

import (
	"fmt"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
)

// RerunField is one resource the rerun form lets you change.
type RerunField struct {
	Name     string // sbatch long option, e.g. "mem"
	Label    string
	Original string // what the job asked for; "" = the cluster default
	FromLine bool   // set on the original command line, not in the script
	Suggest  string // right-size suggestion, "" when none
}

// RerunPlan is what a rerun keeps from the original job: the editable
// fields, the other command-line options it carries over, and those it
// cannot carry.
type RerunPlan struct {
	Fields     []RerunField
	Carried    []Opt
	Dropped    []string // shown as "not carried over"
	ScriptArgs []string // the original script's arguments, which a rerun cannot pass
}

// rerunFields are the form's resources, each with the options that can
// set it; the first one the original used names the field.
var rerunFields = []struct {
	label string
	names []string
}{
	{"Job name", []string{"job-name"}},
	{"Partition", []string{"partition"}},
	{"CPUs per task", []string{"cpus-per-task"}},
	{"Memory", []string{"mem", "mem-per-cpu"}},
	{"Time", []string{"time"}},
	{"GPUs", []string{"gres", "gpus", "gpus-per-node"}},
}

// ignoredOnRerun are options the rerun replaces on purpose.
var ignoredOnRerun = map[string]bool{
	"parsable": true, // added by the rerun itself
	"chdir":    true, // the rerun runs in the job's working directory
	"wrap":     true, // the stored script already holds the wrapped command
}

// PlanRerun combines the original command line (line may be nil when
// Slurm does not record it), the script's #SBATCH lines and the right-size
// suggestions (option name to value) into a plan.
func PlanRerun(line *parse.SbatchLine, directives []parse.SbatchOpt, suggest map[string]string) RerunPlan {
	var p RerunPlan
	var lineOpts []parse.SbatchOpt
	if line != nil {
		lineOpts = line.Opts
		p.ScriptArgs = line.ScriptArgs
	}
	used := map[string]bool{}
	for _, f := range rerunFields {
		name := f.names[0]
		for _, n := range f.names {
			_, inLine := parse.Last(lineOpts, n)
			_, inScript := parse.Last(directives, n)
			if inLine || inScript {
				name = n
				break
			}
		}
		rf := RerunField{Name: name, Label: f.label, Suggest: suggest[name]}
		if v, ok := parse.Last(lineOpts, name); ok {
			rf.Original, rf.FromLine = v, true
		} else if v, ok := parse.Last(directives, name); ok {
			rf.Original = v
		}
		if name == "cpus-per-task" && rf.Suggest != "" {
			if n, _ := lastOf(lineOpts, directives, "ntasks"); n != "" && n != "1" {
				rf.Suggest = "" // a per-task count cannot come from a total
			}
		}
		used[name] = true
		p.Fields = append(p.Fields, rf)
	}
	seen := map[string]bool{}
	for i := len(lineOpts) - 1; i >= 0; i-- { // the last occurrence wins
		o := lineOpts[i]
		if used[o.Name] || ignoredOnRerun[o.Name] || seen[o.Name] {
			continue
		}
		seen[o.Name] = true
		opt := Opt{o.Name, o.Value}
		if o.Flag || CheckOpt(opt) != nil {
			p.Dropped = append([]string{o.String()}, p.Dropped...)
			continue
		}
		p.Carried = append([]Opt{opt}, p.Carried...)
	}
	return p
}

func lastOf(line, script []parse.SbatchOpt, name string) (string, bool) {
	if v, ok := parse.Last(line, name); ok {
		return v, true
	}
	return parse.Last(script, name)
}

// Opts turns the form's values (field name to value) into the options a
// rerun passes: the carried options, then each field that differs from
// the original or was on the original command line.
func (p RerunPlan) Opts(values map[string]string) ([]Opt, error) {
	opts := append([]Opt(nil), p.Carried...)
	for _, f := range p.Fields {
		v, ok := values[f.Name]
		if !ok {
			v = f.Original
		}
		v = strings.TrimSpace(v)
		switch {
		case v == "" && f.Original != "" && !f.FromLine:
			return nil, fmt.Errorf("%s: the script sets it; change it or edit the script (e)", f.Label)
		case v == "":
			continue
		case v == f.Original && !f.FromLine:
			continue // the script still says so
		}
		o := Opt{f.Name, v}
		if err := CheckOpt(o); err != nil {
			return nil, fmt.Errorf("%s: %q is not valid for --%s", f.Label, v, f.Name)
		}
		opts = append(opts, o)
	}
	return opts, nil
}
