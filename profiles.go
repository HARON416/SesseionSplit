package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

type BrowserProfile struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Browser      string          `json:"browser"`
	CreatedAt    string          `json:"createdAt"`
	LastOpenedAt string          `json:"lastOpenedAt"`
	Settings     ProfileSettings `json:"settings"`
}

type profileBrowser struct {
	browser    *rod.Browser
	launcher   *launcher.Launcher
	cancel     context.CancelFunc
	proxyClose func()
	firefox    *firefoxBrowser
}

type ProfileService struct {
	mu          sync.Mutex
	root        string
	profiles    []BrowserProfile
	browsers    map[string]*profileBrowser
	closed      bool
	credentials credentialStore
	// Overridden only by browser integration tests.
	chromePath string
	headless   bool
}

var profileIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func newProfileService(root string) (*ProfileService, error) {
	if root == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("find profile storage: %w", err)
		}
		root, err = defaultProfileRoot(dir)
		if err != nil {
			return nil, err
		}
	}
	s := &ProfileService{root: root, profiles: []BrowserProfile{}, browsers: map[string]*profileBrowser{}, credentials: newOSCredentialStore(root)}
	data, err := os.ReadFile(filepath.Join(root, "profiles.json"))
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read profiles: %w", err)
	}
	if err = json.Unmarshal(data, &s.profiles); err != nil {
		return nil, errors.New("Could not read saved profiles; your data has been preserved")
	}
	seen := map[string]bool{}
	for _, p := range s.profiles {
		if !profileIDPattern.MatchString(p.ID) || seen[p.ID] || strings.TrimSpace(p.Name) == "" {
			return nil, errors.New("Invalid saved profiles; your data has been preserved")
		}
		seen[p.ID] = true
	}
	if s.profiles == nil {
		s.profiles = []BrowserProfile{}
	}
	return s, nil
}

func (s *ProfileService) ListProfiles() []BrowserProfile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]BrowserProfile{}, s.profiles...)
}

func (s *ProfileService) CreateProfile(name string) (BrowserProfile, error) {
	return s.createProfile(name, ProfileSettings{})
}

func (s *ProfileService) CreateProfileWithBrowser(name, engine, browserPath string) (BrowserProfile, error) {
	return s.createProfile(name, profileBrowserSelection(engine, browserPath))
}

func (s *ProfileService) createProfile(name string, settings ProfileSettings) (BrowserProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return BrowserProfile{}, errors.New("Isolate is closing")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return BrowserProfile{}, errors.New("Enter a profile name")
	}
	if err := validateBrowserSelection(settings); err != nil {
		return BrowserProfile{}, err
	}
	for _, p := range s.profiles {
		if strings.EqualFold(p.Name, name) {
			return BrowserProfile{}, errors.New("A profile with this name already exists")
		}
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return BrowserProfile{}, fmt.Errorf("create profile ID: %w", err)
	}
	browser := browserLabelForEngine(settings.Engine)
	p := BrowserProfile{ID: hex.EncodeToString(id[:]), Name: name, Browser: browser, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Settings: settings}
	next := append([]BrowserProfile{p}, s.profiles...)
	if err := s.saveProfiles(next); err != nil {
		return BrowserProfile{}, err
	}
	s.profiles = next
	return p, nil
}

func (s *ProfileService) RenameProfile(id, name string) (BrowserProfile, error) {
	return s.updateProfile(id, name, nil)
}

func (s *ProfileService) UpdateProfile(id, name, engine, browserPath string) (BrowserProfile, error) {
	selection := profileBrowserSelection(engine, browserPath)
	return s.updateProfile(id, name, &selection)
}

func profileBrowserSelection(engine, browserPath string) ProfileSettings {
	if engine == "" {
		engine = "chrome"
	}
	if engine != "custom" {
		browserPath = ""
	}
	return ProfileSettings{Engine: engine, BrowserPath: strings.TrimSpace(browserPath)}
}

func (s *ProfileService) updateProfile(id, name string, selection *ProfileSettings) (BrowserProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return BrowserProfile{}, errors.New("Isolate is closing")
	}
	index := -1
	for i, profile := range s.profiles {
		if profile.ID == id {
			index = i
			break
		}
	}
	if index == -1 {
		return BrowserProfile{}, errors.New("Profile was not found")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return BrowserProfile{}, errors.New("Enter a profile name")
	}
	for _, profile := range s.profiles {
		if profile.ID != id && strings.EqualFold(profile.Name, name) {
			return BrowserProfile{}, errors.New("A profile with this name already exists")
		}
	}
	next := append([]BrowserProfile{}, s.profiles...)
	next[index].Name = name
	if selection != nil {
		current := profileBrowserSelection(next[index].Settings.Engine, next[index].Settings.BrowserPath)
		if current != *selection {
			if s.browsers[id] != nil {
				return BrowserProfile{}, errors.New("Close this profile browser before changing browsers")
			}
			if err := validateBrowserSelection(*selection); err != nil {
				return BrowserProfile{}, err
			}
			next[index].Settings.Engine = selection.Engine
			next[index].Settings.BrowserPath = selection.BrowserPath
			next[index].Browser = browserLabelForEngine(selection.Engine)
		}
	}
	if err := s.saveProfiles(next); err != nil {
		return BrowserProfile{}, err
	}
	s.profiles = next
	return next[index], nil
}

func (s *ProfileService) saveProfiles(profiles []BrowserProfile) error {
	data, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(s.root, 0700); err != nil {
		return fmt.Errorf("create profile storage: %w", err)
	}
	f, err := os.CreateTemp(s.root, ".profiles-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), filepath.Join(s.root, "profiles.json")); err != nil {
		return fmt.Errorf("save profile: %w", err)
	}
	return nil
}

