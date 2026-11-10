package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type firefoxEngine struct {
	path string
	info EngineInfo
}

func (engine firefoxEngine) Info() EngineInfo { return engine.info }

type firefoxBrowser struct {
	command *exec.Cmd
	done    chan struct{}
	conn    *websocket.Conn
	mu      sync.Mutex
	nextID  uint64
}

var firefoxEndpointPattern = regexp.MustCompile(`WebDriver BiDi listening on (ws://(?:127\.0\.0\.1|localhost|\[::1\]):[0-9]+)`)

func (engine firefoxEngine) Launch(ctx context.Context, cfg launchConfig) (*profileBrowser, error) {
	if !profileIDPattern.MatchString(cfg.Profile.ID) {
		return nil, errors.New("Profile was not found")
	}
	if err := validateProfileSettings(cfg.Settings); err != nil {
		return nil, err
	}
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := startup.Err(); err != nil {
		return nil, err
	}
	dir, cache, downloads := engineProfilePaths(cfg.Root, cfg.Profile.ID, "firefox")
	for _, path := range []string{dir, cache, downloads} {
		if err := os.MkdirAll(path, 0700); err != nil {
			return nil, fmt.Errorf("create Firefox profile: %w", err)
		}
	}
	preferences, err := firefoxPreferences(cfg, cache, downloads)
	if err != nil {
		return nil, err
	}
	if err := writeFirefoxPreferences(dir, preferences); err != nil {
		return nil, err
	}
	arguments := []string{"--no-remote", "--new-instance", "--profile", dir, "--remote-debugging-port", "0"}
	if runtime.GOOS == "windows" {
		arguments = append(arguments, "--wait-for-browser")
	}
	if cfg.Headless {
		arguments = append(arguments, "--headless")
	}
	if cfg.Settings.Width > 0 {
		arguments = append(arguments, "--width", strconv.Itoa(cfg.Settings.Width), "--height", strconv.Itoa(cfg.Settings.Height))
	}
	arguments = append(arguments, "about:blank")
	command := exec.Command(engine.path, arguments...)
	command.Stdout = io.Discard
	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		stderr.Close()
		return nil, fmt.Errorf("launch Firefox: %w", err)
	}
	control := &firefoxBrowser{command: command, done: make(chan struct{})}
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			if match := firefoxEndpointPattern.FindStringSubmatch(scanner.Text()); match != nil {
				select {
				case ready <- match[1] + "/session":
				default:
				}
			}
		}
	}()
	go func() {
		_ = command.Wait()
		close(control.done)
	}()
	launched := false
	defer func() {
		if !launched {
			control.kill()
		}
	}()
	var endpoint string
	select {
	case endpoint = <-ready:
	case <-control.done:
		return nil, errors.New("Firefox exited before its control connection was ready")
	case <-startup.Done():
		return nil, fmt.Errorf("connect to Firefox: %w", startup.Err())
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	control.conn, _, err = websocket.Dial(startup, endpoint, &websocket.DialOptions{HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		return nil, fmt.Errorf("connect to Firefox: %w", err)
	}
	control.conn.SetReadLimit(8 << 20)
	if err := control.call(startup, "session.new", map[string]any{"capabilities": map[string]any{}}, nil); err != nil {
		return nil, fmt.Errorf("initialize Firefox session: %w", err)
	}
	for _, override := range []struct{ method, key, value string }{
		{"emulation.setTimezoneOverride", "timezone", cfg.Settings.Timezone},
		{"emulation.setLocaleOverride", "locale", cfg.Settings.Language},
	} {
		if override.value != "" {
			if err := control.call(startup, override.method, map[string]any{override.key: override.value, "userContexts": []string{"default"}}, nil); err != nil {
				return nil, fmt.Errorf("Firefox does not support the selected language or timezone settings; update Firefox or leave these settings blank: %w", err)
			}
		}
	}
	launched = true
	return &profileBrowser{firefox: control, cancel: func() { _ = control.conn.CloseNow() }}, nil
}

func firefoxPreferences(cfg launchConfig, cache, downloads string) (map[string]any, error) {
	permission := 0
	if cfg.Settings.Permissions == "block" {
		permission = 2
	}
	preferences := map[string]any{
		"remote.prefs.recommended":                 false,
		"remote.experimental.enabled":              true,
		"browser.shell.checkDefaultBrowser":        false,
		"browser.startup.page":                     0,
		"browser.startup.homepage":                 "about:blank",
		"browser.startup.homepage_override.mstone": "ignore",
		"browser.sessionstore.resume_from_crash":   false,
		"browser.sessionstore.resume_session_once": false,
		"browser.cache.disk.parent_directory":      cache,
		"browser.download.folderList":              2,
		"browser.download.dir":                     downloads,
		"browser.download.useDownloadDir":          true,
		"identity.fxaccounts.enabled":              false,
		"permissions.default.geo":                  permission,
		"permissions.default.camera":               permission,
		"permissions.default.microphone":           permission,
		"network.proxy.type":                       0,
		"media.peerconnection.enabled":             cfg.ProxyURL == "",
		"network.dns.disablePrefetch":              cfg.ProxyURL != "",
		"network.prefetch-next":                    cfg.ProxyURL == "",
	}
	if cfg.Settings.Language != "" {
		preferences["intl.accept_languages"] = cfg.Settings.Language
		preferences["intl.locale.requested"] = cfg.Settings.Language
	}
	if cfg.ProxyURL != "" {
		proxy, err := url.Parse(cfg.ProxyURL)
		if err != nil || proxy.Scheme != "http" || proxy.Hostname() != "127.0.0.1" || proxy.User != nil {
			return nil, errors.New("Invalid Firefox proxy gateway")
		}
		port, err := strconv.Atoi(proxy.Port())
		if err != nil || port < 1 || port > 65535 {
			return nil, errors.New("Invalid Firefox proxy gateway")
		}
		preferences["network.proxy.type"] = 1
		preferences["network.proxy.http"] = proxy.Hostname()
		preferences["network.proxy.http_port"] = port
		preferences["network.proxy.ssl"] = proxy.Hostname()
		preferences["network.proxy.ssl_port"] = port
		preferences["network.proxy.no_proxies_on"] = ""
		preferences["network.proxy.allow_hijacking_localhost"] = true
		preferences["network.trr.mode"] = 5
	}
	return preferences, nil
}

func writeFirefoxPreferences(dir string, preferences map[string]any) error {
	path := filepath.Join(dir, "prefs.js")
	previous, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := strings.Split(string(previous), "\n")
	kept := []string{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, `user_pref("intl.accept_languages",`) || strings.HasPrefix(trimmed, `user_pref("intl.locale.requested",`) {
			continue
		}
		kept = append(kept, line)
	}
	if next := strings.Join(kept, "\n"); next != string(previous) {
		if err := writeFirefoxFile(path, []byte(next)); err != nil {
			return err
		}
	}
	keys := make([]string, 0, len(preferences))
	for key := range preferences {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var content strings.Builder
	for _, key := range keys {
		name, _ := json.Marshal(key)
		value, err := json.Marshal(preferences[key])
		if err != nil {
			return err
		}
		fmt.Fprintf(&content, "user_pref(%s, %s);\n", name, value)
	}
	return writeFirefoxFile(filepath.Join(dir, "user.js"), []byte(content.String()))
}

func writeFirefoxFile(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".firefox-prefs-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func (browser *firefoxBrowser) call(ctx context.Context, method string, params any, result any) error {
	browser.mu.Lock()
	defer browser.mu.Unlock()
	browser.nextID++
	request, err := json.Marshal(map[string]any{"id": browser.nextID, "method": method, "params": params})
	if err != nil {
		return err
	}
	if err := browser.conn.Write(ctx, websocket.MessageText, request); err != nil {
		return err
	}
	for {
		_, data, err := browser.conn.Read(ctx)
		if err != nil {
			return err
		}
		var response struct {
			ID      uint64          `json:"id"`
			Type    string          `json:"type"`
			Error   string          `json:"error"`
			Message string          `json:"message"`
			Result  json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(data, &response); err != nil {
			return err
		}
		if response.ID != browser.nextID {
			continue
		}
		if response.Type == "error" {
			return fmt.Errorf("%s: %s", response.Error, response.Message)
		}
		if result != nil {
			return json.Unmarshal(response.Result, result)
		}
		return nil
	}
}

func (browser *firefoxBrowser) activate(ctx context.Context) error {
	var tree struct {
		Contexts []struct {
			Context string `json:"context"`
			URL     string `json:"url"`
		} `json:"contexts"`
	}
	if err := browser.call(ctx, "browsingContext.getTree", map[string]any{"maxDepth": 0}, &tree); err != nil {
		return err
	}
	if len(tree.Contexts) == 0 {
		return errors.New("Firefox has no open browser windows")
	}
	selected := tree.Contexts[0]
	if selected.URL == "about:blank" {
		if err := browser.call(ctx, "browsingContext.navigate", map[string]any{"context": selected.Context, "url": "https://google.com/", "wait": "none"}, nil); err != nil {
			return err
		}
	}
	return browser.call(ctx, "browsingContext.activate", map[string]any{"context": selected.Context}, nil)
}

func (browser *firefoxBrowser) close(ctx context.Context) error {
	_ = browser.call(ctx, "browser.close", map[string]any{}, nil)
	select {
	case <-browser.done:
		_ = browser.conn.CloseNow()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (browser *firefoxBrowser) kill() {
	if browser.conn != nil {
		_ = browser.conn.CloseNow()
	}
	_ = browser.command.Process.Kill()
	<-browser.done
}

func (live *profileBrowser) activate(ctx context.Context) error {
	if live.firefox != nil {
		return live.firefox.activate(ctx)
	}
	return activateProfileBrowser(ctx, live.browser)
}

func (live *profileBrowser) close(ctx context.Context) error {
	if live.firefox != nil {
		return live.firefox.close(ctx)
	}
	return live.browser.Context(ctx).Close()
}

func (live *profileBrowser) kill() {
	if live.firefox != nil {
		live.firefox.kill()
		return
	}
	live.launcher.Kill()
}
