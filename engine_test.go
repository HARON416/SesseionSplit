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

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

type engineRecordedCall struct {
	Method string
	Params any
}

func TestChromeDetectionExcludesOtherBrowsers(t *testing.T) {
	for _, platform := range []string{"windows", "linux", "darwin"} {
		paths := chromeCandidates(platform, func(key string) string {
			if key == "LOCALAPPDATA" {
				return filepath.Join("test", "user", "local")
			}
			return ""
		})
		if len(paths) == 0 {
			t.Fatal("no Chrome/Chromium candidates")
		}
		for _, path := range paths {
			lower := strings.ToLower(path)
			if strings.Contains(lower, "edge") || strings.Contains(lower, "brave") {
				t.Fatalf("Chrome selection could launch another browser: %s", path)
			}
		}
		if platform == "windows" {
			want := filepath.Join("test", "user", "local", "Google", "Chrome", "Application", "chrome.exe")
			found := false
			for _, path := range paths {
				if path == want {
					found = true
				}
			}
			if !found {
				t.Fatal("per-user Windows Chrome installation was not considered")
			}
		}
	}
}

type engineRecordingClient struct{ calls []engineRecordedCall }

func (c *engineRecordingClient) Call(_ context.Context, _ string, method string, params any) ([]byte, error) {
	c.calls = append(c.calls, engineRecordedCall{method, params})
	return []byte(`{}`), nil
}

func TestEnginePermissionPolicy(t *testing.T) {
	for _, policy := range []string{"", "ask", "block"} {
		t.Run(policy, func(t *testing.T) {
			c := &engineRecordingClient{}
			if err := configureEnginePermissions(c, policy); err != nil {
				t.Fatal(err)
			}
			if len(c.calls) != 3 {
				t.Fatal(c.calls)
			}
			for i, name := range []string{"geolocation", "microphone", "camera"} {
				request := c.calls[i].Params.(proto.BrowserSetPermission)
				expected := proto.BrowserPermissionSettingPrompt
				if policy == "block" {
					expected = proto.BrowserPermissionSettingDenied
				}
				if request.Permission.Name != name || request.Setting != expected || request.Origin != "" {
					t.Fatalf("incorrect browser-wide policy: %#v", request)
				}
			}
		})
	}
}

func TestEngineResolutionAndCapabilities(t *testing.T) {
	for _, id := range []string{"chrome", "brave"} {
		engine, err := resolveProfileEngine(ProfileSettings{Engine: id}, "/test/explicit-browser")
		if err != nil {
			t.Fatal(err)
		}
		if engine.Info().ID != id || !engine.Info().Available {
			t.Fatal(engine.Info())
		}
	}
	if _, err := resolveProfileEngine(ProfileSettings{Engine: "unknown"}, "/test/browser"); err == nil {
		t.Fatal("accepted unknown engine")
	}
	if findEnginePath("brave") == "" {
		if _, err := resolveProfileEngine(ProfileSettings{Engine: "brave"}, ""); err == nil || !strings.Contains(err.Error(), "Install Brave") {
			t.Fatalf("missing Brave must not fall back to Chrome: %v", err)
		}
	}
	chrome, brave := engineDetails("chrome"), engineDetails("brave")
	if !strings.Contains(chrome.FingerprintProtection, "no added") || !strings.Contains(brave.Notes, "does not verify") {
		t.Fatal("misleading engine capabilities")
	}
}

