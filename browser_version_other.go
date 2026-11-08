//go:build !windows

package main

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

func installedBrowserVersion(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
