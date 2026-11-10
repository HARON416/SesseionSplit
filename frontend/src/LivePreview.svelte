<script module lang="ts">
  let capturesInFlight = 0;
</script>

<script lang="ts">
  import { onMount } from 'svelte';
  import { GetLiveFrame } from '../bindings/isolate/profileservice';

  let { id, name, active = false, onopen }: { id: string; name: string; active?: boolean; onopen: () => void } = $props();
  let element: HTMLButtonElement;
  let image = $state('');
  let failed = $state(false);

  onMount(() => {
    let visible = false;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    const observer = new IntersectionObserver(entries => { visible = entries[0]?.isIntersecting ?? false; });
    observer.observe(element);
    const refresh = async () => {
      let delay = 1000;
      if (visible && !document.hidden && !active && capturesInFlight < 2) {
        capturesInFlight++;
        try {
          const frame = await GetLiveFrame(id, true);
          if (!stopped) { image = frame.image; failed = false; }
        } catch {
          if (!stopped) failed = true;
        } finally {
          capturesInFlight--;
        }
        delay = 10000;
      }
      if (!stopped) timer = setTimeout(refresh, delay);
    };
    void refresh();
    return () => { stopped = true; clearTimeout(timer); observer.disconnect(); };
  });
</script>

<button class="live-preview" bind:this={element} type="button" onclick={onopen} aria-label={`View live session for ${name}`}>
  {#if image}<img src={image} alt={`Live page preview for ${name}`} draggable="false" />
  {:else}<span>{failed ? 'Preview unavailable — reopen to retry' : 'Preparing live preview…'}</span>{/if}
  <span class="badge">{active ? 'Viewing' : failed ? 'Retry live view' : 'Live view'}</span>
</button>

<style>
  .live-preview { display: grid; position: relative; width: 100%; aspect-ratio: 1100 / 760; overflow: hidden; padding: 0; border: 1px solid var(--border); background: var(--surface-hover); color: var(--text-secondary); cursor: pointer; }
  img { width: 100%; height: 100%; object-fit: contain; }
  .live-preview > span:not(.badge) { margin: auto; padding: 16px; font-size: 12px; }
  .badge { position: absolute; right: 8px; bottom: 8px; padding: 4px 8px; background: var(--surface); color: var(--text); border: 1px solid var(--border); font-size: 11px; }
  .live-preview:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }
</style>
