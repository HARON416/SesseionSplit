package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBrowserCatalogResolutionAndStorageIsolation(t *testing.T) {
	root := t.TempDir()
	seen := map[string]bool{}
	for _, browser := range browserCatalog {
		t.Run(browser.id, func(t *testing.T) {
			if err := validateProfileSettings(ProfileSettings{Engine: browser.id}); browser.id != "custom" && err != nil {
				t.Fatal(err)
			}
			engine, err := resolveProfileEngine(ProfileSettings{Engine: browser.id}, "/test/browser")
			if err != nil || engine.Info().ID != browser.id || engine.Info().Name != browser.name {
				t.Fatal("incorrect engine selection", engine, err)
			}
			data, cache, downloads := engineProfilePaths(root, "profile", browser.id)
			if seen[data] || cache == data || downloads != filepath.Join(root, "profiles", "profile", "downloads") {
				t.Fatal("engines must have separate data and caches, with shared profile downloads")
			}
			seen[data] = true
			if browser.id == "chrome" && data != filepath.Join(root, "profiles", "profile", "browser") {
				t.Fatal("legacy Chrome storage moved")
			}
			if browser.id == "brave" && data != filepath.Join(root, "profiles", "profile", "brave", "browser") {
				t.Fatal("legacy Brave storage moved")
			}
		})
	}
	if findEnginePath("unknown") != "" {
		t.Fatal("unknown browser substituted another engine")
	}
}

func TestBrowserCandidatesCoverPlatformInstallations(t *testing.T) {
	getenv := func(key string) string { return filepath.Join("test", key) }
	for _, browser := range browserCatalog {
		if browser.id == "custom" || browser.id == "chrome" {
			continue
		}
		for _, platform := range []string{"windows", "darwin", "linux"} {
			paths := browserCandidates(browser.id, platform, getenv, "/home/test")
			if len(paths) == 0 {
				t.Fatalf("no %s candidates for %s", platform, browser.id)
			}
			if platform == "windows" {
				want := filepath.Join("test", "LOCALAPPDATA", filepath.FromSlash(browser.windows[0]))
				if !containsCandidate(paths, want) {
					t.Fatalf("missing per-user install for %s", browser.id)
				}
				for _, path := range paths {
					if path == "chrome.exe" || path == "opera.exe" {
						t.Fatal("ambiguous executable could substitute another browser", path)
					}
				}
			}
			if platform == "darwin" && !containsCandidate(paths, filepath.Join("/home/test", "Applications", browser.mac)) {
				t.Fatalf("missing per-user macOS install for %s", browser.id)
			}
		}
	}
}

func containsCandidate(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}

func TestCustomBrowserValidationPersistenceAndStorage(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "portable browser.exe")
	if err := os.WriteFile(executable, []byte("browser fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	settings := ProfileSettings{Engine: "custom", BrowserPath: executable}
	engine, err := resolveProfileEngine(settings, "")
	if err != nil || engine.(chromiumEngine).path != executable {
		t.Fatal("custom executable was not selected", err)
	}
	service, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := service.CreateProfile("Portable")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateProfileSettings(profile.ID, SettingsUpdate{Settings: settings}); err != nil {
		t.Fatal(err)
	}
	reopened, err := newProfileService(root)
	if err != nil || reopened.ListProfiles()[0].Settings != settings {
		t.Fatal("custom browser selection did not persist", err)
	}
	first := engineStorageID("custom", executable)
	second := engineStorageID("custom", filepath.Join(root, "another-browser.exe"))
	if first == second || first != engineStorageID("custom", executable) || !strings.HasPrefix(first, "custom-") {
		t.Fatal("custom browser storage must be distinct and stable")
	}
	for _, path := range []string{"", "relative-browser", root, filepath.Join(root, "missing.exe"), executable + " --flag"} {
		if err := validateProfileSettings(ProfileSettings{Engine: "custom", BrowserPath: path}); err == nil {
			t.Fatal("invalid executable accepted", path)
		}
	}
	if runtime.GOOS != "windows" {
		alias := filepath.Join(root, "browser-alias")
		if err := os.Symlink(executable, alias); err != nil {
			t.Fatal(err)
		}
		if engineStorageID("custom", alias) == first {
			t.Fatal("launcher aliases must not share storage")
		}
		if err := os.Chmod(executable, 0600); err != nil {
			t.Fatal(err)
		}
		if err := validateProfileSettings(settings); err == nil {
			t.Fatal("non-executable browser accepted")
		}
	}
}
