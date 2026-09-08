package system

import (
	"context"
	"fmt"
	"os/exec"
)

// CommandServices stops Android init services through the device shell. The
// service names come only from StartupPreparer and are not operator input.
type CommandServices struct {
	Shell string
}

func (s CommandServices) Stop(name string) error {
	shell := s.Shell
	if shell == "" {
		shell = "/system/bin/sh"
	}
	if err := exec.CommandContext(context.Background(), shell, "-c", "stop "+name).Run(); err != nil { //nolint:gosec // name is an internal fixed service name.
		return fmt.Errorf("run stop %s: %w", name, err)
	}
	return nil
}
