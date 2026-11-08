package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/rod/lib/proto"
)

func TestProfilesPersistInNewestFirstOrder(t *testing.T) {
	root := t.TempDir()
	s, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.CreateProfile("  Personal  ")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateProfile("Work")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := time.Parse(time.RFC3339Nano, first.CreatedAt); err != nil {
		t.Fatalf("creation timestamp was not recorded: %v", err)
	}
	if first.ID == second.ID || first.Name != "Personal" {
		t.Fatal("profiles need unique IDs and trimmed names")
	}
	if _, err = s.CreateProfile(" personal "); err == nil {
		t.Fatal("duplicate name accepted")
	}
	if _, err = s.CreateProfile(" \t "); err == nil {
		t.Fatal("blank name accepted")
	}
	list := s.ListProfiles()
	list[0].Name = "Changed copy"
	reopened, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	saved := reopened.ListProfiles()
	if len(saved) != 2 || saved[0] != second || saved[1] != first || s.ListProfiles()[0].Name != "Work" {
		t.Fatal("newest-first order or list isolation was lost", saved)
	}
	if _, err = s.OpenProfile(context.Background(), "../../other"); err == nil {
		t.Fatal("unknown ID accepted")
	}
	if err = s.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateProfile("Late"); err == nil {
		t.Fatal("profile created after shutdown")
	}
}

func TestRenameProfilePreservesIdentityAndPersists(t *testing.T) {
	root := t.TempDir()
	service, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.CreateProfile("Personal")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateProfile("Work")
	if err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	first, err = service.recordProfileOpened(first.ID)
	service.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(root, "profiles", first.ID)
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(dataDir, "browser-data")
	if err := os.WriteFile(markerPath, []byte("saved session"), 0600); err != nil {
		t.Fatal(err)
	}
	// A rename must leave an existing browser connection addressable by its ID.
	live := &profileBrowser{}
	service.browsers[first.ID] = live
	for _, name := range []string{"  Home  ", "HOME"} {
		renamed, err := service.RenameProfile(first.ID, name)
		if err != nil {
			t.Fatal(err)
		}
		want := first
		want.Name = strings.TrimSpace(name)
		if renamed != want {
			t.Fatalf("rename changed profile metadata: got %+v, want %+v", renamed, want)
		}
		reopened, err := newProfileService(root)
		if err != nil {
			t.Fatal(err)
		}
		if saved := reopened.ListProfiles(); !reflect.DeepEqual(saved, []BrowserProfile{second, want}) {
			t.Fatalf("rename changed order or did not persist: %+v", saved)
		}
		if service.browsers[first.ID] != live || len(service.browsers) != 1 {
			t.Fatal("rename changed the running browser")
		}
		if data, err := os.ReadFile(markerPath); err != nil || string(data) != "saved session" {
			t.Fatal("rename changed the profile's browser data", err)
		}
	}
}

