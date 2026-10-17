package state

import (
	"slices"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// Access says whether the user may submit to a partition.
type Access int

// Access levels. The zero value is unknown, so missing data never hides a partition.
const (
	AccessUnknown Access = iota
	AccessAllowed
	AccessDenied
)

func (a Access) String() string {
	switch a {
	case AccessAllowed:
		return "allowed"
	case AccessDenied:
		return "denied"
	}
	return "unknown"
}

// Identity is what is known about the user for partition access checks.
type Identity struct {
	Root          bool
	Groups        []string
	GroupsKnown   bool
	Accounts      []string // the user's own associations; sshare does not list parent accounts
	AccountsKnown bool
}

// PartitionAccess classifies p for the user. why names the list behind a
// denied or unknown answer. Only a deny list that holds every account of the
// user denies; group lists, QOS lists and account lists that might match
// through a parent account stay unknown.
func PartitionAccess(p model.Partition, id Identity) (access Access, why string) {
	if id.Root || !p.Restricted() {
		return AccessAllowed, ""
	}
	unknown := ""
	maybe := func(list string) {
		if unknown == "" {
			unknown = list
		}
	}
	// scontrol without -a already hides partitions closed to the user's groups,
	// so a miss here means the local group list is stale (id -Gn), not a deny.
	if len(p.AllowGroups) > 0 && (!id.GroupsKnown || !overlaps(p.AllowGroups, id.Groups)) {
		maybe("AllowGroups")
	}
	// Account lists include sub-accounts, so a miss on the user's own accounts proves nothing.
	switch {
	case len(p.AllowAccounts) > 0:
		if !id.AccountsKnown || !overlaps(p.AllowAccounts, id.Accounts) {
			maybe("AllowAccounts")
		}
	case len(p.DenyAccounts) > 0:
		if id.AccountsKnown && within(id.Accounts, p.DenyAccounts) {
			return AccessDenied, "DenyAccounts"
		}
		maybe("DenyAccounts")
	}
	switch {
	case len(p.AllowQOS) > 0:
		maybe("AllowQos")
	case len(p.DenyQOS) > 0:
		maybe("DenyQos")
	}
	if unknown != "" {
		return AccessUnknown, unknown
	}
	return AccessAllowed, ""
}

// ApplyAccess sets the access of every summary for the user.
func ApplyAccess(sums []PartSummary, id Identity) {
	for i := range sums {
		sums[i].Access, sums[i].AccessWhy = PartitionAccess(sums[i].Partition, id)
	}
}

func overlaps(list, have []string) bool {
	return slices.ContainsFunc(list, func(s string) bool { return slices.Contains(have, s) })
}

// within reports whether every item of have is in list (false for none).
func within(have, list []string) bool {
	return len(have) > 0 && !slices.ContainsFunc(have, func(s string) bool { return !slices.Contains(list, s) })
}
