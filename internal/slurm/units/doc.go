// Package units parses the small value formats Slurm prints: durations,
// timestamps, memory sizes, job IDs, host lists, GRES/TRES strings, and job
// and node states. Every function is pure and returns an error instead of
// panicking on bad input.
package units
