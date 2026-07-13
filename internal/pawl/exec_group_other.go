//go:build !unix

package pawl

import "os/exec"

// startInOwnProcessGroup is a no-op on non-Unix platforms: the process-group
// kill it performs elsewhere relies on Unix semantics (Setpgid + kill(-pgid)).
// exec.CommandContext's default per-process kill plus WaitDelay remain in
// effect here.
func startInOwnProcessGroup(cmd *exec.Cmd) {}
