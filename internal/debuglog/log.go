package debuglog

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// Rotation limits for debug.log.
const (
	MaxSize  = 5 << 20
	KeepLogs = 3
	FileName = "debug.log"
)

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

// Open returns a logger writing to cacheDir/debug.log. verbose enables
// debug-level records (--debug); otherwise only info and above are kept.
// On any error it returns a discarding logger and the error, so callers can
// mention the problem without failing.
func Open(cacheDir string, verbose bool) (*slog.Logger, io.Closer, error) {
	return open(cacheDir, verbose, MaxSize)
}

func open(cacheDir string, verbose bool, maxSize int64) (*slog.Logger, io.Closer, error) {
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
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
