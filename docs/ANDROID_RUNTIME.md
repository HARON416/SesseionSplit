# Android runtime and streaming

Isolate's Windows/Ubuntu installer includes the guided setup and embedded player.
The optional Windows and Ubuntu amd64 **bundled-tools** editions includes original Temurin
Java and scrcpy artifacts, matching upstream source archives, and notices.
The default **standard** edition downloads those artifacts during setup.
Both editions download Google Android components directly from Google; neither
is a fully offline Google Play installer. See SDK terms §3.4–3.5:
https://developer.android.com/studio/terms.

Prepare and package using `make windows-bundled-installer` or
`make linux-bundled-deb`. Runtime manifests distinguish `windows/amd64` (Java
ZIP) and `linux/amd64` (Java tar.gz); clients reject mismatched targets. Verified
matching source/scrcpy artifacts can be reused between the build caches.
The Ubuntu DEB contains its payload at `/usr/lib/isolate/runtime`, with files
readable by the app user. Generated DEB config inherits the standard metadata,
dependencies, scripts, and app files, then adds only the manifest's explicit
allowlist. Both Ubuntu editions use the `isolate` package name; their output
filenames differ. No application or saved-phone data is copied into the payload.

The payload builder refuses missing matching Java sources and verifies the
original Java/scrcpy binaries before generating its manifest. Packaging verifies
every payload file and uses an explicit file list. The client verifies selected
artifacts before copying them into its private runtime cache; it never modifies
or removes the original files under the installation's `runtime/` directory.
The source archives and notices remain beside them in that directory.
No Google SDK files, system images, AVDs, credentials, or app data are harvested
from the build machine. The manifest rejects unknown components and targets.

Temurin redistribution information: https://adoptium.net/docs/faq/.
scrcpy license: https://github.com/Genymobile/scrcpy/blob/v5.0.1/LICENSE.


The setup panel shows its current step, download bytes/total and speed, and a
percentage for the current operation when the download server or Android tools
provide one. Unmeasurable operations show an animated activity bar. License
prompts pause that bar and show an explicit action-required message. No new data
for 20 seconds produces a visible waiting notice; it is not a claim of failure.

Clients do not open this repository or install Android Studio. Setup needs a
network connection and several GB of disk space. Automatic setup supports
Windows/Linux amd64; other architectures require a separate implementation.

## Runtime sources

- Eclipse Temurin Java 21 JRE: metadata and SHA-256 from
  `api.adoptium.net/v3/assets/latest/21/hotspot`, official Adoptium GitHub assets.
  The runtime is installed privately; it does not change the system Java.
- Android command-line tools revision 15859902: Google `dl.google.com` archives,
  with the platform's SHA-256 pinned from the official Android Studio downloads
  page. sdkmanager installs `platform-tools`, `emulator`, and
  `system-images;android-35;google_apis_playstore;x86_64`.
- scrcpy server 5.0.1: the official Genymobile GitHub release, SHA-256
  `764eb6f79811d5211fe9df341120882ba9994c7a61b897d7bf3fb662e53bc536`.
  The scrcpy desktop executable and FFmpeg are not runtime dependencies.

SDK licenses are displayed from sdkmanager's output. Setup waits for an explicit
response to each prompt. Instagram is installed by the client from Google Play.
Setup does not distribute an Instagram APK, copy sessions, or automate sign-in.
Licenses/notices accompanying Java and Android tools stay in their extracted
runtime directories. scrcpy is Apache-2.0 licensed; its source and license are at
https://github.com/Genymobile/scrcpy/tree/v5.0.1.

## Transport and playback

The backend pushes the pinned server to `/data/local/tmp/` on the selected
emulator, allocates a unique ADB forward, and opens scrcpy video/audio sockets.
The v5 protocol's codec, session, config, keyframe, and timestamp fields are
parsed with bounded packet lengths. A per-session token and a loopback listener
protect the browser transport. Disconnection closes sockets, terminates the
owned server command, removes the ADB forward, and removes its temporary JAR.

