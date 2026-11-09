package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFirefoxPreferencesAndReset(t *testing.T) {
	root := t.TempDir()
	prefsPath := filepath.Join(root, "prefs.js")
	previous := "user_pref(\"intl.accept_languages\", \"fr-FR\");\nuser_pref(\"intl.locale.requested\", \"fr-FR\");\nuser_pref(\"unrelated.preference\", true);\n"
	if err := os.WriteFile(prefsPath, []byte(previous), 0600); err != nil {
		t.Fatal(err)
	}
	for _, proxy := range []string{"http://127.0.0.1:12345", ""} {
		preferences, err := firefoxPreferences(launchConfig{ProxyURL: proxy, Settings: ProfileSettings{Permissions: "block"}}, "/cache", "/downloads")
		if err != nil {
			t.Fatal(err)
		}
		if preferences["remote.prefs.recommended"] != false || preferences["permissions.default.camera"] != 2 || preferences["browser.download.dir"] != "/downloads" {
			t.Fatal("incorrect Firefox profile policy", preferences)
		}
		if proxy != "" && (preferences["network.proxy.type"] != 1 || preferences["network.proxy.ssl_port"] != 12345 || preferences["network.proxy.no_proxies_on"] != "" || preferences["media.peerconnection.enabled"] != false) {
			t.Fatal("Firefox proxy policy is incomplete", preferences)
		}
		if proxy == "" && (preferences["network.proxy.type"] != 0 || preferences["media.peerconnection.enabled"] != true) {
			t.Fatal("direct mode did not reset proxy and WebRTC policies")
		}
		if err := writeFirefoxPreferences(root, preferences); err != nil {
			t.Fatal(err)
		}
	}
	remaining, err := os.ReadFile(prefsPath)
	if err != nil || strings.Contains(string(remaining), "intl.") || !strings.Contains(string(remaining), "unrelated.preference") {
		t.Fatal("reset changed unrelated preferences or retained managed language settings", err)
	}
	for _, proxy := range []string{"http://example.com:80", "http://user:pass@127.0.0.1:80", "socks5://127.0.0.1:80", "http://127.0.0.1:0"} {
		if _, err := firefoxPreferences(launchConfig{ProxyURL: proxy}, "/cache", "/downloads"); err == nil {
			t.Fatal("invalid gateway accepted", proxy)
		}
	}
	engine, err := resolveProfileEngine(ProfileSettings{Engine: "firefox"}, "/test/firefox")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := engine.(firefoxEngine); !ok {
		t.Fatal("Firefox was routed through Chromium")
	}
}

