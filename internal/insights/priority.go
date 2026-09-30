package insights

import (
	"strconv"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// Standing is the user's fairshare position in one account.
type Standing struct {
	Account   string
	FairShare float64 // 0..1; 0.5 means using exactly the share
	UsedPct   float64 // effective usage of the cluster, percent
	SharesPct float64 // normalised shares, percent
	Verdict   string  // one plain line
}

// FairShareStanding picks the user's rows out of sshare output and reads
// them. It returns one Standing per account the user has a row in.
func FairShareStanding(shares []model.Share, user string) []Standing {
	var out []Standing
	for _, s := range shares {
		if s.User == "" || (user != "" && s.User != user) {
			continue
		}
		out = append(out, Standing{
			Account: s.Account, FairShare: s.FairShare, UsedPct: 100 * s.EffectiveUsage, SharesPct: 100 * s.NormShares,
			Verdict: standingVerdict(s.FairShare),
		})
	}
	return out
}

// standingVerdict reads the fairshare factor: 1 is untouched, 0.5 is using
// exactly the share, near 0 is far over it.
func standingVerdict(f float64) string {
	switch {
	case f >= 0.75:
		return "well under your share: your jobs are favoured"
	case f >= 0.5:
		return "under your share"
	case f >= 0.25:
		return "over your share: others' jobs go first"
	}
	return "far over your share: expect to wait"
}

// PendingPriority is what drives one pending job's priority.
type PendingPriority struct {
	JobID    string
	Priority int64
	Biggest  string // "fairshare 3072"
	Line     string // the full breakdown
}

// PendingPriorities summarises the sprio rows of the user's pending jobs.
func PendingPriorities(pfs []model.PriorityFactors) []PendingPriority {
	out := make([]PendingPriority, 0, len(pfs))
	for _, pf := range pfs {
		big, v := "", int64(0)
		for _, f := range []struct {
			name string
			v    int64
		}{{"age", pf.Age}, {"fairshare", pf.FairShare}, {"job size", pf.JobSize}, {"partition", pf.Partition}, {"QOS", pf.QOS}} {
			if f.v > v {
				big, v = f.name, f.v
			}
		}
		p := PendingPriority{JobID: pf.JobID, Priority: pf.Priority, Line: PriorityLine(pf)}
		if big != "" {
			p.Biggest = big + " " + strconv.FormatInt(v, 10)
		}
		out = append(out, p)
	}
	return out
}
