<script lang="ts">
  import { tick } from 'svelte';
  import { CloseProfile, GetProfileSettings, ListBrowserEngines, TestProfileConnection, UpdateProfileSettings } from '../bindings/sessionsplit/profileservice';
  import type { BrowserProfile, ConnectionDiagnostic, EngineInfo, ProfileSettings } from '../bindings/sessionsplit/models';

  let { onupdated, onmessage }: { onupdated: (profile: BrowserProfile) => void; onmessage?: (message: string) => void } = $props();
  let dialog: HTMLDialogElement;
  let profile = $state<BrowserProfile | null>(null);
  let loading = $state(false);
  let loaded = $state(false);
  let operation = $state('');
  let error = $state('');
  let running = $state(false);
  let engines = $state<EngineInfo[]>([]);
  let mode = $state('direct');
  let host = $state('');
  let port = $state<number | undefined>(undefined);
  let username = $state('');
  let password = $state('');
  let hasPassword = $state(false);
  let removePassword = $state(false);
  let engine = $state('chrome');
  let language = $state('');
  let timezone = $state('');
  let width = $state<number | undefined>(undefined);
  let height = $state<number | undefined>(undefined);
  let permissions = $state('ask');
  let baseline = $state('');
  let diagnostic = $state<ConnectionDiagnostic | null>(null);
  let diagnosticError = $state('');
  let generation = 0;
  let diagnosticRequest: ReturnType<typeof TestProfileConnection> | undefined;
  const busy = $derived(operation !== '');
  const selectedEngine = $derived(engines.find(item => item.id === engine));
  const settings = $derived<ProfileSettings>({
    proxy: { mode, host: host.trim(), port: port || 0, username, hasPassword },
    engine, language: language.trim(), timezone: timezone.trim(), width: width || 0, height: height || 0, permissions,
  });
  const dirty = $derived(JSON.stringify(settings) !== baseline || password !== '' || removePassword);

  function safeError(reason: unknown, action: 'load' | 'save' | 'close' | 'test') {
    const detail = reason instanceof Error ? reason.message : String(reason);
    const known = [
      'Choose Chrome or Brave as the browser', 'Enter a language code such as en-US',
      'Choose a valid timezone such as Africa/Nairobi', 'Enter a window width from 320 to 7680 and height from 200 to 4320',
      'Choose Ask or Block for site permissions', 'Choose a supported proxy type',
      'Enter a proxy hostname or IP address without a URL or credentials', 'Enter a valid proxy hostname or IP address',
      'Enter a proxy port from 1 to 65535', 'Enter a valid proxy username of at most 255 characters',
      'Enter a proxy password', 'Use a proxy password of at most 255 characters',
      'Close this profile browser before changing settings',
      'Could not save the proxy password in your system keyring; unlock it and try again',
    ];
    const match = known.find(message => detail.includes(message));
    if (match) return `${match}.`;
    if (detail.includes('Profile was not found')) return 'This profile is no longer available. Close settings and refresh the profile list.';
    if (action === 'load') return 'Could not load settings. Try again, or restart SessionSplit.';
    if (action === 'close') return 'Could not close the browser. Close its windows, then reopen settings to try again.';
    if (action === 'test') return 'Connection test failed. Check the saved proxy address, credentials, and network connection, then try again.';
    return 'Could not save settings. Check available disk space and folder permissions, then try again.';
  }

  export async function open(selected: BrowserProfile) {
    profile = selected;
    error = '';
    diagnostic = null;
    diagnosticError = '';
    password = '';
    removePassword = false;
    loaded = false;
    operation = '';
    dialog.showModal();
    await load();
  }

  async function load() {
    if (!profile || loading) return;
    const current = ++generation;
    const id = profile.id;
    loading = true;
    error = '';
    try {
      const [state, available] = await Promise.all([GetProfileSettings(id), ListBrowserEngines()]);
      if (current !== generation || !dialog.open) return;
      const saved = state.settings;
      mode = saved.proxy?.mode || 'direct';
      host = saved.proxy?.host || '';
      port = saved.proxy?.port || undefined;
      username = saved.proxy?.username || '';
      hasPassword = saved.proxy?.hasPassword || false;
      engine = saved.engine || 'chrome';
      language = saved.language || '';
      timezone = saved.timezone || '';
      width = saved.width || undefined;
      height = saved.height || undefined;
      permissions = saved.permissions || 'ask';
      running = state.running;
      engines = available || [];
      await tick();
      baseline = JSON.stringify(settings);
      loaded = true;
      await tick();
      dialog.querySelector<HTMLSelectElement>('#settings-proxy-mode')?.focus();
    } catch (reason) {
      if (current === generation) error = safeError(reason, 'load');
    } finally {
      if (current === generation) loading = false;
    }
  }

  function closed() {
    generation++;
    diagnosticRequest?.cancel();
    diagnosticRequest = undefined;
    password = '';
    removePassword = false;
    loading = false;
    operation = '';
    if (profile) document.getElementById(`settings-${profile.id}`)?.focus();
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    if (!profile || !loaded || running || busy || !dirty) return;
    operation = 'save';
    error = '';
    try {
      const updated = await UpdateProfileSettings(profile.id, { settings, password, passwordAction: removePassword ? 'remove' : password ? 'replace' : 'keep' });
      password = '';
      onupdated(updated);
      dialog.close();
      onmessage?.('Profile settings saved. They will apply the next time you open the browser.');
    } catch (reason) {
      error = safeError(reason, 'save');
      if (String(reason).includes('Close this profile browser')) running = true;
    } finally { operation = ''; }
  }

  async function closeBrowser() {
    if (!profile || busy) return;
    operation = 'close';
    error = '';
    try {
      await CloseProfile(profile.id);
      running = false;
    } catch (reason) { error = safeError(reason, 'close'); }
    finally { operation = ''; }
  }

  async function testConnection() {
    if (!profile || busy || dirty) return;
    operation = 'test';
    diagnosticError = '';
    diagnostic = null;
    const current = generation;
    try {
      diagnosticRequest = TestProfileConnection(profile.id);
      const result = await diagnosticRequest;
      if (current === generation && dialog.open) diagnostic = result;
    } catch (reason) {
      if (current === generation && dialog.open) diagnosticError = safeError(reason, 'test');
    } finally {
      if (current === generation) { operation = ''; diagnosticRequest = undefined; }
    }
  }
