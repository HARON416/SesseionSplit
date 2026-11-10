package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"isolate/internal/runtimebundle"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const androidImage = "system-images;android-35;google_apis_playstore;x86_64"

type AndroidSetupStatus struct {
	BundledTools    bool    `json:"bundledTools"`
	Phase           string  `json:"phase"`
	Step            int     `json:"step"`
	Percent         float64 `json:"percent"`
	ProgressLabel   string  `json:"progressLabel"`
	Detail          string  `json:"detail"`
	DownloadedBytes int64   `json:"downloadedBytes"`
	TotalBytes      int64   `json:"totalBytes"`
	BytesPerSecond  float64 `json:"bytesPerSecond"`
	LastActivity    int64   `json:"lastActivity"`
	Running         bool    `json:"running"`
	Stage           string  `json:"stage"`
	Log             string  `json:"log"`
	Error           string  `json:"error"`
	WaitingLicense  bool    `json:"waitingLicense"`
	Supported       bool    `json:"supported"`
	Root            string  `json:"root"`
}
type androidSetup struct {
	mu             sync.Mutex
	status         AndroidSetupStatus
	cancel         context.CancelFunc
	input          io.WriteCloser
	promptTail     string
	declined       bool
	outputTail     string
	parseInstaller bool
	downloadClient *http.Client
	bundleDir      string
	bundleTarget   string
}

