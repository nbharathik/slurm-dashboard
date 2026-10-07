// Package debuglog writes the rotating debug log (mode 0600, never blocks startup) and crash reports.
package debuglog

import (
	"io"
	"log/slog"
	"path/filepath"

	"github.com/nbharathik/slurm-dashboard/internal/privatefile"
)

// Rotation limits for debug.log.
const (
	MaxSize  = 5 << 20
	KeepLogs = 3
	FileName = "debug.log"
)

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

// Open returns a logger for cacheDir/debug.log (debug level if verbose).
// On error it returns a discarding logger plus the error.
func Open(cacheDir string, verbose bool) (*slog.Logger, io.Closer, error) {
	return open(cacheDir, verbose, MaxSize)
}

func open(cacheDir string, verbose bool, maxSize int64) (*slog.Logger, io.Closer, error) {
	if err := privatefile.Directory(cacheDir); err != nil {
		return Discard(), nopCloser{}, err
	}
	f, err := openRotating(filepath.Join(cacheDir, FileName), maxSize, KeepLogs)
	if err != nil {
		return Discard(), nopCloser{}, err
	}
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: level})), f, nil
}

// Discard returns a logger that drops everything.
func Discard() *slog.Logger { return slog.New(slog.DiscardHandler) }
