<script lang="ts">
  import { Browser } from '@wailsio/runtime';

  let { oncreate, onerror }: { oncreate: () => void; onerror: (message: string) => void } = $props();

  function openSource(event: MouseEvent) {
    event.preventDefault();
    void Browser.OpenURL('https://www.flaticon.com/free-icon/empty-folder_11696719')
      .catch(() => onerror('Could not open Flaticon in your browser. Visit flaticon.com to view illustrations by andinur.'));
  }
</script>

<section class="empty-state" aria-labelledby="empty-workspace-title">
  <div class="empty-illustration" aria-hidden="true">
    <img src="/icons/empty-workspace.png" alt="" width="112" height="112" draggable="false" />
  </div>
  <h1 id="empty-workspace-title">No profiles yet</h1>
  <p>Add your first profile to keep your accounts<br class="desktop-break" /> and browser sessions organized in one place.</p>
  <button id="empty-create-profile" type="button" onclick={oncreate}>
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>
    Create profile
  </button>
  <a class="illustration-credit" href="https://www.flaticon.com/free-icon/empty-folder_11696719" target="_blank" rel="noopener noreferrer" onclick={openSource}>Illustration by andinur · Flaticon</a>
</section>

<style>
  .empty-state { display: flex; flex-direction: column; align-items: center; width: min(100%, 400px); padding: 24px 0 40px; text-align: center; }
  .empty-illustration { position: relative; display: grid; place-items: center; isolation: isolate; width: 208px; height: 154px; margin-bottom: 16px; }
  .empty-illustration::before { content: ''; position: absolute; inset: 0; z-index: -1; background-image: linear-gradient(var(--border) 1px, transparent 1px), linear-gradient(90deg, var(--border) 1px, transparent 1px); background-size: 20px 20px; opacity: .3; mask-image: radial-gradient(ellipse at center, #000 10%, transparent 68%); }
  .empty-illustration::after { content: ''; position: absolute; bottom: 14px; width: 100px; height: 12px; z-index: -1; background: var(--border); opacity: .25; border-radius: 50%; filter: blur(8px); }
  img { display: block; width: 96px; height: 96px; object-fit: contain; filter: grayscale(1); opacity: .7; }
  h1 { margin: 0 0 10px; color: var(--text); font-size: 18px; line-height: 1.4; font-weight: 600; letter-spacing: -.025em; }
  p { max-width: 340px; margin: 0; color: var(--text-secondary); font-size: 13px; line-height: 1.65; text-wrap: pretty; }
  button { display: inline-flex; align-items: center; justify-content: center; gap: 8px; min-height: 36px; margin-top: 22px; padding: 9px 15px; border: 1px solid var(--text); background: var(--text); color: var(--surface); font-size: 12px; line-height: 1.4; font-weight: 550; cursor: pointer; }
  button:hover { background: var(--text-secondary); border-color: var(--text-secondary); }
  button svg { width: 14px; height: 14px; }
  .illustration-credit { margin-top: 24px; color: var(--text-secondary); font-size: 10px; line-height: 1.5; text-decoration: none; text-underline-offset: 3px; }
  .illustration-credit:hover { text-decoration: underline; }
  .illustration-credit:focus-visible { outline: 2px solid var(--focus); outline-offset: 4px; }
  :global(:root[data-theme='dark']) img { filter: grayscale(1) brightness(1.25); opacity: .8; }
  @media (max-width: 380px) {
    .desktop-break { display: none; }
  }
</style>
