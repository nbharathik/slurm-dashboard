// Command sdash is a terminal dashboard for Slurm users.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/nbharathik/slurm-dashboard/internal/cli"
)

func main() {
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
