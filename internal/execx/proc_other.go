//go:build !unix

package execx

import "os/exec"

// setProcessGroup is a no-op on platforms without process groups; sdash
// only targets Linux and macOS, this keeps the package compiling elsewhere.
func setProcessGroup(*exec.Cmd) {}
