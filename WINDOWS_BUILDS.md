# Windows builds from Ubuntu

Development can stay on Ubuntu. GitHub Actions builds Windows artifacts remotely;
download the installer from a completed run and test it on a Windows machine.
Repository: <https://github.com/HARON416/Isolate> (private).

## Application test installer

In GitHub, open **Actions → Windows installer → Run workflow**. No custom runner
or signing secrets are required. This manual workflow uses `windows-2025`, Go
from `go.mod`, Node 22, the pinned Wails CLI, and NSIS. It checks the frontend,
runs frontend and Go tests, then builds a per-user Windows x64 installer with
the WebView2 bootstrapper. Installing WebView2 may require an internet connection.

Download the `isolate-windows-amd64-<run number>` artifact and extract it.
Run `isolate-amd64-installer.exe`. `SHA256SUMS.txt` and `windows-build.json`
record the checksum, application revision, target, and build scope. Artifacts
expire after 14 days. These are unsigned test installers; Windows may show an
unknown-publisher prompt. Code signing remains release work.

If Actions artifact storage is full, the same files are attached to a draft
release named **Windows test installer <run number>**. Open the repository's
**Releases** page while signed in to download its assets. The draft is not
automatically published. This fallback requires the workflow's job-scoped
`contents: write` permission and does not change account billing settings.

Users need a supported installed browser (Chromium-based or Firefox) and the WebView2 runtime. They do
not need Chromium source, browser build tools, Go, Node, or Visual Studio.
Isolate uses the installed browser's updates and separate persistent profile
directories. No custom Chromium compilation or dedicated browser builder is
required for development or packaging.

## Testing on a Windows machine

Check install/launch, profile creation, renaming, storage isolation and
persistence, browser launch/focus/close, browser switching, proxy authentication,
Credential Manager, downloads, and uninstall. Use temporary test profiles. Check
language/timezone settings across tabs and verify saved sessions survive app and
browser restarts.

Successful CI compilation and automated tests do not establish desktop GUI
behavior or real Credential Manager operations on the user's machine.
