package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsExecutableVersionMetadata(t *testing.T) {
	path := filepath.Join(os.Getenv("SystemRoot"), "System32", "kernel32.dll")
	if version := installedBrowserVersion(path); version == "" {
		t.Fatal("could not read Windows file version metadata")
	}
	if version := installedBrowserVersion(filepath.Join(t.TempDir(), "missing.exe")); version != "" {
		t.Fatal("missing executable returned a version")
	}
}
