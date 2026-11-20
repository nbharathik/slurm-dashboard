package state

import (
	"context"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
)

// Backend is what the dashboard asks of Slurm outside the scheduled
// collectors. Sources, which runs the text commands, is the only
// implementation; a JSON backend (planned for v0.4) would be another.
type Backend interface {
	JobDetail(ctx context.Context, id string) (*model.JobDetail, error)
	OwnJob(ctx context.Context, id string) (model.Job, error)
	RequireOwn(ctx context.Context, id string) error
	BatchScript(ctx context.Context, id string) (string, error)
	SubmitLine(ctx context.Context, id string) (line, workDir string, err error)
	StartEstimate(ctx context.Context, id string) (est parse.StartEstimate, ok bool, err error)
	FinalStates(ctx context.Context, ids []string) ([]parse.FinalState, error)
}

var _ Backend = (*Sources)(nil)
