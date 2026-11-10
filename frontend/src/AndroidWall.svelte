<script lang="ts">
  import { onMount } from 'svelte';
  import { Clipboard } from '@wailsio/runtime';
  import AndroidSetup from './AndroidSetup.svelte';
  import AndroidVideo from './AndroidVideo.svelte';
  import { playAndroidScreen } from './androidMedia';
  import { CreateAndroidDevice, GetAndroidWorkspace, SendAndroidInput, StartAndroidEmulator, StartAndroidScreen, StopAndroidScreen } from '../bindings/isolate/androidservice';
  import type { AndroidEmulator, AndroidInput, AndroidWorkspaceState } from '../bindings/isolate/models';

  let workspaceState = $state<AndroidWorkspaceState | null>(null);
  let loading = $state(true);
  let message = $state('');
  let busy = $state<string[]>([]);
  let sources = $state<Record<string, string>>({});
  let streamErrors = $state<Record<string, string>>({});
  let playbackBlocked = $state<Record<string, boolean>>({});
  let selected = $state('');
  let screenSize = $state<number | null>(null);
  let allowMore = $state(false);
  let showSetup = $state(false);
  let adding = $state(false);
  let refreshing = false;
  let destroyed = false;
  const streams = new Set<string>();
  const sourceNames = new Map<string, string>();
  const queues = new Map<string, Promise<void>>();
  const pending = new Map<string, number>();
  const wheelScrolls = new Map<string, { name: string; x: number; y: number; dx: number; dy: number; timer: ReturnType<typeof setTimeout> }>();
  let pointer: { serial: string; x: number; y: number; time: number } | null = null;
  const running = $derived(workspaceState?.emulators?.filter(device => device.state !== 'stopped').length || 0);
  const effectiveScreenSize = $derived(screenSize ?? (workspaceState?.emulators?.length === 1 ? 320 : 180));

  function errorMessage(error: unknown) {
    return error instanceof Error ? error.message : 'Android operation failed. Check Android setup and emulator readiness, then try again.';
  }

  async function refresh() {
    if (refreshing || destroyed) return;
    refreshing = true;
    try {
      const next = await GetAndroidWorkspace();
      if (!destroyed) {
        workspaceState = next;
        const active = new Set((next.emulators ?? []).filter(device => device.state === 'running').map(device => device.serial));
        for (const serial of streams) {
          if (!active.has(serial) || next.emulators?.find(device => device.serial === serial)?.name !== sourceNames.get(serial)) {
            streams.delete(serial);
            const updated = { ...sources }; delete updated[serial]; sources = updated;
            sourceNames.delete(serial);
            if (selected === serial) selected = '';
            void StopAndroidScreen(serial).catch(() => {});
          }
        }
      }
    } catch (error) { if (!destroyed) message = errorMessage(error); }
    finally { refreshing = false; loading = false; }
  }

  onMount(() => {
    void refresh();
    const timer = setInterval(() => { if (!document.hidden) void refresh(); }, 5000);
    return () => {
      destroyed = true;
      clearInterval(timer);
      for (const scroll of wheelScrolls.values()) clearTimeout(scroll.timer);
      wheelScrolls.clear();
      for (const serial of streams) void StopAndroidScreen(serial).catch(() => {});
    };
  });

  async function action(device: AndroidEmulator, operation: () => Promise<unknown>) {
    if (busy.includes(device.name)) return;
    busy = [...busy, device.name];
    message = '';
    try { await operation(); await refresh(); }
    catch (error) { message = errorMessage(error); }
    finally { busy = busy.filter(name => name !== device.name); }
  }

  async function connect(device: AndroidEmulator) {
    const url = await StartAndroidScreen(device.serial);
    if (destroyed) { await StopAndroidScreen(device.serial); return; }
    streams.add(device.serial);
    sourceNames.set(device.serial, device.name);
    sources = { ...sources, [device.serial]: url };
    playbackBlocked = { ...playbackBlocked, [device.serial]: false };
    streamErrors = { ...streamErrors, [device.serial]: '' };
    selected = device.serial;
  }

  function send(serial: string, input: Partial<AndroidInput>) {
    if (!serial || destroyed) return;
    if ((pending.get(serial) || 0) >= 32) { message = 'Android input is busy. Wait before continuing.'; return; }
    const payload: AndroidInput = { avdName: sourceNames.get(serial) || workspaceState?.emulators?.find(device => device.serial === serial)?.name || '', kind: '', x: 0, y: 0, endX: 0, endY: 0, duration: 200, key: 0, text: '', ...input };
    pending.set(serial, (pending.get(serial) || 0) + 1);
    const next = (queues.get(serial) || Promise.resolve()).then(async () => {
      if (!destroyed) await SendAndroidInput(serial, payload);
    }).catch(error => { if (!destroyed) message = errorMessage(error); }).finally(() => { pending.set(serial, (pending.get(serial) || 1) - 1); });
    queues.set(serial, next);
  }

  function position(event: PointerEvent) {
    const bounds = (event.currentTarget as HTMLElement).getBoundingClientRect();
    return { x: Math.max(0, Math.min(1, (event.clientX - bounds.left) / bounds.width)), y: Math.max(0, Math.min(1, (event.clientY - bounds.top) / bounds.height)) };
  }

  function down(event: PointerEvent, device: AndroidEmulator) {
    if (event.button !== 0) return;
    event.preventDefault();
    const target = event.currentTarget as HTMLButtonElement;
    target.focus(); target.setPointerCapture(event.pointerId);
    selected = device.serial;
    pointer = { serial: device.serial, ...position(event), time: performance.now() };
  }

  function up(event: PointerEvent, device: AndroidEmulator) {
    if (!pointer || pointer.serial !== device.serial) return;
    const start = pointer; pointer = null;
    const end = position(event);
    const swipe = Math.hypot(end.x - start.x, end.y - start.y) > 0.025;
    send(device.serial, { kind: swipe ? 'swipe' : 'tap', x: start.x, y: start.y, endX: end.x, endY: end.y, duration: Math.max(50, Math.min(1500, Math.round(performance.now() - start.time))) });
  }

  function flushWheel(serial: string) {
    const scroll = wheelScrolls.get(serial);
    if (!scroll) return;
    if (destroyed || sourceNames.get(serial) !== scroll.name || !sources[serial]) {
      wheelScrolls.delete(serial);
      return;
    }
    // Coalesce trackpad events and wait for the previous gesture to finish.
    if ((pending.get(serial) || 0) > 0 || pointer) {
      scroll.timer = setTimeout(() => flushWheel(serial), 80);
      return;
    }
    wheelScrolls.delete(serial);
    const horizontal = Math.abs(scroll.dx) > Math.abs(scroll.dy);
    const distance = horizontal ? scroll.dx : scroll.dy;
    if (Math.abs(distance) < 0.002) return;
    const x = Math.max(0.12, Math.min(0.88, scroll.x));
    const y = Math.max(0.12, Math.min(0.88, scroll.y));
    const clamp = (value: number) => Math.max(0.08, Math.min(0.92, value));
    send(serial, { kind: 'swipe', x, y, endX: horizontal ? clamp(x - distance) : x, endY: horizontal ? y : clamp(y - distance), duration: 180 });
  }

  function wheel(event: WheelEvent, device: AndroidEmulator) {
    if (event.ctrlKey || !device.serial || destroyed) return;
    event.preventDefault();
    event.stopPropagation();
    const bounds = (event.currentTarget as HTMLElement).getBoundingClientRect();
    if (!bounds.width || !bounds.height) return;
    const scaleX = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? bounds.width : 1;
    const scaleY = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? bounds.height : 1;
    let scroll = wheelScrolls.get(device.serial);
    if (!scroll) {
      scroll = { name: device.name, x: 0.5, y: 0.5, dx: 0, dy: 0, timer: setTimeout(() => flushWheel(device.serial), 80) };
      wheelScrolls.set(device.serial, scroll);
    }
    scroll.x = (event.clientX - bounds.left) / bounds.width;
    scroll.y = (event.clientY - bounds.top) / bounds.height;
    scroll.dx = Math.max(-0.55, Math.min(0.55, scroll.dx + event.deltaX * scaleX / bounds.width));
    scroll.dy = Math.max(-0.55, Math.min(0.55, scroll.dy + event.deltaY * scaleY / bounds.height));
  }

  function pasteText(value: string, device: AndroidEmulator) {
    const text = value.split(/\r\n?|\n/).map(line => line.replace(/\t/g, ' ').trim()).filter(Boolean).join(' ');
    if (!text) return;
    message = '';
    send(device.serial, { kind: 'text', text });
  }

  async function pasteClipboard(device: AndroidEmulator) {
    try {
      const value = await Clipboard.Text();
      if (!destroyed && sourceNames.get(device.serial) === device.name) pasteText(value, device);
    } catch {
      if (!destroyed) message = 'Could not read the clipboard. Copy the text again and retry pasting.';
    }
  }

  function paste(event: ClipboardEvent, device: AndroidEmulator) {
    event.preventDefault();
    event.stopPropagation();
    if (event.clipboardData) pasteText(event.clipboardData.getData('text/plain'), device);
    else void pasteClipboard(device);
  }

  function keydown(event: KeyboardEvent, device: AndroidEmulator) {
    if (event.key === 'Tab' || event.isComposing) return;
    if (((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'v') || (event.shiftKey && event.key === 'Insert')) {
      event.preventDefault(); event.stopPropagation();
      if (!event.repeat) void pasteClipboard(device);
      return;
    }
    event.preventDefault(); event.stopPropagation();
    const keys: Record<string, number> = { Backspace: 67, Enter: 66, Escape: 4 };
    if (keys[event.key]) send(device.serial, { kind: 'key', key: keys[event.key] });
    else if (event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey) send(device.serial, { kind: 'text', text: event.key });
  }

</script>

<section class="android-wall" aria-labelledby="android-title">
  <div class="wall-heading">
    <div><h1 id="android-title">Android screens</h1></div>
    <label class="zoom">Screen size <input aria-label="Android screen size" type="range" min="100" max="420" step="10" value={effectiveScreenSize} oninput={(event) => screenSize = event.currentTarget.valueAsNumber} /></label>
    <button type="button" disabled={adding || running >= 10} onclick={async () => { adding = true; message = ''; try { await CreateAndroidDevice(); await refresh(); } catch (error) { message = errorMessage(error); showSetup = true; } finally { adding = false; } }}>{adding ? 'Creating phone…' : 'Add virtual phone'}</button>
    <button type="button" onclick={() => showSetup = !showSetup}>{showSetup ? 'Hide setup' : 'Android setup'}</button>
    <button type="button" onclick={() => { message = ''; void refresh(); }}>Refresh devices</button>
  </div>
  {#if running >= 2 && !allowMore}<label class="load-confirmation"><input type="checkbox" bind:checked={allowMore} />I understand that starting more emulators can exhaust RAM and slow or freeze this computer.</label>{/if}
  {#if message}<p class="error" role="alert">{message}</p>{/if}
  {#if loading}<p role="status">Checking Android tools and virtual devices…</p>
  {:else}
    {#if showSetup || !workspaceState?.adb || !workspaceState?.emulator || !workspaceState?.video || !workspaceState?.emulators?.length}
      <AndroidSetup onready={(success) => { showSetup = !success; void refresh(); }} />
    {/if}
    {#if workspaceState?.emulators?.length}
    <div class="android-grid" style={`--screen-size:${effectiveScreenSize}px`}>
      {#each workspaceState.emulators.slice(0, 10) as device (device.name)}
        <section class="device" style={`--phone-ratio:${device.width / device.height};--display-aspect:${device.width}/${device.height}`} class:selected={selected === device.serial && Boolean(device.serial)} aria-label={`Android emulator ${device.name}`}>
          <div class="phone-shell">
          {#if sources[device.serial]}
            <button class="device-screen" type="button" style={`aspect-ratio:${device.width}/${device.height}`} aria-label={`Control Android emulator ${device.name}`} onwheel={(event) => wheel(event, device)} onpointerdown={(event) => down(event, device)} onpointerup={(event) => up(event, device)} onpointercancel={() => pointer = null} onkeydown={(event) => keydown(event, device)} onpaste={(event) => paste(event, device)}>
              <AndroidVideo url={sources[device.serial]} name={device.name} muted={selected !== device.serial} onerror={(error) => { message = error; streamErrors = { ...streamErrors, [device.serial]: error }; }} onblocked={(blocked) => playbackBlocked = { ...playbackBlocked, [device.serial]: blocked }} />
            </button>
          {:else}
            <div class="screen-placeholder">
              <p>{device.state === 'starting' ? 'Android is starting. Wait for it to finish booting, then connect its screen.' : device.state === 'running' ? 'Connect this screen to see the actual Android device.' : 'Emulator stopped. Its installed apps and saved data remain on disk.'}</p>
              {#if device.state === 'running'}
                <button type="button" disabled={busy.includes(device.name)} onclick={() => action(device, () => connect(device))}>Connect screen</button>
              {:else if device.state === 'stopped'}
                <button type="button" disabled={busy.includes(device.name) || running >= 10 || (running >= 2 && !allowMore)} onclick={() => action(device, () => StartAndroidEmulator(device.name))}>Start emulator</button>
              {/if}
            </div>
          {/if}
          {#if sources[device.serial] && streamErrors[device.serial]}
            <div class="playback-prompt"><button type="button" disabled={busy.includes(device.name)} onclick={() => action(device, async () => { await StopAndroidScreen(device.serial); await connect(device); })}>Retry screen</button></div>
          {:else if sources[device.serial] && playbackBlocked[device.serial]}
            <div class="playback-prompt"><button type="button" onclick={(event) => { const player = event.currentTarget.closest('.device')?.querySelector('video'); if (player) void playAndroidScreen(player, blocked => playbackBlocked = { ...playbackBlocked, [device.serial]: blocked }, error => message = error); }}>Play screen</button></div>
          {/if}
          </div>
          {#if device.streamError}
            <details class="error"><summary>Device error details</summary><pre>{device.streamError}</pre></details>
          {/if}
        </section>
      {/each}
    </div>
    {/if}
  {/if}
</section>

<style>
  .android-wall { width: 100%; }
  .wall-heading { display: flex; flex-wrap: wrap; gap: 12px; align-items: center; margin-bottom: 16px; }
  .wall-heading > div { flex: 1; min-width: 180px; }
  h1 { font-size: 20px; margin: 0 0 5px; }
  p { color: var(--text-secondary); font-size: 12px; line-height: 1.65; }
  button { min-height: 30px; padding: 6px 9px; border: 1px solid var(--border); background: var(--surface); color: var(--text); font: inherit; font-size: 11px; cursor: pointer; }
  button:hover:enabled { background: var(--surface-hover); }
  button:disabled { opacity: .5; cursor: default; }
  .zoom { font-size: 12px; display: flex; align-items: center; gap: 8px; }
  .load-confirmation { display: block; padding: 10px 12px; background: var(--surface-hover); border-left: 3px solid var(--accent); font-size: 12px; line-height: 1.6; }
  .load-confirmation { margin: 10px 0; color: var(--text); }

  .android-grid { display: grid; grid-template-columns: repeat(auto-fill, min(var(--screen-size), 100%)); gap: 24px 18px; align-items: start; }
  .device { min-width: 0; }
  .phone-shell { --frame-radius: clamp(20px, calc(var(--screen-size) * .105), 36px); position: relative; width: 100%; padding: 7px; border: 1px solid #666a70; border-radius: var(--frame-radius); background: #090a0c; box-shadow: inset 0 0 0 2px #27292d, inset 0 0 0 3px #111214, 0 4px 10px #0003; }
  .phone-shell::before, .phone-shell::after { content: ''; position: absolute; width: 3px; border: 1px solid #555960; background: #27292d; border-radius: 2px; pointer-events: none; }
  .phone-shell::before { left: -3px; top: 17%; height: 12%; }
  .phone-shell::after { right: -3px; top: 24%; height: 8%; }
  .device.selected .phone-shell { border-color: var(--accent); box-shadow: inset 0 0 0 2px #27292d, inset 0 0 0 3px #111214, 0 0 0 2px var(--accent), 0 4px 10px #0003; }
  .device-screen { display: block; width: 100%; padding: 0; border: 0; border-radius: calc(var(--frame-radius) - 8px); overflow: hidden; background: #111; touch-action: none; }
  .device-screen:hover:enabled { background: #111; }
  .screen-placeholder { display: flex; flex-direction: column; justify-content: center; align-items: center; gap: 8px; aspect-ratio: var(--display-aspect); padding: 12px; border-radius: calc(var(--frame-radius) - 8px); overflow: hidden; background: #111316; }
  .screen-placeholder p { text-align: center; color: #bdc2ca; }
  .screen-placeholder button, .playback-prompt button { border-radius: 6px; border-color: #616975; background: #303740; color: #fff; }
  .playback-prompt { position: absolute; inset: 7px; display: grid; place-items: center; pointer-events: none; }
  .playback-prompt button { pointer-events: auto; }
  .error { padding: 10px; border-left: 3px solid var(--danger-solid); color: var(--text); overflow-wrap: anywhere; }
  .error summary { cursor: pointer; font-size: 12px; }
  .error pre { max-height: 320px; overflow: auto; white-space: pre-wrap; font-size: 11px; user-select: text; }
  button:focus-visible, input:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }
</style>
