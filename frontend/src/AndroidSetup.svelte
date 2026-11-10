<script lang="ts">
  import { onMount } from 'svelte';
  import { Browser } from '@wailsio/runtime';
  import { GetAndroidSetup, StartAndroidSetup, CancelAndroidSetup, RespondAndroidLicense } from '../bindings/isolate/androidservice';
  import type { AndroidSetupStatus } from '../bindings/isolate/models';
  let { onready }: { onready: (success: boolean) => void } = $props();
  let status = $state<AndroidSetupStatus | null>(null);
  let error = $state('');
  let busy = $state(false);
  let acknowledged = $state(false);
  let details = $state(false);
  let stopped = false;
  let polling = false;
  let now = $state(Date.now());
  const phaseNames: Record<string, string> = { preparing: 'Preparing', downloading: 'Downloading', installing: 'Installing', extracting: 'Unpacking files', verifying: 'Verifying download', checking: 'Checking', creating: 'Creating virtual phone', complete: 'Setup complete', paused: 'Setup paused' };
  const phase = $derived(status?.waitingLicense ? 'Waiting for license acceptance' : phaseNames[status?.phase || ''] || 'Working');
  const determinate = $derived(Boolean(status && status.percent >= 0 && !status.waitingLicense));
  const quietSeconds = $derived(status?.lastActivity ? Math.max(0, Math.floor((now - status.lastActivity) / 1000)) : 0);
  function size(bytes: number) {
    return bytes >= 1024 ** 3 ? `${(bytes / 1024 ** 3).toFixed(2)} GiB` : `${(bytes / 1024 ** 2).toFixed(1)} MiB`;
  }
  async function refresh() {
    if (polling || stopped) return;
    polling = true;
    now = Date.now();
    try { const next = await GetAndroidSetup(); if (!stopped) { if (status?.running && !next.running) onready(!next.error); status = next; if (next.waitingLicense) details = true; } }
    catch (e) { if (!stopped) error = e instanceof Error ? e.message : 'Could not check Android setup.'; }
    finally { polling = false; }
  }
  async function run(operation: () => Promise<unknown>) {
    busy = true; error = '';
    try { await operation(); await refresh(); }
    catch (e) { error = e instanceof Error ? e.message : 'Android setup failed.'; }
    finally { busy = false; }
  }
  onMount(() => { void refresh(); const timer = setInterval(() => void refresh(), 1000); return () => { stopped = true; clearInterval(timer); }; });
