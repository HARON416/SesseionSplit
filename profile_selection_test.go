package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCreateProfileWithBrowser(t *testing.T) {
	service, err := newProfileService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, engine := range []string{"firefox", "edge", "brave"} {
		profile, err := service.CreateProfileWithBrowser(engine, engine, "ignored")
		if err != nil {
			t.Fatal(err)
		}
		if profile.Settings.Engine != engine || profile.Settings.BrowserPath != "" || profile.Browser != browserLabelForEngine(engine) {
			t.Fatalf("selected browser was not stored: %+v", profile)
		}
	}
	before := service.ListProfiles()
	for _, engine := range []string{"unsupported", "custom"} {
		if _, err := service.CreateProfileWithBrowser("Invalid", engine, "relative-path"); err == nil {
			t.Fatal("invalid browser selection was saved")
		}
	}
	reopened, err := newProfileService(service.root)
	if err != nil || !reflect.DeepEqual(reopened.ListProfiles(), before) {
		t.Fatal("creation failed to persist atomically", err)
	}
}

func TestUpdateProfileBrowserPreservesSettingsAndData(t *testing.T) {
	service, err := newProfileService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service.credentials = &memoryCredentialStore{values: map[string]string{}}
	profile, err := service.CreateProfileWithBrowser("Work", "firefox", "")
	if err != nil {
		t.Fatal(err)
	}
	settings := profile.Settings
	settings.Language = "en-US"
	settings.Timezone = "Africa/Nairobi"
	settings.Proxy = ProxySettings{Mode: "http", Host: "localhost", Port: 8123}
	profile, err = service.UpdateProfileSettings(profile.ID, SettingsUpdate{Settings: settings, Password: "test-secret", PasswordAction: "replace"})
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(service.root, "profiles", profile.ID, "firefox", "browser", "session-marker")
	if err := os.MkdirAll(filepath.Dir(marker), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("saved session"), 0600); err != nil {
		t.Fatal(err)
	}
	want := profile
	want.Name = "Client"
	want.Browser = "Microsoft Edge"
	want.Settings.Engine = "edge"
	updated, err := service.UpdateProfile(profile.ID, " Client ", "edge", "ignored")
	if err != nil || updated != want {
		t.Fatalf("browser change modified unrelated data: %+v, %v", updated, err)
	}
	if password, err := service.credentials.Get(profile.Settings.Proxy.CredentialID); err != nil || password != "test-secret" {
		t.Fatal("browser change affected saved credentials", err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "saved session" {
		t.Fatal("browser change affected saved browser data", err)
	}
	reopened, err := newProfileService(service.root)
	if err != nil || !reflect.DeepEqual(reopened.ListProfiles(), []BrowserProfile{want}) {
		t.Fatal("profile edit did not persist", err)
	}
}

func TestUpdateProfileRejectsBrowserChangesAtomically(t *testing.T) {
	service, err := newProfileService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	profile, err := service.CreateProfile("Work")
	if err != nil {
		t.Fatal(err)
	}
	registry := filepath.Join(service.root, "profiles.json")
	before, err := os.ReadFile(registry)
	if err != nil {
		t.Fatal(err)
	}
	service.browsers[profile.ID] = &profileBrowser{}
	if _, err := service.UpdateProfile(profile.ID, "Changed", "firefox", ""); err == nil || err.Error() != "Close this profile browser before changing browsers" {
		t.Fatal("running browser change was not blocked", err)
	}
	delete(service.browsers, profile.ID)
	for _, engine := range []string{"unsupported", "custom"} {
		if _, err := service.UpdateProfile(profile.ID, "Changed", engine, "missing"); err == nil {
			t.Fatal("invalid browser change was not blocked")
		}
	}
	if data, err := os.ReadFile(registry); err != nil || string(data) != string(before) || service.ListProfiles()[0] != profile {
		t.Fatal("failed browser edit partially renamed the profile", err)
	}
	service.browsers[profile.ID] = &profileBrowser{}
	updated, err := service.UpdateProfile(profile.ID, "Renamed", "chrome", "")
	if err != nil || updated.Name != "Renamed" || updated.Settings != profile.Settings {
		t.Fatal("name-only edit should work while browser is running", err)
	}
}

func TestCustomBrowserSelection(t *testing.T) {
	service, err := newProfileService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	profile, err := service.CreateProfileWithBrowser("Portable", "custom", " "+executable+" ")
	if err != nil || profile.Settings.BrowserPath != executable {
		t.Fatal("custom executable was not saved", err)
	}
	updated, err := service.UpdateProfile(profile.ID, "Portable Firefox", "firefox", executable)
	if err != nil || updated.Settings.BrowserPath != "" || updated.Settings.Engine != "firefox" {
		t.Fatal("switching from a custom browser retained its executable", err)
	}
}
