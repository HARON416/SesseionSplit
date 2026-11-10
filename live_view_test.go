package main

import (
	"context"
	"encoding/base64"
	"image/jpeg"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

func TestLiveInputValidation(t *testing.T) {
	for _, input := range []LiveInput{{Kind: "click", X: 10, Y: 20}, {Kind: "wheel", X: 10, Y: 20, DeltaY: 200}, {Kind: "key", Key: "Enter", KeyCode: 13}, {Kind: "text", Text: "hello"}, {Kind: "reload"}} {
		if err := validateLiveInput(input); err != nil {
			t.Fatal(input.Kind, err)
		}
	}
	for _, input := range []LiveInput{{Kind: "evaluate"}, {Kind: "click", X: -1}, {Kind: "click", X: liveWidth}, {Kind: "wheel", DeltaY: math.Inf(1)}, {Kind: "click", X: math.NaN()}, {Kind: "text", Text: strings.Repeat("a", 8193)}, {Kind: "key", Modifiers: 16}, {Kind: "key", KeyCode: 256}} {
		if err := validateLiveInput(input); err == nil {
			t.Fatal("invalid input accepted", input.Kind)
		}
	}
}

func TestLiveViewRejectsMissingAndUnsupportedSessions(t *testing.T) {
	service, err := newProfileService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	profile, err := service.CreateProfileWithBrowser("Firefox", "firefox", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.OpenLiveProfile(context.Background(), profile.ID); err == nil || !strings.Contains(err.Error(), "Firefox") {
		t.Fatal("Firefox live session was not rejected", err)
	}
	if _, err := service.GetLiveFrame(context.Background(), profile.ID, true); err == nil {
		t.Fatal("nonrunning session produced a frame")
	}
	if err := service.SendLiveInput(context.Background(), "missing", LiveInput{Kind: "text", Text: "hello"}); err == nil {
		t.Fatal("missing session accepted input")
	}
}

func TestLiveBrowserFrameInputAndSessionIsolation(t *testing.T) {
	if os.Getenv("ISOLATE_BROWSER_TESTS") != "1" {
		t.Skip("set ISOLATE_BROWSER_TESTS=1 for live browser integration")
	}
	chrome, ok := launcher.LookPath()
	if !ok {
		t.Fatal("Chrome or Chromium required")
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/html")
		_, _ = writer.Write([]byte(`<html><head><title>Live test</title></head><body style="margin:0;height:2500px"><input id="entry" style="position:absolute;left:20px;top:20px;width:300px;height:60px"><button id="save" style="position:absolute;left:20px;top:110px;width:200px;height:60px" onclick="localStorage.setItem('entry',document.querySelector('#entry').value)">Save</button></body></html>`))
	}))
	defer server.Close()
	service, err := newProfileService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service.chromePath, service.headless = chrome, true
	t.Cleanup(func() { _ = service.ServiceShutdown() })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	first, err := service.CreateProfile("Live")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateProfile("Separate")
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []BrowserProfile{first, second} {
		if _, err := service.OpenLiveProfile(ctx, profile.ID); err != nil {
			t.Fatal(err)
		}
		live := service.browsers[profile.ID]
		page, err := live.livePage(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := page.Navigate(server.URL); err != nil {
			t.Fatal(err)
		}
		if err := page.WaitLoad(); err != nil {
			t.Fatal(err)
		}
	}
	for _, preview := range []bool{false, true} {
		frame, err := service.GetLiveFrame(ctx, first.ID, preview)
		if err != nil || frame.URL != server.URL+"/" || frame.Title != "Live test" {
			t.Fatalf("invalid frame: %s, %s, %v", frame.URL, frame.Title, err)
		}
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(frame.Image, "data:image/jpeg;base64,"))
		if err != nil {
			t.Fatal(err)
		}
		image, err := jpeg.DecodeConfig(strings.NewReader(string(data)))
		wantWidth := liveWidth
		if preview {
			wantWidth = int(liveWidth * 0.3)
		}
		if err != nil || image.Width != wantWidth {
			t.Fatalf("wrong frame dimensions: %+v, %v", image, err)
		}
	}
	for _, input := range []LiveInput{{Kind: "click", X: 80, Y: 50}, {Kind: "text", Text: "saved account"}, {Kind: "key", Key: "End", Code: "End", KeyCode: 35}, {Kind: "click", X: 80, Y: 140}, {Kind: "wheel", X: 900, Y: 500, DeltaY: 400}} {
		if err := service.SendLiveInput(ctx, first.ID, input); err != nil {
			t.Fatal(input.Kind, err)
		}
	}
	page, err := service.browsers[first.ID].livePage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	page.MustWait(`() => localStorage.getItem('entry') === 'saved account' && scrollY > 0`)
	if _, err := service.GetLiveFrame(ctx, first.ID, false); err != nil {
		t.Fatal("capture after scrolling", err)
	}
	other, err := service.browsers[second.ID].livePage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !other.MustEval(`() => localStorage.getItem('entry') === null`).Bool() {
		t.Fatal("live profiles shared storage")
	}
	if _, err := service.OpenProfile(ctx, first.ID); err == nil || !strings.Contains(err.Error(), "switching") {
		t.Fatal("external launch silently reused a headless session", err)
	}
	if err := service.CloseProfile(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetLiveFrame(ctx, first.ID, false); err == nil {
		t.Fatal("closed session still produced frames")
	}
	if _, err := service.OpenLiveProfile(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	page, err = service.browsers[first.ID].livePage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.Navigate(server.URL); err != nil {
		t.Fatal(err)
	}
	page.MustWait(`() => localStorage.getItem('entry') === 'saved account'`)
	if err := (proto.PageReload{}).Call(page); err != nil {
		t.Fatal(err)
	}
}