func TestEngineEnvironmentFirstScriptsAndNewTabs(t *testing.T) {
	if os.Getenv("ISOLATE_BROWSER_TESTS") != "1" {
		t.Skip("set ISOLATE_BROWSER_TESTS=1 for installed-browser integration")
	}
	for _, definition := range browserCatalog {
		engineID := definition.id
		if engineID == "firefox" {
			continue
		}
		t.Run(engineID, func(t *testing.T) {
			override := os.Getenv("ISOLATE_" + strings.ToUpper(strings.ReplaceAll(engineID, "-", "_")) + "_TEST_BINARY")
			if findEnginePath(engineID) == "" && override == "" {
				t.Skipf("%s is not installed; no download or substituted browser", engineID)
			}
			engine, err := resolveProfileEngine(ProfileSettings{Engine: engineID}, override)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			cfg := launchConfig{Profile: BrowserProfile{ID: strings.Repeat("a", 32)}, Root: t.TempDir(), Headless: true,
				Settings: ProfileSettings{Engine: engineID, BrowserPath: override, Language: "fr-FR", Timezone: "Pacific/Honolulu", Permissions: "block", Width: 800, Height: 600}}
			for restart := 0; restart < 2; restart++ {
				func() {
					live, err := engine.Launch(ctx, cfg)
					if err != nil {
						t.Fatal(err)
					}
					defer live.cancel()
					defer live.launcher.Kill()
					defer live.browser.Context(ctx).Close()
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "text/html")
						fmt.Fprint(w, `<script>window.first={language:navigator.language,locale:new Intl.Locale(Intl.DateTimeFormat().resolvedOptions().locale).maximize().baseName,timezone:Intl.DateTimeFormat().resolvedOptions().timeZone};if(window!==parent)parent.postMessage(window.first,'*');else{window.addEventListener('message',e=>window.childFirst=e.data);const frame=document.createElement('iframe');frame.src=location.href.replace('127.0.0.1','localhost')+'child';document.documentElement.append(frame)}</script>`)
					}))
					defer server.Close()
					// Raw creation navigates immediately; settings must precede the first script,
					// not depend on the caller selecting or configuring a single Rod page.
					for i := 0; i < 2; i++ {
						target, err := (proto.TargetCreateTarget{URL: server.URL}).Call(live.browser.Context(ctx))
						if err != nil {
							t.Fatal(err)
						}
						page, err := live.browser.Context(ctx).PageFromTarget(target.TargetID)
						if err != nil {
							t.Fatal(err)
						}
						if err := page.Wait(rod.Eval(`() => !!window.first`)); err != nil {
							t.Fatal(err)
						}
						value, err := page.Eval(`() => JSON.stringify(window.first)`)
						if err != nil {
							t.Fatal(err)
						}
						var first struct{ Language, Locale, Timezone string }
						if err := json.Unmarshal([]byte(value.Value.Str()), &first); err != nil {
							t.Fatalf("first script absent: %s: %v", value.Value, err)
						}
						if first.Timezone != "Pacific/Honolulu" || first.Locale != "fr-Latn-FR" || first.Language != "fr-FR" {
							t.Fatalf("page %d: %+v", i, first)
						}
						child, err := page.Eval(`() => new Promise(resolve=>{const poll=()=>window.childFirst ? resolve(JSON.stringify(window.childFirst)) : setTimeout(poll,10);poll()})`)
						if err != nil {
							t.Fatal(err)
						}
						if err := json.Unmarshal([]byte(child.Value.Str()), &first); err != nil {
							t.Fatal(err)
						}
						if first.Timezone != "Pacific/Honolulu" || first.Locale != "fr-Latn-FR" || first.Language != "fr-FR" {
							t.Fatalf("cross-origin iframe %d: %+v", i, first)
						}
						permission, err := page.Eval(`() => Promise.all(['geolocation','microphone','camera'].map(async name => (await navigator.permissions.query({name})).state))`)
						if err != nil {
							t.Fatal(err)
						}
						if permission.Value.Get("0").Str() != "denied" || permission.Value.Get("1").Str() != "denied" || permission.Value.Get("2").Str() != "denied" {
							t.Fatalf("permissions not blocked: %s", permission.Value)
						}
					}
				}()
			}

		})
	}
}

