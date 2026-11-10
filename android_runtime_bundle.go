package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"isolate/internal/runtimebundle"
)

func installedRuntimeBundle() string {
	if (runtime.GOOS != "windows" && runtime.GOOS != "linux") || runtime.GOARCH != "amd64" {
		return ""
	}
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	candidates := []string{filepath.Join(filepath.Dir(executable), "runtime")}
	if runtime.GOOS == "linux" {
		candidates = append([]string{"/usr/lib/isolate/runtime"}, candidates...)
	}
	for _, directory := range candidates {
		if _, err = os.Stat(directory); err == nil {
			return directory
		}
	}
	return ""
}

// Copy from installation media into the writable runtime cache. Check the
// selected original artifact before using it, and never delete installer files.
func (setup *androidSetup) restoreBundled(ctx context.Context, name, destination string) (bool, error) {
	if setup.bundleDir == "" {
		return false, nil
	}
	manifest, err := runtimebundle.Load(setup.bundleDir)
	if err != nil {
		return false, fmt.Errorf("Bundled tools are invalid; reinstall Isolate: %w", err)
	}
	if setup.bundleTarget != "" && manifest.Target != setup.bundleTarget {
		return false, fmt.Errorf("Bundled tools target %s does not match this PC (%s)", manifest.Target, setup.bundleTarget)
	}
	entry, ok := manifest.Entry(name)
	if !ok {
		return false, fmt.Errorf("Bundled tool %s is missing", name)
	}
	setup.activity("verifying", "Checking bundled "+name+" — no download needed")
	source := filepath.Join(setup.bundleDir, name)
	if err = validDownload(source, entry.SHA256); err != nil {
		return false, fmt.Errorf("Bundled tool %s is damaged; reinstall Isolate: %w", name, err)
	}
	input, err := os.Open(source)
	if err != nil {
		return false, err
	}
	defer input.Close()
	output, err := os.CreateTemp(filepath.Dir(destination), "bundled-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(output.Name())
	defer output.Close()
	buffer := make([]byte, 128*1024)
	for {
		if err = ctx.Err(); err != nil {
			return false, err
		}
		n, readErr := input.Read(buffer)
		if n > 0 {
			if _, err = output.Write(buffer[:n]); err != nil {
				return false, err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return false, readErr
		}
	}
	if err = output.Close(); err != nil {
		return false, err
	}
	if err = os.Rename(output.Name(), destination); err != nil {
		return false, err
	}
	_, _ = setup.Write([]byte("\nUsing bundled " + name + " from the Isolate installer.\n"))
	return true, nil
}
