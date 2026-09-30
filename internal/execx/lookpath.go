package execx

import (
	"fmt"
	"os/exec"
)

// LookPath reports where an executable would be found in PATH. It only
// inspects the filesystem; nothing is run.
func LookPath(name string) (string, error) {
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, ErrNotFound)
	}
	return p, nil
}
