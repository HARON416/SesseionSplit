---
name: Isolate
description: Postika-derived shell with curated Coolors colors
colors:
  surface: "#ffffff"
  workspace: "#f8f9fa"
  surface-hover: "#e9ecef"
  surface-active: "#e4edf5"
  border: "#ced4da"
  text: "#212529"
  text-secondary: "#495057"
  accent: "#1c5d99"
  focus: "#1c5d99"
  danger: "#b33533"
  danger-bg: "#fff1f0"
  danger-dark: "#ffb4ab"
  danger-bg-dark: "#4b2426"
  danger-solid: "#c94442"
  on-danger: "#ffffff"
typography:
  label:
    fontFamily: '"InterVariable", system-ui, sans-serif'
    fontSize: "13px"
    fontWeight: 550
  notice:
    fontFamily: '"InterVariable", system-ui, sans-serif'
    fontSize: "13px"
    lineHeight: 1.5
rounded:
  dismiss: "0px"
  button: "0px"
  notice: "0px"
components:
  add-profile:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.text}"
    typography: "{typography.label}"
    rounded: "{rounded.button}"
    padding: "0 12px"
  add-profile-hover:
    backgroundColor: "{colors.surface-hover}"
  add-profile-active:
    backgroundColor: "{colors.surface-active}"
  window-control:
    textColor: "{colors.text-secondary}"
    width: "44px"
    padding: "0"
  window-control-close-hover:
    backgroundColor: "{colors.danger-solid}"
    textColor: "{colors.on-danger}"
---

# Design System: Isolate

## Overview

The window layout and Inter font follow Postika. Colors were selected after reviewing four Coolors popular/black pages (192 palettes): Light Steel supplies neutral surfaces and text; Midnight Sky supplies blue action colors. Light mode uses a white titlebar, pale-gray workspace, and deep-blue actions. Dark mode uses a charcoal workspace, lighter slate surfaces, and muted-blue actions.

## Colors

