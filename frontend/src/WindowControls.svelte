<script lang="ts">
  import { onMount } from 'svelte';
  import { Window, Events } from '@wailsio/runtime';

  let { onerror }: { onerror: (message: string) => void } = $props();
  let maximised = $state(false), busy = $state(false);

  async function readState() { maximised = Boolean(await Window.IsMaximised()); }
  async function act(action: 'minimise' | 'maximise' | 'close') {
    if (busy) return;
    busy = true;
    try {
      if (action === 'minimise') await Window.Minimise();
      else if (action === 'close') await Window.Close();
      else { await Window.ToggleMaximise(); await readState(); }
    } catch { onerror(/* @wc-ignore */ 'Could not change the window. Please try again.'); }
    finally { busy = false; }
  }
  export function toggleMaximise() { return act('maximise'); }

  onMount(() => {
    let disposed = false;
    let timer: ReturnType<typeof setTimeout>;
    const sync = async () => {
      try { const value = await Window.IsMaximised(); if (!disposed) maximised = Boolean(value); }
      catch { /* Window state is unavailable in a browser preview. */ }
    };
    const resize = () => { clearTimeout(timer); timer = setTimeout(() => { void sync(); }, 80); };
    const events = [Events.Types.Common.WindowMaximise, Events.Types.Common.WindowUnMaximise, Events.Types.Common.WindowRestore];
    // Events can be broadcast; read the state of this control's own window.
    const stops = events.map((name) => Events.On(name, () => { void sync(); }));
    void sync();
    window.addEventListener('resize', resize);
    window.addEventListener('focus', resize);
    return () => { disposed = true; clearTimeout(timer); stops.forEach((stop) => stop()); window.removeEventListener('resize', resize); window.removeEventListener('focus', resize); };
  });
</script>

<div class="window-controls" aria-label="Window controls">
  <button type="button" aria-label="Minimize window" disabled={busy} onclick={() => act('minimise')}>
    <svg viewBox="0 0 256 256" aria-hidden="true"><path d="M224,128a8,8,0,0,1-8,8H40a8,8,0,0,1,0-16H216A8,8,0,0,1,224,128Z" /></svg>
  </button>
  <button type="button" aria-label={maximised ? 'Restore window' : 'Maximize window'} disabled={busy} onclick={toggleMaximise}>
    {#if maximised}
      <svg class="restore" viewBox="0 0 256 256" aria-hidden="true"><path d="M88 72V40h128v128h-32M40 88h128v128H40Z" /></svg>
    {:else}
      <svg viewBox="0 0 256 256" aria-hidden="true"><path d="M208,32H48A16,16,0,0,0,32,48V208a16,16,0,0,0,16,16H208a16,16,0,0,0,16-16V48A16,16,0,0,0,208,32Zm0,176H48V48H208V208Z" /></svg>
    {/if}
  </button>
  <button type="button" class="close" aria-label="Close window" disabled={busy} onclick={() => act('close')}>
    <svg viewBox="0 0 256 256" aria-hidden="true"><path d="M205.66,194.34a8,8,0,0,1-11.32,11.32L128,139.31,61.66,205.66a8,8,0,0,1-11.32-11.32L116.69,128,50.34,61.66A8,8,0,0,1,61.66,50.34L128,116.69l66.34-66.35a8,8,0,0,1,11.32,11.32L139.31,128Z" /></svg>
  </button>
</div>

<style>
  .window-controls { --wails-draggable: no-drag; display: flex; align-self: stretch; flex-shrink: 0; }
  button { display: grid; place-items: center; width: 4.4rem; padding: 0; border: 0; background: transparent; color: var(--text-secondary); cursor: pointer; }
  button:hover { background: var(--surface-hover); color: var(--text); }
  button.close:hover { background: var(--danger-solid); color: var(--on-danger); }
  button:focus-visible { outline: 2px solid var(--focus); outline-offset: -3px; }
  button:disabled { cursor: default; opacity: .5; }
  svg { width: 1.4rem; height: 1.4rem; fill: currentColor; pointer-events: none; }
  svg.restore { fill: none; stroke: currentColor; stroke-width: 16; stroke-linecap: round; stroke-linejoin: round; }
</style>