func TestEngineLaunchPolicyAndStoragePaths(t *testing.T) {
	for _, engine := range []string{"chrome", "brave"} {
		data, cache, downloads := engineProfilePaths("/root", "profile-id", engine)
		if strings.Contains(downloads, "brave") {
			t.Fatal("downloads should remain at profile root")
		}
		if engine == "chrome" && (data != filepath.Join("/root", "profiles", "profile-id", "browser") || cache != filepath.Join("/root", "profiles", "profile-id", "cache")) {
			t.Fatal("legacy Chrome paths changed")
		}
		if engine == "brave" && (!strings.Contains(data, filepath.Join("brave", "browser")) || !strings.Contains(cache, filepath.Join("brave", "cache"))) {
			t.Fatal("Brave must not share Chrome storage")
		}
	}
	for _, proxy := range []string{"", "http://127.0.0.1:12345"} {
		cfg := launchConfig{ProxyURL: proxy, Settings: ProfileSettings{Width: 800, Height: 600, Language: "fr-FR"}}
		l := newEngineLauncher(context.Background(), "/test/browser", cfg, "/test/data", "/test/cache", 9222)
		if l.Has("no-sandbox") {
			t.Fatal("production browser sandbox disabled")
		}
		if !l.Has("no-startup-window") || l.Get("remote-debugging-address") != "127.0.0.1" {
			t.Fatal("startup or debug binding unsafe")
		}
		if proxy == "" && (!l.Has("no-proxy-server") || l.Has("proxy-server")) {
			t.Fatal("direct mode must explicitly bypass system proxy")
		}
		if proxy != "" && (l.Has("no-proxy-server") || l.Get("proxy-server") != proxy || l.Get("proxy-bypass-list") != "<-loopback>" || !l.Has("disable-quic") || l.Get("force-webrtc-ip-handling-policy") != "disable_non_proxied_udp") {
			t.Fatal("missing proxy routing policy")
		}
		if l.Get("window-size") != "800,600" || l.Has("start-maximized") {
			t.Fatal("custom dimensions not selected")
		}
	}
}

func TestEngineAuthenticatedProxyRoutesBrowserTraffic(t *testing.T) {
	if os.Getenv("ISOLATE_BROWSER_TESTS") != "1" {
		t.Skip("set ISOLATE_BROWSER_TESTS=1 for browser integration")
	}
	for _, definition := range browserCatalog {
		id := definition.id
		if id == "firefox" {
			continue
		}
		t.Run(id, func(t *testing.T) {
			override := os.Getenv("ISOLATE_" + strings.ToUpper(strings.ReplaceAll(id, "-", "_")) + "_TEST_BINARY")
			if override == "" && findEnginePath(id) == "" {
				t.Skip("browser not installed")
			}
			engine, err := resolveProfileEngine(ProfileSettings{Engine: id}, override)
			if err != nil {
				t.Fatal(err)
			}
			seen := make(chan string, 4)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("user:secret")) {
					http.Error(w, "proxy auth required", 407)
					return
				}
				if r.Method == "CONNECT" {
					http.Error(w, "unrelated background connection", 502)
					return
				}
				if r.URL.Path == "/probe" {
					select {
					case seen <- r.URL.Host:
					default:
					}
					w.Header().Set("Content-Type", "text/html")
					fmt.Fprint(w, "<title>authenticated-upstream</title>")
					return
				}
				http.NotFound(w, r)
			}))
			defer upstream.Close()
			u, _ := url.Parse(upstream.URL)
			proxy := proxyTestSettings(t, u.Host, "http")
			proxy.Username = "user"
			proxy.CredentialID = "engine-proxy"
			bridge, closeProxy, err := startProfileProxy(context.Background(), proxy, &memoryCredentialStore{values: map[string]string{"engine-proxy": "secret"}})
			if err != nil {
				t.Fatal(err)
			}
			defer closeProxy()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			live, err := engine.Launch(ctx, launchConfig{Profile: BrowserProfile{ID: strings.Repeat("b", 32)}, Settings: ProfileSettings{Engine: id, BrowserPath: override}, Root: t.TempDir(), Headless: true, ProxyURL: bridge})
			if err != nil {
				t.Fatal(err)
			}
			defer live.cancel()
			defer live.launcher.Kill()
			defer live.browser.Context(ctx).Close()
			for _, target := range []string{"does-not-resolve.invalid:12345", "localhost:12345"} {
				page, err := live.browser.Context(ctx).Page(proto.TargetCreateTarget{URL: "http://" + target + "/probe"})
				if err != nil {
					t.Fatal(err)
				}
				if err := page.Wait(rod.Eval(`() => document.title==='authenticated-upstream'`)); err != nil {
					t.Fatal(err)
				}
				select {
				case host := <-seen:
					if host != target {
						t.Fatalf("wrong proxy destination %s, expected %s", host, target)
					}
				case <-ctx.Done():
					t.Fatal("browser bypassed authenticated upstream")
				}
			}
		})
	}
}
