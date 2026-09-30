//go:build unix

package execx

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup starts cmd in its own process group and makes context
// cancellation kill the whole group, so helpers a command spawned cannot
// outlive a timeout. It also detaches the command from the terminal's
// foreground group, so a Ctrl+C in the TUI never reaches it directly.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// The group ID equals the leader's PID. The leader has not been
		// reaped yet (Cancel runs before Wait returns), so the ID cannot
		// have been reused.
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
