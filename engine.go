package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// EngineInfo reports installed software, not a guarantee of anonymity or active site shields.
type EngineInfo struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	Available             bool   `json:"available"`
	Version               string `json:"version"`
	FingerprintProtection string `json:"fingerprintProtection"`
	Notes                 string `json:"notes"`
}

type browserEngine interface {
	Info() EngineInfo
	Launch(context.Context, launchConfig) (*profileBrowser, error)
}

type launchConfig struct {
	Profile  BrowserProfile
	Settings ProfileSettings
	Root     string
	ProxyURL string
	Headless bool
	OnError  func(error)
}

type chromiumEngine struct {
	path string
	info EngineInfo
}

func (e chromiumEngine) Info() EngineInfo { return e.info }

func browserLabelForEngine(id string) string {
	if id == "" || id == "chrome" {
		if strings.Contains(strings.ToLower(filepath.Base(findEnginePath("chrome"))), "chromium") {
			return "Chromium"
		}
		return "Chrome"
	}
	browser, _ := browserDefinition(id)
	return browser.name
}

func engineDetails(id string) EngineInfo {
	browser, _ := browserDefinition(id)
	info := EngineInfo{ID: browser.id, Name: browser.name, FingerprintProtection: "Standard browser protections; no added fingerprint protection",
		Notes: "Uses the selected installed browser. Profile isolation separates storage, not browser identity. Timezone and locale overrides cover page and iframe targets, not worker execution contexts. Browser updates are managed by the browser vendor or your operating system."}
	if browser.id == "brave" {
		info.FingerprintProtection = "Native Brave fingerprinting protections"
		info.Notes += " Uses native Shields defaults; Isolate does not verify per-site Shields state or guarantee anonymity. https://github.com/brave/brave-browser/wiki/Fingerprinting-Protections"
	}
	if browser.id == "custom" {
		info.Notes += " Choose an executable that supports Chromium remote debugging and separate user-data directories. Compatibility varies by browser."
	}
	if browser.id == "firefox" {
		info.Notes = "Uses installed Firefox with separate saved sessions and native browser protections. Language and timezone settings require a Firefox version with the corresponding WebDriver BiDi commands; unsupported settings stop launch. Proxied profiles disable WebRTC. Browser updates are managed by Mozilla or your operating system."
	}
	return info
}

func resolveProfileEngine(settings ProfileSettings, overridePath string) (browserEngine, error) {
	browser, found := browserDefinition(settings.Engine)
	if !found {
		return nil, errors.New("Choose a supported browser")
	}
	path := overridePath
	if path == "" && browser.id == "custom" {
		if err := validateBrowserSelection(settings); err != nil {
			return nil, err
		}
		path = settings.BrowserPath
	}
	if path == "" {
		path = findEnginePath(browser.id)
	}
	if path == "" {
		if browser.id == "chrome" {
			return nil, errors.New("Install Chrome or Chromium to open a profile")
		}
		return nil, fmt.Errorf("Install %s to open this profile; Isolate will not substitute another browser", browser.name)
	}
	info := engineDetails(browser.id)
	info.Available = true
	if browser.id == "firefox" {
		return firefoxEngine{path: path, info: info}, nil
	}
	return chromiumEngine{path: path, info: info}, nil
}

func chromeCandidates(platform string, getenv func(string) string) []string {
	if platform == "windows" {
		paths := []string{"chrome.exe", "chromium.exe"}
		for _, key := range []string{"PROGRAMFILES", "PROGRAMFILES(X86)", "LOCALAPPDATA"} {
			if root := getenv(key); root != "" {
				for _, product := range []string{"Google", "Chromium"} {
					if product == "Google" {
						paths = append(paths, filepath.Join(root, product, "Chrome", "Application", "chrome.exe"))
					} else {
						paths = append(paths, filepath.Join(root, product, "Application", "chrome.exe"))
					}
				}
			}
		}
		return paths
	}
	if platform == "darwin" {
		return []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Chromium.app/Contents/MacOS/Chromium", "google-chrome", "chromium"}
	}
	return []string{"google-chrome", "google-chrome-stable", "chrome", "chromium", "chromium-browser", "/usr/bin/google-chrome", "/usr/bin/google-chrome-stable", "/usr/bin/chromium", "/usr/bin/chromium-browser", "/snap/bin/chromium"}
}

