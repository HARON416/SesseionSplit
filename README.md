# Isolate

**Android app screens and separate browser sessions. One workspace.**

Isolate is a desktop application with an Android emulator screen wall and a
separate browser-profile workspace. Android mode displays and controls real
Android screens, including the native Instagram app; it does not turn websites
into pretend Android screens. Browser profiles have their own saved browser data, so you can work with
different accounts without repeatedly signing out or mixing their sessions.
It uses browsers already installed on your computer rather than downloading or
compiling a custom browser.

The repository was originally named **SessionSplit**; the application and current
build artifacts are named **Isolate**.

- [Getting started](#getting-started)
- [Privacy and limitations](#privacy-and-limitations)
- [Development](#development)
- [Checks and builds](#checks-and-builds)
- [Project structure](#project-structure)
- [Testing and design notes](#testing-and-design-notes)

## Why Isolate?

Switching between work, personal, and client accounts in a shared browser can
lead to the wrong login, mixed browsing data, and repeated sign-ins. Isolate gives
each context a named profile and keeps those profiles together in one dashboard.
Saved sessions stay with their profile between launches, subject to each
website's login and session policies.

## Use cases

- **Social media work:** open separate sessions for accounts you own or are
  authorized to manage, without signing out of another account first.
- **Client work:** keep each client's web tools and account sessions separate.
- **Work and personal browsing:** maintain distinct browser environments for
  different roles or projects.
- **Website testing:** use independent profiles to check signed-in experiences,
  account roles, and behavior across supported browsers.
- **Proxy-based connectivity:** assign a proxy to a profile and test its route
  before saving the settings.

## What it does

- Set up independent Android virtual phones, stream their screens inside the
  desktop app, and control them with mouse, keyboard, and clipboard text.
- Switch between an Android screen wall and the browser-profile dashboard.
- Create, edit, open, and delete profiles from a single dashboard, choosing a
  browser when creating a profile or changing it later in Edit or Settings.
- Keep cookies, logins, browsing history, cache, and site storage separate between
  profiles, with persistent browser data and per-profile downloads.
- Choose an installed browser for each profile and show its logo on the card.
- Open each profile directly in its selected browser’s normal window.
- Configure per-profile proxies, with proxy passwords stored in the operating
  system's keyring rather than the profile registry.
- Test draft proxy settings before saving and view the public IP, route, and
  response time.
- Configure browser environment settings such as language, timezone, window size,
  and permission handling, within the selected browser integration's limits.
- See profile creation and last-open activity, with light and dark themes.

## Supported browsers and platforms

Browser choices include Chrome, Chromium, Brave, Microsoft Edge, Vivaldi, Opera,
Opera GX, Ungoogled Chromium, Thorium, and Firefox. **Other Chromium-based browser**
lets you select a compatible executable or portable installation manually.
The chosen browser must be installed separately; Safari is not supported.

Distribution targets are **Windows amd64** through an NSIS installer and
**Ubuntu amd64** through a DEB package. The Ubuntu CI build targets Ubuntu
24.04+. Automatic Android setup and bundled runtime tools support these two
64-bit Intel/AMD platforms. Firefox uses its own native integration; supported
environment settings depend on the installed browser version.

## Getting started

### Android Instagram screens

Android screens is the initial workspace; Isolate remembers your workspace
selection. **Profiles** in the header opens the browser-profile manager.
These are separate systems: browser cookies,
proxy settings, and saved logins are not imported into Android. Existing browser
profiles remain available and unchanged.

One-time setup on a client's PC (Windows or Linux, Intel/AMD 64-bit):

1. Install and open Isolate, then select **Android setup → Set up Android**.
2. Allow the runtime downloads (several GB). Isolate downloads a Java 21 runtime,
   Android command-line tools, Platform Tools, Emulator, an Android 15 Google Play
   image, and a pinned scrcpy server. Android Studio and FFmpeg are not required.
3. Read each Google license prompt in **Setup details and license text** and
   choose **Accept / continue** or **Decline** yourself. Isolate does not accept
   SDK or system-image licenses automatically.
4. Setup checks hardware acceleration and creates `Isolate_01`. Windows needs
   virtualization enabled in firmware and Windows Hypervisor Platform; Linux
   needs KVM and access to `/dev/kvm`. Setup reports errors and can be cancelled
   or retried; completed runtime downloads are reused.
5. Select **Start emulator**, wait for Android to boot, then **Connect screen**.
   Open Play Store inside the phone, install Instagram, and sign in manually.
6. Open Instagram inside Android. Use **Screen size** to adjust the cards, and
   click the phone you want to control and hear. Other phones remain muted.
   Tap, drag to swipe, scroll, type, paste, and use Android's navigation gestures.

**Add virtual phone** creates another independent Android installation using the
same downloaded system image, up to ten configured devices. It never overwrites
an existing AVD or copies its account data. Existing Android Studio AVDs are also
listed when available; Android Studio remains an optional manual setup route.
There is no physical-device or remote-server integration in this version.

A single phone defaults to a 320-pixel-wide card; multiple phones default to
180-pixel cards. Use **Screen size** to choose a width from 100 to 420 pixels.
Card size changes the desktop layout;
it does not change the Android display resolution or streaming resolution.
Use Android's app icons and navigation inside the phone. There is no additional
phone controls menu. **Start emulator**, **Connect screen**, **Play screen**, and
error recovery prompts appear on the display when needed. Audio follows the
phone selected by clicking its screen.
Connecting a screen enables Android's native gesture navigation, replacing the
bottom Back, Home, and Overview buttons. Drag inward from either side edge to go
back; drag up from the bottom edge to go home, or drag up and hold for recent apps.

**Ten is a software ceiling, not a tested concurrency guarantee.** Each emulator
started by Isolate requests 2.5 GiB RAM and 2 virtual CPU cores. Ten request 25 GiB RAM
before host OS and graphics overhead. Start with one or two; the UI requires
acknowledging the resource warning before starting more than two. Actual
capacity must be benchmarked on the target machine. The installed Android 35
emulator raises smaller RAM requests to 2560 MiB, so 1.5 GiB is not an effective
limit. Isolate keeps that memory floor. For sharp text and reels/video playback, new
launches use a native 1080 × 2400 framebuffer. Connecting existing phones restores
and verifies a 1080 × 2400 Android display at 420 dpi. Existing apps and device
data are preserved. Full-resolution playback increases graphics and decoding work.

Isolate starts emulators with host GPU rendering and Vulkan disabled. This
avoids the software-rendering path that crashed in RenderThread while opening
Instagram on the tested Intel system. Host graphics drivers must support
OpenGL ES; apps requiring Vulkan are not supported in this mode. Launches use
a cold boot rather than restoring Quick Boot graphics state. Startup takes longer;
installed apps, accounts, and persistent browser/app data remain on disk.

Screen streaming uses the pinned scrcpy 5.0.1 Android server, H.264 video up to
30 fps at native 1080 × 2400 resolution, targeting 16 Mbps video and 192 kbps AAC
audio. The Go client
packages encoded samples as fragmented MP4, retaining device timestamps, over
capability-protected loopback WebSockets. Playback stays inside Isolate using
the system WebView's Media Source Extensions. No FFmpeg process, separate
scrcpy window, temporary recording file, or periodic screenrecord restart is
needed. Actual frame rate depends on the host, emulator, encoder, and WebView.
Audio capture requires Android 11+; Android 11 must be unlocked when connecting.
If Android disables or disconnects audio capture, video continues without audio.
Transient stream failures reconnect automatically up to three times. Persistent
failures include server diagnostics under Device error details. Playback volume stays at 100%;
only the selected phone plays sound.
An unsupported codec or WebView produces a playback error rather than silently
showing stale screenshots. Update WebView2 on Windows or WebKitGTK on Linux.

Display/encoder configuration changes trigger reconnection. If automatic
recovery fails, use **Retry screen**. Hover over a connected phone screen and use the mouse
wheel or trackpad to scroll vertically or horizontally. Wheel movement becomes
short swipes inside that phone; drag-to-swipe remains available. Gestures support
taps and single-pointer swipes. Ctrl+V (or Cmd+V / Shift+Insert) pastes host
clipboard text into the phone's focused input. Pasting trims each line, removes
blank lines, and joins the remaining lines with a space. Text
injection supports printable ASCII except `%`; use Android's on-screen keyboard
for other characters. Secure/protected Android surfaces may not be capturable.

The current screen wall has no per-phone stop button. Switching
workspaces disconnects streaming but leaves emulators running. Closing Isolate
cancels setup, disconnects streams, and stops emulators it started; emulators
started elsewhere are left running. Instagram availability, login, Reels,
inbox behavior, and audio must still be verified on a real configured emulator
and on the target client's Windows PC; generated-media tests do not establish
Instagram compatibility.

Emulators started by Isolate save stdout/stderr and Android's crash buffer under
the user config directory in `isolate/android/logs/`, named
`<phone>-emulator.log` and `<phone>-android-crash.log`. Each log is limited to
2 MiB, retains recent output, and keeps one previous run as `.log.previous`.
Unexpected exits show the actual exit status, log paths, and recent output under
**Device error details**. Logs remain available after closing the app. Capture
starts when the phone is launched by the updated app; older discarded emulator
output cannot be recovered.

Runtime files are stored under the user config directory in `isolate/android/`;
AVDs and their app data use Android's normal user AVD directory. Downloads come
from Google, Eclipse Adoptium, and the official scrcpy release, with SHA-256
verification before extraction/use. Google SDK tools manage system-image
installation. The application installer contains the setup flow; these large
runtime dependencies are downloaded on first use. See
[Android runtime implementation and validation](docs/ANDROID_RUNTIME.md).

References: [Android virtual devices](https://developer.android.com/studio/run/managing-avds),
[ADB](https://developer.android.com/tools/adb), and
[emulator command-line options](https://developer.android.com/studio/run/emulator-commandline).

### Browser profiles

1. Install Isolate and a supported browser.
2. Select **Profiles** in the header, then **Add new profile**. Enter a recognizable name, choose a browser, and create the profile.
3. Use **Edit** to change its name or browser, or open **Settings** to configure and test a proxy.
4. Save the settings, open the profile, and sign in to the websites you need.
5. Reopen the same profile later to use its saved browser data.

New sessions open Google at `https://google.com/`. Click **Open** on a
profile to launch its selected browser in a normal window, using that profile's
saved cookies, sessions, and settings. Opening an already running profile focuses
its browser window. Profiles stay in the dashboard; no live viewer is opened.

Close a profile's running browser before saving settings changes. Changing its
browser does not transfer cookies or logins: each browser keeps separate data
within that profile. Switching back reuses that browser's previous data, although
websites may still require you to sign in again.

Deleting a profile requires confirmation and permanently removes its browser
data, cache, and downloaded files. Other profiles are unaffected.

## Privacy and limitations

Isolate separates local browser storage; it does **not** guarantee anonymity,
prevent websites from linking accounts, or guarantee protection from account
restrictions or bans. A proxy connection test verifies connectivity, not DNS or
WebRTC leak protection. Use accounts you own or are authorized to access, and
follow each platform's rules.

The dashboard launches browser sessions. It is not a unified social inbox,
publishing scheduler, or bulk account automation tool. Running many browser
profiles at once still consumes your computer's memory and CPU.

Profile metadata is stored under the operating system's user config directory in
`isolate/profiles.json`, with browser data under `isolate/profiles/<id>/`.
New installations use `isolate`; existing SessionSplit installations retain their
original `sessionsplit` directory and keyring namespace to preserve saved sessions
and proxy passwords. Renaming the repository folder is not required.

See [browser privacy and networking](BROWSER_PRIVACY.md) for integration details
and protection limits. Implementation progress is tracked in [TODO.md](TODO.md).

## Development

Install Go 1.26.2+, Node.js 22.12+ (or 20.19+), Make, and the platform prerequisites for Wails v3.
The Wails Go module, CLI, and frontend runtime are pinned to `v3.0.0-beta.34`.

On Ubuntu, install the native build libraries and Android playback codecs:

```sh
sudo apt install build-essential pkg-config libgtk-4-dev libwebkitgtk-6.0-dev \
  gstreamer1.0-plugins-bad gstreamer1.0-libav
```

Windows needs WebView2 for the desktop interface and embedded Android playback.
NSIS is required when building Windows installers. Run `make doctor` to check
the local Wails prerequisites.

From the project root:

```sh
npm --prefix frontend ci
make dev
```

`make dev` installs the Wails CLI version pinned in `go.mod` into `.tools/bin`
when needed, then starts the desktop app with frontend hot reload and Go rebuilds.
`make run` is an alias. Vite uses port 9246. Override it with
`make dev VITE_PORT=9250` if needed.

Isolate uses Wails v3, Svelte 5, TypeScript, and Vite 8. Use `npm run dev` in
`frontend` for a browser preview; profile operations and native window controls
require the desktop app.

## Checks and builds

```sh
make check
npm --prefix frontend test
npm --prefix frontend run build
go test ./...
make build
```

`make check` checks Svelte and TypeScript. The frontend tests cover media-player
behavior, timestamps, and theme handling. Build the frontend before Go tests:
the desktop entry point embeds `frontend/dist`, and UI integration tests serve
those built assets. `go test ./...` covers the application and runtime-bundle
tooling; browser, live-emulator, download, and keyring checks are opt-in.

`make build` creates the desktop executable for the host platform. Use the
package commands below to create installers.

For Windows installers built remotely while developing on Ubuntu, see
[Windows CI and testing](WINDOWS_BUILDS.md). The manual workflow builds the
application test installer using an installed supported browser; no custom
browser compilation is required.

### Distribution packages

Only Windows NSIS installers and Ubuntu DEB packages are configured. Unused
platform templates remain in the repository but are not included in the root
Taskfile. Packages are currently unsigned, and users need a supported browser
installed separately.

Run the commands below from the project root. The Make targets include the
project-local CLI path and install the pinned Wails CLI when needed, so no
separate `export PATH` is needed.

| Target | Build command | Output |
| --- | --- | --- |
| Ubuntu standard | `make linux-deb` | `bin/isolate.deb` |
| Ubuntu with Java and scrcpy | `make linux-bundled-deb` | `bin/isolate-bundled-tools.deb` |
| Windows standard | `make windows-installer` | `bin/isolate-amd64-installer.exe` |
| Windows with Java and scrcpy | `make windows-bundled-installer` | `bin/isolate-amd64-bundled-tools-installer.exe` |

To rebuild all four packages from Ubuntu after installing the prerequisites below:

```sh
make linux-deb &&
make linux-bundled-deb &&
make windows-installer &&
make windows-bundled-installer
```

Run these builds sequentially because they share generated bindings and frontend
assets. After rebuilding all four, refresh and verify their checksums:

```sh
(
  cd bin &&
  sha256sum isolate-amd64-installer.exe isolate-amd64-bundled-tools-installer.exe \
    isolate.deb isolate-bundled-tools.deb > SHA256SUMS.txt &&
  sha256sum --check SHA256SUMS.txt
)
```

Builds reuse fixed output filenames in `bin/`, replacing the previous executable
or package for that target instead of creating numbered or timestamped copies.
Windows and Ubuntu outputs remain separate, so building one does not remove the
other. To remove all local build outputs, including old SessionSplit artifacts:

```sh
make clean
```

This removes only `bin/`; it does not uninstall the app or delete saved profiles,
credentials, source files, or the cached Wails CLI. Cleaning is optional between
builds because existing output files are overwritten automatically.

#### Windows installer editions

Two Windows amd64 editions are available:

| Edition | Included | First setup |
| --- | --- | --- |
| Standard | Isolate and guided setup | Downloads Java, streaming tools, and Android components |
| Bundled tools | Isolate, Java, scrcpy, matching source archives and notices | Uses included Java/streaming tools; downloads Android components from Google |

The bundled-tools edition is **not a fully offline Android installer**. Google's
SDK terms restrict redistribution (§3.4, with separately licensed open-source
exceptions in §3.5). We do not bundle Google's SDK packages, Play images, existing
AVDs, Instagram, or account data. A fully offline Google Play edition needs an
appropriate distribution arrangement. See [Google's SDK terms](https://developer.android.com/studio/terms).

Build either edition from Ubuntu with the prerequisites described below:

```sh
make windows-installer
make windows-bundled-installer
```

The second command downloads the permitted original runtime artifacts on the
build machine, verifies them, and includes matching Java/scrcpy sources and
notices. The verified payload is cached under `.tools/runtime/windows-amd64`.
Outputs are `bin/isolate-amd64-installer.exe` and
`bin/isolate-amd64-bundled-tools-installer.exe`; the filenames remain separate.
Both editions still require internet for Google downloads and sign-in.
Microsoft's existing WebView2 bootstrapper may also require a runtime download.
Neither edition changes the client's saved virtual phone when upgrading.

The manual Windows CI workflow has an `installer_flavor` selector for the same
two editions. It builds the selected artifact without publishing a public release.

#### Windows installer from Ubuntu

Install NSIS once:

```sh
sudo apt install nsis
```

Build the standard Windows x64 installer:

```sh
make windows-installer
```

Output: `bin/isolate-amd64-installer.exe`. The build may need internet access
for dependencies and the WebView2 bootstrapper. Test the installer on Windows.

#### Ubuntu installer editions

Ubuntu amd64 now has the same two editions:

```sh
make linux-deb
make linux-bundled-deb
```

The outputs are `bin/isolate.deb` and `bin/isolate-bundled-tools.deb`.
The bundled-tools DEB includes Linux Java, scrcpy, corresponding sources, and
notices under `/usr/lib/isolate/runtime`. Setup verifies and extracts these
local files; Android/Google Play components still download directly from Google.
Both DEBs install the same `isolate` package and preserve saved phone/profile
user data when switching editions.

To install the bundled-tools edition, close Isolate and run:

```sh
sudo apt install --reinstall ./bin/isolate-bundled-tools.deb
```

`make linux-bundled-deb` prepares and verifies its cached payload under
`.tools/runtime/linux-amd64` before packaging. The Ubuntu CI workflow also has an
`installer_flavor` selector. ARM64 runtime bundles are not configured.

#### Ubuntu DEB: rebuild and reinstall

Install native build dependencies once:

```sh
sudo apt install build-essential pkg-config libgtk-4-dev libwebkitgtk-6.0-dev \
  gstreamer1.0-plugins-bad gstreamer1.0-libav
```

Close Isolate, then rebuild and install the standard edition over the existing
version:

```sh
make linux-deb &&
sudo apt install --reinstall ./bin/isolate.deb
```

Or rebuild and reinstall the edition with Java and scrcpy:

```sh
make linux-bundled-deb &&
sudo apt install --reinstall ./bin/isolate-bundled-tools.deb
```

Launch Isolate again after installation. If Android setup was paused by an error
in the previous version, select **Retry setup** to reuse completed downloads.
Reinstalling preserves user profiles; do not remove their data directory.
When upgrading from SessionSplit, APT replaces the old `sessionsplit` package.
Local builds on Ubuntu 26.04 use that system's libraries and may not run on older
Ubuntu releases. The DEB declares GTK4, WebKitGTK 6.0, and GStreamer H.264/AAC
playback dependencies for APT to install.

#### Other build environments

On Windows, with the pinned Wails CLI and NSIS available on `PATH`:

```powershell
wails3 package GOOS=windows ARCH=amd64 INSTALL_SCOPE=user
```

For remote builds, manually run **Windows installer** or **Ubuntu DEB** in GitHub
Actions. The Ubuntu workflow builds on Ubuntu 24.04 for the 24.04+ baseline.
Both workflows upload only the requested package format plus verification files;
if artifact storage is full, files are attached to a draft release instead.

See the Wails v3 [Windows packaging](https://v3.wails.io/guides/build/windows/)
and [Linux packaging](https://v3.wails.io/guides/build/linux/) documentation.

## Project structure

| Path | Purpose |
| --- | --- |
| `main.go` | Desktop window, embedded frontend, and Wails service registration |
| `profiles.go`, `settings.go`, `profile_storage.go` | Profile lifecycle, settings, and storage migration |
| `engine.go`, `browser_catalog.go`, `firefox.go` | Installed-browser discovery and Chromium/Firefox integration |
| `proxy.go`, `profile_network.go`, `credentials.go` | Proxy routing, connection diagnostics, and keyring credentials |
| `android_setup*.go`, `android_runtime_bundle.go` | Guided runtime setup, licenses, downloads, and bundled-tool discovery |
| `android_workspace.go`, `android_diagnostics.go` | Emulator lifecycle, device input, display configuration, and logs |
| `android_scrcpy.go`, `android_media.go` | Screen transport and fragmented MP4 video/audio |
| `frontend/src/` | Svelte interface, Android media player, themes, and styles |
| `frontend/bindings/` | Wails-generated TypeScript service bindings |
| `frontend/tests/`, `*_test.go` | Frontend tests and Go unit/integration tests |
| `build/`, `internal/runtimebundle/` | Platform packaging, runtime manifests, and bundle preparation |
| `.github/workflows/` | Manually triggered Windows installer and Ubuntu DEB builds |

For more detail, see [Android runtime and validation](docs/ANDROID_RUNTIME.md),
[browser privacy and networking](BROWSER_PRIVACY.md),
[Windows builds](WINDOWS_BUILDS.md), [product scope](PRODUCT.md), and
[design notes](DESIGN.md). [TODO.md](TODO.md) tracks remaining release work;
[project history](docs/PROJECT_HISTORY.md) records earlier implementation work.

## Testing and design notes

After the standard checks above, enable integration suites as needed:

```sh
# Installed-browser integration and profile UI (Chrome required for UI checks).
ISOLATE_BROWSER_TESTS=1 go test -v .

# Android setup and screen-wall UI, using Chrome and the built frontend.
ISOLATE_ANDROID_SETUP_UI_TESTS=1 ISOLATE_ANDROID_SCREEN_UI_TESTS=1 go test -run 'TestAndroid(Setup|Screen)' -v .

# Actual desktop credential store; requires an available, unlocked keyring.
ISOLATE_KEYRING_TESTS=1 go test -run TestOSCredentialStoreRoundTrip -v .
```

Android encoded-media, native WebKitGTK, runtime-download, and live-device checks
have separate prerequisites and switches documented in
[Android validation](docs/ANDROID_RUNTIME.md#validation). FFmpeg and FFprobe are
needed for generated-media tests, but are not application runtime dependencies.
Live screen connections configure the emulator's display and gesture navigation;
use a test virtual phone for live checks.

The sun/moon button beside the window controls switches between light and dark
mode. The initial theme follows your system preference; explicit choices are
saved locally and restored on the next launch.

Theme colors were curated from 192 Coolors palettes across four pages:
[Light Steel](https://coolors.co/f8f9fa-e9ecef-dee2e6-ced4da-adb5bd-6c757d-495057-343a40-212529)
for neutrals and [Midnight Sky](https://coolors.co/222222-ffffff-1c5d99-639fab-bbcde5)
for blue accents. The review record is in `frontend/theme-sources/coolors-review.json`.
