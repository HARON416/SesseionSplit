package main

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

func TestAndroidSetupProgressUI(t *testing.T) {
	if os.Getenv("ISOLATE_ANDROID_SETUP_UI_TESTS") != "1" {
		t.Skip("set ISOLATE_ANDROID_SETUP_UI_TESTS=1 for setup progress browser checks")
	}
	var mu sync.Mutex
	state := AndroidSetupStatus{BundledTools: true, Running: true, Supported: true, Phase: "downloading", Stage: "Preparing Java runtime", Step: 1, Percent: 50, ProgressLabel: "Current download", DownloadedBytes: 256 << 20, TotalBytes: 512 << 20, BytesPerSecond: 4 << 20, LastActivity: time.Now().UnixMilli()}
	method := func(name string) uint32 {
		h := fnv.New32a()
		_, _ = h.Write([]byte("main.AndroidService." + name))
		return h.Sum32()
	}
	files := http.FileServer(http.Dir("frontend/dist"))
	accepted := false
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
		var result any = []any{}
		mu.Lock()
		defer mu.Unlock()
		switch call.Args.MethodID {
		case method("GetAndroidWorkspace"):
			result = AndroidWorkspaceState{Limit: 10, Emulators: []AndroidEmulator{}}
		case method("GetAndroidSetup"):
			result = state
		case method("RespondAndroidLicense"):
			if len(call.Args.Args) != 1 || json.Unmarshal(call.Args.Args[0], &accepted) != nil {
				http.Error(w, "invalid license response", 400)
				return
			}
			state.WaitingLicense = false
			state.Phase = "installing"
			state.Percent = -1
			state.ProgressLabel = ""
			state.LastActivity = time.Now().UnixMilli()
			result = nil
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	chrome, ok := launcher.LookPath()
	if !ok {
		t.Fatal("Chrome required")
	}
	launch := launcher.New().Context(ctx).Bin(chrome).Headless(true).NoSandbox(true)
	control, err := launch.Launch()
	if err != nil {
		t.Fatal(err)
	}
	defer launch.Cleanup()
	browser := rod.New().ControlURL(control).Context(ctx).MustConnect()
	defer browser.Close()
	page := browser.MustPage(server.URL)
	page.MustSetViewport(1280, 900, 1, false)
	wait := func(js string) {
		t.Helper()
		if err := page.Wait(rod.Eval(js)); err != nil {
			t.Fatal(err)
		}
	}
	wait(`() => document.querySelector('[role="progressbar"]')?.getAttribute('aria-valuenow') === '50' && document.querySelector('.transfer')?.textContent.includes('256.0 MiB of 512.0 MiB') && document.querySelector('.setup')?.textContent.includes('Java and streaming tools are included')`)
	if directory := os.Getenv("ISOLATE_UI_ARTIFACTS"); directory != "" {
		if err = os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		capture, err := page.Screenshot(false, &proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatPng})
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(directory+"/android-setup-progress.png", capture, 0600); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	state.Phase = "installing"
	state.BundledTools = false
	state.Step = 3
	state.Stage = "Installing Android tools and Google Play image"
	state.Percent = 67
	state.ProgressLabel = "Android installer progress"
	state.DownloadedBytes = 0
	state.TotalBytes = 0
	state.Detail = "Unzipping system image"
	mu.Unlock()
	wait(`() => document.querySelector('[role="progressbar"]')?.getAttribute('aria-valuenow') === '67' && document.querySelector('.progress-heading strong')?.textContent === 'Installing' && !document.querySelector('.transfer')`)
	mu.Lock()
	state.WaitingLicense = true
	state.Log = "Fixture license text. Accept? (y/N): "
	mu.Unlock()
	wait(`() => document.querySelector('.needs-response')?.textContent.includes('Waiting for license acceptance') && document.querySelector('details')?.open && !document.querySelector('[role="progressbar"]')`)
	page.MustElementR("button", "^Accept / continue$").MustClick()
	wait(`() => document.querySelector('.progress-track.indeterminate') && !document.querySelector('.needs-response')`)
	mu.Lock()
	didAccept := accepted
	state.Phase = "downloading"
	state.ProgressLabel = "Current download"
	state.DownloadedBytes = 8 << 20
	state.LastActivity = time.Now().Add(-30 * time.Second).UnixMilli()
	mu.Unlock()
	if !didAccept {
		t.Fatal("license was not explicitly accepted")
	}
	wait(`() => document.querySelector('.transfer')?.textContent.includes('total size unknown') && !document.querySelector('[role="progressbar"]')?.hasAttribute('aria-valuenow') && document.querySelector('.waiting')?.textContent.includes('No new data or status')`)
}
