//go:build unix

package execx

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup runs cmd in its own group and kills the whole group on cancel; Ctrl+C never reaches it.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Group ID is the leader's PID, not yet reaped, so it cannot be reused.
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