The server targets H.264 baseline, no B frames, native display resolution (no downscaling), and up
to 30 fps at a target 16 Mbps, with 192 kbps AAC audio. An idle-frame repeat every 100 ms avoids gaps in the media timeline. AAC-LC audio
and H.264 samples retain the device's shared microsecond clock. The Go MP4 writer
emits per-track initialization and media fragments. The WebView appends them to
Media Source buffers with bounded queues, evicts old data, and seeks near live
when playback falls behind. Only the selected screen may be unmuted.

The input path retains serial plus AVD identity validation. Pointer gestures and
keyboard events target only the selected installation. Rotation or codec
reconfiguration currently requests an explicit reconnect. Physical devices,
remote hosts, calls/microphone forwarding, uploads through the desktop clipboard,
and Unicode text injection are outside this implementation.

## Validation

```sh
go test ./...
npm --prefix frontend run check
npm --prefix frontend test
npm --prefix frontend run build
ISOLATE_ANDROID_VIDEO_TESTS=1 go test -run TestAndroidFragmentedMP4 -v .
ISOLATE_ANDROID_VIDEO_TESTS=1 ISOLATE_ANDROID_BROWSER_TESTS=1 go test -run TestAndroidFragmentedMP4 -v .
ISOLATE_ANDROID_VIDEO_TESTS=1 ISOLATE_ANDROID_BROWSER_TESTS=1 ISOLATE_ANDROID_WEBKIT_TESTS=1 go test -run TestAndroidFragmentedMP4 -v .
ISOLATE_ANDROID_LIVE_TESTS=1 go test -run TestAndroidLiveWebKit -v .
ISOLATE_ANDROID_SCREEN_UI_TESTS=1 go test -run TestAndroidScreenSurvivesDeviceRefresh -v .
ISOLATE_ANDROID_SETUP_TESTS=1 go test -run TestAndroidRuntimeDownloads -v .
```

The media test generates real 60 fps H.264 and AAC fixtures with FFmpeg, then
checks the output with FFprobe and decodes it. FFmpeg/FFprobe are test-only tools.
The browser test exercises the actual TypeScript player with both video-first
and audio-first arrival in headless Chrome. The optional WebKitGTK check requires
`xvfb-run`, `/usr/bin/python3`, PyGObject, and the GTK4/WebKit 6.0 introspection
packages. It runs the same player in the Linux desktop's native media engine.
The player registers both track buffers before parsing either track to avoid
WebKitGTK rejecting a later buffer with `QuotaExceededError`. An explicit
audio-unavailable message permits video-only playback, which the tests also cover.
If autoplay is denied, the stream stays connected and the card offers **Play
screen** to resume with a user gesture. The native test models this action when
its virtual display blocks autoplay.

The live WebKitGTK test requires an already running emulator and configured
Android tools. It connects the screen without sending input or changing apps.
To let the test start a stopped AVD, also set
`ISOLATE_ANDROID_LIVE_START=Isolate_01`; the test stops only phones it starts.

The screen UI regression test uses the built frontend and headless Chrome. It
checks that manual refreshes and two automatic device-polling cycles preserve
the same MediaSource and websocket. An explicit reconnect must replace the
player, and switching workspaces must clean it up.

Download tests check Java metadata, verify the scrcpy artifact, and read the
Android tool ZIP directories with small range requests. Set
`ISOLATE_ANDROID_FULL_DOWNLOAD_TESTS=1` as well to verify full Android tool
archives (about 340 MB);
they never accept licenses, install SDK packages, or create AVDs.

Before shipping to clients, test a clean Windows amd64 installation with:

1. Setup, license acceptance/decline, cancellation, retry, and paths with spaces.
2. Windows Hypervisor Platform/firmware virtualization errors and successful boot.
3. Play Store login, Instagram installation/sign-in, feed, inbox, and Reels audio.
4. Enlarge/return, mute selection, keyboard/paste, swipe, reconnect, and shutdown.
5. Two independent AVDs and measured resource use on the target PC.

A successful build or synthetic playback test does not establish Instagram login
compatibility, native WebView compatibility, or a ten-device concurrency claim.