func (s *ProfileService) DeleteProfile(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("Isolate is closing")
	}
	if !profileIDPattern.MatchString(id) {
		return errors.New("Profile was not found")
	}
	next := make([]BrowserProfile, 0, len(s.profiles))
	found := false
	for _, profile := range s.profiles {
		if profile.ID == id {
			found = true
		} else {
			next = append(next, profile)
		}
	}
	if !found {
		return errors.New("Profile was not found")
	}
	// Fail before touching browser data if the registry cannot be written.
	if err := s.saveProfiles(next); err != nil {
		return err
	}
	if live := s.browsers[id]; live != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := live.close(ctx); err != nil {
			live.kill()
		}
		cancel()
		live.cancel()
		if live.proxyClose != nil {
			live.proxyClose()
		}
		// Deletion intentionally waits for Chrome to exit before removing its data.
		if live.launcher != nil {
			live.launcher.Cleanup()
		}
		delete(s.browsers, id)
	}
	if err := os.RemoveAll(filepath.Join(s.root, "profiles", id)); err != nil {
		// Keep the profile addressable so incomplete removal can be retried.
		if restoreErr := s.saveProfiles(s.profiles); restoreErr != nil {
			s.profiles = next
			return fmt.Errorf("delete profile data: %w (restore profiles: %v)", err, restoreErr)
		}
		return fmt.Errorf("delete profile data: %w", err)
	}
	for _, profile := range s.profiles {
		if profile.ID == id && profile.Settings.Proxy.CredentialID != "" {
			store := s.credentials
			if store == nil {
				store = newOSCredentialStore(s.root)
			}
			if err := store.Delete(profile.Settings.Proxy.CredentialID); err != nil {
				log.Print("Profile deleted, but its unused proxy credential could not be removed from the system keyring")
			}
		}
	}
	s.profiles = next
	return nil
}

func (s *ProfileService) OpenProfile(ctx context.Context, id string) (BrowserProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.openProfile(ctx, id); err != nil {
		return BrowserProfile{}, err
	}
	return s.recordProfileOpened(id)
}

// recordProfileOpened is called with mu held, only after a successful open/focus.
func (s *ProfileService) recordProfileOpened(id string) (BrowserProfile, error) {
	next := append([]BrowserProfile{}, s.profiles...)
	for i := range next {
		if next[i].ID != id {
			continue
		}
		next[i].LastOpenedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := s.saveProfiles(next); err != nil {
			return BrowserProfile{}, fmt.Errorf("Browser opened, but the last-open time could not be saved: %w", err)
		}
		s.profiles = next
		return next[i], nil
	}
	return BrowserProfile{}, errors.New("Profile was not found")
}

// openProfile is called with mu held; timestamps are committed by OpenProfile.
func (s *ProfileService) openProfile(ctx context.Context, id string) error {
	if s.closed {
		return errors.New("Isolate is closing")
	}
	var profile BrowserProfile
	found := false
	for _, p := range s.profiles {
		if p.ID == id {
			profile = p
			found = true
			break
		}
	}
	if !found {
		return errors.New("Profile was not found")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if live := s.browsers[id]; live != nil {
		if err := live.activate(ctx); err == nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		live.cancel()
		live.kill()
		if live.proxyClose != nil {
			live.proxyClose()
		}
		delete(s.browsers, id)
	}
	if err := validateProfileSettings(profile.Settings); err != nil {
		return err
	}
	engine, err := resolveProfileEngine(profile.Settings, s.chromePath)
	if err != nil {
		return err
	}
	if s.credentials == nil {
		s.credentials = newOSCredentialStore(s.root)
	}
	proxyURL, closeProxy, err := startProfileProxy(context.Background(), profile.Settings.Proxy, s.credentials)
	if err != nil {
		return err
	}
	live, err := engine.Launch(ctx, launchConfig{Profile: profile, Settings: profile.Settings, Root: s.root, ProxyURL: proxyURL, Headless: s.headless, OnError: func(err error) {
		log.Printf("Could not apply browser settings for profile %s; affected page closed: %v", profile.ID, err)
	}})
	if err != nil {
		closeProxy()
		return err
	}
	live.proxyClose = closeProxy
	s.browsers[id] = live
	if err := live.activate(ctx); err != nil {
		live.cancel()
		live.kill()
		closeProxy()
		delete(s.browsers, id)
		return err
	}
	return nil
}

func activateProfileBrowser(ctx context.Context, browser *rod.Browser) error {
	// Use raw targets so Rod never caches a page attached to a short-lived request context.
	client := browser.Context(ctx)
	targets, err := (proto.TargetGetTargets{}).Call(client)
	if err != nil {
		return err
	}
	for _, target := range targets.TargetInfos {
		if target.Type == proto.TargetTargetInfoTypePage {
			return (proto.TargetActivateTarget{TargetID: target.TargetID}).Call(client)
		}
	}
	_, err = (proto.TargetCreateTarget{URL: "https://www.instagram.com/"}).Call(client)
	return err
}

func (s *ProfileService) ServiceShutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for id, live := range s.browsers {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := live.close(ctx); err != nil {
			live.kill()
		}
		cancel()
		live.cancel()
		if live.proxyClose != nil {
			live.proxyClose()
		}
		delete(s.browsers, id)
	}
	// Never call launcher.Cleanup: persistent browser data must survive shutdown.
	return nil
}
