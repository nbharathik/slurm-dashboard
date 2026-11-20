// Command sdash is a terminal dashboard for Slurm users.
package main

import (
	"context"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"

	"github.com/nbharathik/slurm-dashboard/internal/cli"
)

// Runtime limits for a shared login node. Each applies only when the user
// has not set the matching Go environment variable.
const (
	maxProcs = 4         // sdash needs two threads; login nodes have 64 to 256 cores
	gcPct    = 50        // collect at 1.5x the live heap instead of 2x
	memLimit = 512 << 20 // soft ceiling; the collector works harder near it
)

func main() {
	limitRuntime()
	// Ctrl+C and SIGTERM cancel the context, which kills any running
	// Slurm command's process group before sdash exits.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Main(ctx, os.Args[1:], cli.Env{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Getenv: os.Getenv,
	})
	stop()
	os.Exit(code)
}

func limitRuntime() {
	if os.Getenv("GOMAXPROCS") == "" {
		runtime.GOMAXPROCS(min(maxProcs, runtime.GOMAXPROCS(0)))
	}
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(gcPct)
	}
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(memLimit)
	}
}
