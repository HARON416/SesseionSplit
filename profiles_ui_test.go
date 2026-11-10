package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

type uiCredentialStore struct {
	mu     sync.Mutex
	values map[string]string
}

func (s *uiCredentialStore) Get(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[key], nil
}
func (s *uiCredentialStore) Set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
	return nil
}
func (s *uiCredentialStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, key)
	return nil
}

func TestProfilesUI(t *testing.T) {
	if os.Getenv("ISOLATE_BROWSER_TESTS") != "1" {
		t.Skip("set ISOLATE_BROWSER_TESTS=1 to run browser integration")
	}
	frameBytes, err := os.ReadFile("build/appicon.png")
	if err != nil {
		t.Fatal(err)
	}
	frameImage := "data:image/png;base64," + base64.StdEncoding.EncodeToString(frameBytes)
	service, err := newProfileService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer service.ServiceShutdown()
	service.credentials = &uiCredentialStore{values: map[string]string{}}
	methodID := func(name string) uint32 {
		h := fnv.New32a()
		_, _ = h.Write([]byte("main.ProfileService." + name))
		return h.Sum32()
	}
	var opened atomic.Value
	opened.Store("")
	var normalOpens, liveOpens atomic.Int32
	var failOpen atomic.Bool
	var failDelete atomic.Bool
	var failRename atomic.Bool
	var failSettings atomic.Bool
	var settingsRunning atomic.Bool
	var connectionTests atomic.Int32
	var testedConnection atomic.Value
	var liveSessions sync.Map
	var lastLiveInput atomic.Value
	lastLiveInput.Store(LiveInput{})
	files := http.FileServer(http.Dir("frontend/dist"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wails/runtime" {
			files.ServeHTTP(w, r)
			return
		}
		var call struct {
			Args struct {
				MethodID uint32            `json:"methodID"`
				Args     []json.RawMessage `json:"args"`
			} `json:"args"`
		}
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		var result any
		var callErr error
		switch call.Args.MethodID {
		case methodID("ListProfiles"):
			result = service.ListProfiles()
		case methodID("ListLiveProfiles"):
			ids := []string{}
			liveSessions.Range(func(key, value any) bool { ids = append(ids, key.(string)); return true })
			result = ids
		case methodID("GetLiveFrame"):
			result = LiveFrame{Image: frameImage, URL: "https://www.instagram.com/", Title: "Instagram test page", Width: liveWidth, Height: liveHeight}
		case methodID("SendLiveInput"):
			var input LiveInput
			if len(call.Args.Args) != 2 || json.Unmarshal(call.Args.Args[1], &input) != nil {
				http.Error(w, "invalid input", 400)
				return
			}
			callErr = validateLiveInput(input)
			lastLiveInput.Store(input)
		case methodID("ListBrowserEngines"):
			engines := []EngineInfo{}
			for _, browser := range browserCatalog {
				info := engineDetails(browser.id)
				info.Available = browser.id == "chrome" || browser.id == "edge" || browser.id == "custom" || browser.id == "firefox"
				engines = append(engines, info)
			}
			result = engines
		case methodID("GetProfileSettings"), methodID("CloseProfile"), methodID("TestProfileConnection"), methodID("UpdateProfileSettings"):
			var id string
			if len(call.Args.Args) == 0 || json.Unmarshal(call.Args.Args[0], &id) != nil {
				http.Error(w, "invalid profile ID", 400)
				return
			}
			switch call.Args.MethodID {
			case methodID("GetProfileSettings"):
				var state ProfileSettingsState
				state, callErr = service.GetProfileSettings(id)
				state.Running = settingsRunning.Load()
				result = state
			case methodID("CloseProfile"):
				settingsRunning.Store(false)
				liveSessions.Delete(id)
			case methodID("TestProfileConnection"):
				var update SettingsUpdate
				if len(call.Args.Args) != 2 || json.Unmarshal(call.Args.Args[1], &update) != nil {
					http.Error(w, "invalid connection test", 400)
					return
				}
				_, _, callErr = service.prepareConnectionTest(id, update)
				if callErr == nil {
					connectionTests.Add(1)
					testedConnection.Store(update)
					result = ConnectionDiagnostic{IP: "203.0.113.25", Route: "HTTP proxy", CheckedAt: time.Now().UTC().Format(time.RFC3339), DurationMS: 42, DNSStatus: "unverified", WebRTCStatus: "unverified"}
				}
			case methodID("UpdateProfileSettings"):
				var update SettingsUpdate
				if len(call.Args.Args) != 2 || json.Unmarshal(call.Args.Args[1], &update) != nil {
					http.Error(w, "invalid settings", 400)
					return
				}
				if failSettings.Load() {
					http.Error(w, "write /private/storage/profiles.json: permission denied", 500)
					return
				}
				if settingsRunning.Load() {
					http.Error(w, "Close this profile browser before changing settings", 400)
					return
				}
				result, callErr = service.UpdateProfileSettings(id, update)
			}
		case methodID("CreateProfileWithBrowser"):
			var name, engine, browserPath string
			if len(call.Args.Args) != 3 || json.Unmarshal(call.Args.Args[0], &name) != nil || json.Unmarshal(call.Args.Args[1], &engine) != nil || json.Unmarshal(call.Args.Args[2], &browserPath) != nil {
				http.Error(w, "invalid name", 400)
				return
			}
			result, callErr = service.CreateProfileWithBrowser(name, engine, browserPath)
		case methodID("UpdateProfile"):
			var id, name, engine, browserPath string
			if len(call.Args.Args) != 4 || json.Unmarshal(call.Args.Args[0], &id) != nil || json.Unmarshal(call.Args.Args[1], &name) != nil || json.Unmarshal(call.Args.Args[2], &engine) != nil || json.Unmarshal(call.Args.Args[3], &browserPath) != nil {
				http.Error(w, "invalid rename arguments", 400)
				return
			}
			if failRename.Load() {
				http.Error(w, "rename /private/storage/profiles.json: permission denied", 500)
				return
			}
			if settingsRunning.Load() && engine != "chrome" {
				http.Error(w, "Close this profile browser before changing browsers", 400)
				return
			}
			result, callErr = service.UpdateProfile(id, name, engine, browserPath)
		case methodID("DeleteProfile"):
			var id string
			if len(call.Args.Args) != 1 || json.Unmarshal(call.Args.Args[0], &id) != nil {
				http.Error(w, "invalid ID", 400)
				return
			}
			if failDelete.Load() {
				http.Error(w, "remove /private/storage/profiles/work: permission denied", 500)
				return
			}
			callErr = service.DeleteProfile(id)

		case methodID("OpenProfile"), methodID("OpenLiveProfile"):
			var id string
			if len(call.Args.Args) != 1 || json.Unmarshal(call.Args.Args[0], &id) != nil {
				http.Error(w, "invalid ID", 400)
				return
			}
			opened.Store(id)
			if failOpen.Load() {
				http.Error(w, "exec /private/storage/browser: process exited with code 17", 500)
				return
			}
			if call.Args.MethodID == methodID("OpenLiveProfile") {
				liveOpens.Add(1)
				liveSessions.Store(id, true)
			} else {
				normalOpens.Add(1)
			}
			service.mu.Lock()
			result, callErr = service.recordProfileOpened(id)
			service.mu.Unlock()
		default:
			result = false // Native window state is not available in the fixture.
		}
		if callErr != nil {
			http.Error(w, callErr.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	chrome, ok := launcher.LookPath()
	if !ok {
		t.Fatal("Chrome or Chromium required")
	}
	l := launcher.New().Context(ctx).Bin(chrome).Headless(true).NoSandbox(true)
	control, err := l.Launch()
	if err != nil {
		t.Fatal(err)
	}
	defer l.Cleanup()
	browser := rod.New().ControlURL(control).Context(ctx).MustConnect()
	defer browser.Close()
	page := browser.MustPage()
	page.MustEvalOnNewDocument(`localStorage.setItem('isolate-workspace', 'browser')`)
	page.MustNavigate(server.URL)
	page.MustWaitLoad()
	wait := func(js string) {
		t.Helper()
		if err := page.Wait(rod.Eval(js)); err != nil {
			t.Fatal(err)
		}
	}
	wait(`() => !document.querySelector('.add-profile').disabled`)
	if len(service.ListProfiles()) != 0 {
		t.Fatal("initial workspace was not empty")
	}
	wait(`() => document.querySelector('#empty-workspace-title')?.textContent==='No profiles yet' && document.querySelector('.empty-illustration img')?.naturalWidth>0`)
	if emptyDir := os.Getenv("ISOLATE_UI_ARTIFACTS"); emptyDir != "" {
		if err := os.MkdirAll(emptyDir, 0755); err != nil {
			t.Fatal(err)
		}
		for _, theme := range []string{"light", "dark"} {
			page.MustEval(`(theme) => { if(document.documentElement.dataset.theme!==theme) document.querySelector('.theme-toggle').click(); }`, theme)
			for _, width := range []int{1200, 480} {
				page.MustSetViewport(width, 800, 1, false)
				wait(`() => document.documentElement.scrollWidth<=innerWidth && document.querySelector('#empty-create-profile').getBoundingClientRect().bottom<innerHeight`)
				capture, err := page.Screenshot(false, &proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatPng})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(emptyDir, "empty-"+theme+"-"+strconv.Itoa(width)+".png"), capture, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		page.MustSetViewport(1200, 800, 1, false)
	}
	page.KeyActions().Press(input.ControlLeft).Type(input.KeyN).Release(input.ControlLeft).MustDo()
	wait(`() => document.querySelector('dialog').open && document.querySelector('#profile-dialog-title')?.textContent==='Add new profile' && document.activeElement.id==='profile-name' && document.querySelector('#profile-name-hint')?.textContent.includes('separate') && document.querySelector('.dialog-actions .secondary').textContent==='Cancel'`)
	page.Keyboard.MustType(input.Escape)
	wait(`() => !document.querySelector('dialog').open && document.activeElement.classList.contains('add-profile')`)
	page.MustElement("#empty-create-profile").MustClick()
	wait(`() => document.querySelector('dialog').open && document.activeElement.id==='profile-name'`)
	page.Keyboard.MustType(input.Escape)
	wait(`() => !document.querySelector('dialog').open && document.activeElement.id==='empty-create-profile'`)
	create := func(name string) {
		t.Helper()
		page.MustElement(".add-profile").MustClick()
		page.MustElement("#profile-name").MustInput(name)
		wait(`() => !document.querySelector('#profile-engine').disabled`)
		page.MustElement(".dialog-actions .primary").MustClick()
		wait(`() => !document.querySelector('dialog').open`)
	}
	page.MustElement(".add-profile").MustClick()
	page.MustElement("#profile-name").MustInput("Personal")
	wait(`() => !document.querySelector('#profile-engine').disabled`)
	page.MustElement("#profile-engine").MustSelect("Firefox")
	page.MustElement(".dialog-actions .primary").MustClick()
	wait(`() => !document.querySelector('dialog').open && document.querySelector('.profile-card .browser-name')?.textContent==='Firefox' && document.querySelector('.profile-brand img')?.getAttribute('src')==='/icons/browser-firefox.png'`)
	if profile := service.ListProfiles()[0]; profile.Settings.Engine != "firefox" {
		t.Fatal("new profile did not save the selected browser")
	}
	create("Work")
	wait(`() => document.querySelector('.profile-card h2')?.textContent==='Work' && document.activeElement.getAttribute('aria-label')==='Open Work'`)
	wait(`() => document.querySelectorAll('.profile-brand img').length===2 && [...document.querySelectorAll('.profile-brand img')].every(image => image.naturalWidth>0) && document.querySelectorAll('.creation-date time').length===2 && !document.querySelector('.open-profile svg')`)
	saved := service.ListProfiles()
	if len(saved) != 2 {
		t.Fatal("profile cards were not saved")
	}
	page.MustElement(".add-profile").MustClick()
	page.MustElement("#profile-name").MustInput(" work ")
	page.MustElement(".dialog-actions .primary").MustClick()
	wait(`() => document.querySelector('[role=alert]')?.textContent.includes('already exists') && !document.querySelector('.dialog-actions .primary').disabled`)
	if len(service.ListProfiles()) != 2 {
		t.Fatal("duplicate profile created")
	}
	page.MustElement(".secondary").MustClick()
	page.MustEval(`() => location.reload()`)
	page.MustWaitLoad()
	wait(`() => document.querySelectorAll('.profile-card').length===2 && document.querySelector('.profile-card h2')?.textContent==='Work'`)
	wait(`() => [...document.querySelectorAll('.last-opened')].every(node => node.textContent.includes('Not recorded'))`)
	page.MustElement(".open-profile").MustClick()
	wait(`() => !document.querySelector('.open-profile').disabled && !!document.querySelector('.last-opened time')`)
	if page.MustEval(`() => !!document.querySelector('.external-profile, .live-workspace, .live-preview')`).Bool() {
		t.Fatal("profiles must open directly in a normal browser without external/live controls")
	}
	if liveOpens.Load() != 0 || normalOpens.Load() != 1 {
		t.Fatal("Open must use the normal browser launch", normalOpens.Load(), liveOpens.Load())
	}
	recorded := service.ListProfiles()[0].LastOpenedAt
	if page.MustElement(".last-opened time").MustAttribute("datetime") == nil || *page.MustElement(".last-opened time").MustAttribute("datetime") != recorded {
		t.Fatal("card did not show the saved last-open time")
	}
	if opened.Load().(string) != saved[0].ID {
		t.Fatal("Open targeted the wrong profile")
	}
	// Labels refresh without a reload while exact timestamps remain accessible.
	wait(`() => document.querySelector('.creation-date time span').textContent==='Today' && [...document.querySelectorAll('time')].every(time => time.title && time.getAttribute('aria-label')===time.title)`)
	page.MustEval(`() => {
		window.originalDateNow = Date.now;
		const opened = new Date(document.querySelector('.last-opened time').dateTime).getTime();
		Date.now = () => opened + 61000;
	}`)
	wait(`() => document.querySelector('.last-opened time').textContent.trim()==='1 min ago'`)
	page.MustEval(`() => {
		const tomorrow = new Date(document.querySelector('.creation-date time').dateTime);
		tomorrow.setDate(tomorrow.getDate() + 1);
		Date.now = () => tomorrow.getTime();
	}`)
	wait(`() => document.querySelector('.creation-date time span').textContent==='Yesterday'`)
	page.MustEval(`() => { Date.now = window.originalDateNow; delete window.originalDateNow; }`)
	wait(`() => document.querySelector('.creation-date time span').textContent==='Today'`)
	failOpen.Store(true)
	page.MustElement(".open-profile").MustClick()
	wait(`() => document.querySelector('.notice')?.textContent.includes('Could not open the browser. Close any unresponsive browser windows and try again.') && !document.body.textContent.includes('/private/storage') && !document.querySelector('.open-profile').disabled`)
	page.MustElement("[aria-label='Dismiss message']").MustClick()
	if service.ListProfiles()[0].LastOpenedAt != recorded {
		t.Fatal("failed open changed the recorded date")
	}
	page.MustEval(`() => location.reload()`)
	page.MustWaitLoad()
	wait(`() => document.querySelector('.last-opened time')?.getAttribute('datetime')===` + strconv.Quote(recorded))
	// Card actions follow the same primary-first order visually and on the keyboard.
	wait(`() => [...document.querySelector('.profile-card').querySelectorAll('.profile-card-actions button')].map(b => b.classList.contains('open-profile') ? 'open' : b.classList.contains('edit-profile') ? 'edit' : b.classList.contains('settings-profile') ? 'settings' : b.classList.contains('delete-profile') ? 'delete' : '').join(',')==='open,edit,settings,delete'`)
	page.MustElement(".open-profile").MustFocus()
	page.Keyboard.MustType(input.Tab)
	wait(`() => document.activeElement.classList.contains('edit-profile')`)
	page.Keyboard.MustType(input.Tab)
	wait(`() => document.activeElement.classList.contains('settings-profile')`)
	page.Keyboard.MustType(input.Tab)
	wait(`() => document.activeElement.classList.contains('delete-profile')`)
	beforeRename := service.ListProfiles()
	editWork := func() {
		t.Helper()
		page.MustElement(".edit-profile[aria-label='Edit Work']").MustClick()
		wait(`() => document.querySelector('dialog').open && document.querySelector('#profile-dialog-title')?.textContent==='Edit profile' && !document.querySelector('#profile-engine').disabled && document.activeElement.id==='profile-name' && document.querySelector('#profile-name').value==='Work' && document.querySelector('#profile-name').selectionStart===0 && document.querySelector('#profile-name').selectionEnd===4`)
	}
	editWork()
	page.MustElement("#profile-engine").MustSelect("Firefox")
	wait(`() => !document.querySelector('.dialog-actions .primary').disabled`)
	settingsRunning.Store(true)
	page.MustElement(".dialog-actions .primary").MustClick()
	wait(`() => document.querySelector('#profile-error')?.textContent.includes('Close this profile’s browser') && !document.querySelector('.dialog-actions .primary').disabled`)
	if service.ListProfiles()[0].Settings.Engine != "chrome" {
		t.Fatal("blocked browser edit changed the profile")
	}
	settingsRunning.Store(false)
	page.MustElement(".dialog-actions .primary").MustClick()
	wait(`() => !document.querySelector('dialog').open && document.querySelector('.profile-card .browser-name')?.textContent==='Firefox' && document.querySelector('.profile-brand img')?.getAttribute('src')==='/icons/browser-firefox.png'`)
	editWork()
	wait(`() => document.querySelector('#profile-engine').value==='firefox'`)
	page.MustElement("#profile-engine").MustSelect("Chrome / Chromium")
	page.MustElement(".dialog-actions .primary").MustClick()
	wait(`() => !document.querySelector('dialog').open && document.querySelector('.profile-brand img')?.getAttribute('src')==='/icons/browser-chrome.png'`)
	editWork()
	page.MustElement("#profile-name").MustSelectAllText().MustInput("   ")
	wait(`() => document.querySelector('.dialog-actions .primary').disabled`)
	page.MustElement(".dialog-actions .secondary").MustClick()
	wait(`() => !document.querySelector('dialog').open && document.activeElement.getAttribute('aria-label')==='Edit Work'`)
	editWork()
	page.MustElement("#profile-name").MustSelectAllText().MustInput("Unsaved name")
	page.Keyboard.MustType(input.Escape)
	wait(`() => !document.querySelector('dialog').open && document.activeElement.getAttribute('aria-label')==='Edit Work' && document.querySelector('.profile-card h2').textContent==='Work'`)
	// Unchanged names cannot be submitted, while case changes exclude this profile from duplicate detection.
	editWork()
	wait(`() => document.querySelector('.dialog-actions .primary').disabled`)
	page.MustElement("#profile-name").MustSelectAllText().MustInput("work")
	page.MustElement(".dialog-actions .primary").MustClick()
	wait(`() => !document.querySelector('dialog').open && document.querySelector('.profile-card h2').textContent==='work'`)
	page.MustElement(".edit-profile[aria-label='Edit work']").MustClick()
	page.MustElement("#profile-name").MustSelectAllText().MustInput("Work")
	page.MustElement(".dialog-actions .primary").MustClick()
	wait(`() => !document.querySelector('dialog').open && document.querySelector('.profile-card h2').textContent==='Work'`)
	editWork()
	page.MustElement("#profile-name").MustSelectAllText().MustInput(" personal ")
	page.MustElement(".dialog-actions .primary").MustClick()
	wait(`() => document.querySelector('dialog [role=alert]')?.textContent.includes('already exists') && !document.querySelector('.dialog-actions .primary').disabled`)
	page.MustElement("#profile-name").MustSelectAllText().MustInput("Office")
	failRename.Store(true)
	page.MustElement(".dialog-actions .primary").MustClick()
	wait(`() => document.querySelector('dialog [role=alert]')?.textContent.includes('Could not save the profile. Check available disk space and folder permissions, then try again.') && !document.body.textContent.includes('/private/storage') && document.querySelector('#profile-name').value==='Office' && document.querySelector('.profile-card h2').textContent==='Work' && !document.querySelector('.dialog-actions .primary').disabled`)
	failRename.Store(false)
	page.MustElement(".dialog-actions .primary").MustClick()
	wait(`() => !document.querySelector('dialog').open && document.querySelector('.profile-card h2').textContent==='Office' && document.querySelector('.open-profile').getAttribute('aria-label')==='Open Office' && document.querySelector('.edit-profile').getAttribute('aria-label')==='Edit Office' && document.querySelector('.delete-profile').getAttribute('aria-label')==='Delete Office'`)
	page.MustEval(`() => location.reload()`)
	page.MustWaitLoad()
	wait(`() => document.querySelector('.profile-card h2')?.textContent==='Office' && !document.querySelector('.add-profile').disabled`)
	afterRename := service.ListProfiles()
	for i, before := range beforeRename {
		after := afterRename[i]
		if before.ID != after.ID || before.CreatedAt != after.CreatedAt || before.LastOpenedAt != after.LastOpenedAt {
			t.Fatal("rename changed profile order, identity, or timestamps")
		}
	}
	if afterRename[0].Name != "Office" {
		t.Fatal("renamed profile was not persisted")
	}
	page.MustElement(".edit-profile[aria-label='Edit Office']").MustClick()
	page.MustElement("#profile-name").MustSelectAllText().MustInput("Work")
	page.MustElement(".dialog-actions .primary").MustClick()
	wait(`() => !document.querySelector('dialog').open && document.querySelector('.profile-card h2').textContent==='Work'`)
	// Long names must wrap without pushing actions outside the card.
	create("A very long browser profile name that wraps across multiple lines without overflowing")
	dir := os.Getenv("ISOLATE_UI_ARTIFACTS")
	if dir == "" {
		dir = t.TempDir()
	}
	if err = os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	openSettings := func() {
		t.Helper()
		page.MustElement(".settings-profile[aria-label='Settings for Work']").MustClick()
		wait(`() => document.querySelector('#settings-dialog').open && document.activeElement.id==='settings-proxy-mode' && !document.querySelector('#settings-dialog form').getAttribute('aria-busy').includes('true')`)
	}
	fillSetting := func(selector, value string) {
		t.Helper()
		page.MustElement(selector).MustSelectAllText().MustType(input.Backspace).MustInput(value)
	}
	openSettings()
	wait(`() => document.querySelector('#settings-dialog footer .primary').disabled && document.querySelector('#settings-engine option[value=brave]').disabled && document.querySelector('#settings-dialog').textContent.includes('Switching browsers does not transfer logins')`)
	wait(`() => {const bounds=document.querySelector('#settings-dialog').getBoundingClientRect();return Math.abs(bounds.right-innerWidth)<1 && Math.abs(bounds.top)<1 && Math.abs(bounds.height-innerHeight)<1;}`)
	if !page.MustEval(`async () => {
		const drawer=document.querySelector('#settings-dialog');
		const initialLeft=drawer.getBoundingClientRect().left;
		drawer.querySelector('.close-drawer').click();
		await new Promise(requestAnimationFrame);
		if(matchMedia('(prefers-reduced-motion: reduce)').matches) return true;
		const animations=drawer.getAnimations();
		animations.forEach(animation => {animation.pause(); animation.currentTime=90;});
		const left=drawer.getBoundingClientRect().left;
		const slidesOut=drawer.open && left>initialLeft && left<innerWidth;
		animations.forEach(animation => animation.finish());
		return slidesOut;
	}`).Bool() {
		t.Fatal("settings drawer disappeared before sliding out")
	}
	wait(`() => !document.querySelector('#settings-dialog').open && document.activeElement.getAttribute('aria-label')==='Settings for Work'`)
	openSettings()
	page.Mouse.MustMoveTo(12, 150).MustClick(proto.InputMouseButtonLeft)
	wait(`() => !document.querySelector('#settings-dialog').open && document.activeElement.getAttribute('aria-label')==='Settings for Work'`)
	openSettings()
	page.Keyboard.MustType(input.Escape)
	wait(`() => !document.querySelector('#settings-dialog').open && document.activeElement.getAttribute('aria-label')==='Settings for Work'`)
	openSettings()
	page.MustElement("#settings-engine").MustSelect("Microsoft Edge")
	page.MustElement("#settings-dialog footer .primary").MustClick()
	wait(`() => !document.querySelector('#settings-dialog').open && [...document.querySelectorAll('.profile-card')].some(card => card.querySelector('h2').textContent==='Work' && card.querySelector('.browser-name').textContent==='Microsoft Edge')`)
	wait(`() => [...document.querySelectorAll('.profile-card')].some(card => card.querySelector('h2').textContent==='Work' && card.querySelector('.profile-brand img')?.getAttribute('src')==='/icons/browser-edge.png' && card.querySelector('.profile-brand img').naturalWidth>0)`)
	openSettings()
	page.MustElement("#settings-engine").MustSelect("Other Chromium-based browser")
	wait(`() => !!document.querySelector('#settings-browser-path')`)
	fillSetting("#settings-browser-path", chrome)
	page.MustElement("#settings-dialog footer .primary").MustClick()
	wait(`() => !document.querySelector('#settings-dialog').open`)
	openSettings()
	wait(`() => document.querySelector('#settings-engine').value==='custom' && document.querySelector('#settings-browser-path').value!==''`)
	wait(`() => [...document.querySelectorAll('.profile-card')].some(card => card.querySelector('h2').textContent==='Work' && card.querySelector('.profile-brand svg') && !card.querySelector('.profile-brand img'))`)
	page.MustElement("#settings-engine").MustSelect("Firefox")
	page.MustElement("#settings-dialog footer .primary").MustClick()
	wait(`() => !document.querySelector('#settings-dialog').open && [...document.querySelectorAll('.profile-card')].some(card => card.querySelector('h2').textContent==='Work' && card.querySelector('.browser-name').textContent==='Firefox')`)
	wait(`() => [...document.querySelectorAll('.profile-card')].some(card => card.querySelector('h2').textContent==='Work' && card.querySelector('.profile-brand img')?.getAttribute('src')==='/icons/browser-firefox.png' && card.querySelector('.profile-brand img').naturalWidth>0)`)
	openSettings()
	wait(`() => document.querySelector('#settings-engine').value==='firefox' && !document.querySelector('#settings-browser-path')`)
	page.MustElement("#settings-engine").MustSelect("Chrome / Chromium")
	page.MustElement("#settings-dialog footer .primary").MustClick()
	wait(`() => !document.querySelector('#settings-dialog').open`)
	openSettings()
	page.MustElement("#settings-proxy-mode").MustSelect("HTTP proxy")
	fillSetting("#settings-proxy-host", "localhost")
	fillSetting("#settings-proxy-port", "8123")
	fillSetting("#settings-proxy-username", "test-user")
	const testPassword = "ui-test-password-not-a-secret"
	fillSetting("#settings-proxy-password", testPassword)
	fillSetting("#settings-language", "en-US")
	fillSetting("#settings-timezone", "Africa/Nairobi")
	fillSetting("#settings-width", "1280")
	fillSetting("#settings-height", "800")
	page.MustElement("#settings-permissions").MustSelect("Block")
	wait(`() => ![...document.querySelectorAll('#settings-dialog button')].find(b => b.textContent==='Test connection').disabled`)
	beforeTest, err := os.ReadFile(filepath.Join(service.root, "profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	page.MustElementR("#settings-dialog button", "^Test connection$").MustClick()
	wait(`() => document.querySelector('#settings-dialog .diagnostic-result')?.textContent.includes('203.0.113.25') && !document.querySelector('#settings-dialog footer .primary').disabled && document.querySelector('#settings-proxy-password').value==='ui-test-password-not-a-secret'`)
	draft := testedConnection.Load().(SettingsUpdate)
	if draft.Settings.Proxy.Host != "localhost" || draft.Settings.Proxy.Port != 8123 || draft.Settings.Proxy.Username != "test-user" || draft.Password != testPassword || draft.PasswordAction != "replace" {
		t.Fatal("connection test did not receive the unsaved proxy inputs")
	}
	afterTest, err := os.ReadFile(filepath.Join(service.root, "profiles.json"))
	if err != nil || string(afterTest) != string(beforeTest) || connectionTests.Load() != 1 {
		t.Fatal("unsaved connection test changed saved profiles", err)
	}
	fillSetting("#settings-proxy-host", "127.0.0.1")
	wait(`() => !document.querySelector('#settings-dialog .diagnostic-result')`)
	fillSetting("#settings-proxy-host", "localhost")
	failSettings.Store(true)
	page.MustElement("#settings-dialog footer .primary").MustClick()
	wait(`() => document.querySelector('#settings-dialog [role=alert]')?.textContent.includes('Could not save settings. Check available disk space and folder permissions, then try again.') && !document.body.textContent.includes('/private/storage') && document.querySelector('#settings-proxy-password').value==='ui-test-password-not-a-secret' && !document.querySelector('#settings-dialog footer .primary').disabled`)
	failSettings.Store(false)
	page.MustElement("#settings-dialog footer .primary").MustClick()
	wait(`() => !document.querySelector('#settings-dialog').open && document.activeElement.getAttribute('aria-label')==='Settings for Work'`)
	registry, readErr := os.ReadFile(filepath.Join(service.root, "profiles.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(registry), testPassword) {
		t.Fatal("proxy password leaked into the profile registry")
	}
	var work BrowserProfile
	for _, profile := range service.ListProfiles() {
		if profile.Name == "Work" {
			work = profile
		}
	}
	if work.Settings.Proxy.Mode != "http" || work.Settings.Proxy.Port != 8123 || work.Settings.Language != "en-US" || work.Settings.Timezone != "Africa/Nairobi" || work.Settings.Width != 1280 || work.Settings.Height != 800 || work.Settings.Permissions != "block" || !work.Settings.Proxy.HasPassword {
		t.Fatal("settings save did not persist the configured environment")
	}
	state, stateErr := service.GetProfileSettings(work.ID)
	encodedState, _ := json.Marshal(state)
	if stateErr != nil || strings.Contains(string(encodedState), testPassword) {
		t.Fatal("settings readback exposed a password or failed")
	}
	page.MustEval(`() => location.reload()`)
	page.MustWaitLoad()
	wait(`() => !document.querySelector('.add-profile').disabled`)
	openSettings()
	wait(`() => document.querySelector('#settings-proxy-mode').value==='http' && document.querySelector('#settings-proxy-host').value==='localhost' && document.querySelector('#settings-proxy-port').value==='8123' && document.querySelector('#settings-language').value==='en-US' && document.querySelector('#settings-timezone').value==='Africa/Nairobi' && document.querySelector('#settings-width').value==='1280' && document.querySelector('#settings-height').value==='800' && document.querySelector('#settings-permissions').value==='block' && document.querySelector('#settings-proxy-password').value==='' && !document.body.textContent.includes('ui-test-password-not-a-secret') && document.querySelector('#settings-password-hint').textContent.includes('Leave this blank to keep it')`)
	if connectionTests.Load() != 1 {
		t.Fatal("opening or saving settings triggered an automatic network test")
	}
	page.MustElementR("#settings-dialog button", "^Test connection$").MustClick()
	wait(`() => document.querySelector('#settings-dialog .diagnostic-result')?.textContent.includes('203.0.113.25') && document.querySelector('#settings-dialog .diagnostic-result').textContent.includes('HTTP proxy') && document.querySelector('#settings-dialog .diagnostic-result').textContent.includes('42 ms') && document.querySelector('#settings-dialog .diagnostic-result').textContent.includes('unverified')`)
	if connectionTests.Load() != 2 {
		t.Fatal("manual connection test did not issue exactly one additional request")
	}
	page.MustElement("#settings-dialog footer .secondary").MustClick()
	wait(`() => !document.querySelector('#settings-dialog').open && document.activeElement.getAttribute('aria-label')==='Settings for Work'`)
	settingsRunning.Store(true)
	openSettings()
	fillSetting("#settings-language", "en-GB")
	wait(`() => document.querySelector('#settings-dialog footer .primary').disabled && document.querySelector('#settings-dialog').textContent.includes('Close this profile’s browser before saving')`)
	page.MustElementR("#settings-dialog button", "^Close browser$").MustClick()
	wait(`() => !document.querySelector('#settings-dialog footer .primary').disabled && document.querySelector('#settings-dialog').textContent.includes('Changes apply the next time')`)
	page.MustElement("#settings-dialog footer .secondary").MustClick()
	if settingsRunning.Load() {
		t.Fatal("Close browser did not clear running state")
	}
	for _, mode := range []string{"light", "dark"} {
		page.MustEval(`(mode) => {if(document.documentElement.dataset.theme!==mode)document.querySelector('.theme-toggle').click()}`, mode)
		for _, viewport := range [][2]int{{1200, 800}, {480, 800}, {480, 320}} {
			page.MustSetViewport(viewport[0], viewport[1], 1, false)
			openSettings()
			wait(`() => { const dialog=document.querySelector('#settings-dialog'); const save=dialog.querySelector('footer .primary').getBoundingClientRect(); const bounds=dialog.getBoundingClientRect(); return dialog.scrollWidth<=dialog.clientWidth && save.bottom<=innerHeight && save.top>=0 && Math.abs(bounds.right-innerWidth)<1 && Math.abs(bounds.height-innerHeight)<1; }`)
			if viewport[1] == 320 {
				page.MustEval(`() => {const content=document.querySelector('#settings-dialog .settings-content'); content.scrollTop=content.scrollHeight;}`)
				wait(`() => document.querySelector('#settings-dialog .settings-content').scrollTop>0`)
			}
			data, screenshotErr := page.Screenshot(true, &proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatPng})
			if screenshotErr != nil {
				t.Fatal(screenshotErr)
			}
			if err = os.WriteFile(filepath.Join(dir, "settings-"+mode+"-"+strconv.Itoa(viewport[0])+"x"+strconv.Itoa(viewport[1])+".png"), data, 0644); err != nil {
				t.Fatal(err)
			}
			page.Keyboard.MustType(input.Escape)
			wait(`() => !document.querySelector('#settings-dialog').open`)
		}
	}
	// Restore defaults and explicitly remove the dummy credential before later flows.
	page.MustSetViewport(1200, 800, 1, false)
	openSettings()
	page.MustElement("#settings-remove-password").MustClick()
	fillSetting("#settings-proxy-host", "")
	fillSetting("#settings-proxy-port", "")
	fillSetting("#settings-proxy-username", "")
	page.MustElement("#settings-proxy-mode").MustSelect("Direct connection")
	fillSetting("#settings-language", "")
	fillSetting("#settings-timezone", "")
	fillSetting("#settings-width", "")
	fillSetting("#settings-height", "")
	page.MustElement("#settings-permissions").MustSelect("Ask before allowing")
	page.MustElement("#settings-dialog footer .primary").MustClick()
	wait(`() => !document.querySelector('#settings-dialog').open`)
	for _, profile := range service.ListProfiles() {
		if profile.ID == work.ID && (profile.Settings.Proxy.Mode != "direct" || profile.Settings.Proxy.Host != "" || profile.Settings.Proxy.Port != 0 || profile.Settings.Proxy.Username != "" || profile.Settings.Proxy.HasPassword || profile.Settings.Engine != "chrome" || profile.Settings.Language != "" || profile.Settings.Timezone != "" || profile.Settings.Width != 0 || profile.Settings.Height != 0 || profile.Settings.Permissions != "ask") {
			t.Fatal("settings defaults were not restored")
		}
	}
	for _, mode := range []string{"light", "dark"} {
		page.MustEval(`(mode) => {if(document.documentElement.dataset.theme!==mode)document.querySelector('.theme-toggle').click()}`, mode)
		for _, width := range []int{1200, 480} {
			page.MustSetViewport(width, 800, 1, false)
			page.MustEval(`() => document.fonts.ready`)
			wait(`() => document.documentElement.scrollWidth<=innerWidth && [...document.querySelectorAll('.profile-card')].every(c => c.scrollWidth<=c.clientWidth)`)
			data, err := page.Screenshot(true, &proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatPng})
			if err != nil {
				t.Fatal(err)
			}
			suffix := "desktop"
			if width == 480 {
				suffix = "narrow"
			}
			if err = os.WriteFile(filepath.Join(dir, "profiles-"+mode+"-"+suffix+".png"), data, 0644); err != nil {
				t.Fatal(err)
			}
		}
		page.MustElement(".edit-profile[aria-label='Edit Work']").MustClick()
		wait(`() => document.querySelector('dialog').open && document.querySelector('#profile-name-hint')?.textContent.includes('name')`)
		editScreenshot, screenshotErr := page.Screenshot(true, &proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatPng})
		if screenshotErr != nil {
			t.Fatal(screenshotErr)
		}
		if err = os.WriteFile(filepath.Join(dir, "edit-dialog-"+mode+".png"), editScreenshot, 0644); err != nil {
			t.Fatal(err)
		}
		page.MustElement(".dialog-actions .secondary").MustClick()
		// Exercise the custom scrollbar with real overflow at the minimum window size.
		page.MustSetViewport(480, 320, 1, false)
		wait(`() => {
			const main = document.querySelector('main');
			const bar = getComputedStyle(main, '::-webkit-scrollbar');
			const thumb = getComputedStyle(main, '::-webkit-scrollbar-thumb');
			return main.scrollHeight > main.clientHeight && bar.width === '8px'
				&& thumb.backgroundColor !== 'rgba(0, 0, 0, 0)'
				&& main.offsetWidth - main.clientWidth <= 8;
		}`)
		page.MustElement("main").MustHover()
		page.Mouse.MustScroll(0, 300)
		wait(`() => document.querySelector('main').scrollTop > 0`)
		data, err := page.Screenshot(true, &proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatPng})
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "scrollbar-"+mode+".png"), data, 0644); err != nil {
			t.Fatal(err)
		}
		page.MustEval(`() => document.querySelector('main').scrollTo(0, 0)`)
	}
	page.MustElement(".delete-profile[aria-label='Delete Work']").MustClick()
	wait(`() => document.querySelector('.delete-dialog').open && document.querySelector('#delete-dialog-description strong')?.textContent==='Work' && document.activeElement.textContent==='Cancel'`)
	page.MustElement(".delete-dialog .secondary").MustClick()
	wait(`() => !document.querySelector('.delete-dialog').open`)
	if len(service.ListProfiles()) != 3 {
		t.Fatal("cancel deleted a profile")
	}
	page.MustElement(".delete-profile[aria-label='Delete Work']").MustClick()
	failDelete.Store(true)
	page.MustElement(".delete-dialog .danger").MustClick()
	wait(`() => document.querySelector('.delete-dialog [role=alert]')?.textContent.includes('Could not finish deleting the profile. Check available disk space and folder permissions, then try again.') && !document.body.textContent.includes('/private/storage') && !document.querySelector('.delete-dialog .danger').disabled`)
	if len(service.ListProfiles()) != 3 {
		t.Fatal("failed delete removed a profile")
	}
	failDelete.Store(false)
	page.MustElement(".delete-dialog .danger").MustClick()
	wait(`() => !document.querySelector('.delete-dialog').open && document.querySelectorAll('.profile-card').length===2`)
	page.MustEval(`() => location.reload()`)
	page.MustWaitLoad()
	wait(`() => !document.querySelector('.add-profile').disabled && document.querySelectorAll('.profile-card').length===2`)
	for _, profile := range service.ListProfiles() {
		if profile.Name == "Work" {
			t.Fatal("deletion targeted the wrong card")
		}
	}
	for _, mode := range []string{"light", "dark"} {
		page.MustEval(`(mode) => {if(document.documentElement.dataset.theme!==mode)document.querySelector('.theme-toggle').click()}`, mode)
		page.MustElement(".delete-profile[aria-label='Delete Personal']").MustClick()
		data, err := page.Screenshot(true, &proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatPng})
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "delete-dialog-"+mode+".png"), data, 0644); err != nil {
			t.Fatal(err)
		}
		page.MustElement(".delete-dialog .secondary").MustClick()
	}
	for len(service.ListProfiles()) > 0 {
		page.MustElement(".delete-profile").MustClick()
		page.MustElement(".delete-dialog .danger").MustClick()
		wait(`() => !document.querySelector('.delete-dialog').open`)
	}
	wait(`() => !document.querySelector('.profile-card') && !document.querySelector('.add-profile').disabled && document.activeElement.classList.contains('add-profile')`)

}
