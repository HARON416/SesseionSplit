# Isolate

Isolate is a desktop application using Wails v3, Svelte, and Vite.

The confirmed initial scope is an empty workspace with a custom header like Postika: an **Add new profile** button on the left, window controls on the right, and the same Inter font as Postika.

Users can create named browser profiles. Newly created cards appear first and
remain across app restarts. Each card shows its profile name, browser name, and
an Open action. Chrome/Chromium profiles use isolated persistent storage and are
launched through Rod, following Postika's launch pattern. Profiles must be
independent: separate processes, data directories, sessions, site storage,
caches, and downloads. Chrome Sync is disabled.

Broader application behavior is undecided.

Profiles can be deleted from their cards after confirmation. Deletion closes the
selected browser and permanently removes that profile's data, cache, and
downloads without affecting other profiles.

Cards track the last successful Open action (launching or focusing the profile's
browser), retain it across restarts, and display its local date and time.

Users can rename a profile through Edit on its card. Renaming changes only the
name and preserves its browser data, active browser, dates, identity, and card
position. Names remain unique without regard to case. The creation dialog and
first-profile guidance explain what stays separate between profiles.

Each profile now has Settings for direct/HTTP/HTTPS/SOCKS5 connections, OS-keyring
proxy passwords, language, timezone, browser-window dimensions, and camera,
microphone, and location permission policy. Changes require closing the browser
and apply at its next launch. A manual saved-route diagnostic reports the public
IP observed by api.ipify.org; DNS/WebRTC leak isolation is explicitly unverified.

Installed Brave is an additional browser engine using its native privacy
protections, with separate engine data directories and reported capabilities.
No per-site Shields verification or undetectability guarantee is claimed.
