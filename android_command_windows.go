package main

import (
	"os/exec"
	"strconv"
	"syscall"
)

func hideAndroidCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

func configureAndroidEmulator(command *exec.Cmd) {}

func killAndroidEmulator(command *exec.Cmd) error {
	stop := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(command.Process.Pid))
	hideAndroidCommand(stop)
	return stop.Run()
}
