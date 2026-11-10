package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func fakeAndroidService() *AndroidService {
	display, density := "720x1600", "210"
	return &AndroidService{adb: "adb", emulator: "emulator", serverPath: "scrcpy-server", owned: map[string]androidProcess{}, streams: map[string]*androidStream{}, run: func(ctx context.Context, name string, arguments ...string) ([]byte, error) {
		if name == "emulator" {
			return []byte("Isolate_01\nIsolate_02\n"), nil
		}
		command := strings.Join(arguments, " ")
		switch command {
		case "devices":
			return []byte("List of devices attached\nemulator-5554\tdevice\nphysical-phone\tdevice\nemulator-5556\tunauthorized\n"), nil
		case "-s emulator-5554 emu avd name":
			return []byte("Isolate_01\nOK\n"), nil
		case "-s emulator-5554 shell wm size":
			return []byte("Physical size: 1080x2400\nOverride size: " + display + "\n"), nil
		case "-s emulator-5554 shell wm size 1080x2400":
			display = "1080x2400"
			return nil, nil
		case "-s emulator-5554 shell wm density":
			return []byte("Physical density: 420\nOverride density: " + density + "\n"), nil
		case "-s emulator-5554 shell wm density 420":
			density = "420"
			return nil, nil
		case "-s emulator-5554 shell pm path com.instagram.android":
			return []byte("package:/data/app/instagram/base.apk\n"), nil
		}
		return nil, nil
	}}
}

func TestAndroidWorkspaceDiscovery(t *testing.T) {
	service := fakeAndroidService()
	state, err := service.GetAndroidWorkspace(context.Background())
	if err != nil || state.Limit != 10 || len(state.Emulators) != 2 || !state.ADB || !state.Emulator || !state.Video {
		t.Fatalf("invalid workspace: %+v, %v", state, err)
	}
	if state.Emulators[0].Serial != "emulator-5554" || state.Emulators[0].State != "running" || state.Emulators[0].Width != 720 || state.Emulators[0].Height != 1600 || state.Emulators[1].State != "stopped" {
		t.Fatalf("device targeting or display size is incorrect: %+v", state.Emulators)
	}
	if err := service.StartAndroidEmulator(context.Background(), "../unsafe"); err == nil {
		t.Fatal("unconfigured emulator name was accepted")
	}
	if err := service.StartAndroidEmulator(context.Background(), "Isolate_01"); err == nil {
		t.Fatal("running emulator was started twice")
	}
}

func TestAndroidEmulatorLimit(t *testing.T) {
	service := fakeAndroidService()
	service.run = func(ctx context.Context, name string, arguments ...string) ([]byte, error) {
		if name == "emulator" {
			var names strings.Builder
			for index := 0; index < 11; index++ {
				fmt.Fprintf(&names, "Isolate_%02d\n", index)
			}
			return []byte(names.String()), nil
		}
		if len(arguments) == 1 && arguments[0] == "devices" {
			var devices strings.Builder
			for index := 0; index < 10; index++ {
				fmt.Fprintf(&devices, "emulator-%d device\n", 5554+2*index)
			}
			return []byte(devices.String()), nil
		}
		if len(arguments) >= 5 && arguments[2] == "emu" {
			var port int
			_, _ = fmt.Sscanf(arguments[1], "emulator-%d", &port)
			return []byte(fmt.Sprintf("Isolate_%02d\nOK\n", (port-5554)/2)), nil
		}
		return []byte("Physical size: 720x1600"), nil
	}
	if err := service.StartAndroidEmulator(context.Background(), "Isolate_10"); err == nil || !strings.Contains(err.Error(), "10-emulator limit") {
		t.Fatal("ten-emulator ceiling was not enforced", err)
	}
}

func TestAndroidInputSafety(t *testing.T) {
	arguments, err := androidInputArguments(AndroidInput{Kind: "tap", X: 1, Y: 0.5}, 720, 1600)
	if err != nil || !reflect.DeepEqual(arguments, []string{"shell", "input", "tap", "719", "799"}) {
		t.Fatal(arguments, err)
	}
	arguments, err = androidInputArguments(AndroidInput{Kind: "text", Text: "a'; reboot & b"}, 720, 1600)
	if err != nil || len(arguments) != 2 || arguments[1] != "input text 'a'\\'';%sreboot%s&%sb'" {
		t.Fatal("text was not shell-quoted safely", arguments, err)
	}
	for _, input := range []AndroidInput{{Kind: "tap", X: -1}, {Kind: "tap", X: math.NaN()}, {Kind: "swipe", Duration: 1}, {Kind: "key", Key: 26}, {Kind: "text", Text: "你好"}, {Kind: "text", Text: "%s"}, {Kind: "shell"}} {
		if _, err := androidInputArguments(input, 720, 1600); err == nil {
			t.Fatal("unsafe input accepted", input.Kind)
		}
	}
	service := fakeAndroidService()
	if err := service.SendAndroidInput(context.Background(), "physical-phone", AndroidInput{Kind: "key", Key: 3}); err == nil {
		t.Fatal("physical device was targeted")
	}
	if err := service.SendAndroidInput(context.Background(), "emulator-5556", AndroidInput{Kind: "key", Key: 3}); err == nil {
		t.Fatal("unauthorized emulator was targeted")
	}
}

