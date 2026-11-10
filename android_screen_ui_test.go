package main

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
)

// Exercise the compiled Svelte component: fresh device objects must not recreate
// MediaSource or its websocket, while an explicit reconnect must replace both.
func TestAndroidScreenSurvivesDeviceRefresh(t *testing.T) {
	if os.Getenv("ISOLATE_ANDROID_SCREEN_UI_TESTS") != "1" {
		t.Skip("set ISOLATE_ANDROID_SCREEN_UI_TESTS=1 with Chrome and a built frontend")
	}
	method := func(name string) uint32 {
		h := fnv.New32a()
		_, _ = h.Write([]byte("main.AndroidService." + name))
		return h.Sum32()
	}
	var reads, sessions atomic.Int32
	var inputs atomic.Int32
	type receivedInput struct {
		Serial string
		Input  AndroidInput
	}
	var lastInput atomic.Value
	var clipboardReads atomic.Int32
	files := http.FileServer(http.Dir("frontend/dist"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wails/runtime" {
			files.ServeHTTP(w, r)
			return
		}
		var call struct {
			Object int `json:"object"`
			Method int `json:"method"`
			Args   struct {
				MethodID uint32            `json:"methodID"`
				Args     []json.RawMessage `json:"args"`
			} `json:"args"`
		}
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		var result any = []any{}
		if call.Object == 1 && call.Method == 1 {
			clipboardReads.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(" \n  from clipboard  \r\n\n second line \n ")
			return
		}
		switch call.Args.MethodID {
		case method("GetAndroidWorkspace"):
			result = AndroidWorkspaceState{Limit: 10, ADB: true, Emulator: true, Video: true, Emulators: []AndroidEmulator{{Name: "Isolate_01", Serial: "emulator-5554", State: "running", Width: 1080, Height: 2400, Audio: fmt.Sprintf("Status %d", reads.Add(1))}}}
		case method("StartAndroidScreen"):
			result = fmt.Sprintf("ws://127.0.0.1/fixture?session=%d", sessions.Add(1))
		case method("SendAndroidInput"):
			var received receivedInput
			if len(call.Args.Args) != 2 || json.Unmarshal(call.Args.Args[0], &received.Serial) != nil || json.Unmarshal(call.Args.Args[1], &received.Input) != nil {
				http.Error(w, "invalid input", http.StatusBadRequest)
				return
			}
			lastInput.Store(received)
			inputs.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
	page.MustWait(`() => document.querySelectorAll('.phone-shell').length===1`)
	page.MustEval(`() => {
  window.mediaAttachments=0;window.mediaCleanups=0;window.mediaSockets=0;window.mediaSocket=null;
  const create=URL.createObjectURL.bind(URL),revoke=URL.revokeObjectURL.bind(URL);
  URL.createObjectURL=value=>{if(value instanceof MediaSource)window.mediaAttachments++;return create(value);};
  URL.revokeObjectURL=value=>{window.mediaCleanups++;revoke(value);};
  window.WebSocket=class {constructor(){window.mediaSockets++;window.mediaSocket=this;}close(){}};
 }`)
	page.MustElementR("button", "^Connect screen$").MustClick()
	page.MustWait(`() => window.mediaSockets>=1`)
	initial := page.MustEval(`() => document.querySelector('video').getAttribute('src')`).Str()
	if !page.MustEval(`() => !document.querySelector('.phone-menu, .device-controls, [title="Phone controls"]')`).Bool() {
		t.Fatal("phone controls menu must be removed")
	}
	page.MustElement(".device-screen").MustClick()
	deadline := time.Now().Add(time.Second)
	for inputs.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if inputs.Load() != 1 {
		t.Fatal("only the direct screen click should send Android input", inputs.Load())
	}
	for _, direction := range []string{"down", "up", "right"} {
		before := inputs.Load()
		if !page.MustEval(`direction => {
  const screen = document.querySelector('.device-screen'), bounds = screen.getBoundingClientRect();
  return Array.from({length: 8}, () => {
    const event = new WheelEvent('wheel', {bubbles:true, cancelable:true, clientX:bounds.x+bounds.width/2, clientY:bounds.y+bounds.height/2,
      deltaX:direction==='right'?12:0, deltaY:direction==='down'?12:direction==='up'?-12:0});
    screen.dispatchEvent(event);
    return event.defaultPrevented;
  }).every(Boolean);
 }`, direction).Bool() {
			t.Fatal("wheel must not scroll the desktop workspace")
		}
		deadline = time.Now().Add(2 * time.Second)
		for inputs.Load() <= before && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if inputs.Load() != before+1 {
			t.Fatal("wheel burst must produce one swipe", inputs.Load()-before)
		}
		received := lastInput.Load().(receivedInput)
		gesture := received.Input
		if received.Serial != "emulator-5554" || gesture.AVDName != "Isolate_01" || gesture.Kind != "swipe" {
			t.Fatal("wheel targeted the wrong device or input", received)
		}
		if (direction == "down" && gesture.EndY >= gesture.Y) || (direction == "up" && gesture.EndY <= gesture.Y) || (direction == "right" && gesture.EndX >= gesture.X) {
			t.Fatal("wheel swipe moved in the wrong direction", direction, gesture)
		}
	}
	beforePaste := inputs.Load()
	page.MustEval(`() => {
  const screen = document.querySelector('.device-screen');
  for (const text of ['', ' \t\r\n ', ' \t hello  world \r\n\n   next line \n ']) {
    const clipboardData = new DataTransfer();
    clipboardData.setData('text/plain', text);
    screen.dispatchEvent(new ClipboardEvent('paste', {bubbles:true, cancelable:true, clipboardData}));
  }
 }`)
	deadline = time.Now().Add(time.Second)
	for inputs.Load() <= beforePaste && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if inputs.Load() != beforePaste+1 {
		t.Fatal("empty pastes must be ignored", inputs.Load()-beforePaste)
	}
	pasted := lastInput.Load().(receivedInput)
	if pasted.Serial != "emulator-5554" || pasted.Input.Kind != "text" || pasted.Input.Text != "hello  world next line" {
		t.Fatal("paste must remove blank lines and trim lines while preserving internal spaces", pasted)
	}
	beforePaste = inputs.Load()
	page.MustElement(".device-screen").MustFocus()
	page.KeyActions().Press(input.ControlLeft).Type(input.KeyV).Release(input.ControlLeft).MustDo()
	deadline = time.Now().Add(2 * time.Second)
	for inputs.Load() <= beforePaste && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if inputs.Load() != beforePaste+1 || clipboardReads.Load() != 1 {
		t.Fatal("Ctrl+V must read the host clipboard and paste exactly once", inputs.Load()-beforePaste, clipboardReads.Load())
	}
	pasted = lastInput.Load().(receivedInput)
	if pasted.Serial != "emulator-5554" || pasted.Input.Kind != "text" || pasted.Input.Text != "from clipboard second line" {
		t.Fatal("Ctrl+V did not send cleaned clipboard text", pasted)
	}
	assertSession := func(attachments, cleanups int) {
		t.Helper()
		result := page.MustEval(`() => ({attachments:window.mediaAttachments,cleanups:window.mediaCleanups,sockets:window.mediaSockets})`)
		if result.Get("attachments").Int() != attachments || result.Get("cleanups").Int() != cleanups || result.Get("sockets").Int() != attachments {
			t.Fatal("unexpected player lifecycle", result.JSON("", ""))
		}
	}
	for i := 0; i < 3; i++ {
		before := reads.Load()
		page.MustElementR("button", "^Refresh devices$").MustClick()
		deadline := time.Now().Add(time.Second)
		for reads.Load() <= before && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if reads.Load() <= before {
			t.Fatal("device refresh did not run")
		}
		assertSession(1, 0)
	}
	// Wait for two real polling intervals, matching the flicker in the recording.
	before := reads.Load()
	deadline = time.Now().Add(12 * time.Second)
	for reads.Load() < before+2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if reads.Load() < before+2 {
		t.Fatal("device polling stopped")
	}
	assertSession(1, 0)
	if page.MustEval(`() => document.querySelector('video').getAttribute('src')`).Str() != initial {
		t.Fatal("status polling changed the media URL")
	}
	// Exhaust automatic retries; each failure should create exactly one new
	// session, while fresh status callbacks must not reset the retry budget.
	for attempt := 0; attempt < 4; attempt++ {
		page.MustEval(`() => window.mediaSocket.onerror()`)
		if attempt < 3 {
			page.MustWait(`expected => window.mediaSockets === expected`, attempt+2)
		}
	}
	assertSession(4, 3)
	page.MustElementR("button", "^Retry screen$").MustClick()
	page.MustWait(`() => window.mediaSockets===5`)
	assertSession(5, 4)
	if page.MustEval(`() => document.querySelector('video').getAttribute('src')`).Str() == initial {
		t.Fatal("reconnect did not replace the media URL")
	}
	page.MustElementR("button", "^Profiles$").MustClick()
	page.MustWait(`() => !document.querySelector('.android-wall')`)
	assertSession(5, 5)
}
