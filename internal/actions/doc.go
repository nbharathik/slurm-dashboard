// Package actions is the single registry of things sdash can do to jobs:
// cancel, hold, release and requeue your jobs, open a shell, sample GPUs,
// and rerun a job. Keys, the palette and row menus
// all use it.
//
// It is the only package allowed to authorise state-changing Slurm
// commands (execx.WithMutation; golangci-lint enforces this). An action
// first builds the exact argv lists, the caller shows them to the user,
// and Run then grants
// permission for exactly those argv lists and nothing else.
package actions
