// Package parse turns the raw text of Slurm commands into model types.
//
// Every parser is a pure function of its input. Malformed lines are skipped
// and reported as model.ParseWarning values; a parser never panics (each
// one also recovers from its own bugs and reports them as a warning).
//
// The exact command lines these parsers expect are built by package slurm.
package parse
