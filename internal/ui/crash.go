package ui

import (
	"fmt"
	"runtime/debug"

	"github.com/nbharathik/slurm-dashboard/internal/debuglog"
)

// crashPath is the crash report written on a UI panic, shown after the
// terminal is restored.
var crashPath string

// CrashError reports a UI panic that was saved to a crash report.
type CrashError struct {
	Path string
	Err  error
}

func (e *CrashError) Error() string {
	return fmt.Sprintf("the dashboard crashed; a crash report was saved to %s", e.Path)
}

func (e *CrashError) Unwrap() error { return e.Err }

// crashGuard records a panic in Update or View to a crash report and
// re-panics, so Bubble Tea restores the terminal and stops the program.
func (a *App) crashGuard() {
	if r := recover(); r != nil {
		if crashPath == "" {
			if p, err := debuglog.WriteCrash(a.opt.CacheDir, r, debug.Stack()); err == nil {
				crashPath = p
			}
		}
		panic(r)
	}
}
