//go:build !unix

package logs

import "os"

func openLogFile(path string) (*os.File, error) { return os.Open(path) }