func (s *ProfileService) ListBrowserEngines() []EngineInfo {
	result := []EngineInfo{}
	for _, browser := range browserCatalog {
		info := engineDetails(browser.id)
		if browser.id == "custom" {
			info.Available = true
		} else if path := findEnginePath(browser.id); path != "" {
			info.Available = true
			info.Version = installedBrowserVersion(path)
		}
		result = append(result, info)
	}
	return result
}

func (e chromiumEngine) Launch(ctx context.Context, cfg launchConfig) (*profileBrowser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !profileIDPattern.MatchString(cfg.Profile.ID) {
		return nil, errors.New("Profile was not found")
	}
	if err := validateProfileSettings(cfg.Settings); err != nil {
		return nil, err
	}
	dir, cacheDir, downloadDir := engineProfilePaths(cfg.Root, cfg.Profile.ID, engineStorageID(e.info.ID, e.path))
	for _, path := range []string{dir, cacheDir, downloadDir} {
		if err := os.MkdirAll(path, 0700); err != nil {
			return nil, fmt.Errorf("create profile directory: %w", err)
		}
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err = listener.Close(); err != nil {
		return nil, err
	}
	browserCtx, cancel := context.WithCancel(context.Background())
	l := newEngineLauncher(browserCtx, e.path, cfg, dir, cacheDir, port)
	cleanup := func() { cancel(); l.Kill() }
	stopCancel := context.AfterFunc(ctx, cleanup)
	defer stopCancel()
	control, err := l.Launch()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("launch browser: %w", err)
	}
	browser := rod.New().ControlURL(control).Context(browserCtx).NoDefaultDevice()
	if err = browser.Connect(); err != nil {
		cleanup()
		return nil, fmt.Errorf("connect to browser: %w", err)
	}
	if err = (proto.BrowserSetDownloadBehavior{Behavior: proto.BrowserSetDownloadBehaviorBehaviorAllow, DownloadPath: downloadDir}).Call(browser.Context(ctx)); err != nil {
		cleanup()
		return nil, fmt.Errorf("configure profile downloads: %w", err)
	}
	if err = configureEnginePermissions(browser.Context(ctx), cfg.Settings.Permissions); err != nil {
		cleanup()
		return nil, fmt.Errorf("configure profile permissions: %w", err)
	}
	if err = configureEngineTargets(browser, cfg.Settings, cfg.OnError); err != nil {
		cleanup()
		return nil, fmt.Errorf("configure profile environment: %w", err)
	}
	if err = ctx.Err(); err != nil {
		cleanup()
		return nil, err
	}
	return &profileBrowser{browser: browser, launcher: l, cancel: cancel}, nil
}

func configureEnginePermissions(client proto.Client, policy string) error {
	setting := proto.BrowserPermissionSettingPrompt
	if policy == "block" {
		setting = proto.BrowserPermissionSettingDenied
	}
	for _, name := range []string{"geolocation", "microphone", "camera"} {
		if err := (proto.BrowserSetPermission{Permission: &proto.BrowserPermissionDescriptor{Name: name}, Setting: setting}).Call(client); err != nil {
			return err
		}
	}
	return nil
}

