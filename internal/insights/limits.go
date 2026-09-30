package insights

import (
	"fmt"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
)

// NearLimit is the fraction of a limit from which a line is flagged.
const NearLimit = 0.9

// LimitLine is one limit ready to show.
type LimitLine struct {
	Scope string  // "account research", "you in research", "QOS normal"
	Owner string  // the same, short, to put before What: "account research", "your", "QOS normal"
	What  string  // "CPUs running", "jobs running", "longest job"
	Text  string  // "180 of 256 (70%)" or "up to 3d"
	Frac  float64 // Used/Max, -1 when Slurm reports no usage
	Limit model.Limit
}

// Near reports whether the limit is close to being hit.
func (l LimitLine) Near() bool { return l.Frac >= NearLimit }

// Limits turns associations and QOS into lines in Slurm's order; per-job caps read "up to X".
func Limits(scopes []model.LimitScope) []LimitLine {
	var out []LimitLine
	for _, sc := range scopes {
		for _, l := range sc.Limits {
			out = append(out, limitLine(sc, l))
		}
	}
	return out
}

func limitLine(sc model.LimitScope, l model.Limit) LimitLine {
	ln := LimitLine{Scope: scopeName(sc), Owner: ownerName(sc), What: limitWhat(l), Frac: -1, Limit: l}
	top := limitValue(l, l.Max)
	if l.Used >= 0 && l.Max > 0 && isUsageLimit(l) {
		ln.Frac = l.Used / l.Max
		ln.Text = fmt.Sprintf("%s of %s (%.0f%%)", limitValue(l, l.Used), top, 100*ln.Frac)
		return ln
	}
	ln.Text = "up to " + top
	if isUsageLimit(l) {
		ln.Text = "limit " + top
	}
	return ln
}

// isUsageLimit tells a limit on what is in use or spent (Grp*, MaxJobs)
// from a cap on each job (Max*PJ, MaxWall), which has no usage.
func isUsageLimit(l model.Limit) bool {
	return strings.HasPrefix(l.Name, "Grp") || l.Name == "MaxJobs" || l.Name == "MaxSubmitJobs"
}

func scopeName(sc model.LimitScope) string {
	switch sc.Kind {
	case "user":
		return "you in " + sc.Name
	case "qos":
		return "QOS " + sc.Name
	}
	return "account " + sc.Name
}

func ownerName(sc model.LimitScope) string {
	if sc.Kind == "user" {
		return "your"
	}
	return scopeName(sc)
}

var tresNames = map[string]string{
	"cpu": "CPUs", "gres/gpu": "GPUs", "mem": "memory", "node": "nodes", "billing": "billing units", "energy": "energy",
}

// limitWhat names what a limit caps in plain words.
func limitWhat(l model.Limit) string {
	name, res, _ := strings.Cut(l.Name, " ")
	r := tresNames[res]
	if r == "" {
		r = res
	}
	switch name {
	case "GrpTRES":
		return r + " in use"
	case "GrpTRESMins":
		return r + " time spent"
	case "GrpTRESRunMins":
		return r + " time running"
	case "MaxTRESPJ":
		return r + " per job"
	case "MaxTRESPN":
		return r + " per node"
	case "MaxTRESPU":
		return r + " per user"
	case "MaxTRESMinsPJ":
		return r + " time per job"
	case "GrpJobs":
		return "running jobs (whole group)"
	case "GrpSubmitJobs":
		return "submitted jobs (whole group)"
	case "GrpWall":
		return "wall time (whole group)"
	case "MaxJobs", "MaxJobsPU":
		return "running jobs"
	case "MaxSubmitJobs", "MaxSubmitJobsPU":
		return "submitted jobs"
	case "MaxWall":
		return "longest job"
	}
	return l.Name
}

// limitValue formats v in the unit of l.
func limitValue(l model.Limit, v float64) string {
	switch {
	case l.Unit == "MB":
		return fmt.Sprintf("%.0f GB", v/1024)
	case l.Unit == "min" && strings.HasPrefix(l.Name, "Max"):
		return units.FormatLimit(time.Duration(v) * time.Minute)
	case l.Unit == "min": // a budget of resource-minutes, shown in hours
		return fmt.Sprintf("%.0f h", v/60)
	}
	return fmt.Sprintf("%.0f", v)
}

// FormatHours formats resource-hours for a summary line: one decimal below
// ten, whole hours above.
func FormatHours(h float64) string {
	if h < 10 {
		return fmt.Sprintf("%.1f h", h)
	}
	return fmt.Sprintf("%.0f h", h)
}

// Percent is part of whole as "56%", or "-" when whole is not positive.
func Percent(part, whole float64) string {
	if whole <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", 100*part/whole)
}
