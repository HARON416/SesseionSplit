<script lang="ts">
  let { engine = '', browser = '' }: { engine?: string; browser?: string } = $props();
  const logos: Record<string, string> = {
    chrome: 'chrome',
    chromium: 'chromium',
    brave: 'brave',
    edge: 'edge',
    vivaldi: 'vivaldi',
    opera: 'opera',
    'opera-gx': 'opera-gx',
    'ungoogled-chromium': 'chromium',
    thorium: 'thorium',
    firefox: 'firefox',
  };
  const legacyNames: Record<string, string> = {
    Chrome: 'chrome',
    'Google Chrome': 'chrome',
    Chromium: 'chromium',
    Brave: 'brave',
    'Microsoft Edge': 'edge',
    Vivaldi: 'vivaldi',
    Opera: 'opera',
    'Opera GX': 'opera-gx',
    'Ungoogled Chromium': 'ungoogled-chromium',
    Thorium: 'thorium',
    Firefox: 'firefox',
  };
  const selected = $derived(engine === 'chrome' && browser === 'Chromium' ? 'chromium' : engine || legacyNames[browser]);
  const logo = $derived(logos[selected]);
  const source = $derived(logo ? `/icons/browser-${logo}.png` : '');
  let failedSource = $state('');
</script>

{#if source && failedSource !== source}
  <img src={source} alt="" width="32" height="32" draggable="false" onerror={() => failedSource = source} />
{:else}
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><rect x="3" y="4" width="18" height="16" rx="2" /><path d="M3 9h18M6 6.5h.01M9 6.5h.01" /></svg>
{/if}

<style>
  img, svg { display: block; width: 32px; height: 32px; object-fit: contain; }
</style>
