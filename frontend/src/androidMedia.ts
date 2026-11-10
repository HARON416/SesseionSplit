// Recreate both MediaSource and the device session after a transient disconnect.
// Each URL owns its retry budget and timers; unmounting cancels all pending work.
export function attachAndroidMedia(video: HTMLVideoElement, url: string, report: (message: string) => void, onPlaybackBlocked: (blocked: boolean) => void = () => {}) {
  video.volume = 1;
  let stopped = false;
  let attempts = 0;
  let retry: ReturnType<typeof setTimeout> | undefined;
  let stable: ReturnType<typeof setTimeout> | undefined;
  let detach: (() => void) | undefined;
  const playing = () => {
    clearTimeout(stable);
    stable = setTimeout(() => { attempts = 0; }, 30000);
  };
  video.addEventListener('playing', playing);
  const connect = () => {
    if (stopped) return;
    onPlaybackBlocked(false);
    detach = attachAndroidMediaOnce(video, url, message => {
      clearTimeout(stable);
      if (attempts >= 3 || !('MediaSource' in window) || message.includes('does not support')) { report(message); return; }
      const delay = 500 * 2 ** attempts++;
      retry = setTimeout(() => { detach?.(); connect(); }, delay);
    }, onPlaybackBlocked);
  };
  connect();
  return () => {
    stopped = true;
    clearTimeout(retry);
    clearTimeout(stable);
    video.removeEventListener('playing', playing);
    detach?.();
  };
}