func TestRenameProfileFailuresPreserveRegistry(t *testing.T) {
	root := t.TempDir()
	service, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.CreateProfile("Personal")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateProfile("Work"); err != nil {
		t.Fatal(err)
	}
	registry := filepath.Join(root, "profiles.json")
	before, err := os.ReadFile(registry)
	if err != nil {
		t.Fatal(err)
	}
	want := service.ListProfiles()
	for _, test := range []struct{ id, name, message string }{
		{first.ID, " \t ", "Enter a profile name"},
		{first.ID, " work ", "A profile with this name already exists"},
		{strings.Repeat("0", 32), "Home", "Profile was not found"},
		{"../../outside", "Home", "Profile was not found"},
	} {
		if _, err := service.RenameProfile(test.id, test.name); err == nil || err.Error() != test.message {
			t.Fatalf("rename %q: got %v, want %s", test.name, err, test.message)
		}
		if !reflect.DeepEqual(service.ListProfiles(), want) {
			t.Fatal("rejected rename changed the in-memory registry")
		}
		if after, err := os.ReadFile(registry); err != nil || string(after) != string(before) {
			t.Fatal("rejected rename changed the saved registry", err)
		}
	}
	// Blocking the atomic destination exercises failure after temporary-file writes.
	if err := os.Rename(registry, registry+".backup"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(registry, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RenameProfile(first.ID, "Home"); err == nil {
		t.Fatal("rename succeeded despite a blocked registry")
	}
	if !reflect.DeepEqual(service.ListProfiles(), want) {
		t.Fatal("failed save changed the in-memory registry")
	}
	if after, err := os.ReadFile(registry + ".backup"); err != nil || string(after) != string(before) {
		t.Fatal("failed save changed the persisted registry", err)
	}
	if err := os.Remove(registry); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(registry+".backup", registry); err != nil {
		t.Fatal(err)
	}
	if err := service.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RenameProfile(first.ID, "Home"); err == nil || err.Error() != "SessionSplit is closing" {
		t.Fatal("rename accepted after shutdown", err)
	}
	if !reflect.DeepEqual(service.ListProfiles(), want) {
		t.Fatal("closed-service rename changed the in-memory registry")
	}
	if after, err := os.ReadFile(registry); err != nil || string(after) != string(before) {
		t.Fatal("closed-service rename changed the persisted registry", err)
	}
}

func TestProfileFailuresPreserveSavedData(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "profiles.json")
	bad := []byte(`[{"id":"../other","name":"Invalid"}]`)
	if err := os.WriteFile(path, bad, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newProfileService(root); err == nil {
		t.Fatal("invalid registry accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(bad) {
		t.Fatal("invalid data overwritten")
	}
	blocked := filepath.Join(t.TempDir(), "file")
	if err = os.WriteFile(blocked, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	s := &ProfileService{root: blocked, profiles: []BrowserProfile{}}
	if _, err = s.CreateProfile("Unsaved"); err == nil || len(s.ListProfiles()) != 0 {
		t.Fatal("failed save changed the registry")
	}
}

func TestBrowserProfilesKeepSeparatePersistentCookies(t *testing.T) {
	if os.Getenv("SESSIONSPLIT_BROWSER_TESTS") != "1" {
		t.Skip("set SESSIONSPLIT_BROWSER_TESTS=1 to run Chrome integration")
	}
	chrome, ok := launcher.LookPath()
	if !ok {
		t.Fatal("Chrome or Chromium required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	root := t.TempDir()
	s, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	s.chromePath = chrome
	s.headless = true
	t.Cleanup(func() { _ = s.ServiceShutdown(); time.Sleep(500 * time.Millisecond) })
	first, err := s.CreateProfile("Personal")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateProfile("Work")
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range []BrowserProfile{first, second} {
		requestCtx, requestCancel := context.WithCancel(ctx)
		if _, err = s.OpenProfile(requestCtx, p.ID); err != nil {
			t.Fatal(err)
		}
		requestCancel() // Browser connection must survive the opening request.
		b := s.browsers[p.ID].browser.Context(ctx)
		cookies, err := b.GetCookies()
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cookies {
			if c.Name == "account" {
				t.Fatal("new profile reused another profile's cookies")
			}
		}
		if err = b.SetCookies([]*proto.NetworkCookieParam{{Name: "account", Value: []string{"personal", "work"}[i], Domain: "example.com", Path: "/", Expires: proto.TimeSinceEpoch(time.Now().Add(time.Hour).Unix())}}); err != nil {
			t.Fatal(err)
		}
		pid := s.browsers[p.ID].launcher.PID()
		if _, err = s.OpenProfile(ctx, p.ID); err != nil {
			t.Fatal(err)
		}
		if s.browsers[p.ID].launcher.PID() != pid {
			t.Fatal("Open launched a duplicate browser")
		}
	}
	if s.browsers[first.ID].launcher.PID() == s.browsers[second.ID].launcher.PID() {
		t.Fatal("profiles share a process")
	}
	if err = s.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(350 * time.Millisecond) // Chrome flushes its cookie database after Close.
	reopened, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	reopened.chromePath = chrome
	reopened.headless = true
	t.Cleanup(func() { _ = reopened.ServiceShutdown(); time.Sleep(500 * time.Millisecond) })
	for i, p := range []BrowserProfile{first, second} {
		if _, err = reopened.OpenProfile(ctx, p.ID); err != nil {
			t.Fatal(err)
		}
		cookies, err := reopened.browsers[p.ID].browser.Context(ctx).GetCookies()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, c := range cookies {
			if c.Name == "account" {
				found = true
				if c.Value != []string{"personal", "work"}[i] {
					t.Fatal("reopened the wrong profile")
				}
			}
		}
		if !found {
			t.Fatal("cookie did not survive restart")
		}
	}
}

func TestBrowserProfilesIsolateSiteStorageAndDownloads(t *testing.T) {
	if os.Getenv("SESSIONSPLIT_BROWSER_TESTS") != "1" {
		t.Skip("set SESSIONSPLIT_BROWSER_TESTS=1 to run Chrome integration")
	}
	chrome, ok := launcher.LookPath()
	if !ok {
		t.Fatal("Chrome or Chromium required")
	}
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sw.js":
			w.Header().Set("Content-Type", "application/javascript")
			fmt.Fprint(w, `self.addEventListener('install', () => self.skipWaiting()); self.addEventListener('activate', e => e.waitUntil(self.clients.claim()));`)
		case "/cached", "/download":
			account, err := r.Cookie("account")
			if err != nil {
				http.Error(w, "missing session", 401)
				return
			}
			if r.URL.Path == "/cached" {
				w.Header().Set("Cache-Control", "private, max-age=3600")
			} else {
				w.Header().Set("Content-Disposition", `attachment; filename="identity.txt"`)
				w.Header().Set("Content-Type", "application/octet-stream")
			}
			fmt.Fprint(w, account.Value)
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<!doctype html><title>Isolation fixture</title><a id="download" href="/download" download>Download</a>`)
		}
	}))
	defer fixture.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	root := t.TempDir()
	service, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	service.chromePath = chrome
	service.headless = true
	t.Cleanup(func() { _ = service.ServiceShutdown(); time.Sleep(500 * time.Millisecond) })
	first, err := service.CreateProfile("Personal")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateProfile("Work")
	if err != nil {
		t.Fatal(err)
	}
	readSite := func(page *rod.Page) []string {
		t.Helper()
		result, err := page.Eval(`async () => {
   const db=await new Promise((resolve,reject)=>{const r=indexedDB.open('profile-store',1);r.onupgradeneeded=()=>r.result.createObjectStore('values');r.onsuccess=()=>resolve(r.result);r.onerror=()=>reject(r.error)});
   const indexed=await new Promise((resolve,reject)=>{const r=db.transaction('values').objectStore('values').get('account');r.onsuccess=()=>resolve(r.result??'');r.onerror=()=>reject(r.error)});db.close();
   const cached=await (await caches.open('profile-cache')).match('/identity');
   const workers=await navigator.serviceWorker.getRegistrations();
   return [localStorage.getItem('account')??'',sessionStorage.getItem('account')??'',indexed,cached?await cached.text():'',workers.map(w=>new URL(w.active?.scriptURL??w.installing?.scriptURL??w.waiting.scriptURL).searchParams.get('owner')).join(',')].join('|');
  }`)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(result.Value.Str(), "|")
	}
	writeSite := func(page *rod.Page, owner string) {
		t.Helper()
		_, err := page.Eval(`async (owner) => {
   document.cookie='account='+owner+'; path=/; max-age=3600';
   localStorage.setItem('account',owner);sessionStorage.setItem('account',owner);
   const db=await new Promise((resolve,reject)=>{const r=indexedDB.open('profile-store',1);r.onupgradeneeded=()=>r.result.createObjectStore('values');r.onsuccess=()=>resolve(r.result);r.onerror=()=>reject(r.error)});
   await new Promise((resolve,reject)=>{const tx=db.transaction('values','readwrite');tx.objectStore('values').put(owner,'account');tx.oncomplete=resolve;tx.onerror=()=>reject(tx.error)});db.close();
   await (await caches.open('profile-cache')).put('/identity',new Response(owner));
   await navigator.serviceWorker.register('/sw.js?owner='+owner);await navigator.serviceWorker.ready;
   return true;
  }`, owner)
		if err != nil {
			t.Fatal(err)
		}
	}
	pages := map[string]*rod.Page{}
	for _, profile := range []BrowserProfile{first, second} {
		if _, err = service.OpenProfile(ctx, profile.ID); err != nil {
			t.Fatal(err)
		}
		page := service.browsers[profile.ID].browser.Context(ctx).MustPage(fixture.URL).MustWaitLoad()
		if got := strings.Join(readSite(page), "|"); got != "||||" {
			t.Fatal("new profile inherited another profile's storage:", got)
		}
		writeSite(page, profile.Name)
		pages[profile.ID] = page
		got := strings.Join(readSite(page), "|")
		if got != strings.Repeat(profile.Name+"|", 4)+profile.Name {
			t.Fatal("site data did not remain in its profile:", got)
		}
		cache := page.MustEval(`async () => await (await fetch('/cached')).text()`).Str()
		if cache != profile.Name {
			t.Fatal("HTTP cache leaked another profile's response:", cache)
		}
		page.MustElement("#download").MustClick()
		path := filepath.Join(root, "profiles", profile.ID, "downloads", "identity.txt")
		deadline := time.Now().Add(5 * time.Second)
		for {
			data, readErr := os.ReadFile(path)
			if readErr == nil {
				if string(data) != profile.Name {
					t.Fatal("download contains another session's data")
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("profile download did not arrive: %v", readErr)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	for _, profile := range []BrowserProfile{first, second} {
		if got := strings.Join(readSite(pages[profile.ID]), "|"); got != strings.Repeat(profile.Name+"|", 4)+profile.Name {
			t.Fatal("another profile changed existing session data:", got)
		}
		launch := service.browsers[profile.ID].launcher
		if launch.Get(flags.UserDataDir) != filepath.Join(root, "profiles", profile.ID, "browser") || launch.Get("disk-cache-dir") != filepath.Join(root, "profiles", profile.ID, "cache") {
			t.Fatal("browser storage paths do not belong to the profile")
		}
		if !launch.Has("disable-sync") {
			t.Fatal("Chrome Sync can merge profile data")
		}
	}
	if service.browsers[first.ID].launcher.Get(flags.RemoteDebuggingPort) == service.browsers[second.ID].launcher.Get(flags.RemoteDebuggingPort) {
		t.Fatal("profiles share a debugging port")
	}
	if err = service.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	reopened, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	reopened.chromePath = chrome
	reopened.headless = true
	t.Cleanup(func() { _ = reopened.ServiceShutdown(); time.Sleep(500 * time.Millisecond) })
	for _, profile := range []BrowserProfile{first, second} {
		if _, err = reopened.OpenProfile(ctx, profile.ID); err != nil {
			t.Fatal(err)
		}
		page := reopened.browsers[profile.ID].browser.Context(ctx).MustPage(fixture.URL).MustWaitLoad()
		got := readSite(page)
		// sessionStorage is tab-scoped; the other stores and worker registration persist.
		for _, i := range []int{0, 2, 3, 4} {
			if got[i] != profile.Name {
				t.Fatal("wrong persistent storage after reopening:", got)
			}
		}
	}
}

func TestLegacyProfilesDoNotGetInventedCreationDates(t *testing.T) {
	root := t.TempDir()
	data := []byte(`[{"id":"0123456789abcdef0123456789abcdef","name":"Legacy","browser":"Chrome"}]`)
	if err := os.WriteFile(filepath.Join(root, "profiles.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	service, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	if service.ListProfiles()[0].CreatedAt != "" {
		t.Fatal("invented a creation date for a legacy profile")
	}
	if _, err := service.CreateProfile("New"); err != nil {
		t.Fatal(err)
	}
	reopened, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	profiles := reopened.ListProfiles()
	if profiles[0].CreatedAt == "" || profiles[1].CreatedAt != "" {
		t.Fatal("creation timestamps were not preserved correctly")
	}
}

func TestDeleteProfileRemovesOnlyItsOwnData(t *testing.T) {
	root := t.TempDir()
	service, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.CreateProfile("Personal")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateProfile("Work")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{first.ID, second.ID, strings.Repeat("f", 32)} {
		for _, part := range []string{"browser", "cache", "downloads"} {
			dir := filepath.Join(root, "profiles", id, part)
			if err = os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, "keep"), []byte(id), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, id := range []string{"../", strings.Repeat("f", 32)} {
		if err = service.DeleteProfile(id); err == nil {
			t.Fatal("unknown ID accepted")
		}
	}
	if err = service.DeleteProfile(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "profiles", first.ID)); !os.IsNotExist(err) {
		t.Fatal("deleted profile data remains", err)
	}
	for _, id := range []string{second.ID, strings.Repeat("f", 32)} {
		for _, part := range []string{"browser", "cache", "downloads"} {
			data, err := os.ReadFile(filepath.Join(root, "profiles", id, part, "keep"))
			if err != nil || string(data) != id {
				t.Fatal("another profile's data was touched", err)
			}
		}
	}
	reopened, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	if list := reopened.ListProfiles(); len(list) != 1 || list[0].ID != second.ID {
		t.Fatal("deletion did not persist", list)
	}
	if err = reopened.DeleteProfile(second.ID); err != nil {
		t.Fatal(err)
	}
	empty, err := newProfileService(root)
	if err != nil || len(empty.ListProfiles()) != 0 {
		t.Fatal("last profile was not removed", err)
	}
	if _, err = empty.CreateProfile("Personal"); err != nil {
		t.Fatal("cannot create after deleting all profiles", err)
	}
}

func TestDeleteProfileSaveFailurePreservesData(t *testing.T) {
	root := t.TempDir()
	service, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := service.CreateProfile("Personal")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "profiles", profile.ID, "browser")
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "cookies"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	registry := filepath.Join(root, "profiles.json")
	if err = os.Rename(registry, registry+".backup"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(registry, 0700); err != nil {
		t.Fatal(err)
	}
	if err = service.DeleteProfile(profile.ID); err == nil {
		t.Fatal("failed registry commit reported success")
	}
	if data, err := os.ReadFile(filepath.Join(dir, "cookies")); err != nil || string(data) != "keep" {
		t.Fatal("deleted data before the registry could be saved", err)
	}
	if len(service.ListProfiles()) != 1 {
		t.Fatal("failed deletion removed the in-memory profile")
	}
}

func TestDeleteRunningProfileKeepsOtherProfiles(t *testing.T) {
	if os.Getenv("SESSIONSPLIT_BROWSER_TESTS") != "1" {
		t.Skip("set SESSIONSPLIT_BROWSER_TESTS=1 to run Chrome integration")
	}
	chrome, ok := launcher.LookPath()
	if !ok {
		t.Fatal("Chrome or Chromium required")
	}
	root := t.TempDir()
	service, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	service.chromePath = chrome
	service.headless = true
	t.Cleanup(func() { _ = service.ServiceShutdown(); time.Sleep(500 * time.Millisecond) })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	first, err := service.CreateProfile("Personal")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateProfile("Work")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []BrowserProfile{first, second} {
		if _, err = service.OpenProfile(ctx, p.ID); err != nil {
			t.Fatal(err)
		}
	}
	other := service.browsers[second.ID]
	if err = other.browser.SetCookies([]*proto.NetworkCookieParam{{Name: "account", Value: "work", Domain: "example.com", Path: "/"}}); err != nil {
		t.Fatal(err)
	}
	if err = service.DeleteProfile(first.ID); err != nil {
		t.Fatal(err)
	}
	if service.browsers[first.ID] != nil || service.browsers[second.ID] != other {
		t.Fatal("deleting one profile changed another browser")
	}
	cookies, err := other.browser.GetCookies()
	if err != nil {
		t.Fatal("another profile's browser was closed", err)
	}
	found := false
	for _, cookie := range cookies {
		if cookie.Name == "account" && cookie.Value == "work" {
			found = true
		}
	}
	if !found {
		t.Fatal("another profile's session was lost")
	}
	if _, err = os.Stat(filepath.Join(root, "profiles", first.ID)); !os.IsNotExist(err) {
		t.Fatal("deleted browser data remains", err)
	}
	if _, err = service.OpenProfile(ctx, first.ID); err == nil {
		t.Fatal("deleted profile could be opened")
	}
	if err = service.DeleteProfile(second.ID); err != nil {
		t.Fatal(err)
	}
	if len(service.browsers) != 0 || len(service.ListProfiles()) != 0 {
		t.Fatal("deleting the last live profile left it active")
	}
}

func TestLastOpenedTimestampPersistsWithoutChangingOtherProfiles(t *testing.T) {
	root := t.TempDir()
	service, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.CreateProfile("Personal")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateProfile("Work")
	if err != nil {
		t.Fatal(err)
	}
	if first.LastOpenedAt != "" || second.LastOpenedAt != "" {
		t.Fatal("new profile has an invented last-open time")
	}
	service.mu.Lock()
	opened, err := service.recordProfileOpened(first.ID)
	service.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = time.Parse(time.RFC3339Nano, opened.LastOpenedAt); err != nil {
		t.Fatal("invalid last-open timestamp", err)
	}
	if opened.CreatedAt != first.CreatedAt {
		t.Fatal("opening changed the creation date")
	}
	reopened, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	list := reopened.ListProfiles()
	if list[0] != second || list[1] != opened {
		t.Fatal("last-open time was not persisted, or another profile changed", list)
	}
	// A failed open must preserve the previous successful timestamp.
	service.chromePath = filepath.Join(t.TempDir(), "missing-browser")
	if _, err = service.OpenProfile(context.Background(), first.ID); err == nil {
		t.Fatal("missing browser reported success")
	}
	if service.ListProfiles()[1].LastOpenedAt != opened.LastOpenedAt {
		t.Fatal("failed open changed the timestamp")
	}
	// Registry write failure must not leak an unpersisted timestamp into memory.
	registry := filepath.Join(root, "profiles.json")
	if err = os.Rename(registry, registry+".backup"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(registry, 0700); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	_, err = service.recordProfileOpened(first.ID)
	service.mu.Unlock()
	if err == nil || service.ListProfiles()[1].LastOpenedAt != opened.LastOpenedAt {
		t.Fatal("failed save changed the last-open time")
	}
}

func TestLastOpenedTracksLaunchingAndFocusingBrowser(t *testing.T) {
	if os.Getenv("SESSIONSPLIT_BROWSER_TESTS") != "1" {
		t.Skip("set SESSIONSPLIT_BROWSER_TESTS=1 to run Chrome integration")
	}
	chrome, ok := launcher.LookPath()
	if !ok {
		t.Fatal("Chrome or Chromium required")
	}
	root := t.TempDir()
	service, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	service.chromePath = chrome
	service.headless = true
	t.Cleanup(func() { _ = service.ServiceShutdown(); time.Sleep(500 * time.Millisecond) })
	profile, err := service.CreateProfile("Personal")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	first, err := service.OpenProfile(ctx, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.LastOpenedAt == "" {
		t.Fatal("launch was not recorded")
	}
	pid := service.browsers[profile.ID].launcher.PID()
	second, err := service.OpenProfile(ctx, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.LastOpenedAt == first.LastOpenedAt || service.browsers[profile.ID].launcher.PID() != pid {
		t.Fatal("focus did not update last-open time on the same browser")
	}
	reopened, err := newProfileService(root)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.ListProfiles()[0].LastOpenedAt != second.LastOpenedAt {
		t.Fatal("latest open was not restored from disk")
	}
}
