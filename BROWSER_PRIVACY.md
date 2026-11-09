# Profile networking and browser privacy

Isolate separates profile storage and configures selected browser/network
settings. It does not guarantee anonymity, an unrecognizable device, or immunity
from website account restrictions.

## Profile settings

Open **Settings** on a profile card. Changes are saved per profile and apply at
the next browser launch. Close an active profile browser using **Close browser**
before saving launch settings; this preserves its persistent data.

- **Connection:** direct, HTTP proxy, HTTPS proxy, or SOCKS5 proxy. Enter a host
  and port, not a URL containing credentials. Direct explicitly bypasses system
  proxy configuration. Proxy mode does not silently fall back to direct.
- **Credentials:** usernames are profile settings. Passwords live in the OS
  credential store: Secret Service on Linux, Keychain on macOS, and Credential
  Manager on Windows through `go-keyring`. An unavailable/locked keyring fails
  the operation; there is no plaintext fallback. Blank password input keeps the
  current secret; replacement and removal are explicit. Secrets are not
  returned by settings readback or stored in `profiles.json`.
- **Browser:** installed Chrome/Chromium, Brave, Edge, Vivaldi, Opera, Opera GX,
  Ungoogled Chromium, Thorium, Firefox, or a custom compatible Chromium executable.
  An unavailable selected browser produces an error instead of substituting
  another browser. For portable or undetected installations, choose **Other
  Chromium-based browser** and provide the full executable path without arguments.
  Compatibility depends on support for Chromium remote debugging and isolated
  user-data directories. Firefox uses its own WebDriver BiDi integration; select
  its named entry. Safari is not supported.
- **Environment:** language, timezone, window width/height, and Ask/Block for
  camera, microphone, and location. Empty fields use browser/system defaults.
  Width and height describe the browser window, not fabricated screen hardware.

Profile IDs, names, timestamps, card order, and downloads survive settings
changes. Chrome keeps its existing `profiles/<id>/browser` and `cache`
directories. Other named browsers use `profiles/<id>/<engine>/browser` and
`<engine>/cache`; existing Brave paths remain unchanged. Custom executables use
a stable directory derived from the selected executable path, so changing
custom browsers does not mix their databases. Switching browsers does not transfer
logins; switching back reuses that browser's previous data. Deleting a profile
removes both engines' data and that profile's downloads.

## Proxy implementation

Firefox keeps its own persistent profile under `profiles/<id>/firefox/browser`.
Its native HTTP/HTTPS proxy settings route through the same per-profile gateway
used by Chromium browsers. Proxied Firefox profiles disable WebRTC and DNS
prefetch; this does not certify browser-wide DNS isolation. Direct profiles reset
the managed proxy and WebRTC preferences at the next launch. Firefox's sandbox
stays enabled, and no geckodriver download is required.

Firefox language and timezone overrides use user-context-scoped WebDriver BiDi
commands before navigation. If the installed Firefox cannot apply a requested
override, launch fails rather than silently ignoring it. Leave these settings
blank to use native defaults. Worker coverage is not claimed. Managed preferences
are written to that profile's `user.js`; unrelated saved preferences are retained.
Remote automation's recommended preference changes are disabled so they do not
disable normal browser updates or replace ordinary browser defaults.

Firefox launch and profile isolation are implemented separately from the
Chromium engine. Closing and deleting a Firefox profile waits for its browser
process to exit before releasing connections or removing its data.

Each proxied browser receives a separate loopback HTTP gateway. It forwards
HTTP, HTTPS CONNECT, and WebSocket upgrades using only that profile's upstream
proxy and authentication. HTTP Basic and SOCKS5 username/password authentication
are supported. HTTPS proxy certificates are verified normally; no destination
certificate interception or custom CA is installed.

SOCKS5 target hostnames are sent to the proxy for resolution. Proxy mode disables
Chromium's implicit localhost bypass and QUIC, and requests the
`disable_non_proxied_udp` WebRTC policy. These settings reduce unintended direct
paths; they do not establish that every browser subsystem is isolated. The
gateway is loopback-only and trusts local processes; it is not a security
boundary against other programs running on the same machine.

Saved passwords are loaded into memory for the gateway's lifetime. Browser
shutdown closes gateway listeners and active connections. Replacement credentials
receive new random references so a failed registry save does not destroy the
previous working credential. Cleanup of obsolete keyring entries is best-effort
if the credential service itself fails.

## Connection diagnostics

**Test connection** makes an explicit request to
`https://api.ipify.org?format=json` using the proxy fields currently entered in
Settings, including unsaved edits. It reports the public IP observed by that
service, route, and response time. Opening or saving settings does not issue this
request. Testing does not save settings, change the running browser, or write
passwords to the registry or system keyring.

