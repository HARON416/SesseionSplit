package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFingerprintIdentitySurvivesProfileChangesAndRestarts(t *testing.T) {
	root := t.TempDir()
	s, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.CreateProfile("First")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateProfile("Second")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Fingerprint.validate(); err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint == second.Fingerprint {
		t.Fatal("profiles share a fingerprint seed")
	}
	renamed, err := s.RenameProfile(first.ID, "Renamed")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.UpdateProfileSettings(first.ID, SettingsUpdate{
		Settings: ProfileSettings{Engine: "brave", Language: "fr-FR", Timezone: "Africa/Nairobi"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Fingerprint != first.Fingerprint || updated.Fingerprint != first.Fingerprint {
		t.Fatal("profile edits regenerated its fingerprint identity")
	}
	reopened, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.ListProfiles()[1].Fingerprint != first.Fingerprint {
		t.Fatal("restart regenerated the fingerprint identity")
	}
}

func TestLegacyFingerprintMigrationPreservesDataAndIsStable(t *testing.T) {
	root := t.TempDir()
	registry := filepath.Join(root, "profiles.json")
	legacy := `[{"id":"0123456789abcdef0123456789abcdef","name":"Legacy","browser":"Chrome","lastOpenedAt":"2026-11-07T10:00:00Z"}]`
	if err := os.WriteFile(registry, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	profile := s.ListProfiles()[0]
	if profile.Name != "Legacy" || profile.CreatedAt != "" || profile.LastOpenedAt != "2026-11-07T10:00:00Z" {
		t.Fatal("migration changed existing metadata")
	}
	if err := profile.Fingerprint.validate(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(registry)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(registry)
	if err != nil || string(after) != string(before) || reopened.ListProfiles()[0] != profile {
		t.Fatal("migration repeated or identity changed on restart", err)
	}
}

func TestInvalidFingerprintIdentityPreservesRegistry(t *testing.T) {
	for _, identity := range []FingerprintIdentity{
		{SchemaVersion: 2, Seed: strings.Repeat("a", 64)},
		{SchemaVersion: 1, Seed: strings.Repeat("A", 64)},
		{SchemaVersion: 1, Seed: strings.Repeat("a", 63)},
		{Seed: strings.Repeat("a", 64)},
	} {
		root := t.TempDir()
		registry := filepath.Join(root, "profiles.json")
		profiles := []map[string]any{
			{"id": strings.Repeat("a", 32), "name": "Legacy"},
			{"id": strings.Repeat("b", 32), "name": "Invalid", "fingerprint": identity},
		}
		before, err := json.Marshal(profiles)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(registry, before, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := newProfileService(root); err == nil {
			t.Fatal("invalid or unsupported fingerprint identity accepted")
		}
		after, err := os.ReadFile(registry)
		if err != nil || string(after) != string(before) {
			t.Fatal("invalid registry was overwritten during migration", err)
		}
	}
}

func TestExplicitInvalidFingerprintIsNotRegeneratedAsLegacy(t *testing.T) {
	for _, identity := range []string{`null`, `{}`, `{"schemaVersion":0,"seed":""}`, `{"schemaVersion":"1"}`} {
		root := t.TempDir()
		registry := filepath.Join(root, "profiles.json")
		before := `[{"id":"0123456789abcdef0123456789abcdef","name":"Invalid","fingerprint":` + identity + `}]`
		if err := os.WriteFile(registry, []byte(before), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := newProfileService(root); err == nil {
			t.Fatalf("explicit invalid identity was regenerated: %s", identity)
		}
		after, err := os.ReadFile(registry)
		if err != nil || string(after) != before {
			t.Fatal("invalid fingerprint was overwritten", err)
		}
	}
}

func TestFailedFingerprintMigrationDoesNotPublishUnsavedSeeds(t *testing.T) {
	root := t.TempDir()
	// A directory at the registry destination prevents the atomic save.
	if err := os.Mkdir(filepath.Join(root, "profiles.json"), 0700); err != nil {
		t.Fatal(err)
	}
	legacy := BrowserProfile{ID: strings.Repeat("a", 32), Name: "Legacy"}
	s := &ProfileService{root: root, profiles: []BrowserProfile{legacy}}
	if err := s.migrateFingerprintIdentities(); err == nil {
		t.Fatal("migration succeeded without saving its seed")
	}
	if s.profiles[0] != legacy {
		t.Fatal("failed migration changed the in-memory profile")
	}
}
