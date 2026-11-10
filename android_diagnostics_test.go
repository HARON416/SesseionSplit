package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAndroidDiagnosticProcessHelper(t *testing.T) {
	if os.Getenv("ISOLATE_DIAGNOSTIC_TEST_PROCESS") != "1" {
		return
	}
	fmt.Fprintln(os.Stdout, "emulator startup output")
	fmt.Fprintln(os.Stderr, "fatal: graphics device failed")
	os.Exit(17)
}

func TestAndroidDiagnosticsCaptureProcessOutputAndExit(t *testing.T) {
	log := newAndroidDiagnosticLog(t.TempDir(), "Isolate_01", "emulator")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestAndroidDiagnosticProcessHelper$")
	command.Env = append(os.Environ(), "ISOLATE_DIAGNOSTIC_TEST_PROCESS=1")
	command.Stdout, command.Stderr = log, log
	err = command.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 17 {
		t.Fatal("exit status lost", err)
	}
	fmt.Fprintf(log, "exit: %v", err)
	log.close()
	data, err := os.ReadFile(log.path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"emulator startup output", "fatal: graphics device failed", "exit status 17"} {
		if !strings.Contains(string(data), want) || !strings.Contains(log.details(), want) {
			t.Fatal("missing diagnostic", want, log.details())
		}
	}
}

func TestAndroidDiagnosticLogsBoundedAndPreviousRunPreserved(t *testing.T) {
	root := t.TempDir()
	log := newAndroidDiagnosticLog(root, "Isolate_01", "emulator")
	for i := 0; i < 4; i++ {
		_, _ = log.Write([]byte(strings.Repeat("x", androidLogLimit)))
	}
	_, _ = log.Write([]byte("\nlast error\n"))
	log.close()
	info, err := os.Stat(log.path)
	if err != nil || info.Size() > androidLogLimit {
		t.Fatal("unbounded log", info, err)
	}
	if len(log.tail) > androidLogTail || !strings.Contains(log.details(), "last error") {
		t.Fatal("last error lost")
	}
	next := newAndroidDiagnosticLog(root, "Isolate_01", "emulator")
	defer next.close()
	previous, err := os.ReadFile(next.path + ".previous")
	if err != nil || !strings.Contains(string(previous), "last error") {
		t.Fatal("previous session lost", err)
	}
}

func TestAndroidDiagnosticDiskFailureKeepsMemoryOutput(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "file")
	if err := os.WriteFile(blocked, nil, 0600); err != nil {
		t.Fatal(err)
	}
	log := newAndroidDiagnosticLog(blocked, "Isolate_01", "emulator")
	n, err := log.Write([]byte("fatal encoder error"))
	log.close()
	if err != nil || n != len("fatal encoder error") || !strings.Contains(log.details(), "fatal encoder error") || !strings.Contains(log.details(), "Could not save") {
		t.Fatal("log failure must not interrupt emulator output", n, err, log.details())
	}
}

func TestAndroidEmulatorStartFailureIncludesDiagnostics(t *testing.T) {
	service := fakeAndroidService()
	service.emulator = filepath.Join(t.TempDir(), "missing-emulator")
	service.setup = &androidSetup{status: AndroidSetupStatus{Root: t.TempDir()}}
	original := service.run
	service.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == service.emulator {
			return []byte("Isolate_02\n"), nil
		}
		return original(ctx, name, args...)
	}
	err := service.StartAndroidEmulator(context.Background(), "Isolate_02")
	if err == nil || !strings.Contains(err.Error(), "missing-emulator") || !strings.Contains(err.Error(), "Isolate_02-emulator.log") {
		t.Fatal("start failure hid the actual error or log path", err)
	}
}
