package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type memoryCredentialStore struct {
	values map[string]string
	fail   bool
}

func (m *memoryCredentialStore) Get(key string) (string, error) {
	v, ok := m.values[key]
	if !ok {
		return "", errors.New("missing")
	}
	return v, nil
}
func (m *memoryCredentialStore) Set(key, value string) error {
	if m.fail {
		return errors.New("locked")
	}
	m.values[key] = value
	return nil
}
func (m *memoryCredentialStore) Delete(key string) error { delete(m.values, key); return nil }
func TestProfileSettingsCredentialsTransactions(t *testing.T) {
	root := t.TempDir()
	s, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryCredentialStore{values: map[string]string{}}
	s.credentials = store
	p, err := s.CreateProfile("Work")
	if err != nil {
		t.Fatal(err)
	}
	settings := ProfileSettings{Proxy: ProxySettings{Mode: "http", Host: "localhost", Port: 8080, Username: "user", CredentialID: "untrusted", HasPassword: true}, Engine: "brave", Language: "en-US", Timezone: "Africa/Nairobi", Width: 1200, Height: 800, Permissions: "ask"}
	saved, err := s.UpdateProfileSettings(p.ID, SettingsUpdate{Settings: settings, Password: "secret", PasswordAction: "replace"})
	if err != nil {
		t.Fatal(err)
	}
	firstID := saved.Settings.Proxy.CredentialID
	if saved.Browser != "Brave" {
		t.Fatal("profile browser label did not follow the selected engine")
	}
	if firstID == "" || firstID == "untrusted" || store.values[firstID] != "secret" || !saved.Settings.Proxy.HasPassword {
		t.Fatal("password reference not safely stored")
	}
	data, err := os.ReadFile(filepath.Join(root, "profiles.json"))
	if err != nil || strings.Contains(string(data), "secret") {
		t.Fatal("plaintext secret written to registry")
	}
	settings.Proxy.CredentialID = "spoofed"
	settings.Proxy.HasPassword = false
	kept, err := s.UpdateProfileSettings(p.ID, SettingsUpdate{Settings: settings, Password: "ignored", PasswordAction: "keep"})
	if err != nil || kept.Settings.Proxy.CredentialID != firstID || !kept.Settings.Proxy.HasPassword {
		t.Fatal("client changed credential reference", err)
	}
	reopened, err := newProfileService(root)
	if err != nil || reopened.ListProfiles()[0] != kept {
		t.Fatal("settings not persisted", err)
	}
	before := s.ListProfiles()
	registry := filepath.Join(root, "profiles.json")
	if err := os.Rename(registry, registry+".backup"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(registry, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateProfileSettings(p.ID, SettingsUpdate{Settings: settings, Password: "replacement", PasswordAction: "replace"}); err == nil {
		t.Fatal("blocked save succeeded")
	}
	if !reflect.DeepEqual(before, s.ListProfiles()) || len(store.values) != 1 || store.values[firstID] != "secret" {
		t.Fatal("failed save changed credentials or metadata")
	}
	if err := os.Remove(registry); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(registry+".backup", registry); err != nil {
		t.Fatal(err)
	}
	store.fail = true
	if _, err := s.UpdateProfileSettings(p.ID, SettingsUpdate{Settings: settings, Password: "new", PasswordAction: "replace"}); err == nil {
		t.Fatal("locked keyring accepted")
	}
	if !reflect.DeepEqual(before, s.ListProfiles()) {
		t.Fatal("keyring failure changed metadata")
	}
	store.fail = false
	replaced, err := s.UpdateProfileSettings(p.ID, SettingsUpdate{Settings: settings, Password: "replacement", PasswordAction: "replace"})
	if err != nil {
		t.Fatal(err)
	}
	if len(store.values) != 1 || store.values[firstID] != "" || store.values[replaced.Settings.Proxy.CredentialID] != "replacement" {
		t.Fatal("old password not cleaned after successful replacement")
	}
	removed, err := s.UpdateProfileSettings(p.ID, SettingsUpdate{Settings: settings, PasswordAction: "remove"})
	if err != nil || removed.Settings.Proxy.CredentialID != "" || removed.Settings.Proxy.HasPassword || len(store.values) != 0 {
		t.Fatal("password removal failed", err)
	}
	s.browsers[p.ID] = &profileBrowser{}
	if _, err := s.UpdateProfileSettings(p.ID, SettingsUpdate{Settings: settings}); err == nil || !strings.Contains(err.Error(), "Close this profile browser") {
		t.Fatal("running profile settings changed", err)
	}
	delete(s.browsers, p.ID)
	if _, err := s.UpdateProfileSettings("missing", SettingsUpdate{}); err == nil {
		t.Fatal("unknown profile accepted")
	}
	s.closed = true
	if _, err := s.UpdateProfileSettings(p.ID, SettingsUpdate{}); err == nil {
		t.Fatal("closed service accepted update")
	}
}
func TestValidateProfileSettings(t *testing.T) {
	for _, settings := range []ProfileSettings{
		{Proxy: ProxySettings{Mode: "ftp"}}, {Proxy: ProxySettings{Mode: "http", Host: "http://user:pass@proxy", Port: 80}}, {Proxy: ProxySettings{Mode: "socks5", Host: "proxy", Port: 0}}, {Engine: "other"}, {Language: "bad language"}, {Timezone: "MadeUp/Zone"}, {Width: 320, Height: 0}, {Width: 100, Height: 800}, {Permissions: "allow"},
	} {
		if err := validateProfileSettings(settings); err == nil {
			t.Fatalf("invalid settings accepted: %+v", settings)
		}
	}
	for _, settings := range []ProfileSettings{{}, {Proxy: ProxySettings{Mode: "direct"}}, {Proxy: ProxySettings{Mode: "https", Host: "::1", Port: 443}, Language: "zh-Hant-TW", Timezone: "UTC", Width: 320, Height: 200}} {
		if err := validateProfileSettings(settings); err != nil {
			t.Fatal(err)
		}
	}
}
