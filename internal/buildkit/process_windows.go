//go:build windows

package buildkit

import (
	"context"
	"os/exec"
	"strconv"
	"time"
)

func prepareCommand(command *exec.Cmd) {
	command.WaitDelay = 5 * time.Second
	command.Cancel = func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, "taskkill.exe", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F").Run(); err != nil {
			return command.Process.Kill()
		}
		return nil
	}
}
