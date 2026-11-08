package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	if id == "brave" {
		return "Brave"
	}
	if strings.Contains(strings.ToLower(filepath.Base(findEnginePath("chrome"))), "chromium") {
		return "Chromium"
	}
	return "Chrome"
}

func engineDetails(id string) EngineInfo {
	if id == "brave" {
		return EngineInfo{ID: id, Name: "Brave", FingerprintProtection: "Native Brave fingerprinting protections", Notes: "Uses the installed Brave browser and its native Shields defaults. SessionSplit does not verify per-site Shields state or guarantee anonymity. Timezone and locale overrides cover page and iframe targets, not worker execution contexts. Browser updates are managed by Brave or your operating system. https://github.com/brave/brave-browser/wiki/Fingerprinting-Protections"}
	}
	return EngineInfo{ID: "chrome", Name: "Chrome / Chromium", FingerprintProtection: "Standard browser protections; no added fingerprint protection", Notes: "Uses installed Chrome or Chromium. Profile isolation separates storage, not browser identity. Timezone and locale overrides cover page and iframe targets, not worker execution contexts. Browser updates are managed by the browser vendor or your operating system."}
}

func resolveProfileEngine(settings ProfileSettings, overridePath string) (browserEngine, error) {
	id := settings.Engine
	if id == "" {
		id = "chrome"
	}
	if id != "chrome" && id != "brave" {
		return nil, errors.New("Choose Chrome or Brave as the browser")
	}
	path := overridePath
	if path == "" {
		path = findEnginePath(id)
	}
	if path == "" {
		if id == "brave" {
			return nil, errors.New("Install Brave to open this profile; SessionSplit will not substitute another browser")
		}
		return nil, errors.New("Install Chrome or Chromium to open a profile")
	}
	info := engineDetails(id)
	info.Available = true
	return chromiumEngine{path: path, info: info}, nil
}

func findEnginePath(id string) string {
	if id == "chrome" {
		for _, path := range chromeCandidates(runtime.GOOS, os.Getenv) {
			if resolved, err := exec.LookPath(path); err == nil {
				return resolved
			}
		}
		return ""
	}
	for _, name := range []string{"brave-browser", "brave-browser-stable", "brave"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	candidates := []string{}
	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates, "/Applications/Brave Browser.app/Contents/MacOS/Brave Browser")
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, "Applications/Brave Browser.app/Contents/MacOS/Brave Browser"))
		}
	case "windows":
		for _, key := range []string{"PROGRAMFILES", "PROGRAMFILES(X86)", "LOCALAPPDATA"} {
			if root := os.Getenv(key); root != "" {
				candidates = append(candidates, filepath.Join(root, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"))
			}
		}
	default:
		candidates = append(candidates, "/opt/brave.com/brave/brave-browser", "/snap/bin/brave")
	}
	for _, path := range candidates {
		if stat, err := os.Stat(path); err == nil && !stat.IsDir() && (runtime.GOOS == "windows" || stat.Mode()&0111 != 0) {
			return path
		}
	}
	return ""
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
	for _, id := range []string{"chrome", "brave"} {
		info := engineDetails(id)
		if path := findEnginePath(id); path != "" {
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
	dir, cacheDir, downloadDir := engineProfilePaths(cfg.Root, cfg.Profile.ID, e.info.ID)
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

func engineProfilePaths(root, id, engineID string) (data, cache, downloads string) {
	profileRoot := filepath.Join(root, "profiles", id)
	engineRoot := profileRoot
	if engineID == "brave" {
		engineRoot = filepath.Join(profileRoot, "brave")
	}
	return filepath.Join(engineRoot, "browser"), filepath.Join(engineRoot, "cache"), filepath.Join(profileRoot, "downloads")
}
