package governance

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func platformProcess(ctx context.Context, resolved string, args []string) (*exec.Cmd, error) {
	if !strings.EqualFold(filepath.Ext(resolved), ".bat") && !strings.EqualFold(filepath.Ext(resolved), ".cmd") {
		return exec.CommandContext(ctx, resolved, args...), nil
	}
	// SDK batch launchers require cmd.exe. Supply its native command line rather
	// than Go's CommandLineToArgvW escaping, which cmd.exe does not implement.
	parts := append([]string{resolved}, args...)
	for i, p := range parts {
		if strings.ContainsAny(p, "\"\r\n%!&|<>^") {
			return nil, fmt.Errorf("unsafe Windows batch argument")
		}
		parts[i] = "\"" + p + "\""
	}
	cmd := exec.CommandContext(ctx, "cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /d /s /c "` + strings.Join(parts, " ") + `"`}
	return cmd, nil
}

func configureProcess(c *exec.Cmd) {
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(c.Process.Pid)).Run()
	}
}
