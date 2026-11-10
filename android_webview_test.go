package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

func testAndroidWebView(t *testing.T, ctx context.Context, video []byte, mime string, audio []byte, audioFirst bool) {
	t.Helper()
	command := exec.CommandContext(ctx, "node", "--input-type=module", "-e", `import ts from 'typescript'; import fs from 'node:fs'; process.stdout.write(ts.transpileModule(fs.readFileSync('src/androidMedia.ts','utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.ES2022}}).outputText);`)
	command.Dir = "frontend"
	script, err := command.Output()
	if err != nil {
		t.Fatal("transpile Android player", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/player.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write(script)
	})
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx := conn.CloseRead(r.Context())
		tracks := []struct {
			mime string
			data []byte
		}{{mime, video}, {`audio/mp4; codecs="mp4a.40.2"`, audio}}
		if len(audio) == 0 {
			tracks = tracks[:1]
			if err = conn.Write(ctx, websocket.MessageText, []byte(`{"track":1,"disabled":true}`)); err != nil {
				return
			}
		} else if audioFirst {
			tracks[0], tracks[1] = tracks[1], tracks[0]
		}
		for _, track := range tracks {
			index := 0
			if track.mime[0] == 'a' {
				index = 1
			}
			info, _ := json.Marshal(map[string]any{"track": index, "mime": track.mime})
			if err = conn.Write(ctx, websocket.MessageText, info); err != nil {
				return
			}
			if err = conn.Write(ctx, websocket.MessageBinary, append([]byte{byte(index)}, track.data...)); err != nil {
				return
			}
			// Let the first track's initialization reach the decoder before the
			// second track arrives, as scrcpy's independent encoders can do.
			time.Sleep(100 * time.Millisecond)
		}
		<-ctx.Done()
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<video muted autoplay playsinline></video><script type="module">import {attachAndroidMedia} from '/player.js';document.querySelector('video').muted=true;window.playerError='';const video=document.querySelector('video');const detach=attachAndroidMedia(video,'ws://'+location.host+'/stream',message=>window.playerError=message);window.stopPlayer=detach;</script>`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	if os.Getenv("ISOLATE_ANDROID_WEBKIT_TESTS") == "1" {
		command := exec.CommandContext(ctx, "xvfb-run", "-a", "/usr/bin/python3", "build/test/android-webkit.py", server.URL)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("native WebKitGTK playback: %v\n%s", err, output)
		}
		t.Log(string(output))
	}
	chrome, ok := launcher.LookPath()
	if !ok {
		t.Fatal("Chrome required")
	}
	launch := launcher.New().Bin(chrome).Headless(true).UserDataDir(filepath.Join(t.TempDir(), "chrome")).Set("autoplay-policy", "no-user-gesture-required").NoSandbox(true)
	defer launch.Cleanup()
	url, err := launch.Launch()
	if err != nil {
		t.Fatal(err)
	}
	browser := rod.New().ControlURL(url).Context(ctx)
	if err = browser.Connect(); err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	page, err := browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		t.Fatal(err)
	}
	if err = page.Navigate(server.URL); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		result, err := page.Eval(`() => ({error:window.playerError || '', width:document.querySelector('video').videoWidth, time:document.querySelector('video').currentTime, frames:document.querySelector('video').getVideoPlaybackQuality().totalVideoFrames})`)
		if err != nil {
			t.Fatal(err)
		}
		if result.Value.Get("error").Str() != "" {
			t.Fatal("Android browser playback failed", result.Value.String())
		}
		if result.Value.Get("width").Int() == 360 && result.Value.Get("time").Num() > 0.4 && result.Value.Get("frames").Int() > 10 {
			page.MustEval(`() => window.stopPlayer()`)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("Android player did not decode and play video and audio", page.MustEval(`() => ({error:window.playerError,ready:document.querySelector('video').readyState,mediaError:document.querySelector('video').error?.message,time:document.querySelector('video').currentTime})`).String())
}
