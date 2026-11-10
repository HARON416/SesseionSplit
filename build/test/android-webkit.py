"""Exercise the Android media player in the Linux desktop's WebKitGTK engine."""
import json
import sys
import time
import gi

gi.require_version('Gtk', '4.0')
gi.require_version('WebKit', '6.0')
from gi.repository import GLib, Gtk, WebKit

Gtk.init()
loop = GLib.MainLoop()
view = WebKit.WebView(website_policies=WebKit.WebsitePolicies(autoplay=WebKit.AutoplayPolicy.ALLOW))
view.get_settings().set_media_playback_requires_user_gesture(False)
window = Gtk.Window()
window.set_default_size(800, 1000)
window.set_child(view)
window.present()
exit_code = 1
last = {}
measure_seconds = float(sys.argv[2]) if len(sys.argv) > 2 else 0
playback_started = None
last_progress = None
first_frames = 0
previous_frames = 0
previous_time = 0


def finish(message, code):
    global exit_code
    print(message, flush=True)
    exit_code = code
    loop.quit()


def result_ready(webview, result, _):
    global last, playback_started, last_progress, first_frames, previous_frames, previous_time
    try:
        value = webview.evaluate_javascript_finish(result)
        last = json.loads(value.to_string())
        if last.get('error'):
            finish('WebKitGTK playback failed: ' + json.dumps(last), 1)
        elif last.get('width', 0) > 0 and last.get('time', 0) > 0.4 and last.get('frames', 0) > 10:
            now = time.monotonic()
            if playback_started is None:
                playback_started = last_progress = now
                first_frames = previous_frames = last['frames']
                previous_time = last['time']
            # WebKit may stop presenting frames for an occluded Xvfb window,
            # while continuing to play media. Check its timeline in that case.
            if last['frames'] > previous_frames or (last.get('visibility') == 'hidden' and last['time'] > previous_time):
                last_progress = now
                previous_frames = last['frames']
                previous_time = last['time']
            if now - last_progress > 3:
                finish('WebKitGTK video stalled for over 3 seconds: ' + json.dumps(last), 1)
            elif now - playback_started >= measure_seconds:
                elapsed = now - playback_started
                if measure_seconds:
                    last['observedSeconds'] = round(elapsed, 2)
                    last['presentedFramesPerSecond'] = round((last['frames'] - first_frames) / elapsed, 2)
                    last['renderMeasurementLimited'] = last.get('visibility') != 'visible'
                finish('PASS: WebKitGTK decoded Android media: ' + json.dumps(last), 0)
    except Exception as error:
        finish('WebKitGTK inspection failed: ' + str(error), 1)


def inspect():
    view.evaluate_javascript("""(() => {
      const player = document.querySelector('video');
      // evaluate_javascript runs with a user gesture in WebKitGTK. This models
      // selecting Play screen when this virtual display blocks autoplay.
      if (player?.paused && player.readyState >= 2) void player.play().catch(error => window.playPermissionError = error.name + ': ' + error.message);
      return JSON.stringify({
      error: window.playerError || '',
      width: document.querySelector('video')?.videoWidth || 0,
      time: document.querySelector('video')?.currentTime || 0,
      frames: document.querySelector('video')?.getVideoPlaybackQuality().totalVideoFrames || 0,
      droppedFrames: document.querySelector('video')?.getVideoPlaybackQuality().droppedVideoFrames || 0,
      mediaError: document.querySelector('video')?.error?.message || '',
      playPermissionError: window.playPermissionError || '',
      paused: document.querySelector('video')?.paused,
      muted: document.querySelector('video')?.muted,
      visibility: document.visibilityState,
      ready: document.querySelector('video')?.readyState,
      ranges: Array.from({length: document.querySelector('video')?.buffered.length || 0}, (_,i) => [document.querySelector('video').buffered.start(i), document.querySelector('video').buffered.end(i)])
    }); })()""", -1, None, None, None, result_ready, None)
    return GLib.SOURCE_CONTINUE


def timeout():
    finish('WebKitGTK playback timed out: ' + json.dumps(last), 1)
    return GLib.SOURCE_REMOVE


view.load_uri(sys.argv[1])
GLib.timeout_add(150, inspect)
GLib.timeout_add_seconds(int(12 + measure_seconds), timeout)
loop.run()
window.destroy()
sys.exit(exit_code)
