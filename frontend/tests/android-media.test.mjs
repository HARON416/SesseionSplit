import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';
import ts from 'typescript';

const source = await readFile(new URL('../src/androidMedia.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } });

function player() {
  const timers = new Map();
  const sockets = [];
  const media = [];
  const errors = [];
  let tick;
  let id = 0;
  class MediaSource extends EventTarget {
    static isTypeSupported() { return true; }
    readyState = 'open';
    removed = [];
    constructor() { super(); media.push(this); }
    addSourceBuffer() { return Object.assign(new EventTarget(), { buffered: { length: 0 }, appendBuffer() {} }); }
    removeSourceBuffer(buffer) { this.removed.push(buffer); }
  }
  class WebSocket {
    constructor() { sockets.push(this); }
    close() { this.closed = true; this.onclose?.(); }
  }
  const exports = {};
  vm.runInNewContext(outputText, { exports, window: { MediaSource }, MediaSource, WebSocket, EventTarget, Uint8Array,
    URL: { createObjectURL: () => 'blob:test', revokeObjectURL() {} },
    setInterval: callback => { tick = callback; return 0; }, clearInterval() {},
    setTimeout: (callback, delay) => { timers.set(++id, { callback, delay }); return id; },
    clearTimeout: id => timers.delete(id),
  });
  const video = Object.assign(new EventTarget(), { pause() {}, load() {}, removeAttribute() {} });
  const stop = exports.attachAndroidMedia(video, 'ws://test', error => errors.push(error));
  const open = () => media.at(-1).dispatchEvent(new Event('sourceopen'));
  const message = value => sockets.at(-1).onmessage({ data: JSON.stringify(value) });
  return { timers, sockets, media, errors, stop, open, message, video, tick: () => tick(), playAndroidScreen: exports.playAndroidScreen };
}

test('transient failures reconnect with bounded backoff and release old sockets', () => {
  const p = player();
  for (const delay of [500, 1000, 2000]) {
    p.open();
    p.message({ error: 'Android video packets: EOF' });
    assert.equal(p.errors.length, 0);
    const [id, timer] = [...p.timers][0];
    assert.equal(timer.delay, delay);
    p.timers.delete(id);
    timer.callback();
    assert.equal(p.sockets.at(-1).closed, true);
  }
  p.open();
  p.message({ error: 'encoder failed' });
  assert.deepEqual(p.errors, ['encoder failed']);
  p.stop();
  assert.equal(p.timers.size, 0);
});

test('unmount cancels a pending reconnect', () => {
  const p = player();
  p.open();
  p.message({ error: 'EOF' });
  p.stop();
  assert.equal(p.timers.size, 0);
  assert.equal(p.media.length, 1);
});

test('audio loss removes its buffer while video keeps its session', () => {
  const p = player();
  p.open();
  p.message({ track: 0, mime: 'video/mp4' });
  p.message({ track: 1, mime: 'audio/mp4' });
  p.message({ track: 1, disabled: true });
  assert.equal(p.media[0].removed.length, 1);
  assert.equal(p.sockets[0].closed, undefined);
  assert.equal(p.errors.length, 0);
  p.stop();
});

test('a pending play request still catches up to a later live buffer', () => {
  const p = player();
  Object.assign(p.video, { currentTime: 0, paused: true,
    buffered: { length: 1, start: () => 0, end: () => 0.5 },
    play: () => new Promise(() => {}),
  });
  p.tick(); // play() is pending while the decoder waits for playable data.
  p.video.buffered = { length: 1, start: () => 1.1, end: () => 3 };
  p.tick();
  assert.equal(p.video.currentTime, 2.75);
  p.stop();
});

test('manual playback distinguishes interruptions, autoplay restrictions, and decoder failures', async () => {
  const p = player();
  for (const name of ['AbortError', 'NotAllowedError', 'NotSupportedError']) {
    const blocked = [], errors = [];
    const video = { isConnected: true, getAttribute: () => 'blob:current',
      play: () => Promise.reject(new DOMException('playback rejected', name)) };
    await p.playAndroidScreen(video, value => blocked.push(value), error => errors.push(error));
    assert.deepEqual(blocked, name === 'NotAllowedError' ? [true] : []);
    assert.equal(errors.length, name === 'NotSupportedError' ? 1 : 0);
  }
  p.stop();
});

test('a superseded playback request cannot change the current player UI', async () => {
  const p = player();
  for (const outcome of ['resolve', 'reject', 'detach']) {
    const blocked = [], errors = [];
    let source = 'blob:old', resolve, reject;
    const video = { isConnected: true, getAttribute: () => source,
      play: () => new Promise((done, fail) => { resolve = done; reject = fail; }) };
    const pending = p.playAndroidScreen(video, value => blocked.push(value), error => errors.push(error));
    if (outcome === 'detach') video.isConnected = false;
    else source = 'blob:new';
    if (outcome === 'resolve') resolve();
    else reject(new DOMException('old source failed', 'NotSupportedError'));
    await pending;
    assert.deepEqual(blocked, []);
    assert.deepEqual(errors, []);
  }
  p.stop();
});
