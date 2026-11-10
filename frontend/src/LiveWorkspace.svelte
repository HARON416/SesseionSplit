<script lang="ts">
  import { onMount } from 'svelte';
  import { GetLiveFrame, SendLiveInput } from '../bindings/isolate/profileservice';
  import type { BrowserProfile, LiveFrame, LiveInput } from '../bindings/isolate/models';
  import BrowserLogo from './BrowserLogo.svelte';

  let { profile, onclose, onstop, onexternal }: { profile: BrowserProfile; onclose: () => void; onstop: () => Promise<void>; onexternal: () => Promise<void> } = $props();
  let dialog: HTMLDialogElement;
  let screen: HTMLButtonElement;
  let frame = $state<LiveFrame | null>(null);
  let error = $state('');
  let busy = $state(false);
  let stopped = false;
  let inputQueue = Promise.resolve();
  let queuedInputs = 0;

  onMount(() => {
    dialog.showModal();
    let timer: ReturnType<typeof setTimeout>;
    const refresh = async () => {
      if (!document.hidden && !busy) {
        try {
          const next = await GetLiveFrame(profile.id, false);
          if (!stopped) frame = next;
        } catch {
          if (!stopped) error = 'Could not refresh this session. Try Reload, or close the session and reopen the profile.';
        }
      }
      if (!stopped) timer = setTimeout(refresh, 250);
    };
    void refresh();
    return () => { stopped = true; clearTimeout(timer); if (dialog.open) dialog.close(); };
  });

  function send(input: Partial<LiveInput>) {
    if (busy || stopped) return;
    if (queuedInputs >= 64) { error = 'The page is busy. Wait for pending input before continuing.'; return; }
    const payload: LiveInput = { kind: '', x: 0, y: 0, deltaX: 0, deltaY: 0, text: '', key: '', code: '', keyCode: 0, modifiers: 0, ...input };
    queuedInputs++;
    inputQueue = inputQueue.then(async () => {
      if (stopped) return;
      try { await SendLiveInput(profile.id, payload); }
      catch { if (!stopped) error = 'Could not send input. Try reopening the session or use External browser.'; }
    }).finally(() => { queuedInputs--; });
  }

  function coordinates(event: MouseEvent | WheelEvent) {
    const bounds = screen.getBoundingClientRect();
    return { x: Math.max(0, Math.min(1099, (event.clientX - bounds.left) * 1100 / bounds.width)), y: Math.max(0, Math.min(759, (event.clientY - bounds.top) * 760 / bounds.height)) };
  }

  function click(event: MouseEvent) {
    screen.focus();
    if (frame) send({ kind: 'click', ...coordinates(event) });
  }

  function wheel(event: WheelEvent) {
    event.preventDefault();
    const multiplier = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? 760 : 1;
    if (frame) send({ kind: 'wheel', ...coordinates(event), deltaX: Math.max(-3000, Math.min(3000, event.deltaX * multiplier)), deltaY: Math.max(-3000, Math.min(3000, event.deltaY * multiplier)) });
  }

  function keydown(event: KeyboardEvent) {
    if (event.key === 'Escape' || event.isComposing) return;
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'v') return;
    event.preventDefault();
    event.stopPropagation();
    if (!frame) return;
    if (event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey) send({ kind: 'text', text: event.key });
    else send({ kind: 'key', key: event.key, code: event.code, keyCode: event.keyCode, modifiers: (event.altKey ? 1 : 0) | (event.ctrlKey ? 2 : 0) | (event.metaKey ? 4 : 0) | (event.shiftKey ? 8 : 0) });
  }

  function paste(event: ClipboardEvent) {
    event.preventDefault();
    const text = event.clipboardData?.getData('text/plain') || '';
    if (text.length > 8192) { error = 'Paste up to 8,192 characters at a time.'; return; }
    if (frame && text) send({ kind: 'text', text });
  }

  async function finish(action: () => Promise<void>) {
    busy = true;
    error = '';
    try { await inputQueue; await action(); }
    catch { error = 'Could not close or switch this session. Try again.'; }
    finally { busy = false; }
  }