func TestAndroidLaunchAndInputAreSerialScoped(t *testing.T) {
	service := fakeAndroidService()
	original := service.run
	calls := [][]string{}
	service.run = func(ctx context.Context, name string, arguments ...string) ([]byte, error) {
		calls = append(calls, arguments)
		return original(ctx, name, arguments...)
	}
	if err := service.OpenAndroidInstagram(context.Background(), "emulator-5554"); err != nil {
		t.Fatal(err)
	}
	if err := service.OpenAndroidPlayStore(context.Background(), "emulator-5554"); err != nil {
		t.Fatal(err)
	}
	if err := service.SendAndroidInput(context.Background(), "emulator-5554", AndroidInput{AVDName: "Isolate_01", Kind: "text", Text: "hello world"}); err != nil {
		t.Fatal(err)
	}
	if err := service.SendAndroidInput(context.Background(), "emulator-5554", AndroidInput{AVDName: "Different_AVD", Kind: "text", Text: "private"}); err == nil {
		t.Fatal("input was sent to a reused emulator serial")
	}
	for _, arguments := range calls {
		if len(arguments) == 1 && arguments[0] == "devices" {
			continue
		}
		if len(arguments) < 2 || arguments[0] != "-s" || arguments[1] != "emulator-5554" {
			t.Fatal("operation was not scoped to the chosen emulator", arguments)
		}
	}
}

func TestAndroidPrivateStream(t *testing.T) {
	service := fakeAndroidService()
	service.token = "test-secret"
	for _, path := range []string{"/wrong-token/emulator-5554", "/test-secret/physical-phone", "/test-secret/emulator-5554"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		service.serveScreen(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatal("An unavailable or unauthorized stream was exposed", response.Code)
		}
	}
}

func TestAndroidMissingDependenciesAndShutdown(t *testing.T) {
	service := fakeAndroidService()
	service.serverPath = ""
	if _, err := service.StartAndroidScreen(context.Background(), "emulator-5554"); err == nil || !strings.Contains(err.Error(), "scrcpy") {
		t.Fatal("missing server was not reported", err)
	}
	if err := service.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetAndroidWorkspace(context.Background()); err == nil {
		t.Fatal("closed workspace accepted operations")
	}
}

func TestAndroidScreenConfiguresNativeNavigationOncePerSession(t *testing.T) {
	service := fakeAndroidService()
	defer service.ServiceShutdown()
	original := service.run
	changes := 0
	service.run = func(ctx context.Context, name string, arguments ...string) ([]byte, error) {
		if strings.Contains(strings.Join(arguments, " "), "shell cmd overlay enable-exclusive") {
			want := []string{"-s", "emulator-5554", "shell", "cmd", "overlay", "enable-exclusive", "--user", "0", "--category", "com.android.internal.systemui.navbar.gestural"}
			if !reflect.DeepEqual(arguments, want) {
				t.Fatal("navigation settings targeted the wrong phone or overlay", arguments)
			}
			changes++
		}
		return original(ctx, name, arguments...)
	}
	first, err := service.StartAndroidScreen(context.Background(), "emulator-5554")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.StartAndroidScreen(context.Background(), "emulator-5554")
	if err != nil || second != first || changes != 1 {
		t.Fatal("reusing a stream reset its navigation", changes, err)
	}
	_ = service.StopAndroidScreen("emulator-5554")
	third, err := service.StartAndroidScreen(context.Background(), "emulator-5554")
	if err != nil || third == first || changes != 2 {
		t.Fatal("reconnecting did not prepare native navigation", changes, err)
	}
}

func TestAndroidNavigationFailureDoesNotPublishScreen(t *testing.T) {
	service := fakeAndroidService()
	original := service.run
	service.run = func(ctx context.Context, name string, arguments ...string) ([]byte, error) {
		if strings.Contains(strings.Join(arguments, " "), "shell cmd overlay enable-exclusive") {
			return nil, errors.New("Android is still booting")
		}
		return original(ctx, name, arguments...)
	}
	if _, err := service.StartAndroidScreen(context.Background(), "emulator-5554"); err == nil || !strings.Contains(err.Error(), "gesture navigation") {
		t.Fatal("navigation setup failure was not explained", err)
	}
	if len(service.streams) != 0 || service.server != nil {
		t.Fatal("a screen was published despite navigation setup failing")
	}
}

func TestAndroidDisplayRestorationAndInputCoordinates(t *testing.T) {
	service := fakeAndroidService()
	defer service.ServiceShutdown()
	if _, err := service.StartAndroidScreen(context.Background(), "emulator-5554"); err != nil {
		t.Fatal(err)
	}
	stream := service.streams["emulator-5554"]
	if stream.width != 1080 || stream.height != 2400 {
		t.Fatal("stream did not use the restored display", stream.width, stream.height)
	}
	args, err := androidInputArguments(AndroidInput{Kind: "tap", X: 1, Y: 1}, stream.width, stream.height)
	if err != nil || !reflect.DeepEqual(args, []string{"shell", "input", "tap", "1079", "2399"}) {
		t.Fatal("input coordinates did not follow display", args, err)
	}
}

func TestAndroidDisplayFailureDoesNotPublishStream(t *testing.T) {
	for _, failure := range []string{"shell wm size 1080x2400", "shell wm density 420", "ignored"} {
		t.Run(failure, func(t *testing.T) {
			service := fakeAndroidService()
			defer service.ServiceShutdown()
			original := service.run
			service.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
				command := strings.Join(args, " ")
				if failure == "ignored" && strings.HasSuffix(command, "shell wm size 1080x2400") {
					return nil, nil
				}
				if failure != "ignored" && strings.HasSuffix(command, failure) {
					return nil, errors.New("Android not ready")
				}
				return original(ctx, name, args...)
			}
			if _, err := service.StartAndroidScreen(context.Background(), "emulator-5554"); err == nil {
				t.Fatal("failed display configuration accepted")
			}
			if len(service.streams) != 0 || service.server != nil {
				t.Fatal("stream published with unverified display")
			}
		})
	}
}
