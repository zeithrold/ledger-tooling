//go:build !windows

package governance

import (
	"context"
	"os/exec"
	"syscall"
)

func platformProcess(ctx context.Context, resolved string, args []string) (*exec.Cmd, error) {
	return exec.CommandContext(ctx, resolved, args...), nil
}

func configureProcess(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
}
