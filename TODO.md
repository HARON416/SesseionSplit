# Isolate todo

Updated 2026-11-09. Isolate manages separate saved browser sessions using
installed Chromium-based browsers and Firefox. The custom Chromium fork, build workspace,
native fingerprint drafts, and unused fingerprint seeds have been removed.
Profile isolation does not guarantee immunity from account restrictions.

## Completed foundation

- [x] Persistent profiles: create, rename, delete, activity dates, and isolated browser storage, caches, and downloads.
- [x] Chromium-family browser catalog, installation discovery, custom executable paths, and separate browser storage.
- [x] Firefox discovery and native WebDriver BiDi integration with isolated saved sessions.
- [x] Optional per-profile proxies, authentication, OS-keyring secrets, and bounded connection diagnostics.
- [x] Language, timezone, window dimensions, and site-permission settings, with documented coverage limits.
- [x] Windows application CI, frontend/Go checks, NSIS test installer, and installer checksum.
- [x] Draft-release download fallback for full GitHub Actions artifact storage.

## Remaining verification and release work

- [ ] Runtime-test Edge, Vivaldi, Opera, Opera GX, Ungoogled Chromium, and Thorium on machines where they are installed.

- [ ] Test managing 100+ saved profiles with only a small number of browsers open at once; measure memory use and dashboard responsiveness.
- [ ] Validate install/launch/uninstall, browser lifecycle, profile persistence/isolation, downloads, proxies, and real Credential Manager operations on Windows.
- [ ] Independently measure browser DNS and WebRTC/STUN paths; document observed coverage and limits.
- [ ] Complete clean-install, upgrade, regression, and accessibility checks on supported platforms.
- [ ] Sign Windows release artifacts and finalize supported-platform packaging.
- [ ] Retain required licences/notices and publish artifact hashes and build metadata.
- [ ] Keep product/privacy documentation aligned with verified capabilities.

## Supporting documents

- [Windows CI, installer downloads, and testing](WINDOWS_BUILDS.md)
- [Current privacy and networking capabilities](BROWSER_PRIVACY.md)
- [Archived implementation and verification history](docs/PROJECT_HISTORY.md)
