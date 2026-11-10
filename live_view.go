package main

import (
	"context"
	"encoding/base64"
	"errors"
	"math"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

const liveWidth = 1100
const liveHeight = 760

type LiveFrame struct {
	Image  string `json:"image"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type LiveInput struct {
	Kind      string  `json:"kind"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	DeltaX    float64 `json:"deltaX"`
	DeltaY    float64 `json:"deltaY"`
	Text      string  `json:"text"`
	Key       string  `json:"key"`
	Code      string  `json:"code"`
	KeyCode   int     `json:"keyCode"`
	Modifiers int     `json:"modifiers"`
}

func (s *ProfileService) ListLiveProfiles() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := []string{}
	for id, live := range s.browsers {
		if live.liveView {
			ids = append(ids, id)
		}
	}
	return ids
}

func (s *ProfileService) OpenLiveProfile(ctx context.Context, id string) (BrowserProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return BrowserProfile{}, errors.New("Isolate is closing")
	}
	for _, profile := range s.profiles {
		if profile.ID == id && profile.Settings.Engine == "firefox" {
			return BrowserProfile{}, errors.New("Firefox uses an external browser; live view supports Chromium-based browsers")
		}
	}
	if err := s.openProfileInMode(ctx, id, true); err != nil {
		return BrowserProfile{}, err
	}
	return s.recordProfileOpened(id)
}

func (s *ProfileService) liveBrowser(id string) (*profileBrowser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("Isolate is closing")
	}
	live := s.browsers[id]
	if live == nil || !live.liveView || live.browser == nil {
		return nil, errors.New("This live session is not running; close and reopen the profile")
	}
	return live, nil
}

func (live *profileBrowser) livePage(ctx context.Context) (*rod.Page, error) {
	targets, err := (proto.TargetGetTargets{}).Call(live.browser.Context(ctx))
	if err != nil {
		return nil, err
	}
	var selected proto.TargetTargetID
	for _, target := range targets.TargetInfos {
		if target.Type != proto.TargetTargetInfoTypePage {
			continue
		}
		selected = target.TargetID
		if selected == live.liveTarget {
			break
		}
	}
	if selected == "" {
		return nil, errors.New("The live browser has no open page")
	}
	page, err := live.browser.PageFromTarget(selected)
	if err != nil {
		return nil, err
	}
	page = page.Context(ctx)
	if live.liveTarget != selected {
		if err := (proto.EmulationSetDeviceMetricsOverride{Width: liveWidth, Height: liveHeight, DeviceScaleFactor: 1, Mobile: false}).Call(page); err != nil {
			return nil, err
		}
		live.liveTarget = selected
	}
	return page, nil
}

func (s *ProfileService) GetLiveFrame(ctx context.Context, id string, preview bool) (LiveFrame, error) {
	live, err := s.liveBrowser(id)
	if err != nil {
		return LiveFrame{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	live.liveMu.Lock()
	defer live.liveMu.Unlock()
	page, err := live.livePage(ctx)
	if err != nil {
		return LiveFrame{}, err
	}
	quality, scale := 70, 1.0
	if preview {
		quality, scale = 40, 0.3
	}
	metrics, err := (proto.PageGetLayoutMetrics{}).Call(page)
	if err != nil {
		return LiveFrame{}, err
	}
	viewport := metrics.CSSLayoutViewport
	if viewport == nil {
		return LiveFrame{}, errors.New("The live browser viewport is unavailable")
	}
	capture, err := (proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatJpeg, Quality: &quality, FromSurface: true, Clip: &proto.PageViewport{X: float64(viewport.PageX), Y: float64(viewport.PageY), Width: liveWidth, Height: liveHeight, Scale: scale}}).Call(page)
	if err != nil {
		return LiveFrame{}, err
	}
	info, err := page.Info()
	if err != nil {
		return LiveFrame{}, err
	}
	return LiveFrame{Image: "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(capture.Data), URL: info.URL, Title: info.Title, Width: liveWidth, Height: liveHeight}, nil
}

func validateLiveInput(input LiveInput) error {
	if input.Modifiers < 0 || input.Modifiers > 15 || len(input.Text) > 8192 || len(input.Key) > 64 || len(input.Code) > 64 || input.KeyCode < 0 || input.KeyCode > 255 {
		return errors.New("Invalid live input")
	}
	switch input.Kind {
	case "click", "wheel":
		if math.IsNaN(input.X) || math.IsNaN(input.Y) || math.IsInf(input.X, 0) || math.IsInf(input.Y, 0) || input.X < 0 || input.X >= liveWidth || input.Y < 0 || input.Y >= liveHeight || math.IsNaN(input.DeltaX) || math.IsNaN(input.DeltaY) || math.IsInf(input.DeltaX, 0) || math.IsInf(input.DeltaY, 0) || math.Abs(input.DeltaX) > 3000 || math.Abs(input.DeltaY) > 3000 {
			return errors.New("Invalid live pointer coordinates")
		}
	case "text", "key", "reload":
	default:
		return errors.New("Unsupported live input")
	}
	return nil
}

func (s *ProfileService) SendLiveInput(ctx context.Context, id string, input LiveInput) error {
	if err := validateLiveInput(input); err != nil {
		return err
	}
	live, err := s.liveBrowser(id)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	live.liveMu.Lock()
	defer live.liveMu.Unlock()
	page, err := live.livePage(ctx)
	if err != nil {
		return err
	}
	switch input.Kind {
	case "reload":
		return (proto.PageReload{}).Call(page)
	case "text":
		return (proto.InputInsertText{Text: input.Text}).Call(page)
	case "key":
		if err := (proto.InputDispatchKeyEvent{Type: proto.InputDispatchKeyEventTypeKeyDown, Key: input.Key, Code: input.Code, WindowsVirtualKeyCode: input.KeyCode, Modifiers: input.Modifiers}).Call(page); err != nil {
			return err
		}
		return (proto.InputDispatchKeyEvent{Type: proto.InputDispatchKeyEventTypeKeyUp, Key: input.Key, Code: input.Code, WindowsVirtualKeyCode: input.KeyCode, Modifiers: input.Modifiers}).Call(page)
	case "wheel":
		return (proto.InputDispatchMouseEvent{Type: proto.InputDispatchMouseEventTypeMouseWheel, X: input.X, Y: input.Y, DeltaX: input.DeltaX, DeltaY: input.DeltaY, Modifiers: input.Modifiers}).Call(page)
	case "click":
		if err := (proto.InputDispatchMouseEvent{Type: proto.InputDispatchMouseEventTypeMousePressed, X: input.X, Y: input.Y, Button: proto.InputMouseButtonLeft, ClickCount: 1, Modifiers: input.Modifiers}).Call(page); err != nil {
			return err
		}
		return (proto.InputDispatchMouseEvent{Type: proto.InputDispatchMouseEventTypeMouseReleased, X: input.X, Y: input.Y, Button: proto.InputMouseButtonLeft, ClickCount: 1, Modifiers: input.Modifiers}).Call(page)
	}
	return nil
}
