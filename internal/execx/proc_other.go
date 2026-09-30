//go:build !unix

package execx

import "os/exec"

// setProcessGroup is a no-op where process groups are unavailable.
func setProcessGroup(*exec.Cmd) {}
