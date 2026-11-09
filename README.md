# Isolate

**Separate browser sessions. One place to manage them.**

Isolate is a desktop application for organizing and opening independent browser
profiles. Each profile has its own saved browser data, so you can work with
different accounts without repeatedly signing out or mixing their sessions.
It uses browsers already installed on your computer rather than downloading or
compiling a custom browser.

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

- Create, edit, open, and delete profiles from a single dashboard, choosing a
  browser when creating a profile or changing it later in Edit or Settings.
- Keep cookies, logins, browsing history, cache, and site storage separate between
  profiles, with persistent browser data and per-profile downloads.
- Choose an installed browser for each profile and show its logo on the card.
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

Distribution targets are **Windows** through an NSIS installer and **Ubuntu**
through a DEB package. Firefox uses its own native integration; supported
environment settings depend on the installed browser version.

## Getting started

1. Install Isolate and a supported browser.
2. Select **Add new profile**, enter a recognizable name, choose a browser, and create the profile.
3. Use **Edit** to change its name or browser, or open **Settings** to configure and test a proxy.
4. Save the settings, open the profile, and sign in to the websites you need.
5. Reopen the same profile later to use its saved browser data.

New browser windows open Instagram at `https://www.instagram.com/`. Reopening an
already running profile focuses its existing window rather than replacing its tabs.

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
From the project root:

```sh
npm --prefix frontend install
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
make build
```

For Windows installers built remotely while developing on Ubuntu, see
[Windows CI and testing](WINDOWS_BUILDS.md). The manual workflow builds the
application test installer using an installed supported browser; no custom
browser compilation is required.

### Distribution packages

Only Windows NSIS installers and Ubuntu DEB packages are configured. Unused
platform templates remain in the repository but are not included in the root
Taskfile. Packages are currently unsigned, and users need a supported browser
installed separately.

Run the commands below from the project root. Each Ubuntu build command includes
the project-local CLI path, so no separate `export PATH` is needed. If the CLI
is missing, install the pinned version with `make install-cli` first.

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

#### Windows installer from Ubuntu

Install NSIS once:

```sh
sudo apt install nsis
```

Build the Windows x64 installer:

```sh
PATH="$PWD/.tools/bin:$PATH" wails3 package GOOS=windows ARCH=amd64 CGO_ENABLED=0 INSTALL_SCOPE=user
```

Output: `bin/isolate-amd64-installer.exe`. The build may need internet access
for dependencies and the WebView2 bootstrapper. Test the installer on Windows.

#### Ubuntu DEB: rebuild and reinstall

Install native build dependencies once:

```sh
sudo apt install build-essential pkg-config libgtk-4-dev libwebkitgtk-6.0-dev
```

Close Isolate, then rebuild and install over the existing version:

```sh
PATH="$PWD/.tools/bin:$PATH" wails3 package GOOS=linux ARCH=amd64 &&
sudo apt install --reinstall ./bin/isolate.deb
```

Output: `bin/isolate.deb`. Launch Isolate again after installation.
Reinstalling preserves user profiles; do not remove their data directory.
When upgrading from SessionSplit, APT replaces the old `sessionsplit` package.
Local builds on Ubuntu 26.04 use that system's libraries and may not run on older
Ubuntu releases. The DEB declares GTK4 and WebKitGTK 6.0 runtime dependencies for
APT to install.

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

## Testing and design notes

Run the profile tests with `go test ./...`. Run Chrome integration and UI checks
with `ISOLATE_BROWSER_TESTS=1 go test -v .` after building the frontend.

The sun/moon button beside the window controls switches between light and dark
mode. The initial theme follows your system preference; explicit choices are
saved locally and restored on the next launch.

Theme colors were curated from 192 Coolors palettes across four pages:
[Light Steel](https://coolors.co/f8f9fa-e9ecef-dee2e6-ced4da-adb5bd-6c757d-495057-343a40-212529)
for neutrals and [Midnight Sky](https://coolors.co/222222-ffffff-1c5d99-639fab-bbcde5)
for blue accents. The review record is in `frontend/theme-sources/coolors-review.json`.
