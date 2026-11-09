# Project history through 2026-11-08

Archived from the previous TODO.md during the roadmap refresh. This is a
historical record: running statuses, test counts, and next steps below may
have been superseded. Use [the current todo](../TODO.md) for active work.

# Browser privacy and profile networking

Implement in this order. Checked items require code and passing verification.

1. [x] Profile settings and networking
   - Persist direct/HTTP/HTTPS/SOCKS5 proxy settings without passwords in profile JSON.
   - Store proxy passwords in the operating-system credential store.
   - Route browser traffic through the selected profile proxy, including authentication.
   - Require closing/restarting a running profile before changing launch settings.
   - Expose settings and connection mode in the interface.
2. [x] Connection diagnostics
   - Test the configured route with a bounded request and report observed public IP.
   - Distinguish request routing from DNS/WebRTC leak verification.
   - Cover unavailable proxies, authentication failures, and cancellation.
3. [x] Browser-engine interface and environment settings
   - Separate engine launch/configuration from profile registry management.
   - Save supported language, timezone, browser-window dimensions, and permission settings per profile.
   - Expose engine availability/version and supported capabilities.
4. [x] Maintained fingerprint-protection integration
   - Integrate a maintained browser with native fingerprint protection.
   - Describe actual capabilities and limitations; never claim undetectability.
   - Verify supported settings across tabs and browser restarts.
5. [x] Verification and documentation
   - Test storage/network separation, secret handling, failed saves, and launch configuration.
   - Exercise settings and diagnostics in both UI themes and narrow layouts.
   - Document browser update responsibility and unverified network paths/platforms.

Initial baseline scope: preserve existing profile data and sessions, and use
maintained browser protections. The completed baseline above did not implement
a custom fingerprint engine. The next phase follows the user's custom Chromium
fork direction below.

## Verification record — 2026-11-07

- Frontend tests, Svelte/TypeScript checks, production build, and Impeccable scan passed.
- `go test -race ./...` passed; the native desktop application compiled successfully.
- Full Chrome + Brave + OS-keyring integration suite passed in 40.210 seconds.
- Chrome and official portable Brave 1.99.55 verified environment settings before
  scripts in multiple tabs and cross-origin iframes, including same-profile
  restarts, permissions, authenticated upstream routing, and localhost routing.
- UI tests cover secret-free registry/readback, failed saves, manual diagnostics,
  running-browser gates, focus, and both themes at 1200×800, 480×800, and 480×320.
- Proxy tests cover HTTP, CONNECT, SOCKS5 remote DNS/authentication, TLS rejection,
  WebSocket upgrades, cancellation, active-connection cleanup, and no direct fallback.
- Additional HTTP407/SOCKS authentication rejection tests passed with the race detector.
- The real desktop keyring saved, read, and removed a dummy credential successfully.

## Limits carried forward

Completed items describe the implemented settings, routing, engine integration,
and tests above. Independent DNS/STUN leak measurements, worker-wide environment
emulation, per-site Brave Shields verification, other-OS integration testing,
and native WebKit accessibility remain outside this verification. No custom
Chromium fork, distinct fabricated device identity, or undetectability guarantee
is provided. See BROWSER_PRIVACY.md for operational details and update responsibility.

Windows portability follow-up: explicit browser discovery, bundled IANA timezone
rules, and native version-resource reads are implemented. Windows amd64
cross-compilation and Linux regressions passed. Actual Windows runtime and
Credential Manager validation remain pending on a Windows machine.

## Next phase: custom Chromium fork

Implementation plan: [CHROMIUM_FORK_PLAN.md](CHROMIUM_FORK_PLAN.md).

- [x] Research native Linux/Windows build requirements and inspect host capacity.
- [x] Record build, configuration, signal coverage, integration, and release gates.
- [x] Select a durable build location and pin the stable upstream revision.
- [ ] Check out source and produce an unmodified Linux baseline.
- [ ] Set up a Windows builder and produce its unmodified baseline.
- [ ] Implement versioned native per-profile configuration and coherent UA/Client Hints/environment controls.
- [ ] Implement Canvas/OffscreenCanvas controls with worker coverage.
- [ ] Implement WebGL controls and rendering regressions.
- [ ] Implement a consistent font environment and audit adjacent signals.
- [ ] Integrate the actual custom engine with version/capability checks.
- [ ] Complete native browser tests, security-update workflow, and release packaging.

Windows CI preparation: manual application-installer and Chromium-baseline
workflows are implemented. The baseline workflow requires a provisioned Windows
runner and `WINDOWS_CHROMIUM_RUNNER`. It checks the toolchain, runs a Windows
headless smoke probe, and publishes an upstream baseline installer with hashes.
The application test installer currently uses installed Chrome/Brave; custom
engine bundling and client runtime validation remain open. See [WINDOWS_BUILDS.md](WINDOWS_BUILDS.md).

