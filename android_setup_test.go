package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRuntimeArchiveTraversalAndLinks(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "C:/absolute", "nested/../../escape", "..\\escape"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			archive := filepath.Join(root, "test.zip")
			var data bytes.Buffer
			writer := zip.NewWriter(&data)
			entry, err := writer.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = entry.Write([]byte("bad"))
			_ = writer.Close()
			_ = os.WriteFile(archive, data.Bytes(), 0600)
			if err = extractRuntime(archive, filepath.Join(root, "unpacked"), true); err == nil {
				t.Fatal("unsafe archive path accepted")
			}
		})
	}
	if runtime.GOOS == "windows" {
		t.Skip("tar symlinks are used only by the Linux runtime")
	}
	root := t.TempDir()
	archive := filepath.Join(root, "runtime.tar.gz")
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	writer := tar.NewWriter(gz)
	_ = writer.WriteHeader(&tar.Header{Name: "java/bin/java", Mode: 0755, Size: 4})
	_, _ = writer.Write([]byte("java"))
	_ = writer.WriteHeader(&tar.Header{Name: "java/bin/link", Typeflag: tar.TypeSymlink, Linkname: "java"})
	_ = writer.Close()
	_ = gz.Close()
	_ = os.WriteFile(archive, data.Bytes(), 0600)
	destination := filepath.Join(root, "unpacked")
	if err := extractRuntime(archive, destination, false); err != nil {
		t.Fatal(err)
	}
	result, err := os.ReadFile(filepath.Join(destination, "java/bin/link"))
	if err != nil || string(result) != "java" {
		t.Fatal("safe JRE link did not extract", err)
	}
}
func TestRuntimeChecksum(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime")
	data := []byte("verified bytes")
	_ = os.WriteFile(path, data, 0600)
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	if err := validDownload(path, hash); err != nil {
		t.Fatal(err)
	}
	if err := validDownload(path, strings.Repeat("0", 64)); err == nil {
		t.Fatal("modified runtime accepted")
	}
}

type licenseInput struct{ bytes.Buffer }

func (input *licenseInput) Close() error { return nil }
func TestSetupLicensePromptsRequireExplicitResponse(t *testing.T) {
	input := &licenseInput{}
	setup := &androidSetup{status: AndroidSetupStatus{Running: true}, input: input}
	service := &AndroidService{setup: setup}
	_, _ = setup.Write([]byte("Full license text\nAccept? (y/"))
	if setup.status.WaitingLicense {
		t.Fatal("partial prompt accepted")
	}
	_, _ = setup.Write([]byte("N): "))
	if !setup.status.WaitingLicense || input.Len() != 0 {
		t.Fatal("license auto accepted or prompt missing")
	}
	if err := service.RespondAndroidLicense(true); err != nil {
		t.Fatal(err)
	}
	if input.String() != "y\n" || setup.status.WaitingLicense {
		t.Fatal("accept response was not sent")
	}
	_, _ = setup.Write([]byte("Next license text"))
	if setup.status.WaitingLicense {
		t.Fatal("previous prompt was reused")
	}
	_, _ = setup.Write([]byte("Accept? (y/N): "))
	if err := service.RespondAndroidLicense(false); err != nil {
		t.Fatal(err)
	}
	if input.String() != "y\nn\n" {
		t.Fatal("decline response was not sent")
	}
	if err := service.RespondAndroidLicense(true); err == nil {
		t.Fatal("unsolicited acceptance was allowed")
	}
}
func TestRuntimeArchiveRejectsSymlinkParent(t *testing.T) {
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	writer := tar.NewWriter(gz)
	_ = writer.WriteHeader(&tar.Header{Name: "java/link", Typeflag: tar.TypeSymlink, Linkname: "../../outside"})
	_ = writer.WriteHeader(&tar.Header{Name: "java/link/entry", Mode: 0644, Size: 1})
	_, _ = io.WriteString(writer, "x")
	_ = writer.Close()
	_ = gz.Close()
	root := t.TempDir()
	path := filepath.Join(root, "archive")
	_ = os.WriteFile(path, data.Bytes(), 0600)
	if err := extractRuntime(path, filepath.Join(root, "unpacked"), false); err == nil {
		t.Fatal("symlink parent was accepted")
	}
}