// All page/iframe targets pause before their first script. The persistent browser
// context owns this listener; request contexts must never cancel it after Open.
// Existing targets also attach, but --no-startup-window prevents restored pages
// from running before this policy is installed at launch.
func configureEngineTargets(browser *rod.Browser, settings ProfileSettings, onError func(error)) error {
	auto := proto.TargetSetAutoAttach{AutoAttach: true, WaitForDebuggerOnStart: true, Flatten: true,
		Filter: proto.TargetTargetFilter{{Type: "page"}, {Type: "iframe"}, {Exclude: true}}}
	var configured sync.Map
	failures := make(chan error, 1)
	wait := browser.EachEvent(func(event *proto.TargetAttachedToTarget) {
		// Manual Rod attachments are not policy sessions. Reserve ownership in
		// event order before spawning work, so a later manual attach cannot race it.
		if !event.WaitingForDebugger {
			return
		}
		if _, loaded := configured.LoadOrStore(event.TargetInfo.TargetID, true); loaded {
			return
		}
		// Work independently: recursive auto-attach can emit events during the call.
		go func() {
			ctx, cancel := context.WithTimeout(browser.GetContext(), 10*time.Second)
			defer cancel()
			page := browser.Context(ctx).PageFromSession(event.SessionID)
			err := applyEngineEnvironment(page, settings)
			if err == nil {
				err = auto.Call(page)
			}
			if err == nil {
				err = (proto.RuntimeRunIfWaitingForDebugger{}).Call(page)
			}
			if err != nil {
				if browser.GetContext().Err() != nil {
					return
				}
				// Fail closed rather than silently run a page with only some chosen settings.
				closeCtx, closeCancel := context.WithTimeout(browser.GetContext(), 3*time.Second)
				_, _ = (proto.TargetCloseTarget{TargetID: event.TargetInfo.TargetID}).Call(browser.Context(closeCtx))
				closeCancel()
				select {
				case failures <- err:
				default:
				}
				if onError != nil {
					onError(fmt.Errorf("apply browser environment: %w", err))
				}
			}
		}()
	})
	go wait()
	if err := auto.Call(browser); err != nil {
		return err
	}
	// Launch deliberately has no initial pages. For callers applying to an existing
	// browser, synchronous attachment makes setup errors visible before return.
	targets, err := (proto.TargetGetTargets{}).Call(browser)
	if err != nil {
		return err
	}
	for _, target := range targets.TargetInfos {
		if target.Type != proto.TargetTargetInfoTypePage && target.Type != "iframe" {
			continue
		}
		attached, err := (proto.TargetAttachToTarget{TargetID: target.TargetID, Flatten: true}).Call(browser)
		if err != nil {
			return err
		}
		page := browser.PageFromSession(attached.SessionID)
		if err := applyEngineEnvironment(page, settings); err != nil {
			return err
		}
	}
	select {
	case err := <-failures:
		return err
	default:
		return nil
	}
}

func applyEngineEnvironment(client proto.Client, settings ProfileSettings) error {
	if settings.Timezone != "" {
		if err := (proto.EmulationSetTimezoneOverride{TimezoneID: settings.Timezone}).Call(client); err != nil {
			return err
		}
	}
	if settings.Language != "" {
		// Chromium shares this override between targets in a renderer. A native
		// LANG/LC_ALL default also initializes later isolates with the same locale.
		if err := (proto.EmulationSetLocaleOverride{Locale: settings.Language}).Call(client); err != nil && !strings.Contains(err.Error(), "Another locale override is already in effect") {
			return err
		}
	}
	return nil
}

func newEngineLauncher(browserCtx context.Context, path string, cfg launchConfig, dir, cacheDir string, port int) *launcher.Launcher {
	l := launcher.New()
	l.Flags = launcher.NewUserMode().Flags
	l = l.Context(browserCtx).Bin(path).Leakless(true).Headless(cfg.Headless).Devtools(false).
		UserDataDir(dir).RemoteDebuggingPort(port).Set("remote-debugging-address", "127.0.0.1").
		Set("disk-cache-dir", cacheDir).Set("disable-sync").Set("disable-notifications").Set("no-startup-window")
	// Only the explicit integration-test mode disables the OS sandbox.
	l.NoSandbox(cfg.Headless)
	if cfg.ProxyURL == "" {
		l.Set("no-proxy-server")
	} else {
		l.Set("proxy-server", cfg.ProxyURL).Set("proxy-bypass-list", "<-loopback>").Set("disable-quic").Set("force-webrtc-ip-handling-policy", "disable_non_proxied_udp")
	}
	if cfg.Settings.Width > 0 {
		l.Set("window-size", fmt.Sprintf("%d,%d", cfg.Settings.Width, cfg.Settings.Height))
	} else {
		l.Set("start-maximized")
	}
	if cfg.Settings.Language != "" {
		l.Set("lang", cfg.Settings.Language).Set("accept-lang", cfg.Settings.Language)
		l.Env(append(os.Environ(), "LANG="+strings.ReplaceAll(cfg.Settings.Language, "-", "_")+".UTF-8", "LC_ALL="+strings.ReplaceAll(cfg.Settings.Language, "-", "_")+".UTF-8")...)
	}
	return l
}

func engineStorageID(id, path string) string {
	if id != "custom" {
		return id
	}
	path = filepath.Clean(path)
	return fmt.Sprintf("custom-%x", sha256.Sum256([]byte(path)))
}

func engineProfilePaths(root, id, engineID string) (data, cache, downloads string) {
	profileRoot := filepath.Join(root, "profiles", id)
	engineRoot := profileRoot
	if engineID != "" && engineID != "chrome" {
		engineRoot = filepath.Join(profileRoot, engineID)
	}
	return filepath.Join(engineRoot, "browser"), filepath.Join(engineRoot, "cache"), filepath.Join(profileRoot, "downloads")
}
