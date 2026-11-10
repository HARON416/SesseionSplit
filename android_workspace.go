package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const androidEmulatorLimit = 10

var avdNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,79}$`)
var emulatorSerialPattern = regexp.MustCompile(`^emulator-[0-9]{4,5}$`)
var androidSizePattern = regexp.MustCompile(`([0-9]+)x([0-9]+)`)

type AndroidEmulator struct {
	Name        string `json:"name"`
	Serial      string `json:"serial"`
	State       string `json:"state"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	StreamURL   string `json:"streamURL"`
	StreamError string `json:"streamError"`
	Audio       string `json:"audio"`
}

type AndroidWorkspaceState struct {
	Emulators []AndroidEmulator `json:"emulators"`
	Limit     int               `json:"limit"`
	ADB       bool              `json:"adb"`
	Emulator  bool              `json:"emulator"`
	Video     bool              `json:"video"`
}

type AndroidInput struct {
	AVDName  string  `json:"avdName"`
	Kind     string  `json:"kind"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	EndX     float64 `json:"endX"`
	EndY     float64 `json:"endY"`
	Duration int     `json:"duration"`
	Key      int     `json:"key"`
	Text     string  `json:"text"`
}

type androidProcess struct {
	command  *exec.Cmd
	serial   string
	stopping bool
}

type androidStream struct {
	key           string
	avdName       string
	width         int
	height        int
	mu            sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	sessionCancel context.CancelFunc
	err           string
	audio         string
}

type AndroidService struct {
	mu         sync.Mutex
	adb        string
	emulator   string
	serverPath string
	setup      *androidSetup
	run        func(context.Context, string, ...string) ([]byte, error)
	owned      map[string]androidProcess
	failures   map[string]string
	streams    map[string]*androidStream
	server     *http.Server
	address    string
	token      string
	closed     bool
}

func newAndroidService() *AndroidService {
	home, _ := os.UserHomeDir()
	roots := []string{os.Getenv("ANDROID_HOME"), os.Getenv("ANDROID_SDK_ROOT"), filepath.Join(home, "Android", "Sdk"), filepath.Join(os.Getenv("LOCALAPPDATA"), "Android", "Sdk"), filepath.Join(home, "Library", "Android", "sdk")}
	find := func(name, directory string) string {
		executable := name
		if runtime.GOOS == "windows" {
			executable += ".exe"
		}
		for _, root := range roots {
			if root == "" {
				continue
			}
			candidate := filepath.Join(root, directory, executable)
			if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
				return candidate
			}
		}
		path, _ := exec.LookPath(name)
		return path
	}
	service := &AndroidService{adb: find("adb", "platform-tools"), emulator: find("emulator", "emulator"), run: androidCommandOutput, owned: map[string]androidProcess{}, streams: map[string]*androidStream{}}
	service.setup = newAndroidSetup()
	service.refreshManagedTools()
	return service
}

func androidCommand(name string, arguments ...string) *exec.Cmd {
	command := exec.Command(name, arguments...)
	hideAndroidCommand(command)
	return command
}

func androidCommandOutput(ctx context.Context, name string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, arguments...)
	hideAndroidCommand(command)
	return command.Output()
}

func parseAndroidDevices(data []byte) []string {
	serials := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "device" && emulatorSerialPattern.MatchString(fields[0]) {
			serials = append(serials, fields[0])
		}
	}
	return serials
}

func (s *AndroidService) state(ctx context.Context) (AndroidWorkspaceState, error) {
	state := AndroidWorkspaceState{Emulators: []AndroidEmulator{}, Limit: androidEmulatorLimit, ADB: s.adb != "", Emulator: s.emulator != "", Video: s.serverPath != ""}
	if s.closed {
		return state, errors.New("Isolate is closing")
	}
	if !state.ADB || !state.Emulator {
		return state, nil
	}
	avds, err := s.run(ctx, s.emulator, "-list-avds")
	if err != nil {
		return state, errors.New("Could not list Android virtual devices; check the Android SDK installation")
	}
	devices, err := s.run(ctx, s.adb, "devices")
	if err != nil {
		return state, errors.New("Could not contact ADB; check the Android SDK Platform Tools installation")
	}
	running := map[string]string{}
	for _, serial := range parseAndroidDevices(devices) {
		name, err := s.run(ctx, s.adb, "-s", serial, "emu", "avd", "name")
		if err == nil {
			fields := strings.Fields(string(name))
			if len(fields) > 0 && avdNamePattern.MatchString(fields[0]) {
				running[fields[0]] = serial
			}
		}
	}
	for _, name := range strings.Fields(string(avds)) {
		if !avdNamePattern.MatchString(name) {
			continue
		}
		device := AndroidEmulator{Name: name, State: "stopped", Width: 360, Height: 800, StreamError: s.failures[name]}
		if owned, ok := s.owned[name]; ok {
			device.State, device.Serial = "starting", owned.serial
			if owned.stopping {
				device.State = "stopping"
			}
		}
		if serial := running[name]; serial != "" {
			device.State, device.Serial = "running", serial
			if data, err := s.run(ctx, s.adb, "-s", serial, "shell", "wm", "size"); err == nil {
				if width, height, err := androidDisplaySize(data); err == nil {
					device.Width, device.Height = width, height
				}
			}
		}
		if stream := s.streams[device.Serial]; stream != nil {
			stream.mu.Lock()
			device.StreamError = stream.err
			device.Audio = stream.audio
			stream.mu.Unlock()
			device.StreamURL = "ws://" + s.address + "/" + s.token + "/" + device.Serial + "?session=" + stream.key
		}
		state.Emulators = append(state.Emulators, device)
	}
	return state, nil
}

func (s *AndroidService) GetAndroidWorkspace(ctx context.Context) (AndroidWorkspaceState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.state(ctx)
}

func (s *AndroidService) StartAndroidEmulator(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	state, err := s.state(ctx)
	if err != nil {
		return err
	}
	if !state.ADB || !state.Emulator {
		return errors.New("Run Android setup to install the Android runtime first")
	}
	found, count := false, 0
	used := map[string]bool{}
	for _, device := range state.Emulators {
		if device.State != "stopped" {
			count++
			used[device.Serial] = true
		}
		if device.Name == name {
			found = true
			if device.State != "stopped" {
				return errors.New("This emulator is already running or starting")
			}
		}
	}
	if !found || !avdNamePattern.MatchString(name) {
		return errors.New("Choose an existing Android virtual device")
	}
	if count >= androidEmulatorLimit {
		return errors.New("The 10-emulator limit has been reached")
	}
	devices, err := s.run(ctx, s.adb, "devices")
	if err != nil {
		return errors.New("Could not check available emulator ports")
	}
	for _, line := range strings.Split(string(devices), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && emulatorSerialPattern.MatchString(fields[0]) {
			used[fields[0]] = true
		}
	}
	if len(used) >= androidEmulatorLimit {
		return errors.New("The 10-emulator limit has been reached")
	}
	port := 5554
	for ; port <= 5584; port += 2 {
		if !used[fmt.Sprintf("emulator-%d", port)] {
			break
		}
	}
	if port > 5584 {
		return errors.New("No emulator port is available")
	}
	// The current Android 35 image/emulator raises smaller requests to 2560 MiB.
	// Keep the native framebuffer for sharp text and full-resolution video.
	// Cold boot avoids restoring stale GPU buffers after display/renderer changes.
	// Use host GLES rendering instead of the software renderer that crashes
	// in RenderThread when Instagram opens. Keep guest Vulkan disabled.
	command := androidCommand(s.emulator, "-avd", name, "-port", strconv.Itoa(port), "-no-window", "-no-boot-anim", "-no-snapshot", "-memory", "2560", "-cores", "2", "-skin", "1080x2400", "-gpu", "host", "-feature", "-Vulkan")
	if filepath.Base(filepath.Dir(s.emulator)) == "emulator" {
		sdk := filepath.Dir(filepath.Dir(s.emulator))
		command.Env = append(os.Environ(), "ANDROID_HOME="+sdk, "ANDROID_SDK_ROOT="+sdk)
	}
	configureAndroidEmulator(command)
	root := androidRuntimeRoot()
	if s.setup != nil && s.setup.status.Root != "" {
		root = s.setup.status.Root
	}
	output := newAndroidDiagnosticLog(root, name, "emulator")
	fmt.Fprintf(output, "Started %s\nCommand: %s\n", time.Now().UTC().Format(time.RFC3339), command.String())
	command.Stdout, command.Stderr = output, output
	command.WaitDelay = 2 * time.Second
	if err := command.Start(); err != nil {
		fmt.Fprintf(output, "Start failed: %v\n", err)
		output.close()
		return fmt.Errorf("Could not start the emulator: %v\n%s", err, output.details())
	}
	s.owned[name] = androidProcess{command: command, serial: fmt.Sprintf("emulator-%d", port)}
	crashes := newAndroidDiagnosticLog(root, name, "android-crash")
	stopCrashCapture := monitorAndroidCrashes(s.adb, fmt.Sprintf("emulator-%d", port), crashes)
	delete(s.failures, name)
	go func() {
		exitErr := command.Wait()
		exitStatus := "exit code 0"
		if exitErr != nil {
			exitStatus = exitErr.Error()
		}
		fmt.Fprintf(output, "\nExited %s: %s\n", time.Now().UTC().Format(time.RFC3339), exitStatus)
		output.close()
		stopCrashCapture()
		s.mu.Lock()
		if process, ok := s.owned[name]; ok && process.command == command {
			delete(s.owned, name)
			if !process.stopping && !s.closed {
				if s.failures == nil {
					s.failures = map[string]string{}
				}
				s.failures[name] = fmt.Sprintf("The emulator exited unexpectedly (%s).\n%s\n\nAndroid crash diagnostics:\n%s", exitStatus, output.details(), crashes.details())
			}
		}
		s.mu.Unlock()
	}()
	return nil
}

func (s *AndroidService) connected(ctx context.Context, serial string) error {
	if s.closed {
		return errors.New("Isolate is closing")
	}
	if s.adb == "" || !emulatorSerialPattern.MatchString(serial) {
		return errors.New("Choose a connected Android emulator")
	}
	data, err := s.run(ctx, s.adb, "devices")
	if err != nil {
		return errors.New("Could not contact ADB")
	}
	for _, connected := range parseAndroidDevices(data) {
		if connected == serial {
			return nil
		}
	}
	return errors.New("The emulator is offline or not ready; wait for it to finish booting")
}

func (s *AndroidService) StopAndroidEmulator(ctx context.Context, serial string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var owned *exec.Cmd
	for name, process := range s.owned {
		if process.serial == serial {
			owned = process.command
			process.stopping = true
			s.owned[name] = process
		}
	}
	if err := s.connected(ctx, serial); err != nil && owned == nil {
		return err
	}
	if stream := s.streams[serial]; stream != nil {
		stream.cancel()
		delete(s.streams, serial)
	}
	if _, err := s.run(ctx, s.adb, "-s", serial, "emu", "kill"); err != nil {
		if owned == nil || killAndroidEmulator(owned) != nil {
			return errors.New("Could not stop the Android emulator")
		}
	}
	return nil
}

func (s *AndroidService) OpenAndroidInstagram(ctx context.Context, serial string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.connected(ctx, serial); err != nil {
		return err
	}
	installed, err := s.run(ctx, s.adb, "-s", serial, "shell", "pm", "path", "com.instagram.android")
	if err != nil || !strings.Contains(string(installed), "package:") {
		return errors.New("Instagram is not installed in this emulator; install it from Google Play first")
	}
	if _, err := s.run(ctx, s.adb, "-s", serial, "shell", "monkey", "-p", "com.instagram.android", "-c", "android.intent.category.LAUNCHER", "1"); err != nil {
		return errors.New("Could not open Instagram in this emulator")
	}
	return nil
}

func (s *AndroidService) OpenAndroidPlayStore(ctx context.Context, serial string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.connected(ctx, serial); err != nil {
		return err
	}
	if _, err := s.run(ctx, s.adb, "-s", serial, "shell", "am", "start", "-a", "android.intent.action.VIEW", "-d", "market://details?id=com.instagram.android"); err != nil {
		return errors.New("Could not open Google Play; use a Google Play-enabled Android system image")
	}
	return nil
}

func androidInputArguments(input AndroidInput, width, height int) ([]string, error) {
	coordinate := func(value float64, size int) (string, error) {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return "", errors.New("Invalid Android pointer coordinates")
		}
		return strconv.Itoa(int(value * float64(size-1))), nil
	}
	switch input.Kind {
	case "tap", "swipe":
		startX, err := coordinate(input.X, width)
		if err != nil {
			return nil, err
		}
		startY, err := coordinate(input.Y, height)
		if err != nil {
			return nil, err
		}
		if input.Kind == "tap" {
			return []string{"shell", "input", "tap", startX, startY}, nil
		}
		endX, err := coordinate(input.EndX, width)
		if err != nil {
			return nil, err
		}
		endY, err := coordinate(input.EndY, height)
		if err != nil {
			return nil, err
		}
		if input.Duration < 50 || input.Duration > 1500 {
			return nil, errors.New("Invalid Android swipe duration")
		}
		return []string{"shell", "input", "swipe", startX, startY, endX, endY, strconv.Itoa(input.Duration)}, nil
	case "key":
		if input.Key != 3 && input.Key != 4 && input.Key != 61 && input.Key != 66 && input.Key != 67 {
			return nil, errors.New("Unsupported Android key")
		}
		return []string{"shell", "input", "keyevent", strconv.Itoa(input.Key)}, nil
	case "text":
		if len(input.Text) == 0 || len(input.Text) > 1024 {
			return nil, errors.New("Enter between 1 and 1024 characters")
		}
		for _, character := range input.Text {
			if character < 32 || character > 126 || character == '%' {
				return nil, errors.New("Use the Android on-screen keyboard for Unicode, percent signs, or multiline text")
			}
		}
		text := strings.ReplaceAll(input.Text, " ", "%s")
		return []string{"shell", "input text '" + strings.ReplaceAll(text, "'", "'\\''") + "'"}, nil
	}
	return nil, errors.New("Unsupported Android input")
}

func (s *AndroidService) SendAndroidInput(ctx context.Context, serial string, input AndroidInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.connected(ctx, serial); err != nil {
		return err
	}
	name, err := s.run(ctx, s.adb, "-s", serial, "emu", "avd", "name")
	fields := strings.Fields(string(name))
	if err != nil || len(fields) == 0 || !avdNamePattern.MatchString(input.AVDName) || fields[0] != input.AVDName {
		return errors.New("The emulator identity changed; reconnect its screen before sending input")
	}
	width, height := 0, 0
	if stream := s.streams[serial]; stream != nil && stream.avdName == input.AVDName {
		width, height = stream.width, stream.height
	}
	if width == 0 || height == 0 {
		size, err := s.run(ctx, s.adb, "-s", serial, "shell", "wm", "size")
		if err != nil {
			return errors.New("Could not read the emulator display size")
		}
		width, height, err = androidDisplaySize(size)
		if err != nil {
			return err
		}
	}
	arguments, err := androidInputArguments(input, width, height)
	if err != nil {
		return err
	}
	if _, err := s.run(ctx, s.adb, append([]string{"-s", serial}, arguments...)...); err != nil {
		return errors.New("Could not send input to the emulator")
	}
	return nil
}

func androidDisplaySize(data []byte) (int, int, error) {
	matches := androidSizePattern.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return 0, 0, errors.New("Android display size is unavailable")
	}
	match := matches[len(matches)-1]
	width, _ := strconv.Atoi(string(match[1]))
	height, _ := strconv.Atoi(string(match[2]))
	if width < 100 || height < 100 || width > 8192 || height > 8192 {
		return 0, 0, errors.New("Unsupported Android display size")
	}
	return width, height, nil
}

// Migrate existing phones too. wm overrides preserve apps and account data,
// and input coordinates use the verified Android display size below.
func (s *AndroidService) configureAndroidDisplay(ctx context.Context, serial string) (int, int, error) {
	run := func(args ...string) ([]byte, error) {
		return s.run(ctx, s.adb, append([]string{"-s", serial, "shell", "wm"}, args...)...)
	}
	size, err := run("size")
	if err != nil {
		return 0, 0, fmt.Errorf("Could not read Android display size: %w", err)
	}
	width, height, err := androidDisplaySize(size)
	if err != nil {
		return 0, 0, err
	}
	if width != 1080 || height != 2400 {
		if _, err = run("size", "1080x2400"); err != nil {
			return 0, 0, fmt.Errorf("Could not restore Android display size: %w", err)
		}
	}
	density, err := run("density")
	if err != nil {
		return 0, 0, fmt.Errorf("Could not read Android display density: %w", err)
	}
	values := strings.Fields(string(density))
	if len(values) == 0 {
		return 0, 0, errors.New("Android display density is unavailable")
	}
	if values[len(values)-1] != "420" {
		if _, err = run("density", "420"); err != nil {
			return 0, 0, fmt.Errorf("Could not adjust Android display density: %w", err)
		}
	}
	size, err = run("size")
	if err != nil {
		return 0, 0, fmt.Errorf("Could not verify Android display size: %w", err)
	}
	width, height, err = androidDisplaySize(size)
	if err != nil {
		return 0, 0, err
	}
	if width != 1080 || height != 2400 {
		return 0, 0, errors.New("Android did not apply the requested display size; retry after boot completes")
	}
	density, err = run("density")
	if err != nil {
		return 0, 0, fmt.Errorf("Could not verify Android display density: %w", err)
	}
	values = strings.Fields(string(density))
	if len(values) == 0 || values[len(values)-1] != "420" {
		return 0, 0, errors.New("Android did not apply the requested display density; retry after boot completes")
	}
	return width, height, nil
}

func (s *AndroidService) StartAndroidScreen(ctx context.Context, serial string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.connected(ctx, serial); err != nil {
		return "", err
	}
	if s.serverPath == "" {
		return "", errors.New("Run Android setup to install the scrcpy streaming server")
	}
	name, err := s.run(ctx, s.adb, "-s", serial, "emu", "avd", "name")
	fields := strings.Fields(string(name))
	if err != nil || len(fields) == 0 || !avdNamePattern.MatchString(fields[0]) {
		return "", errors.New("Could not identify the Android virtual device")
	}
	if stream := s.streams[serial]; stream != nil && (stream.avdName != fields[0] || stream.ctx.Err() != nil) {
		stream.cancel()
		delete(s.streams, serial)
	}
	if s.streams[serial] == nil {
		if len(s.streams) >= androidEmulatorLimit {
			return "", errors.New("The 10-screen limit has been reached")
		}
		// Enable native edge gestures instead of the three-button navigation bar.
		// Apply on reconnect as well to migrate phones configured by older versions.
		if _, err := s.run(ctx, s.adb, "-s", serial, "shell", "cmd", "overlay", "enable-exclusive", "--user", "0", "--category", "com.android.internal.systemui.navbar.gestural"); err != nil {
			return "", errors.New("Could not enable Android gesture navigation; wait for Android to finish booting, then retry the screen")
		}
		width, height, err := s.configureAndroidDisplay(ctx, serial)
		if err != nil {
			return "", err
		}
		if err := s.startScreenServer(); err != nil {
			return "", err
		}
		streamCtx, streamCancel := context.WithCancel(context.Background())
		var key [16]byte
		if _, err := rand.Read(key[:]); err != nil {
			return "", err
		}
		stream := &androidStream{key: base64.RawURLEncoding.EncodeToString(key[:]), ctx: streamCtx, cancel: streamCancel, avdName: fields[0], width: width, height: height}
		s.streams[serial] = stream
	}
	return "ws://" + s.address + "/" + s.token + "/" + serial + "?session=" + s.streams[serial].key, nil
}

func (s *AndroidService) StopAndroidScreen(serial string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stream := s.streams[serial]; stream != nil {
		stream.cancel()
		delete(s.streams, serial)
	}
	return nil
}

func (s *AndroidService) startScreenServer() error {
	if s.server != nil {
		return nil
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return errors.New("Could not secure the Android screen server")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return errors.New("Could not start the local Android screen server")
	}
	s.token, s.address = base64.RawURLEncoding.EncodeToString(token[:]), listener.Addr().String()
	s.server = &http.Server{Handler: http.HandlerFunc(s.serveScreen), ReadHeaderTimeout: 3 * time.Second, MaxHeaderBytes: 4096}
	go func() { _ = s.server.Serve(listener) }()
	return nil
}

func (s *AndroidService) ServiceShutdown() error {
	s.mu.Lock()
	s.closed = true
	if s.setup != nil {
		s.setup.cancelWork()
	}
	for _, stream := range s.streams {
		stream.cancel()
	}
	for _, process := range s.owned {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if _, err := s.run(ctx, s.adb, "-s", process.serial, "emu", "kill"); err != nil && process.command.Process != nil {
			_ = killAndroidEmulator(process.command)
		}
		cancel()
	}
	server := s.server
	s.mu.Unlock()
	if server != nil {
		return server.Close()
	}
	return nil
}