Source palettes: [Light Steel](https://coolors.co/f8f9fa-e9ecef-dee2e6-ced4da-adb5bd-6c757d-495057-343a40-212529), page 1; [Midnight Sky](https://coolors.co/222222-ffffff-1c5d99-639fab-bbcde5), page 4. The full review record is `frontend/theme-sources/coolors-review.json`; it includes all 192 reviewed swatches and identifies derived interaction colors. The existing red remains reserved for window-close hover.

Dark roles: workspace #212529, surface #343a40, hover #495057, selection #354d5a, text #f8f9fa, secondary text #ced4da, action #639fab, action-hover/focus #bbcde5, action foreground #212529. Inputs use #6c757d borders in light mode and #adb5bd in dark mode.

The accent supplies the outer frame and keyboard focus. Neutral surfaces distinguish the titlebar from the workspace; subtle green-tinted surfaces mark button hover and press. Primary text labels actions, while secondary text colors window icons. The danger surface and its foreground appear on close-button hover.

## Typography

Self-hosted Inter Variable uses the same normal and italic font files as Postika, with weights from 100–900 available. The implemented profile-action label uses the label token. Status notices use the notice token. The root font size is 62.5%, making one rem equal to 10px at the default browser size. Profile names display in uppercase: card titles use 18px semibold type with a 1.4 line height and 0.02em letter spacing, the name field uses 13px at weight 550, and delete confirmations use 13px at weight 600. Uppercase is a presentation rule that also covers existing profiles; saved names retain their original spelling. Long names wrap within the card. Browser badges use 11px type; date labels use 12px and date/time values use 13px type.

## Layout

Scrollable surfaces use Postika-style 8px custom scrollbars with transparent tracks and square thumbs. Thumb colors are #6c757d in light mode and #adb5bd in dark mode, switching to secondary text color on hover. Engines without WebKit scrollbar selectors use the standard thin scrollbar with the same theme colors.

The shell fills the window inside a 2px accent border. Its titlebar has a fixed 48px flex basis and a 1px bottom divider. The profile-action group has 10px horizontal padding. Flexible draggable space separates that group from three right-aligned, 44px-wide window controls. The action button has a minimum height of 32px, an 8px icon gap, and a 16px icon. Window-control icons are 14px.

The native window starts at 1200 × 800px with a 480 × 320px minimum. The titlebar retains the same composition at the minimum width; there are no responsive breakpoints. The workspace takes the remaining height and offers brief first-profile guidance until a profile is created. It has 24px padding and a responsive card grid with a 400px minimum column size and 16px gaps.

## Elevation & Depth

Profile cards use [CSS Scan example #9 (Sketch)](https://getcssscan.com/css-box-shadow-examples): `0 4px 12px rgba(0, 0, 0, 0.1)`. Its compact, soft shadow separates square cards from the canvas. Dark mode keeps the same offset and blur with opacity 0.32. Both are defined by `--card-shadow`; cards retain square corners and have no outer border. Other surfaces keep their existing borders and flat treatment.

## Shapes

All interface elements have square corners: cards, buttons, inputs, dialogs, badges, notices, and icon containers use zero border radius. Icons are SVG paths; window-control shapes come from Postika.

## Components

The profile action is a bordered white button with tinted hover and pressed surfaces. Its keyboard focus uses a 2px accent outline with a 2px outward offset. Activation opens a native modal dialog with a labeled profile-name input, a short explanation of profile isolation, and Cancel and Create actions. Ctrl+N or Command+N also opens creation when no dialog is active; the button tooltip and aria-keyshortcuts expose this accelerator. Create requires a nonblank, unique name and shows a busy state while saving. The dialog uses a 420px maximum width, 24px padding, zero border radius, and a dimmed backdrop. Escape and Cancel dismiss it, returning focus to the trigger.

Window controls occupy the full titlebar height. Hover changes the surface and foreground; close hover uses the danger colors. Keyboard focus uses a 2px outline inset by 3px. Controls are disabled at half opacity during a runtime action. The maximize icon changes to restore when maximized. Native runtime calls handle minimize, maximize/restore, and close; the central caption supports dragging and double-click maximize.

The theme toggle sits immediately before the native window controls, using the supplied 18px sun/moon SVGs inside a 36 × 32px button. It shows the destination mode, follows the system on first launch, and saves an explicit choice in local storage. Both themes cover the shell, dialog, inputs, notices, focus states, and actions.

Profile cards follow the supplied reference: borderless surface backgrounds, square corners, 20px padding, and a 180px minimum height. The Chrome icon sits beside the profile name and browser badge. Cards size independently so a long name does not stretch neighboring cards. The footer places metadata beside the actions where space allows and wraps the actions below on narrower cards. A square container holds the user-supplied 32px Google Chrome SVG, using the current text color in each theme. The uppercase 18px semibold profile name has a single Google Chrome browser badge beneath it. A divider separates the footer, with Created and Last opened metadata in two columns, followed by text-only Open, Edit, Settings, and Delete buttons in that visual and keyboard order at the bottom right. The Open button uses the theme's text/surface colors for a high-contrast neutral treatment and shows Opening while launching. Edit and Delete use secondary outlined styling. Edit opens the shared name dialog with the current name selected, a name-only explanation, and Cancel and Save changes actions. Blank and unchanged names disable Save. Duplicate names show an inline error; Saving disables the form. Successful edits preserve profile identity, stored browser data, timestamps, and position, then return focus to Edit. Cancel and Escape discard edits and return focus to Edit. The Chrome icon is decorative to avoid repeating the browser badge for screen readers. Delete sits last at the bottom of the card. It opens a confirmation naming the profile and explaining that browser data and downloaded files will be removed. Cancel receives initial focus; the destructive confirmation uses the danger color and shows Deleting while busy. Errors stay in the confirmation for retry. Successful deletion moves focus to another card or Add new profile when none remain.

Last-open timestamps are recorded only after a successful browser launch or focus and saved in UTC. Last opened displays Now below 5 seconds, seconds below a minute, minutes below an hour, hours below 24 hours, days below 7 days, and weeks below 28 days; older or future timestamps display the local date and time on separate lines. Created displays Today or Yesterday based on local calendar dates, otherwise the full local date, with time beneath. Valid timestamps expose the exact local date, seconds, and timezone through native hover text and accessible labels. A shared clock refreshes visible labels every second and on window focus or visibility changes without live-region announcements. Missing or invalid activity timestamps show Not recorded.

Creation timestamps are saved in UTC and displayed with local calendar labels and times on separate lines; legacy profiles without timestamps show Creation date unavailable. New cards prepend to the grid; creation moves keyboard focus to the new card's Open button. Duplicate-name errors stay in the modal; launch errors use the dismissible notice.

The status notice is fixed 20px from the bottom and sides, capped at 400px wide, with 12px vertical and 16px horizontal padding. It uses a surface background, border, and 28px dismiss button. It also reports window-action errors. Expected profile failures map to actionable messages; technical errors are logged separately and do not appear in notices or inline errors.

## Do's and Don'ts

- Do preserve Postika's Inter font files and window-control styling, with the curated Coolors color roles when maintaining this shell.
- Do keep the profile action left and native window controls right.
- Don't infer future profile behavior or workspace content from this initial shell.

## Profile settings and diagnostics

Cards show the selected browser and a plain Direct connection or proxy-type
label. Open stays primary; Edit, Settings, and Delete are secondary, in the same
visual and keyboard order. Brave uses a neutral browser-window icon rather than
the Chrome brand mark.

Settings opens a square, 620px maximum-width dialog with a fixed heading and
Cancel/Save footer around a scrolling content area. Connection and browser
settings use labeled native inputs/selects; paired fields stack on narrow windows.
The active profile name remains uppercase. A running-browser notice offers Close
browser before saving changes, preserving sessions/data. Password entry never
shows the stored password, defaults to keeping it, and offers explicit removal.

Browser settings disclose separate sessions when switching engines and the
scope of native fingerprint protection. Availability/version and expandable
capability notes prevent implying unsupported protection. Diagnostics are manual,
use saved settings only, disclose the public IP-service request, and label
DNS/WebRTC leaks unverified. Error messages are sanitized, entered values remain
available for retry, and closing clears password input and cancels diagnostics.