// Optional download smoke test: does not install an SDK, accept licenses, or
// create devices. It checks the exact release artifacts used by client setup.
func TestAndroidRuntimeDownloads(t *testing.T) {
	if os.Getenv("ISOLATE_ANDROID_SETUP_TESTS") != "1" {
		t.Skip("set ISOLATE_ANDROID_SETUP_TESTS=1 to check official runtime downloads")
	}
	timeout := 5 * time.Minute
	if os.Getenv("ISOLATE_ANDROID_FULL_DOWNLOAD_TESTS") == "1" {
		timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	setup := &androidSetup{}
	root := t.TempDir()
	var assets []struct {
		Binary struct {
			Package struct {
				Link     string `json:"link"`
				Checksum string `json:"checksum"`
			} `json:"package"`
		} `json:"binary"`
	}
	for _, platform := range []string{"linux", "windows"} {
		if err := downloadJSON(ctx, "https://api.adoptium.net/v3/assets/latest/21/hotspot?architecture=x64&image_type=jre&os="+platform+"&vendor=eclipse", &assets); err != nil || len(assets) == 0 {
			t.Fatal("Java metadata", platform, err)
		}
		if len(assets[0].Binary.Package.Checksum) != 64 || !strings.HasPrefix(assets[0].Binary.Package.Link, "https://github.com/adoptium/") {
			t.Fatal("invalid Java runtime metadata", platform)
		}
	}
	if err := setup.download(ctx, "https://github.com/Genymobile/scrcpy/releases/download/v"+scrcpyVersion+"/scrcpy-server-v"+scrcpyVersion, scrcpySHA256, filepath.Join(root, "scrcpy-server")); err != nil {
		t.Fatal(err)
	}
	for _, platform := range []struct{ name, hash string }{{"linux", "4e4c464f145a7512b57d088ac6c278c03c9eea610886b35a5e0804e74eedf583"}, {"win", "90ae805d20434428bffcb699c290860f19bb5f66a67e6b330067e3de801fb04a"}} {
		if os.Getenv("ISOLATE_ANDROID_FULL_DOWNLOAD_TESTS") == "1" {
			destination := filepath.Join(root, platform.name+".zip")
			if err := setup.download(ctx, "https://dl.google.com/android/repository/commandlinetools-"+platform.name+"-15859902_latest.zip", platform.hash, destination); err != nil {
				t.Fatal(platform.name, err)
			}
			continue
		}
		// Read the ZIP central directory with a range request. This checks the exact
		// archive and Java launchers without downloading hundreds of MB per test.
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://dl.google.com/android/repository/commandlinetools-"+platform.name+"-15859902_latest.zip", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Range", "bytes=-65536")
		response, err := (&http.Client{Timeout: 60 * time.Second}).Do(request)
		if err != nil {
			t.Fatal(err)
		}
		directory, readErr := io.ReadAll(io.LimitReader(response.Body, 128*1024))
		_ = response.Body.Close()
		if readErr != nil || response.StatusCode != http.StatusPartialContent {
			t.Fatalf("%s archive range: HTTP %d, %v", platform.name, response.StatusCode, readErr)
		}
		for _, tool := range []string{"sdkmanager", "avdmanager"} {
			if !bytes.Contains(directory, []byte("cmdline-tools/lib/"+tool+"-classpath.jar")) {
				t.Fatal("Java launcher classpath missing", platform.name, tool)
			}
		}
	}
}

func TestSetupCommandLicenseAndCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process fixture; Windows is cross-compiled separately")
	}
	for _, cancelPrompt := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelPrompt), func(t *testing.T) {
			root := t.TempDir()
			script := filepath.Join(root, "license-fixture")
			if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'License text\\nAccept? (y/N): '\nread answer\nif [ \"$answer\" = y ]; then exit 0; fi\nexit 1\n"), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			setup := &androidSetup{status: AndroidSetupStatus{Running: true}, cancel: cancel}
			service := &AndroidService{setup: setup}
			result := make(chan error, 1)
			go func() { result <- setup.command(ctx, "", root, script, "") }()
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				if service.GetAndroidSetup().WaitingLicense {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !service.GetAndroidSetup().WaitingLicense {
				t.Fatal("child process prompt did not reach setup UI")
			}
			if cancelPrompt {
				service.CancelAndroidSetup()
			} else if err := service.RespondAndroidLicense(true); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-result:
				if cancelPrompt && (err == nil || !strings.Contains(err.Error(), "cancelled")) {
					t.Fatal("cancel did not stop setup", err)
				}
				if !cancelPrompt && err != nil {
					t.Fatal("accepted license command failed", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("setup process did not exit")
			}
		})
	}
}