func TestFirefoxSessionsEnvironmentAndPersistence(t *testing.T) {
	if os.Getenv("ISOLATE_BROWSER_TESTS") != "1" {
		t.Skip("set ISOLATE_BROWSER_TESTS=1 for installed-browser integration")
	}
	path := findEnginePath("firefox")
	if override := os.Getenv("ISOLATE_FIREFOX_TEST_BINARY"); override != "" {
		path = override
	}
	if path == "" {
		t.Skip("Firefox is not installed")
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/html")
		fmt.Fprint(writer, `<script>window.first={language:navigator.language,locale:Intl.DateTimeFormat().resolvedOptions().locale,timezone:Intl.DateTimeFormat().resolvedOptions().timeZone};</script>`)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root := t.TempDir()
	cfg := launchConfig{Root: root, Headless: true, Profile: BrowserProfile{ID: strings.Repeat("c", 32), Name: "First"}, Settings: ProfileSettings{Engine: "firefox", Language: "fr-FR", Timezone: "Pacific/Honolulu", Permissions: "block"}}
	engine, err := resolveProfileEngine(cfg.Settings, path)
	if err != nil {
		t.Fatal(err)
	}
	launch := func(config launchConfig) *profileBrowser {
		t.Helper()
		live, err := engine.Launch(ctx, config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { live.kill(); live.cancel() })
		return live
	}
	contextID := func(live *profileBrowser) string {
		t.Helper()
		var tree struct {
			Contexts []struct {
				Context string `json:"context"`
			} `json:"contexts"`
		}
		if err := live.firefox.call(ctx, "browsingContext.getTree", map[string]any{"maxDepth": 0}, &tree); err != nil || len(tree.Contexts) == 0 {
			t.Fatal("no Firefox context", err)
		}
		return tree.Contexts[0].Context
	}
	navigate := func(live *profileBrowser, target string) {
		t.Helper()
		if err := live.firefox.call(ctx, "browsingContext.navigate", map[string]any{"context": target, "url": server.URL, "wait": "complete"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	evaluate := func(live *profileBrowser, target, expression string) string {
		t.Helper()
		var result struct {
			Type   string `json:"type"`
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		}
		if err := live.firefox.call(ctx, "script.evaluate", map[string]any{"expression": expression, "target": map[string]string{"context": target}, "awaitPromise": true}, &result); err != nil || result.Type != "success" {
			t.Fatal("Firefox script failed", err, result)
		}
		return result.Result.Value
	}
	first := launch(cfg)
	t.Log("first Firefox profile launched")
	page := contextID(first)
	navigate(first, page)
	var environment struct{ Language, Locale, Timezone string }
	if err := json.Unmarshal([]byte(evaluate(first, page, "JSON.stringify(window.first)")), &environment); err != nil || environment.Language != "fr-FR" || !strings.HasPrefix(environment.Locale, "fr") || environment.Timezone != "Pacific/Honolulu" {
		t.Fatal("environment was not applied before the first page script", environment, err)
	}
	evaluate(first, page, `localStorage.setItem('account','first');document.cookie='account=first;max-age=3600;path=/';'saved'`)
	var tab struct {
		Context string `json:"context"`
	}
	if err := first.firefox.call(ctx, "browsingContext.create", map[string]any{"type": "tab", "background": true}, &tab); err != nil {
		t.Fatal(err)
	}
	navigate(first, tab.Context)
	t.Log("second tab navigated")
	if err := json.Unmarshal([]byte(evaluate(first, tab.Context, "JSON.stringify(window.first)")), &environment); err != nil || environment.Timezone != "Pacific/Honolulu" || environment.Language != "fr-FR" {
		t.Fatal("new tab lost Firefox environment settings", environment, err)
	}
	otherCfg := cfg
	otherCfg.Profile.ID = strings.Repeat("d", 32)
	otherCfg.Profile.Name = "Second"
	second := launch(otherCfg)
	t.Log("second Firefox profile launched")
	otherPage := contextID(second)
	navigate(second, otherPage)
	if value := evaluate(second, otherPage, `JSON.stringify([localStorage.getItem('account'),document.cookie])`); value != `[null,""]` {
		t.Fatal("Firefox profiles share account data", value)
	}
	if err := first.close(ctx); err != nil {
		t.Fatal(err)
	}
	t.Log("first Firefox profile closed")
	restarted := launch(cfg)
	t.Log("first Firefox profile restarted")
	page = contextID(restarted)
	navigate(restarted, page)
	if value := evaluate(restarted, page, `JSON.stringify([localStorage.getItem('account'),document.cookie])`); value != `["first","account=first"]` {
		t.Fatal("Firefox lost saved account data on restart", value)
	}
	if err := restarted.activate(ctx); err != nil {
		t.Fatal("could not focus Firefox", err)
	}
	service := &ProfileService{root: root, profiles: []BrowserProfile{cfg.Profile, otherCfg.Profile}, browsers: map[string]*profileBrowser{cfg.Profile.ID: restarted, otherCfg.Profile.ID: second}}
	if err := service.CloseProfile(cfg.Profile.ID); err != nil {
		t.Fatal(err)
	}
	t.Log("profile service closed Firefox")
	data, _, _ := engineProfilePaths(root, cfg.Profile.ID, "firefox")
	if _, err := os.Stat(filepath.Join(data, "cookies.sqlite")); err != nil {
		t.Fatal("closing Firefox removed its persistent data", err)
	}
	if err := service.DeleteProfile(otherCfg.Profile.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "profiles", otherCfg.Profile.ID)); !os.IsNotExist(err) {
		t.Fatal("deleting Firefox profile retained browser data", err)
	}
	if _, err := os.Stat(data); err != nil {
		t.Fatal("deleting another Firefox profile changed existing data", err)
	}
}

func TestFirefoxAuthenticatedProxy(t *testing.T) {
	if os.Getenv("ISOLATE_BROWSER_TESTS") != "1" {
		t.Skip("set ISOLATE_BROWSER_TESTS=1 for installed-browser integration")
	}
	path := findEnginePath("firefox")
	if path == "" {
		t.Skip("Firefox is not installed")
	}
	seen := make(chan string, 8)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("user:secret")) {
			http.Error(writer, "proxy authentication required", 407)
			return
		}
		if request.URL.Path == "/probe" {
			seen <- request.URL.Host
			writer.Header().Set("Content-Type", "text/html")
			fmt.Fprint(writer, "<title>Firefox proxy probe</title>")
			return
		}
		http.Error(writer, "background request", 502)
	}))
	defer upstream.Close()
	address, _ := url.Parse(upstream.URL)
	proxy := proxyTestSettings(t, address.Host, "http")
	proxy.Username = "user"
	proxy.CredentialID = "firefox-proxy"
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	gateway, closeProxy, err := startProfileProxy(ctx, proxy, &memoryCredentialStore{values: map[string]string{"firefox-proxy": "secret"}})
	if err != nil {
		t.Fatal(err)
	}
	defer closeProxy()
	engine, err := resolveProfileEngine(ProfileSettings{Engine: "firefox"}, path)
	if err != nil {
		t.Fatal(err)
	}
	live, err := engine.Launch(ctx, launchConfig{Root: t.TempDir(), Profile: BrowserProfile{ID: strings.Repeat("e", 32)}, Settings: ProfileSettings{Engine: "firefox"}, ProxyURL: gateway, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer live.kill()
	defer live.close(ctx)
	var tree struct {
		Contexts []struct {
			Context string `json:"context"`
		} `json:"contexts"`
	}
	if err := live.firefox.call(ctx, "browsingContext.getTree", map[string]any{"maxDepth": 0}, &tree); err != nil || len(tree.Contexts) == 0 {
		t.Fatal("no Firefox page", err)
	}
	for _, target := range []string{"does-not-resolve.invalid:12345", "localhost:12345"} {
		if err := live.firefox.call(ctx, "browsingContext.navigate", map[string]any{"context": tree.Contexts[0].Context, "url": "http://" + target + "/probe", "wait": "complete"}, nil); err != nil {
			t.Fatal(err)
		}
		select {
		case host := <-seen:
			if host != target {
				t.Fatal("unexpected proxy destination", host)
			}
		case <-ctx.Done():
			t.Fatal("Firefox bypassed the authenticated proxy")
		}
	}
}
