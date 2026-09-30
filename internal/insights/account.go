package insights

import "github.com/nbharathik/slurm-dashboard/internal/model"

// AccountInput is what the site says about limits and priority. Each part
// carries whether it was read and why not, so the summary can say so.
type AccountInput struct {
	User string

	Scopes       []model.LimitScope
	LimitsRead   bool   // the limits query has succeeded
	LimitsErr    string // its last error, "" when none
	NoAccounting bool   // the site has no accounting database

	Shares        []model.Share
	Priorities    []model.PriorityFactors
	PriorityRead  bool   // sshare has succeeded
	PriorityErr   string // its last error, "" when none
	BasicPriority bool   // PriorityType=priority/basic
}

// Account is the part of the usage view that is not about finished jobs:
// the limits that apply to you and your priority.
type Account struct {
	Limits     []LimitLine
	LimitsNote string // why Limits is empty, "" when it is not
	Standing   []Standing
	Pending    []PendingPriority
	// PriorityNote says why there is no standing, "" when there is.
	PriorityNote string
}

// BuildAccount reads limits and priority, with a plain reason for each
// part that is missing.
func BuildAccount(in AccountInput) Account {
	var a Account
	switch {
	case in.NoAccounting:
		a.LimitsNote = "this cluster has no accounting database, so Slurm sets no account or QOS limits"
	case in.LimitsErr != "":
		a.LimitsNote = "limits are hidden by the site or could not be read: " + in.LimitsErr
	case !in.LimitsRead:
		a.LimitsNote = "limits have not been read yet"
	default:
		if a.Limits = Limits(in.Scopes); len(a.Limits) == 0 {
			a.LimitsNote = "no account or QOS limit is set for you"
		}
	}

	switch {
	case in.BasicPriority:
		a.PriorityNote = "PriorityType=priority/basic has no fairshare or priority factors"
	case in.NoAccounting:
		a.PriorityNote = "this cluster has no accounting database, so there is no fairshare"
	case in.PriorityErr != "":
		a.PriorityNote = "fairshare could not be read: " + in.PriorityErr
	case !in.PriorityRead:
		a.PriorityNote = "fairshare has not been read yet"
	default:
		if a.Standing = FairShareStanding(in.Shares, in.User); len(a.Standing) == 0 {
			a.PriorityNote = "you have no fairshare row"
		}
		a.Pending = PendingPriorities(in.Priorities)
	}
	return a
}
