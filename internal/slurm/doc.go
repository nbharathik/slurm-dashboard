// Package slurm knows how to talk to Slurm: the exact command lines sdash
// runs, the capabilities of the installed version, and who the
// current user is. It builds argv slices only; running them is the job of
// internal/execx and parsing them the job of internal/slurm/parse.
package slurm