func androidRuntimeRoot() string {
	root, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(root, "isolate", "android")
}
func newAndroidSetup() *androidSetup {
	setup := &androidSetup{bundleDir: installedRuntimeBundle(), bundleTarget: runtime.GOOS + "/" + runtime.GOARCH, status: AndroidSetupStatus{Percent: -1, Root: androidRuntimeRoot(), Supported: runtime.GOARCH == "amd64" && (runtime.GOOS == "windows" || runtime.GOOS == "linux")}}
	if setup.bundleDir != "" {
		manifest, err := runtimebundle.Load(setup.bundleDir)
		setup.status.BundledTools = err == nil && manifest.Target == setup.bundleTarget
	}
	return setup
}
func (s *AndroidService) refreshManagedTools() {
	root := androidRuntimeRoot()
	if s.setup != nil {
		root = s.setup.status.Root
	}
	if root == "" {
		return
	}
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	for name, target := range map[string]*string{"platform-tools/adb": &s.adb, "emulator/emulator": &s.emulator} {
		path := filepath.Join(root, "sdk", filepath.FromSlash(name)+suffix)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			*target = path
		}
	}
	path := filepath.Join(root, "scrcpy-server-v"+scrcpyVersion)
	if validDownload(path, scrcpySHA256) == nil {
		s.serverPath = path
	}
}
func (s *AndroidService) GetAndroidSetup() AndroidSetupStatus {
	s.setup.mu.Lock()
	defer s.setup.mu.Unlock()
	return s.setup.status
}
func (s *AndroidService) StartAndroidSetup() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("Isolate is closing")
	}
	setup := s.setup
	setup.mu.Lock()
	defer setup.mu.Unlock()
	if setup.status.Running {
		return errors.New("Android setup is already running")
	}
	if !setup.status.Supported {
		return errors.New("Automatic setup currently supports Windows and Linux on Intel/AMD 64-bit PCs")
	}
	if setup.status.Root == "" {
		return errors.New("Could not locate your application data folder")
	}
	ctx, cancel := context.WithCancel(context.Background())
	setup.cancel = cancel
	setup.status.Running = true
	setup.resetProgressLocked("preparing")
	setup.status.Step = 1
	setup.status.Stage = "Preparing Android setup"
	setup.status.Error = ""
	setup.status.Log = ""
	setup.promptTail = ""
	setup.declined = false
	setup.status.WaitingLicense = false
	go func() {
		err := setup.install(ctx)
		setup.mu.Lock()
		if setup.declined {
			err = errors.New("Android license declined. Setup stopped; retry if you decide to accept the required terms")
		}
		setup.status.Running = false
		setup.status.WaitingLicense = false
		setup.input = nil
		if err != nil {
			setup.status.Error = err.Error()
			setup.status.Stage = "Setup paused — retry when ready"
			setup.status.Phase = "paused"
		} else {
			setup.status.Stage = "Ready — start your virtual phone, open Play Store, and install Instagram"
			setup.resetProgressLocked("complete")
			setup.status.Percent = 100
		}
		setup.mu.Unlock()
		s.mu.Lock()
		s.refreshManagedTools()
		s.mu.Unlock()
	}()
	return nil
}
func (s *AndroidService) CancelAndroidSetup() { s.setup.cancelWork() }
func (setup *androidSetup) cancelWork() {
	setup.mu.Lock()
	defer setup.mu.Unlock()
	if setup.cancel != nil {
		setup.cancel()
	}
}
func (s *AndroidService) RespondAndroidLicense(accept bool) error {
	setup := s.setup
	setup.mu.Lock()
	defer setup.mu.Unlock()
	if !setup.status.Running || !setup.status.WaitingLicense || setup.input == nil {
		return errors.New("There is no pending Android license prompt")
	}
	answer := "n\n"
	if accept {
		answer = "y\n"
	}
	if _, err := io.WriteString(setup.input, answer); err != nil {
		return err
	}
	setup.status.WaitingLicense = false
	setup.promptTail = ""
	if !accept {
		setup.declined = true
		if setup.cancel != nil {
			setup.cancel()
		}
	}
	return nil
}
func (setup *androidSetup) stage(phase string, step int, message string) {
	setup.mu.Lock()
	setup.status.Stage = message
	setup.resetProgressLocked(phase)
	setup.status.Step = step
	setup.mu.Unlock()
}
func (setup *androidSetup) Write(data []byte) (int, error) {
	setup.mu.Lock()
	defer setup.mu.Unlock()
	setup.status.LastActivity = time.Now().UnixMilli()
	if setup.parseInstaller {
		setup.installerProgressLocked(string(data))
	}
	setup.status.Log += string(data)
	if len(setup.status.Log) > 1024*1024 {
		setup.status.Log = setup.status.Log[len(setup.status.Log)-1024*1024:]
	}
	setup.promptTail += string(data)
	tail := strings.ToLower(setup.promptTail)
	if len(tail) > 200 {
		tail = tail[len(tail)-200:]
	}
	setup.promptTail = tail
	if strings.Contains(tail, "(y/n)") || strings.Contains(tail, "[y/n]") {
		setup.status.WaitingLicense = true
	}
	return len(data), nil
}
func sdkTool(sdk, name string) string {
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".bat"
	}
	return filepath.Join(sdk, "cmdline-tools", "latest", "bin", name+suffix)
}
func (setup *androidSetup) command(ctx context.Context, javaHome, sdk, name string, input string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	// Invoke the same Java entry points as Google's launcher scripts. Running a
	// .bat through cmd.exe would make SDK paths with spaces fragile and complicate
	// cancelling the child JVM on Windows.
	tool := strings.TrimSuffix(filepath.Base(name), ".bat")
	setup.mu.Lock()
	setup.parseInstaller = tool == "sdkmanager"
	setup.outputTail = ""
	setup.mu.Unlock()
	defer func() { setup.mu.Lock(); setup.parseInstaller = false; setup.outputTail = ""; setup.mu.Unlock() }()
	if tool == "sdkmanager" || tool == "avdmanager" {
		java := filepath.Join(javaHome, "bin", "java")
		if runtime.GOOS == "windows" {
			java += ".exe"
		}
		mainClass := "com.android.sdklib.tool.sdkmanager.SdkManagerCli"
		toolsProperty := "com.android.sdklib.toolsdir"
		if tool == "avdmanager" {
			mainClass = "com.android.sdklib.tool.AvdManagerCli"
			toolsProperty = "com.android.sdkmanager.toolsdir"
		}
		toolsDir := filepath.Join(sdk, "cmdline-tools", "latest")
		// The two launchers use different property names in Google's scripts.
		javaArgs := []string{"-D" + toolsProperty + "=" + toolsDir, "-classpath", filepath.Join(toolsDir, "lib", tool+"-classpath.jar"), mainClass}
		command = exec.CommandContext(ctx, java, append(javaArgs, args...)...)
	}
	hideAndroidCommand(command)
	configureAndroidEmulator(command)
	command.Cancel = func() error { return killAndroidEmulator(command) }
	command.WaitDelay = 3 * time.Second
	command.Env = append(os.Environ(), "JAVA_HOME="+javaHome, "ANDROID_HOME="+sdk, "ANDROID_SDK_ROOT="+sdk)
	command.Stdout = setup
	command.Stderr = setup
	if input != "" {
		command.Stdin = strings.NewReader(input)
	} else {
		pipe, err := command.StdinPipe()
		if err != nil {
			return err
		}
		setup.mu.Lock()
		setup.input = pipe
		setup.mu.Unlock()
		defer func() {
			_ = pipe.Close()
			setup.mu.Lock()
			setup.input = nil
			setup.status.WaitingLicense = false
			setup.mu.Unlock()
		}()
	}
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return errors.New("Setup cancelled; completed downloads are kept for retry")
		}
		return fmt.Errorf("%s failed. See setup details for the cause: %w", filepath.Base(name), err)
	}
	return nil
}
func (setup *androidSetup) install(ctx context.Context) error {
	root := setup.status.Root
	sdk := filepath.Join(root, "sdk")
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	// The application downloads runtimes only into its own directory. Existing
	// Android Studio installations, AVDs, licenses, and Java installs are untouched.
	setup.stage("preparing", 1, "Preparing Java runtime")
	javaHome := filepath.Join(root, "java")
	javaName := "java"
	if runtime.GOOS == "windows" {
		javaName += ".exe"
	}
	if _, err := os.Stat(filepath.Join(javaHome, "bin", javaName)); err != nil {
		archive := filepath.Join(root, "java.download")
		bundleArchive, _ := runtimebundle.JavaArchive(runtime.GOOS + "/" + runtime.GOARCH)
		bundled, err := setup.restoreBundled(ctx, bundleArchive, archive)
		if err != nil {
			return err
		}
		if !bundled {
			var assets []struct {
				Binary struct {
					Package struct {
						Link     string `json:"link"`
						Checksum string `json:"checksum"`
					} `json:"package"`
				} `json:"binary"`
			}
			url := "https://api.adoptium.net/v3/assets/latest/21/hotspot?architecture=x64&image_type=jre&os=" + runtime.GOOS + "&vendor=eclipse"
			if err := downloadJSON(ctx, url, &assets); err != nil {
				return fmt.Errorf("Java runtime metadata: %w", err)
			}
			if len(assets) == 0 {
				return errors.New("No supported Java runtime was returned")
			}
			pkg := assets[0].Binary.Package
			if !strings.HasPrefix(pkg.Link, "https://github.com/adoptium/") {
				return errors.New("Unexpected Java download source")
			}
			if err := setup.download(ctx, pkg.Link, pkg.Checksum, archive); err != nil {
				return err
			}
		}

		staging, err := os.MkdirTemp(root, "java-unpack-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(staging)
		setup.activity("extracting", "Unpacking Java runtime")
		if err = extractRuntime(archive, staging, runtime.GOOS == "windows"); err != nil {
			return err
		}
		entries, err := os.ReadDir(staging)
		if err != nil || len(entries) != 1 {
			return errors.New("Unexpected Java runtime archive layout")
		}
		if err = os.Rename(filepath.Join(staging, entries[0].Name()), javaHome); err != nil {
			return err
		}
		_ = os.Remove(archive)
	}
	setup.stage("preparing", 2, "Downloading Android command-line tools")
	if _, err := os.Stat(sdkTool(sdk, "sdkmanager")); err != nil {
		platform, hash := "linux", "4e4c464f145a7512b57d088ac6c278c03c9eea610886b35a5e0804e74eedf583"
		if runtime.GOOS == "windows" {
			platform = "win"
			hash = "90ae805d20434428bffcb699c290860f19bb5f66a67e6b330067e3de801fb04a"
		}
		archive := filepath.Join(root, "command-tools.zip")
		if err = setup.download(ctx, "https://dl.google.com/android/repository/commandlinetools-"+platform+"-15859902_latest.zip", hash, archive); err != nil {
			return err
		}
		staging, err := os.MkdirTemp(root, "sdk-unpack-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(staging)
		setup.activity("extracting", "Unpacking Android command-line tools")
		if err = extractRuntime(archive, staging, true); err != nil {
			return err
		}
		if err = os.MkdirAll(filepath.Join(sdk, "cmdline-tools"), 0700); err != nil {
			return err
		}
		if err = os.Rename(filepath.Join(staging, "cmdline-tools"), filepath.Join(sdk, "cmdline-tools", "latest")); err != nil {
			return err
		}
		_ = os.Remove(archive)
	}
	setup.stage("installing", 3, "Installing Android tools and Google Play image — several GB; review license prompts below")
	if err := setup.command(ctx, javaHome, sdk, sdkTool(sdk, "sdkmanager"), "", "--sdk_root="+sdk, "--install", "platform-tools", "emulator", androidImage); err != nil {
		return err
	}
	setup.stage("preparing", 4, "Installing scrcpy video and audio streaming")
	if err := setup.download(ctx, "https://github.com/Genymobile/scrcpy/releases/download/v"+scrcpyVersion+"/scrcpy-server-v"+scrcpyVersion, scrcpySHA256, filepath.Join(root, "scrcpy-server-v"+scrcpyVersion)); err != nil {
		return err
	}
	setup.stage("checking", 5, "Checking hardware acceleration")
	emulator := filepath.Join(sdk, "emulator", "emulator")
	if runtime.GOOS == "windows" {
		emulator += ".exe"
	}
	if err := setup.command(ctx, javaHome, sdk, emulator, "\n", "-accel-check"); err != nil {
		return errors.New("Hardware acceleration is unavailable. On Windows enable virtualization in BIOS and Windows Hypervisor Platform, then restart. On Linux enable KVM and give your user access to /dev/kvm. See setup details, then retry")
	}

	setup.stage("creating", 6, "Creating your first virtual phone")
	devices, err := androidCommandOutput(ctx, emulator, "-list-avds")
	if err != nil {
		return err
	}
	for _, name := range strings.Fields(string(devices)) {
		if name == "Isolate_01" {
			return nil
		}
	}
	if len(strings.Fields(string(devices))) >= androidEmulatorLimit {
		return errors.New("The 10-emulator limit has been reached")
	}
	return setup.command(ctx, javaHome, sdk, sdkTool(sdk, "avdmanager"), "no\n", "create", "avd", "--name", "Isolate_01", "--package", androidImage, "--device", "pixel_6")
}
func downloadJSON(ctx context.Context, url string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 60 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("Download returned HTTP %d", response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 4*1024*1024)).Decode(target)
}
func validDownload(path, hash string) error {
	if len(hash) != 64 {
		return errors.New("Missing SHA-256 checksum")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err = io.Copy(digest, file); err != nil {
		return err
	}
	if !strings.EqualFold(hex.EncodeToString(digest.Sum(nil)), hash) {
		return errors.New("Download checksum does not match")
	}
	return nil
}
func (setup *androidSetup) download(ctx context.Context, url, hash, destination string) error {
	if validDownload(destination, hash) == nil {
		setup.activity("checking", "Reusing a verified download already on this PC")
		return nil
	}
	if filepath.Base(destination) == "scrcpy-server-v"+scrcpyVersion {
		if bundled, err := setup.restoreBundled(ctx, "scrcpy-server-v"+scrcpyVersion, destination); err != nil || bundled {
			return err
		}
	}
	if !strings.HasPrefix(url, "https://") {
		return errors.New("Runtime downloads require HTTPS")
	}
	if len(hash) != 64 {
		return errors.New("Runtime download is missing a checksum")
	}
	setup.activity("downloading", "Connecting to the download server…")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := setup.downloadClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Minute}
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("Runtime download returned HTTP %d", response.StatusCode)
	}
	file, err := os.CreateTemp(filepath.Dir(destination), "download-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	buffer := make([]byte, 128*1024)
	var total int64
	started := time.Now()
	setup.transferProgress(0, response.ContentLength, started)
	last := started
	for {
		n, readErr := response.Body.Read(buffer)
		if n > 0 {
			if _, err = file.Write(buffer[:n]); err != nil {
				return err
			}
			total += int64(n)
			setup.transferProgress(total, response.ContentLength, started)
			if total > 1024*1024*1024 {
				return errors.New("Runtime download exceeds its size limit")
			}
			if time.Since(last) > time.Second {
				_, _ = setup.Write([]byte(fmt.Sprintf("\nDownloaded %d MiB", total/(1024*1024))))
				last = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if err = file.Close(); err != nil {
		return err
	}
	setup.activity("verifying", "Download finished — checking file integrity")
	if err = validDownload(file.Name(), hash); err != nil {
		return err
	}
	return os.Rename(file.Name(), destination)
}

// Extract files before links, rejecting traversal, absolute paths, special files,
// and oversized archives. Links can never become parents during extraction.
func extractRuntime(archive, directory string, zipFormat bool) error {
	var total int64
	links := map[string]string{}
	target := func(name string) (string, error) {
		normalized := strings.ReplaceAll(name, "\\", "/")
		if strings.HasPrefix(normalized, "/") {
			return "", errors.New("Unsafe runtime archive path")
		}
		clean := filepath.Clean(filepath.FromSlash(normalized))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || strings.Contains(clean, ":") {
			return "", errors.New("Unsafe runtime archive path")
		}
		return filepath.Join(directory, clean), nil
	}
	write := func(name string, mode os.FileMode, reader io.Reader) error {
		path, err := target(name)
		if err != nil {
			return err
		}
		if mode.IsDir() {
			return os.MkdirAll(path, 0755)
		}
		if !mode.IsRegular() {
			return errors.New("Unsupported archive entry")
		}
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm()|0600)
		if err != nil {
			return err
		}
		n, err := io.Copy(file, io.LimitReader(reader, 1024*1024*1024+1))
		closeErr := file.Close()
		total += n
		if total > 2*1024*1024*1024 || n > 1024*1024*1024 {
			return errors.New("Runtime archive is too large")
		}
		if err != nil {
			return err
		}
		return closeErr
	}
	if zipFormat {
		reader, err := zip.OpenReader(archive)
		if err != nil {
			return err
		}
		defer reader.Close()
		for _, entry := range reader.File {
			if entry.Mode()&os.ModeSymlink != 0 {
				return errors.New("Unexpected symlink in ZIP runtime")
			}
			body, err := entry.Open()
			if err != nil {
				return err
			}
			err = write(entry.Name, entry.Mode(), body)
			_ = body.Close()
			if err != nil {
				return err
			}
		}
	} else {
		file, err := os.Open(archive)
		if err != nil {
			return err
		}
		defer file.Close()
		gz, err := gzip.NewReader(file)
		if err != nil {
			return err
		}
		defer gz.Close()
		reader := tar.NewReader(gz)
		for {
			entry, err := reader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if entry.Typeflag == tar.TypeSymlink {
				links[entry.Name] = entry.Linkname
				continue
			}
			if entry.Typeflag == tar.TypeXGlobalHeader {
				continue
			}
			mode := os.FileMode(entry.Mode)
			if entry.Typeflag == tar.TypeDir {
				mode |= os.ModeDir
			} else if entry.Typeflag != tar.TypeReg {
				return errors.New("Unsupported runtime archive entry")
			}
			if err = write(entry.Name, mode, reader); err != nil {
				return err
			}
		}
	}
	for name, link := range links {
		path, err := target(name)
		if err != nil {
			return err
		}
		resolved, err := target(filepath.Join(filepath.Dir(name), link))
		if err != nil || filepath.IsAbs(link) {
			return errors.New("Unsafe runtime symlink")
		}
		if _, err = os.Lstat(path); !os.IsNotExist(err) {
			return errors.New("Duplicate runtime symlink")
		}
		relative, err := filepath.Rel(filepath.Dir(path), resolved)
		if err != nil {
			return err
		}
		if err = os.Symlink(relative, path); err != nil {
			return err
		}
	}
	return nil
}

// Create another independent Android installation using the image already
// downloaded by setup. Never overwrite an existing AVD or copy account data.
func (s *AndroidService) CreateAndroidDevice(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("Isolate is closing")
	}
	setup := s.setup
	setup.mu.Lock()
	if setup.status.Running {
		setup.mu.Unlock()
		s.mu.Unlock()
		return errors.New("Wait for Android setup to finish")
	}
	setup.status.Running = true
	setup.resetProgressLocked("creating")
	setup.status.Step = 0
	setup.status.Stage = "Creating another virtual phone"
	setup.status.Error = ""
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	setup.cancel = cancel
	setup.mu.Unlock()
	s.mu.Unlock()
	defer cancel()
	var result error
	defer func() {
		setup.mu.Lock()
		setup.status.Running = false
		setup.status.WaitingLicense = false
		setup.input = nil
		if result != nil {
			setup.status.Error = result.Error()
		}
		setup.status.Stage = "Virtual phone creation finished"
		if result != nil {
			setup.status.Phase = "paused"
		} else {
			setup.resetProgressLocked("complete")
			setup.status.Percent = 100
		}
		setup.mu.Unlock()
	}()
	sdk := filepath.Join(setup.status.Root, "sdk")
	tool := sdkTool(sdk, "avdmanager")
	if _, err := os.Stat(tool); err != nil {
		result = errors.New("Run Android setup before adding another virtual phone")
		return result
	}
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	emulator := filepath.Join(sdk, "emulator", "emulator"+suffix)
	devices, err := androidCommandOutput(ctx, emulator, "-list-avds")
	if err != nil {
		result = err
		return result
	}
	names := strings.Fields(string(devices))
	if len(names) >= androidEmulatorLimit {
		result = errors.New("The 10-emulator limit has been reached")
		return result
	}
	existing := map[string]bool{}
	for _, name := range names {
		existing[name] = true
	}
	for number := 1; number <= androidEmulatorLimit; number++ {
		name := fmt.Sprintf("Isolate_%02d", number)
		if !existing[name] {
			result = setup.command(ctx, filepath.Join(setup.status.Root, "java"), sdk, tool, "no\n", "create", "avd", "--name", name, "--package", androidImage, "--device", "pixel_6")
			return result
		}
	}
	result = errors.New("No virtual phone name is available")
	return result
}
