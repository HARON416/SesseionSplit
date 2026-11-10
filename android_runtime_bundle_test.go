package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"isolate/internal/runtimebundle"
)

func bundleFixture(t *testing.T) string { return bundleFixtureForTarget(t, "windows/amd64") }
func bundleFixtureForTarget(t *testing.T, target string) string {
	t.Helper()
	root := t.TempDir()
	manifest := runtimebundle.Manifest{Version: 1, Target: target, JavaRelease: "jdk-21-test"}
	javaArchive, err := runtimebundle.JavaArchive(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{javaArchive, "java-sources.tar.gz", "scrcpy-server-v" + scrcpyVersion, "scrcpy-sources.tar.gz", "THIRD_PARTY_NOTICES.txt"} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
		hash, err := runtimebundle.Hash(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(name, "scrcpy-server") {
			hash = scrcpySHA256
		}
		manifest.Files = append(manifest.Files, runtimebundle.File{Name: name, SHA256: hash})
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return root
}
func TestBundledJavaUsesLocalVerifiedArtifact(t *testing.T) {
	root := bundleFixture(t)
	setup := &androidSetup{bundleDir: root}
	destination := filepath.Join(t.TempDir(), "java.download")
	copied, err := setup.restoreBundled(context.Background(), "java.zip", destination)
	if err != nil || !copied {
		t.Fatal(copied, err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "java.zip" {
		t.Fatal("bundled Java was not restored", err)
	}
	if _, err = os.Stat(filepath.Join(root, "java.zip")); err != nil {
		t.Fatal("installer source was removed", err)
	}
	if setup.status.Phase != "verifying" || !strings.Contains(setup.status.Log, "Using bundled") {
		t.Fatal("local preparation not surfaced")
	}
	if err = os.WriteFile(filepath.Join(root, "java.zip"), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = setup.restoreBundled(context.Background(), "java.zip", destination); err == nil {
		t.Fatal("corrupt bundle used")
	}
}
func TestBundledToolsRejectForbiddenFilesAndWrongTarget(t *testing.T) {
	for _, test := range []string{"image", "missing-sources", "target", "traversal"} {
		t.Run(test, func(t *testing.T) {
			root := bundleFixture(t)
			manifest, err := runtimebundle.Load(root)
			if err != nil {
				t.Fatal(err)
			}
			switch test {
			case "image":
				manifest.Files = append(manifest.Files, runtimebundle.File{Name: "google-play-image.zip", SHA256: scrcpySHA256})
			case "missing-sources":
				manifest.Files = manifest.Files[1:]
			case "target":
				manifest.Target = "windows/arm64"
			case "traversal":
				manifest.Files[0].Name = "../java.zip"
			}
			data, _ := json.Marshal(manifest)
			_ = os.WriteFile(filepath.Join(root, "manifest.json"), data, 0600)
			if _, err = runtimebundle.Load(root); err == nil {
				t.Fatal("invalid bundle accepted")
			}
		})
	}
	setup := &androidSetup{}
	if used, err := setup.restoreBundled(context.Background(), "java.zip", filepath.Join(t.TempDir(), "java")); used || err != nil {
		t.Fatal("standard installer tried to use a bundle")
	}
}

// Opt-in check of the real payload assembled for the installer. It does not
// execute Windows binaries, install Android, or touch the user's runtime data.
func TestPreparedRuntimeBundle(t *testing.T) {
	root := os.Getenv("ISOLATE_RUNTIME_BUNDLE_DIR")
	if root == "" {
		t.Skip("set ISOLATE_RUNTIME_BUNDLE_DIR to verify a prepared installer payload")
	}
	manifest, err := runtimebundle.Verify(root)
	if err != nil {
		t.Fatal(err)
	}
	archiveName, err := runtimebundle.JavaArchive(manifest.Target)
	if err != nil {
		t.Fatal(err)
	}
	setup := &androidSetup{bundleDir: root, bundleTarget: manifest.Target}
	destination := t.TempDir()
	archive := filepath.Join(destination, archiveName)
	if copied, err := setup.restoreBundled(context.Background(), archiveName, archive); err != nil || !copied {
		t.Fatal(copied, err)
	}
	extracted := filepath.Join(destination, "java")
	if err := extractRuntime(archive, extracted, strings.HasSuffix(archiveName, ".zip")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(extracted)
	if err != nil || len(entries) != 1 {
		t.Fatal("unexpected Java archive layout", err)
	}
	javaName := "java"
	if manifest.Target == "windows/amd64" {
		javaName += ".exe"
	}
	for _, name := range []string{"bin/" + javaName, "legal/java.base/LICENSE"} {
		if _, err = os.Stat(filepath.Join(extracted, entries[0].Name(), filepath.FromSlash(name))); err != nil {
			t.Fatal("Java executable or upstream license is missing", name, err)
		}
	}
	if manifest.Target == runtime.GOOS+"/"+runtime.GOARCH {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		java := filepath.Join(extracted, entries[0].Name(), "bin", javaName)
		output, err := exec.CommandContext(ctx, java, "-version").CombinedOutput()
		if err != nil || !strings.Contains(string(output), "21.") {
			t.Fatalf("bundled native Java failed: %s, %v", output, err)
		}
	}
	server := filepath.Join(destination, "scrcpy-server-v"+scrcpyVersion)
	if copied, err := setup.restoreBundled(context.Background(), filepath.Base(server), server); err != nil || !copied {
		t.Fatal(copied, err)
	}
	if err = validDownload(server, scrcpySHA256); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxBundleRejectsWindowsPayload(t *testing.T) {
	root := bundleFixtureForTarget(t, "linux/amd64")
	setup := &androidSetup{bundleDir: root, bundleTarget: "linux/amd64"}
	destination := filepath.Join(t.TempDir(), "java.download")
	if copied, err := setup.restoreBundled(context.Background(), "java.tar.gz", destination); err != nil || !copied {
		t.Fatal(copied, err)
	}
	setup.bundleTarget = "windows/amd64"
	if _, err := setup.restoreBundled(context.Background(), "java.tar.gz", destination); err == nil {
		t.Fatal("wrong OS runtime was accepted")
	}
}
