package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Uses an already running emulator unless ISOLATE_ANDROID_LIVE_START names an
// AVD to start for this test. Only phones started by the test are stopped.
func TestAndroidLiveWebKit(t *testing.T) {
	if os.Getenv("ISOLATE_ANDROID_LIVE_TESTS") != "1" {
		t.Skip("set ISOLATE_ANDROID_LIVE_TESTS=1 with a running emulator and WebKitGTK")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	service := newAndroidService()
	defer service.ServiceShutdown()
	devices, err := service.run(ctx, service.adb, "devices")
	if err != nil {
		t.Fatal(err)
	}
	serials := parseAndroidDevices(devices)
	if len(serials) == 0 && os.Getenv("ISOLATE_ANDROID_LIVE_START") != "" {
		name := os.Getenv("ISOLATE_ANDROID_LIVE_START")
		if err := service.StartAndroidEmulator(ctx, name); err != nil {
			t.Fatal(err)
		}
		for ctx.Err() == nil {
			state, err := service.GetAndroidWorkspace(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, device := range state.Emulators {
				if device.Name == name && device.State == "running" {
					booted, err := service.run(ctx, service.adb, "-s", device.Serial, "shell", "getprop", "sys.boot_completed")
					if err == nil && strings.TrimSpace(string(booted)) == "1" {
						serials = []string{device.Serial}
					}
				}
			}
			if len(serials) > 0 {
				break
			}
			time.Sleep(time.Second)
		}
	}
	if len(serials) == 0 {
		t.Fatal("a running emulator is required")
	}
	source, err := service.StartAndroidScreen(ctx, serials[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range [][]string{{"shell", "wm", "size"}, {"shell", "wm", "density"}, {"shell", "head", "-n", "1", "/proc/meminfo"}} {
		output, err := service.run(ctx, service.adb, append([]string{"-s", serials[0]}, setting...)...)
		if err != nil {
			t.Fatal("could not read effective Android settings", err)
		}
		t.Logf("Effective %s: %s", strings.Join(setting, " "), strings.TrimSpace(string(output)))
	}
	command := exec.CommandContext(ctx, "node", "--input-type=module", "-e", `import ts from 'typescript'; import fs from 'node:fs'; process.stdout.write(ts.transpileModule(fs.readFileSync('src/androidMedia.ts','utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.ES2022}}).outputText);`)
	command.Dir = "frontend"
	script, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/player.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Write(script)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<video muted autoplay playsinline></video><script type="module">import {attachAndroidMedia} from '/player.js';document.querySelector('video').muted=true;window.playerError='';window.stopPlayer=attachAndroidMedia(document.querySelector('video'),%q,message=>window.playerError=message);</script>`, source)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	args := []string{"-a", "/usr/bin/python3", "build/test/android-webkit.py", server.URL}
	if duration := os.Getenv("ISOLATE_ANDROID_LIVE_PLAYBACK_SECONDS"); duration != "" {
		args = append(args, duration)
	}
	command = exec.CommandContext(ctx, "xvfb-run", args...)
	out, err := command.CombinedOutput()
	if err != nil {
		service.mu.Lock()
		stream := service.streams[serials[0]]
		service.mu.Unlock()
		stream.mu.Lock()
		detail := stream.err
		stream.mu.Unlock()
		t.Fatalf("live WebKitGTK playback: %v\n%s\nserver: %s", err, out, detail)
	}
	t.Log(string(out))
}