</script>

<dialog class="settings-dialog" id="settings-dialog" bind:this={dialog} aria-labelledby="settings-title" onclose={closed} oncancel={(event) => { if (operation === 'save' || operation === 'close') event.preventDefault(); }}>
  <form onsubmit={save} aria-busy={loading || busy}>
    <header>
      <h1 id="settings-title">Profile settings</h1>
      <p class="profile-name">{profile?.name}</p>
    </header>
    <div class="settings-content">
      {#if loading}<p role="status">Loading settings…</p>{/if}
      {#if loaded}
        <div class="running-state">
          {#if running}
            <p>Close this profile’s browser before saving changes. Its sessions and saved data will stay with the profile.</p>
            <button class="secondary" type="button" disabled={busy} onclick={closeBrowser}>{operation === 'close' ? 'Closing browser…' : 'Close browser'}</button>
          {:else}<p>Changes apply the next time you open this profile’s browser.</p>{/if}
        </div>
        <fieldset disabled={busy}>
          <legend>Connection</legend>
          <label for="settings-proxy-mode">Connection type</label>
          <select id="settings-proxy-mode" bind:value={mode}>
            <option value="direct">Direct connection</option><option value="http">HTTP proxy</option><option value="https">HTTPS proxy</option><option value="socks5">SOCKS5 proxy</option>
          </select>
          {#if mode !== 'direct'}
            <div class="connection-address">
              <div><label for="settings-proxy-host">Host</label><input id="settings-proxy-host" bind:value={host} placeholder="proxy.example.com" autocomplete="off" required /></div>
              <div><label for="settings-proxy-port">Port</label><input id="settings-proxy-port" type="number" bind:value={port} min="1" max="65535" step="1" placeholder="8080" required /></div>
            </div>
            <label for="settings-proxy-username">Username <span>(optional)</span></label>
            <input id="settings-proxy-username" bind:value={username} autocomplete="off" maxlength="255" />
            <label for="settings-proxy-password">{hasPassword ? 'Replace password' : 'Password'} <span>(optional)</span></label>
            <input id="settings-proxy-password" type="password" bind:value={password} disabled={removePassword} autocomplete="new-password" maxlength="255" aria-describedby="settings-password-hint" />
            <p class="hint" id="settings-password-hint">{hasPassword ? 'A password is saved in your system keyring. Leave this blank to keep it.' : 'Passwords are stored in your system keyring and are never shown here.'}</p>
          {/if}
          {#if hasPassword}<label class="checkbox"><input id="settings-remove-password" type="checkbox" bind:checked={removePassword} onchange={() => { if (removePassword) password = ''; }} />Remove saved password</label>{/if}
        </fieldset>
        <fieldset disabled={busy}>
          <legend>Browser environment</legend>
          <label for="settings-engine">Browser</label>
          <select id="settings-engine" bind:value={engine}>
            {#each ['chrome', 'brave'] as id}
              {@const available = engines.find(item => item.id === id)}
              <option value={id} disabled={!available?.available}>{id === 'chrome' ? 'Chrome / Chromium' : 'Brave'}{available?.available ? '' : ' — not installed'}</option>
            {/each}
          </select>
          <p class="hint">Each browser keeps its own sessions. Switching browsers does not transfer logins.</p>
          {#if selectedEngine}
            <p class="hint">{selectedEngine.fingerprintProtection}{selectedEngine.version ? ` · ${selectedEngine.version}` : ''}</p>
            {#if selectedEngine.notes}
              <details><summary>Browser capabilities</summary><p class="hint">{selectedEngine.notes.replace(/https?:\/\/\S+/g, '').trim()}</p></details>
            {/if}
          {/if}
          <div class="two-columns">
            <div><label for="settings-language">Language <span>(optional)</span></label><input id="settings-language" bind:value={language} placeholder="en-US" autocomplete="off" /></div>
            <div><label for="settings-timezone">Timezone <span>(optional)</span></label><input id="settings-timezone" bind:value={timezone} placeholder="Africa/Nairobi" autocomplete="off" /></div>
          </div>
          <p class="hint">Leave blank to use your browser’s language and system timezone. Overrides apply to pages and iframes; worker contexts are not covered.</p>
          <div class="two-columns">
            <div><label for="settings-width">Window width <span>(px)</span></label><input id="settings-width" type="number" bind:value={width} min="320" max="7680" step="1" placeholder="Browser default" /></div>
            <div><label for="settings-height">Window height <span>(px)</span></label><input id="settings-height" type="number" bind:value={height} min="200" max="4320" step="1" placeholder="Browser default" /></div>
          </div>
          <p class="hint">Set both dimensions, or leave both blank for the browser default.</p>
          <label for="settings-permissions">Camera, microphone, and location</label>
          <select id="settings-permissions" bind:value={permissions}><option value="ask">Ask before allowing</option><option value="block">Block</option></select>
        </fieldset>
        <section aria-labelledby="settings-diagnostics-title">
          <h2 id="settings-diagnostics-title">Diagnostics</h2>
          <p class="hint">Test the saved connection by sending a request to api.ipify.org through its configured route. This does not check all browser traffic.</p>
          <button class="secondary" type="button" disabled={busy || dirty} onclick={testConnection}>{operation === 'test' ? 'Testing connection…' : 'Test saved connection'}</button>
          {#if dirty}<p class="hint">Save changes before testing.</p>{/if}
          {#if diagnosticError}<p class="error" role="alert">{diagnosticError}</p>{/if}
          {#if diagnostic}
            <div class="diagnostic-result" role="status">
              <dl><div><dt>Public IP</dt><dd>{diagnostic.ip}</dd></div><div><dt>Route</dt><dd>{diagnostic.route === 'direct' ? 'Direct connection' : ['http', 'https', 'socks5'].includes(diagnostic.route) ? `${diagnostic.route.toUpperCase()} proxy` : diagnostic.route}</dd></div><div><dt>Response time</dt><dd>{diagnostic.durationMS} ms</dd></div></dl>
              <p class="hint">DNS leaks and WebRTC leaks are unverified. This connection test does not certify their protection.</p>
            </div>
          {/if}
        </section>
      {/if}
      {#if error}<p class="error" role="alert">{error}</p>{/if}
      {#if !loading && !loaded}<button class="secondary" type="button" onclick={load}>Try again</button>{/if}
    </div>
    <footer>
      <button class="secondary" type="button" disabled={operation === 'save' || operation === 'close'} onclick={() => dialog.close()}>Cancel</button>
      <button class="primary" type="submit" disabled={loading || !loaded || running || busy || !dirty}>{operation === 'save' ? 'Saving…' : 'Save changes'}</button>
    </footer>
  </form>
</dialog>

<style>
  dialog { --wails-draggable: no-drag; width: min(620px, calc(100% - 40px)); max-height: calc(100% - 32px); margin: auto; padding: 0; overflow: hidden; border: 1px solid var(--border); border-radius: 0; background: var(--surface); color: var(--text); }
  dialog::backdrop { background: var(--backdrop); }
  form { display: flex; flex-direction: column; max-height: calc(100dvh - 34px); }
  header { flex-shrink: 0; padding: 20px 24px 16px; border-bottom: 1px solid var(--border); }
  h1 { margin: 0 0 4px; font-size: 18px; line-height: 1.4; font-weight: 600; }
  .profile-name { margin: 0; overflow-wrap: anywhere; color: var(--text-secondary); font-size: 13px; font-weight: 550; text-transform: uppercase; }
  .settings-content { min-height: 0; padding: 20px 24px 24px; overflow: auto; }
  p { margin: 0; font-size: 13px; line-height: 1.6; }
  .running-state { margin-bottom: 24px; color: var(--text-secondary); }
  .running-state button { margin-top: 12px; }
  fieldset { min-width: 0; margin: 0 0 26px; padding: 0; border: 0; }
  legend, h2 { width: 100%; margin: 0 0 14px; padding: 0; color: var(--text); font-size: 15px; line-height: 1.4; font-weight: 600; }
  label { display: block; margin: 14px 0 6px; font-size: 13px; line-height: 1.5; font-weight: 550; }
  label span { color: var(--text-secondary); font-weight: 400; }
  input, select { width: 100%; min-width: 0; min-height: 38px; padding: 8px 10px; border: 1px solid var(--input-border); border-radius: 0; background: var(--surface); color: var(--text); font: inherit; font-size: 13px; line-height: 1.5; caret-color: var(--accent); }
  input::placeholder { color: var(--text-secondary); }
  input:focus-visible, select:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }
  .connection-address { display: grid; grid-template-columns: minmax(0, 1fr) 112px; gap: 12px; }
  .two-columns { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
  .hint { margin: 8px 0 0; color: var(--text-secondary); font-size: 12px; line-height: 1.6; overflow-wrap: anywhere; }
  section > .hint { margin: 0 0 12px; }
  details { margin-top: 8px; font-size: 12px; line-height: 1.6; }
  summary { width: fit-content; color: var(--text-secondary); cursor: pointer; }
  summary:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }
  .checkbox { display: flex; align-items: center; gap: 8px; font-weight: 400; }
  .checkbox input { width: 16px; height: 16px; min-height: 16px; margin: 0; accent-color: var(--accent); }
  button { min-height: 36px; padding: 8px 14px; border: 1px solid var(--border); border-radius: 0; font: inherit; font-size: 13px; line-height: 1.4; font-weight: 550; cursor: pointer; }
  .secondary { background: var(--surface); color: var(--text); }
  .secondary:hover:enabled { background: var(--surface-hover); }
  .primary { background: var(--accent); border-color: var(--accent); color: var(--on-accent); }
  .primary:hover:enabled { background: var(--accent-hover); }
  button:disabled, input:disabled, select:disabled { opacity: .5; cursor: default; }
  footer { display: flex; flex-shrink: 0; justify-content: flex-end; gap: 8px; padding: 16px 24px; border-top: 1px solid var(--border); background: var(--surface); }
  .error { margin-top: 14px; color: var(--danger); overflow-wrap: anywhere; }
  .diagnostic-result { margin-top: 16px; }
  dl { display: flex; flex-wrap: wrap; gap: 12px 24px; margin: 0; font-size: 13px; line-height: 1.5; }
  dt { color: var(--text-secondary); }
  dd { margin: 2px 0 0; overflow-wrap: anywhere; font-weight: 550; font-variant-numeric: tabular-nums; }
  @media (max-width: 520px) { header { padding: 16px; } .settings-content { padding: 16px; } footer { padding: 12px 16px; } .two-columns { grid-template-columns: minmax(0, 1fr); gap: 0; } }
</style>