</script>
<section class="setup" aria-labelledby="setup-title">
  <h2 id="setup-title">Set up your Android phone</h2>
  <p>Isolate can install the Android runtime and create a virtual phone for you. Android Studio is not required.</p>
  <p>This setup is normally needed only once per PC. Your virtual phone and installed apps are saved for next time.</p>
  {#if status?.bundledTools}<p><strong>Java and streaming tools are included in this installer.</strong> Android and Google Play files still download directly from Google during setup.</p>{/if}
  <ol><li>{status?.bundledTools ? 'Prepare the included tools and download Android components from Google.' : 'Download the components needed to run Android on this PC.'}</li><li>Review Google’s license terms and prepare a phone with Google Play.</li><li>Start the phone, open Play Store, install Instagram, and sign in yourself.</li></ol>
  {#if status && !status.supported}<p>Automatic setup supports Windows and Linux on Intel/AMD 64-bit PCs.</p>
  {:else if !status?.running}
    <label><input type="checkbox" bind:checked={acknowledged} />Download several GB of runtime files to this PC and create a virtual phone. Hardware virtualization is required.</label>
    <button type="button" disabled={!status || !acknowledged || busy} onclick={() => run(StartAndroidSetup)}>{status?.error ? 'Retry setup' : 'Set up Android'}</button>
  {:else}
    <div class="progress-panel" class:needs-response={status.waitingLicense}>
      <div class="progress-heading">
        <strong role="status">{phase}</strong>
        {#if status.step > 0}<span>Step {status.step} of 6</span>{/if}
      </div>
      <p class="current-stage">{status.stage}</p>
      {#if status.waitingLicense}
        <p><strong>Setup is paused until you respond.</strong> Read the license text below, then choose Accept / continue or Decline.</p>
      {:else}
        <div class="progress-track" class:indeterminate={!determinate} role="progressbar" aria-label={status.progressLabel || phase} aria-valuemin="0" aria-valuemax="100" aria-valuenow={determinate ? status.percent : undefined} aria-valuetext={determinate ? `${status.percent.toFixed(1)}% — ${status.progressLabel}` : 'Working; percentage unavailable'}>
          <span style:width={determinate ? `${Math.max(0, Math.min(100, status.percent))}%` : '35%'}></span>
        </div>
        {#if determinate}<p class="progress-caption">{status.percent.toFixed(1)}% · {status.progressLabel}</p>
        {:else}<p class="progress-caption">Working — percentage is not available for this operation.</p>{/if}
        {#if status.progressLabel === 'Current download'}
          <p class="transfer">{size(status.downloadedBytes)}{status.totalBytes > 0 ? ` of ${size(status.totalBytes)}` : ' downloaded · total size unknown'}{status.bytesPerSecond > 0 ? ` · ${size(status.bytesPerSecond)}/s` : ''}</p>
        {/if}
        {#if status.detail}<p class="operation-detail">{status.detail}</p>{/if}
        {#if quietSeconds >= 20}<p class="waiting">No new data or status for {quietSeconds}s. Setup is still running; downloads and file checks can take time. You can inspect setup details below or cancel and retry.</p>{/if}
      {/if}
      <button type="button" disabled={busy} onclick={() => run(CancelAndroidSetup)}>Cancel setup</button>
    </div>
  {/if}
  {#if status?.stage && !status.running}<p role="status">{status.stage}</p>{/if}
  {#if status?.error || error}<p class="error" role="alert">{error || status?.error}</p>{/if}
  <details bind:open={details}><summary>Setup details and license text</summary><p>Runtime folder: {status?.root}</p><pre aria-label="Android setup output">{status?.log || 'Setup has not started.'}</pre></details>
  {#if status?.waitingLicense}
    <button type="button" disabled={busy} onclick={() => run(() => RespondAndroidLicense(true))}>Accept / continue</button>
    <button type="button" disabled={busy} onclick={() => run(() => RespondAndroidLicense(false))}>Decline</button>
  {/if}
  <p><button class="link" type="button" onclick={() => void Browser.OpenURL('https://developer.android.com/studio/terms').catch(() => error = 'Could not open the SDK terms.')}>Android SDK terms</button> · <button class="link" type="button" onclick={() => void Browser.OpenURL('https://github.com/Genymobile/scrcpy').catch(() => error = 'Could not open scrcpy details.')}>About scrcpy</button></p>
</section>
<style>
  .setup { max-width: 760px; padding: 24px; margin: 24px auto; border: 1px solid var(--border); background: var(--surface); }
  h2 { font-size: 18px; margin-top: 0; } p, li, label, summary { font-size: 12px; line-height: 1.7; color: var(--text-secondary); }
  label { display: block; margin: 16px 0; } button { padding: 8px 12px; border: 1px solid var(--border); background: var(--surface); color: var(--text); font: inherit; font-size: 12px; cursor: pointer; margin: 6px 4px 6px 0; }
  button:disabled { opacity: .5; cursor: default; } .link { padding: 0; border: 0; text-decoration: underline; } details { margin-top: 16px; } summary { cursor: pointer; }
  pre { white-space: pre-wrap; overflow-wrap: anywhere; max-height: 45vh; overflow: auto; font-size: 11px; line-height: 1.6; padding: 12px; background: var(--surface-hover); user-select: text; }
  .error { border-left: 3px solid var(--danger-solid); padding-left: 10px; color: var(--text); }
  .progress-panel { border: 1px solid var(--border); background: var(--surface-hover); padding: 16px; margin: 16px 0; }
  .progress-heading { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 8px; font-size: 13px; }
  .progress-heading span { color: var(--text-secondary); font-size: 12px; }
  .current-stage { margin: 8px 0 12px; }
  .progress-track { height: 10px; overflow: hidden; border-radius: 5px; background: var(--border); }
  .progress-track span { display: block; height: 100%; background: var(--accent); transition: width .3s ease; border-radius: inherit; }
  .progress-track.indeterminate span { animation: activity 1.6s ease-in-out infinite; }
  .progress-caption { margin: 8px 0 0; }
  .transfer { margin: 5px 0; font-variant-numeric: tabular-nums; color: var(--text); }
  .operation-detail { margin: 5px 0; overflow-wrap: anywhere; }
  .needs-response { border-left: 4px solid var(--accent); }
  .waiting { padding: 8px; border-left: 3px solid var(--accent); }
  @keyframes activity { from { transform: translateX(-100%); } to { transform: translateX(390%); } }
  @media (prefers-reduced-motion: reduce) { .progress-track.indeterminate span { animation: none; } .progress-track span { transition: none; } }
</style>
