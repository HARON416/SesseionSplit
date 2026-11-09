package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestUnsavedConnectionCredentialsDoNotChangeProfiles(t *testing.T) {
	service, err := newProfileService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryCredentialStore{values: map[string]string{"saved": "old-secret", "other-profile": "unrelated-secret"}}
	service.credentials = store
	profile, err := service.CreateProfile("Work")
	if err != nil {
		t.Fatal(err)
	}
	service.profiles[0].Settings.Proxy = ProxySettings{Mode: "http", Host: "saved.example", Port: 80, CredentialID: "saved", HasPassword: true}
	if err := service.saveProfiles(service.profiles); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(service.root, "profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	saved := service.ListProfiles()
	for _, test := range []struct{ action, password, want string }{
		{"keep", "", "old-secret"}, {"replace", "new-secret", "new-secret"}, {"remove", "", ""},
	} {
		t.Run(test.action, func(t *testing.T) {
			draft := ProxySettings{Mode: "socks5", Host: "draft.example", Port: 1080, Username: "draft-user", CredentialID: "other-profile", HasPassword: true}
			settings, credentials, err := service.prepareConnectionTest(profile.ID, SettingsUpdate{Settings: ProfileSettings{Proxy: draft}, Password: test.password, PasswordAction: test.action})
			if err != nil {
				t.Fatal(err)
			}
			password, err := proxyPassword(settings, credentials)
			if err != nil || password != test.want || settings.Host != draft.Host || settings.Port != draft.Port || settings.Username != draft.Username {
				t.Fatal("connection test used different inputs or another profile's credentials", settings, err)
			}
			if !reflect.DeepEqual(service.ListProfiles(), saved) || len(store.values) != 2 || store.values["saved"] != "old-secret" {
				t.Fatal("connection test changed saved settings or keyring credentials")
			}
			after, err := os.ReadFile(filepath.Join(service.root, "profiles.json"))
			if err != nil || string(after) != string(before) {
				t.Fatal("connection test changed the profile registry", err)
			}
		})
	}
	for _, update := range []SettingsUpdate{
		{Settings: ProfileSettings{Proxy: ProxySettings{Mode: "ftp"}}},
		{PasswordAction: "unknown"}, {PasswordAction: "replace"}, {PasswordAction: "replace", Password: strings.Repeat("x", 256)},
	} {
		if _, _, err := service.prepareConnectionTest(profile.ID, update); err == nil {
			t.Fatal("invalid connection draft accepted")
		}
	}
	if _, _, err := service.prepareConnectionTest("missing", SettingsUpdate{}); err == nil {
		t.Fatal("unknown profile accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.TestProfileConnection(ctx, profile.ID, SettingsUpdate{}); err == nil {
		t.Fatal("canceled connection test succeeded")
	}
}

func TestConnectionDiagnosticReportsOnlyObservedRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credentials reached destination")
		}
		_, _ = w.Write([]byte(`{"ip":"203.0.113.9"}`))
	}))
	defer server.Close()
	result, err := testProxyConnection(context.Background(), ProxySettings{}, nil, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if result.IP != "203.0.113.9" || result.Route != "direct" || result.CheckedAt == "" {
		t.Fatalf("invalid diagnostic: %+v", result)
	}
	if !strings.Contains(result.DNSStatus, "Not verified") || !strings.Contains(result.WebRTCStatus, "Not verified") {
		t.Fatal("diagnostic overstated coverage")
	}
}

func TestConnectionDiagnosticRejectsInvalidResponsesAndCancellation(t *testing.T) {
	for _, body := range []string{`{"ip":"not-an-ip"}`, `<html>private information</html>`, strings.Repeat("x", 5000)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		_, err := testProxyConnection(context.Background(), ProxySettings{}, nil, server.URL)
		server.Close()
		if err == nil || strings.Contains(err.Error(), body) {
			t.Fatal("invalid diagnostic response was accepted or exposed")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := testProxyConnection(ctx, ProxySettings{}, nil, "https://example.invalid"); err == nil {
		t.Fatal("canceled diagnostic succeeded")
	}
}

func TestProfileSettingsStateAndMissingProfile(t *testing.T) {
	s, err := newProfileService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateProfile("Work")
	if err != nil {
		t.Fatal(err)
	}
	state, err := s.GetProfileSettings(p.ID)
	if err != nil || state.Running || state.Settings != p.Settings {
		t.Fatal("wrong profile settings", err)
	}
	if err = s.CloseProfile(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetProfileSettings("missing"); err == nil {
		t.Fatal("missing profile accepted")
	}
	if err = s.CloseProfile("missing"); err == nil {
		t.Fatal("missing profile closed")
	}
}