A newly entered password is used only in memory for the test. Leaving the field
blank keeps this profile's saved password; **Remove saved password** tests without
it. Changes to connection fields hide results that no longer match those values.
The saved configuration and credentials change only after **Save changes**.

The request has a bounded timeout and response size, validates the returned IP,
does not follow redirects, and exposes no raw network/credential errors.
Canceling/closing the dialog cancels an active test.

**DNS and WebRTC leak isolation remain unverified by this diagnostic.** A
single successful HTTPS request is not a browser-wide leak test. Independent
verification needs controlled DNS and WebRTC/STUN endpoints and browser traffic
observation; Isolate does not label these as protected based on the IP test.

## Browser engines and fingerprint protection

The engine interface owns process launch, storage paths, downloads, permission
policy, and environment setup. Page and out-of-process iframe targets pause
before their first scripts while selected environment settings are applied.
Language also uses native browser/process locale configuration. Timezone and
locale emulation are not claimed to cover every worker context.

Chrome/Chromium adds no Isolate-specific fingerprint protection. Brave uses
the installed browser's native fingerprinting protections and Shields defaults.
Isolate does not fabricate Canvas/WebGL/audio results, assert a distinct
device identity per profile, or verify every per-site Shields setting. Engine
availability, version, capabilities, and limitations are shown in Settings.

Isolate uses installed browsers with isolated saved sessions. The custom
Chromium fork and unused fingerprint seeds were removed on 2026-11-09. Existing
profiles still load with the same IDs, settings, and browser data. Old seed fields
are ignored when loading and omitted the next time the registry is saved.

Users must keep their installed browser up to date through its vendor or OS
package manager. Isolate neither bundles an independently patched Chromium
fork nor silently downloads a replacement browser. Custom executable paths are
saved per profile and do not change the global browser selection.

## Verification

```sh
npm --prefix frontend test
npm --prefix frontend run check
npm --prefix frontend run build
go test -race ./...
ISOLATE_BROWSER_TESTS=1 go test -v .
ISOLATE_KEYRING_TESTS=1 go test -run TestOSCredentialStoreRoundTrip -timeout 25s .
```

An optional official portable Brave binary can be tested with
`ISOLATE_BRAVE_TEST_BINARY=/path/to/brave` alongside
`ISOLATE_BROWSER_TESTS=1`. Other browser fixtures use
`ISOLATE_<ENGINE>_TEST_BINARY`, such as `ISOLATE_EDGE_TEST_BINARY` or
`ISOLATE_CUSTOM_TEST_BINARY`; replace hyphens with underscores in engine IDs.
Unavailable browsers are skipped rather than substituted. Integration fixtures use local test proxies,
synthetic credentials, and temporary profile data. UI diagnostic responses are
mocked so the UI suite never contacts a public IP service.

The desktop keyring integration test creates and removes only a dummy credential.
Cross-platform keyring support comes from the library; only the current desktop
environment is exercised by that test. Chrome/Rod UI captures do not validate
native WebKit accessibility or every supported OS.

References:

- [Chromium proxy behavior](https://chromium.googlesource.com/chromium/src/+/HEAD/net/docs/proxy.md)
- [Chromium user-data directories](https://chromium.googlesource.com/chromium/src/+/HEAD/docs/user_data_dir.md)
- [OS credential-store implementation](https://github.com/zalando/go-keyring)
- [Brave fingerprinting protections](https://github.com/brave/brave-browser/wiki/Fingerprinting-Protections)
- [Firefox WebDriver BiDi connection](https://developer.mozilla.org/en-US/docs/Web/WebDriver/How_to/Create_BiDi_connection)
- [Firefox remote preferences](https://firefox-source-docs.mozilla.org/remote/Prefs.html)

## Windows portability check — 2026-11-07

The Windows amd64 executable cross-compiles successfully. Browser discovery uses
explicit Chrome/Chromium and Brave locations (including per-user installations)
and never treats Edge as Chrome. IANA timezone data is embedded for installations
without Go or system zoneinfo. Windows browser-version reporting reads executable
resources rather than starting the GUI browser. Windows Credential Manager is the
password backend.

Compilation is not a Windows runtime certification. A Windows machine still
needs the WebView2 runtime and the selected browser, and should be used to check
browser launch/focus/close, engine switching, locale/timezone across tabs, proxy
routing, and real Credential Manager operations. Existing live integration tests
were run on Linux.

On 2026-11-08, the Windows Server 2025 CI workflow passed frontend checks/tests,
Go tests (including native file-version metadata), the offline browser tooling
tests, and native app/NSIS compilation. The unsigned test installer uses installed
Chrome/Chromium or Brave. This does not establish desktop GUI behavior, real
Credential Manager operations on Windows.
See [Windows build and testing instructions](WINDOWS_BUILDS.md).
