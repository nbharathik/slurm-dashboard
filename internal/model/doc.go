// Package model defines the domain types every parser produces and every
// view and CLI command consumes. Nothing outside the parsers sees
// raw Slurm text.
//
// Units: memory is in MB, sizes on disk in bytes, and a nil *time.Duration
// limit means unlimited (or not reported).
package model
