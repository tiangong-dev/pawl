//go:build unix

package pawl

import (
	"os/exec"
	"syscall"
)

// startInOwnProcessGroup puts the command in its own process group and makes
// timeout cancellation kill that whole group. A tool adapter runs `sh -c`,
// and the shell often forks the real tool as a grandchild that inherits the
// stdout pipe. Killing only the shell leaves that grandchild alive holding
// the pipe open, so Wait blocks until the grandchild exits on its own — the
// timeout is detected but not honored. Killing the group (negative PID) takes
// the grandchild down with the shell, so the pipe closes and Wait returns at
// the deadline.
func startInOwnProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Setpgid makes the child's PGID equal its PID, so -PID addresses
		// the whole group. ESRCH (already gone) is not an error worth
		// surfacing.
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
			return err
		}
		return nil
	}
}