</script>

<dialog class="live-workspace" bind:this={dialog} aria-labelledby="live-title" oncancel={(event) => { event.preventDefault(); if (!busy) onclose(); }}>
  <header>
    <BrowserLogo engine={profile.settings?.engine} browser={profile.browser} />
    <div class="identity"><h1 id="live-title">{profile.name}</h1><p>{frame?.title || 'Connecting to live browser…'}</p></div>
    <button type="button" onclick={onclose} disabled={busy}>Back to profiles</button>
  </header>
  <div class="toolbar">
    <button type="button" disabled={busy || !frame} onclick={() => { error = ''; send({ kind: 'reload' }); }}>Reload</button>
    <span class="page-url" title={frame?.url}>{frame?.url || 'Loading…'}</span>
    <button type="button" disabled={busy} title="Close this live session and reopen its saved data in a normal browser window. Unsaved page state may be lost." onclick={() => finish(onexternal)}>External browser</button>
    <button type="button" disabled={busy} onclick={() => finish(onstop)}>Close session</button>
  </div>
  <p class="help">Click the page to interact. Type, paste, or scroll. Escape returns to profiles; the session keeps running. For uploads, popups, or browser dialogs, use External browser.</p>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  <div class="stage">
    <button class="screen" type="button" bind:this={screen} aria-roledescription="interactive browser" aria-label={`Interactive browser for ${profile.name}`} aria-busy={!frame || busy} onclick={click} onkeydown={keydown} onwheel={wheel} onpaste={paste}>
      {#if frame}<img src={frame.image} alt={`Live browser page for ${profile.name}`} draggable="false" />
      {:else}<span role="status">Loading live page…</span>{/if}
    </button>
  </div>
</dialog>

<style>
  .live-workspace { inset: 12px; margin: auto; width: calc(100% - 24px); height: calc(100% - 24px); max-width: none; max-height: none; padding: 16px; border: 1px solid var(--border); background: var(--surface); color: var(--text); }
  .live-workspace[open] { display: flex; flex-direction: column; gap: 12px; }
  .live-workspace::backdrop { background: var(--backdrop); }
  header, .toolbar { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
  .identity { flex: 1; min-width: 100px; }
  h1 { font-size: 16px; margin: 0; overflow-wrap: anywhere; }
  .identity p { font-size: 12px; color: var(--text-secondary); margin: 3px 0 0; overflow-wrap: anywhere; }
  button { border: 1px solid var(--border); background: var(--surface); color: var(--text); padding: 7px 10px; font: inherit; font-size: 12px; cursor: pointer; }
  button:hover:enabled { background: var(--surface-hover); }
  button:disabled { opacity: .5; cursor: default; }
  .page-url { flex: 1; min-width: 80px; font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .help, .error { font-size: 12px; line-height: 1.5; margin: 0; }
  .help { color: var(--text-secondary); }
  .error { color: var(--text); border-left: 3px solid var(--danger-solid); padding-left: 10px; }
  .stage { flex: 1; min-height: 80px; display: flex; justify-content: center; align-items: center; overflow: auto; background: var(--surface-hover); }
  .screen { width: min(1100px, 100%, calc(max(80px, 100dvh - 240px) * 1100 / 760)); aspect-ratio: 1100 / 760; background: white; color: #212529; touch-action: none; border: 0; padding: 0; flex-shrink: 0; }
  .screen:hover:enabled { background: white; }
  .screen img { display: block; width: 100%; height: 100%; user-select: none; }
  .screen > span { display: block; padding: 16px; font-size: 13px; }
  .screen:focus-visible, button:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }
  @media (max-width: 600px) { .live-workspace { inset: 0; width: 100%; height: 100%; padding: 10px; overflow: auto; } .identity p { display: none; } }
</style>
