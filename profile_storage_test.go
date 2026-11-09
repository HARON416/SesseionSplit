package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultProfileRoot(t *testing.T) {
	tests := []struct {
		name        string
		directories []string
		want        string
	}{
		{name: "fresh installation", want: "isolate"},
		{name: "legacy installation", directories: []string{"sessionsplit"}, want: "sessionsplit"},
		{name: "renamed installation", directories: []string{"isolate"}, want: "isolate"},
		{name: "both installations", directories: []string{"isolate", "sessionsplit"}, want: "isolate"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configDir := t.TempDir()
			for _, directory := range test.directories {
				if err := os.Mkdir(filepath.Join(configDir, directory), 0700); err != nil {
					t.Fatal(err)
				}
			}
			got, err := defaultProfileRoot(configDir)
			if err != nil || got != filepath.Join(configDir, test.want) {
				t.Fatalf("got %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestDefaultProfileRootRejectsNonDirectory(t *testing.T) {
	for _, name := range []string{"isolate", "sessionsplit"} {
		t.Run(name, func(t *testing.T) {
			configDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(configDir, name), []byte("preserve me"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := defaultProfileRoot(configDir); err == nil {
				t.Fatal("invalid storage location was ignored")
			}
		})
	}
}

func TestLegacyProfilesReopenAfterRename(t *testing.T) {
	configDir := t.TempDir()
	legacyRoot := filepath.Join(configDir, "sessionsplit")
	legacy, err := newProfileService(legacyRoot)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := legacy.CreateProfile("Existing account")
	if err != nil {
		t.Fatal(err)
	}
	sessionPath := filepath.Join(legacyRoot, "profiles", profile.ID, "browser", "session-marker")
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionPath, []byte("saved login"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := defaultProfileRoot(configDir)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	profiles := reopened.ListProfiles()
	if len(profiles) != 1 || profiles[0] != profile {
		t.Fatalf("saved profile changed after rename: %+v", profiles)
	}
	if data, err := os.ReadFile(sessionPath); err != nil || string(data) != "saved login" {
		t.Fatalf("saved browser data changed: %q, %v", data, err)
	}
	if reopened.credentials.(osCredentialStore).service != legacy.credentials.(osCredentialStore).service {
		t.Fatal("legacy credential namespace changed after rename")
	}
}

func TestCredentialNamespacePreservesLegacyProfiles(t *testing.T) {
	configDir := t.TempDir()
	for directory, name := range map[string]string{"isolate": "Isolate", "sessionsplit": "SessionSplit"} {
		root := filepath.Join(configDir, directory)
		store := newOSCredentialStore(root).(osCredentialStore)
		want := fmt.Sprintf("%s-%x", name, sha256.Sum256([]byte(root)))
		if store.service != want {
			t.Fatalf("credential namespace for %s: got %q, want %q", directory, store.service, want)
		}
	}
}
