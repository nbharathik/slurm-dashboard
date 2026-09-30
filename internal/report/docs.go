package report

import (
	"github.com/nbharathik/slurm-dashboard/internal/insights"
	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// Why builds the why-pending document for a pending job. prio is the
// sprio explanation, or "".
func Why(h Header, j model.Job, prio string) WhyDoc {
	p := insights.Explain(j.Reason)
	doc := WhyDoc{
		Header: h, Job: FromJob(j), Code: p.Code, Explanation: p.Explanation,
		Action: p.Action, NeverStarts: p.Never, KnownReason: p.Known, Priority: prio,
	}
	if !j.StartTime.IsZero() {
		t := j.StartTime
		doc.EstimatedStart = &t
	}
	return doc
}

// Eff is a finished job's efficiency with its failure hint and
// right-sizing suggestions. uid is the user's numeric ID.
func Eff(j model.HistoryJob, uid string) Efficiency {
	var sugg []Suggestion
	for _, s := range insights.RightSize(j) {
		sugg = append(sugg, Suggestion{What: s.What, Reason: s.Reason, Line: s.Line})
	}
	hint := insights.ExplainFailure(j, uid)
	text := ""
	if hint.Title != "" {
		text = hint.Title + ": " + hint.Text
	}
	return FromHistoryJob(j, text, sugg)
}

// Quotas is the storage document.
func Quotas(h Header, qs []model.Quota) QuotaDoc {
	doc := QuotaDoc{Header: h, Locations: []Quota{}}
	for _, q := range qs {
		doc.Locations = append(doc.Locations, FromQuota(q))
	}
	return doc
}
