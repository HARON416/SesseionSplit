//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

func hideAndroidCommand(command *exec.Cmd) {}

func configureAndroidEmulator(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killAndroidEmulator(command *exec.Cmd) error {
	return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
}