// scrcpy packets arrive as per-track fragmented MP4 over a capability websocket.
// Keep bounded queues and a small live buffer, including when the app is hidden.
function attachAndroidMediaOnce(video: HTMLVideoElement, url: string, report: (message: string) => void, onPlaybackBlocked: (blocked: boolean) => void = () => {}) {
  if (!('MediaSource' in window)) { report('This system WebView cannot play live Android video. Update WebView2 on Windows or WebKitGTK on Linux.'); return () => {}; }
  const media = new MediaSource();
  const objectURL = URL.createObjectURL(media);
  video.src = objectURL;
  let socket: WebSocket | undefined;
  let stopped = false;
  let failed = false;
  let audioUnavailable = false;
  let playbackBlocked = false;
  let playPending = false;
  const buffers = new Map<number, { mime: string; source?: SourceBuffer; queue: ArrayBuffer[]; bytes: number }>();
  const errorDetail = (error: unknown) => error instanceof Error ? `${error.name}: ${error.message}` : String(error);
  const fail = (message: string) => { if (stopped || failed) return; failed = true; report(message); socket?.close(); };
  const pump = (track: number) => {
    const state = buffers.get(track);
    const source = state?.source;
    if (!state || !source || source.updating || stopped || failed || media.readyState !== 'open') return;
    try {
      if (source.buffered.length && video.currentTime > 8 && source.buffered.start(0) < video.currentTime - 6) {
        source.remove(0, video.currentTime - 5); return;
      }
      const data = state.queue.shift();
      if (data) { state.bytes -= data.byteLength; source.appendBuffer(data); }
    } catch (error) { fail(`Android ${track === 0 ? 'video' : 'audio'} buffer failed (${errorDetail(error)}). Retry screen.`); }
  };
  const open = () => {
    if (stopped) return;
    socket = new WebSocket(url);
    socket.binaryType = 'arraybuffer';
    socket.onmessage = event => {
      if (stopped || failed) return;
      let operation = 'reading a packet';
      try {
        if (typeof event.data === 'string') {
          const info = JSON.parse(event.data) as { track: number; mime: string; disabled?: boolean; error?: string };
          if (info.error) { fail(info.error); return; }
          if (info.track === 1 && info.disabled === true && !audioUnavailable) {
            audioUnavailable = true;
            const audio = buffers.get(1);
            if (audio?.source && media.readyState === 'open') media.removeSourceBuffer(audio.source);
            buffers.delete(1);
          } else {
            if ((info.track !== 0 && info.track !== 1) || typeof info.mime !== 'string' || buffers.has(info.track) || (info.track === 1 && audioUnavailable)) { fail('Android media configuration is invalid or repeated. Retry screen.'); return; }
            if (!MediaSource.isTypeSupported(info.mime)) { fail(`Your system WebView does not support Android ${info.track === 0 ? 'video' : 'audio'} (${info.mime}). Update WebView2 or WebKitGTK.`); return; }
            buffers.set(info.track, { mime: info.mime, queue: [], bytes: 0 });
          }
          // WebKitGTK can reject a second track with QuotaExceededError after
          // parsing the first track's init segment. Register every SourceBuffer
          // before appending either track, regardless of encoder arrival order.
          // An explicit disabled-audio message also permits video-only devices.
          if (!buffers.has(0) || (!buffers.has(1) && !audioUnavailable)) return;
          for (const track of [0, 1]) {
            const state = buffers.get(track);
            if (!state || state.source) continue;
            operation = `adding ${track === 0 ? 'video' : 'audio'} buffer ${state.mime}`;
            const source = media.addSourceBuffer(state.mime);
            state.source = source;
            source.addEventListener('updateend', () => pump(track));
            source.addEventListener('error', () => fail(`Android ${track === 0 ? 'video' : 'audio'} decoding failed. Retry screen.`));
          }
          pump(0);
          pump(1);
        } else {
          const packet = new Uint8Array(event.data as ArrayBuffer);
          if (packet.length < 2) { fail('Android media packet was empty. Retry screen.'); return; }
          const state = buffers.get(packet[0]);
          if (!state) { fail('Android media configuration was lost. Retry screen.'); return; }
          const data = packet.slice(1).buffer;
          state.queue.push(data); state.bytes += data.byteLength;
          if (state.bytes > 12 * 1024 * 1024 || state.queue.length > 360) { fail('Android playback fell behind. Retry screen to reconnect live.'); return; }
          pump(packet[0]);
        }
      } catch (error) { fail(`Android playback failed while ${operation} (${errorDetail(error)}). Retry screen.`); }
    };
    socket.onerror = () => fail('Could not connect Android video. Retry screen.');
    socket.onclose = () => { if (!stopped && !failed) fail('Android video disconnected. Retry screen.'); };
  };
  const live = () => {
    if (!video.buffered.length || stopped || failed) return;
    const end = video.buffered.end(video.buffered.length - 1);
    if (end - video.currentTime > 1.2) video.currentTime = Math.max(video.buffered.start(video.buffered.length - 1), end - 0.25);
    // Recover small timestamp gaps after an encoder stalls, without accumulating
    // minutes of stale video when the window has been backgrounded.
    for (let i = 0; i < video.buffered.length; i++) {
      if (video.currentTime < video.buffered.start(i) && video.buffered.start(i) - video.currentTime < 1) { video.currentTime = video.buffered.start(i); break; }
    }
    // A pending play() can be waiting for time zero while the encoders' first
    // shared range starts later. Catch up before waiting for that promise.
    if (playPending || playbackBlocked) return;
    if (video.paused) {
      playPending = true;
      void video.play().catch(error => {
        if (stopped || failed) return;
        if (playbackErrorName(error) === 'NotAllowedError') {
          playbackBlocked = true;
          onPlaybackBlocked(true);
        } else if (playbackErrorName(error) !== 'AbortError') {
          fail(`Android playback could not start (${errorDetail(error)}). Retry screen.`);
        }
      }).finally(() => { playPending = false; });
    }
  };
  media.addEventListener('sourceopen', open, { once: true });
  const mediaError = () => fail('Your system could not decode Android media. Retry screen or update the system WebView.');
  video.addEventListener('error', mediaError, { once: true });
  const playing = () => { playbackBlocked = false; onPlaybackBlocked(false); };
  video.addEventListener('playing', playing);
  const timer = setInterval(live, 250);
  return () => {
    stopped = true; clearInterval(timer); socket?.close();
    video.removeEventListener('error', mediaError);
    video.removeEventListener('playing', playing);
    video.pause(); video.removeAttribute('src'); video.load(); URL.revokeObjectURL(objectURL);
    buffers.clear();
  };
}
function playbackErrorName(error: unknown) {
  return typeof error === 'object' && error !== null && 'name' in error ? String(error.name) : '';
}

// A reconnect/reset can abort play() after the caller has moved to a new source.
// Only the current, attached player may update the UI when its promise settles.
export function playAndroidScreen(video: HTMLVideoElement, onblocked: (blocked: boolean) => void, report: (message: string) => void) {
  const source = video.getAttribute('src');
  const current = () => video.isConnected && video.getAttribute('src') === source;
  return video.play().then(() => { if (current()) onblocked(false); }).catch(error => {
    if (!current() || playbackErrorName(error) === 'AbortError') return;
    if (playbackErrorName(error) === 'NotAllowedError') { onblocked(true); return; }
    const detail = error instanceof Error ? error.message : String(error);
    report(`Could not start Android playback: ${detail}`);
  });
}