Windows application CI verification — 2026-11-08:

- [Run 42469074712](https://github.com/HARON416/SesseionSplit/actions/runs/42469074712)
  passed on Windows Server 2025: frontend checks/tests/build, Go tests including
  native file-version metadata, offline Chromium tooling tests, and NSIS packaging.
- The unsigned x64 application installer, checksum, and source metadata were
  uploaded to a private draft release because Actions artifact storage is full.
  Both Windows workflows now provide this release-asset fallback.
- This verifies current-app compilation and packaging. GUI/browser integration,
  real Credential Manager operations, the Windows Chromium baseline, and custom
  engine integration remain pending. No Windows cloud builder has been provisioned.

Native-configuration substep: application-side versioned 32-byte seed persistence
is implemented for new and existing profiles. Migration is atomic; identities
survive edits and restarts. This does not complete native configuration,
propagation, or any fingerprint-surface control.

These items remain open. Current Chrome/Brave launches do not implement custom
per-profile fingerprint spoofing.

### Baseline progress — 2026-11-08

- Selected `.tools/chromium/` on the persistent project disk for the upstream
  workspace, toolchain, caches, and logs; it is ignored by the application repo.
- Pinned Chromium Stable Linux `157.0.8103.38` at
  `a733b5afbb3ba618844ff8876b89ad1a774b2f18` and depot_tools at
  `289a37bda3d848cfe16e6c1c599741ed3890ab55` in `browser-engine/upstream.json`.
  Rechecked the Chromium pin against the official release service today.
- Full depot_tools bootstrap passed, including Python and all managed launcher
  packages. The managed Python environment initialized successfully. gsutil 5.35
  is now initialized sequentially before sync to avoid the first-install lock
  contention encountered during the first sync attempt; the updated tools stage
  passed and dependency sync was resumed.
  All 24 offline bootstrap and verification tests passed, covering the Linux
  dependency-check stage, managed launcher setup, and the new
  baseline runtime verifier's failure handling. The verifier has not yet run
  against a compiled baseline browser.
- Source fetch and checkout passed at the pinned revision. Dependency sync is
  running through the continuation pipeline. Hooks, compilation, and runtime
  verification remain pending. The combined Linux-baseline checkbox remains open.
- Required Ubuntu packages were installed manually. The pinned Linux dependency
  checker now passes (`--quick-check --no-arm --no-chromeos-fonts`).
- The maintained `browser-engine/baseline_pipeline.py` replaces the one-time
  local continuation. It runs dependency check, sync, hooks, configure, build
  (`--jobs 2`), and verify sequentially, stopping on any failure. State is in
  `.tools/chromium/baseline-pipeline.json`, with separate per-stage logs in
  `.tools/chromium/logs/`. Workspace locks prevent overlapping stages and
  duplicate pipelines even when sandbox process visibility differs. Sync was
  resumed through this command; queued work is not completed work.
- This session has an Ubuntu 26.04 host. The user confirmed no Windows host is
  available and directed continuing Linux work. The Windows baseline and runtime
  verification remain pending; Linux implementation can proceed independently.
- A native configuration parser and standalone native unit-test target are
  drafted separately in `browser-engine/native-draft/`, with a generated patch
  in `browser-engine/patches/`. Patch applicability passed against the pinned
  revision. The patch is not applied; native compilation/tests and browser
  integration remain pending. This does not complete the configuration item.
- Build commands and resource limits: [browser-engine/README.md](browser-engine/README.md).

### Recovery and preparation verification — 2026-11-08

- 33 offline Python tests pass, including cross-process exclusion, stale lock
  recovery, pipeline failure/interruption, and lock-based activity reporting.
- Go race tests and frontend type checks pass. New Go tests cover unique seed
  generation, legacy migration, restart/edit persistence, invalid identity data,
  and failed atomic migration without publishing unsaved seeds.
- The source remains unpatched while the baseline pipeline is running. No new
  browser fingerprint-control capability has been verified or enabled.

### Dependency recovery follow-up — 2026-11-08

- Sync and hooks exited successfully, but GN configuration failed because the
  Perfetto checkout was empty. An audit of 168 recorded Git dependencies found
  three unborn repositories: Perfetto, PipeWire, and Skia. The other recorded
  dependency HEADs matched their pins.
- Bootstrap now rejects missing or mismatched dependency HEADs before hooks,
  configuration, builds, verification, and Windows packaging. All 47 offline
  tooling tests pass, including the new dependency guard tests.
- A bounded repair is fetching only those three pinned revisions into the empty
  repositories, preserving existing checkouts. It then resumes sync, hooks,
  configuration, build, and verification. The Linux baseline remains unverified.
