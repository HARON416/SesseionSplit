<script lang="ts">
  import { tick } from 'svelte';
  import { CloseProfile, GetProfileSettings, ListBrowserEngines, TestProfileConnection, UpdateProfileSettings } from '../bindings/isolate/profileservice';
  import type { BrowserProfile, ConnectionDiagnostic, EngineInfo, ProfileSettings } from '../bindings/isolate/models';
  import BrowserLogo from './BrowserLogo.svelte';

  let { onupdated, onmessage }: { onupdated: (profile: BrowserProfile) => void; onmessage?: (message: string) => void } = $props();
  let dialog: HTMLDialogElement;
  let profile = $state<BrowserProfile | null>(null);
  let loading = $state(false);
  let loaded = $state(false);
  let closing = $state(false);
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
  let browserPath = $state('');
  let language = $state('');
  let timezone = $state('');
  let width = $state<number | undefined>(undefined);
  let height = $state<number | undefined>(undefined);
  let permissions = $state('ask');
  let baseline = $state('');
  let diagnostic = $state<ConnectionDiagnostic | null>(null);
  let diagnosticError = $state('');
  let testedConnection = $state('');
  let generation = 0;
  let diagnosticRequest: ReturnType<typeof TestProfileConnection> | undefined;
  const busy = $derived(operation !== '');
  const selectedEngine = $derived(engines.find(item => item.id === engine));
  const settings = $derived<ProfileSettings>({
    proxy: { mode, host: host.trim(), port: port || 0, username, hasPassword },
    engine, browserPath: engine === 'custom' ? browserPath.trim() : undefined,
    language: language.trim(), timezone: timezone.trim(), width: width || 0, height: height || 0, permissions,
  });
  const dirty = $derived(JSON.stringify(settings) !== baseline || password !== '' || removePassword);
  const connectionDraft = $derived(JSON.stringify({ proxy: settings.proxy, password, removePassword }));

  function safeError(reason: unknown, action: 'load' | 'save' | 'close' | 'test') {
    const detail = reason instanceof Error ? reason.message : String(reason);
    const known = [
      'Choose a supported browser', 'Enter a language code such as en-US',
      'Enter the full path to a Chromium-based browser executable',
      'The selected browser executable is missing or cannot be run',
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
    if (action === 'load') return 'Could not load settings. Try again, or restart Isolate.';
    if (action === 'close') return 'Could not close the browser. Close its windows, then reopen settings to try again.';
    if (action === 'test') return 'Connection test failed. Check the proxy address, credentials, and network connection, then try again.';
    return 'Could not save settings. Check available disk space and folder permissions, then try again.';
  }

  export async function open(selected: BrowserProfile) {
    if (dialog.open || closing) return;
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
      browserPath = saved.browserPath || '';
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
    closing = false;
    generation++;
    diagnosticRequest?.cancel();
    diagnosticRequest = undefined;
    testedConnection = '';
    password = '';
    removePassword = false;
    loading = false;
    operation = '';
    if (profile) document.getElementById(`settings-${profile.id}`)?.focus();
  }

  async function closeDrawer() {
    if (!dialog.open || closing) return;
    closing = true;
    generation++;
    diagnosticRequest?.cancel();
    diagnosticRequest = undefined;
    await tick();
    await Promise.allSettled(dialog.getAnimations().map(animation => animation.finished));
    if (dialog.open && closing) dialog.close();
  }

  function dismissBackdrop(event: MouseEvent) {
    if (event.target !== dialog || operation === 'save' || operation === 'close') return;
    const bounds = dialog.getBoundingClientRect();
    if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) void closeDrawer();
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
      await closeDrawer();
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
    if (!profile || !loaded || busy) return;
    operation = 'test';
    diagnosticError = '';
    diagnostic = null;
    testedConnection = connectionDraft;
    const current = generation;
    try {
      diagnosticRequest = TestProfileConnection(profile.id, { settings, password, passwordAction: removePassword ? 'remove' : password ? 'replace' : 'keep' });
      const result = await diagnosticRequest;
      if (current === generation && dialog.open) diagnostic = result;
    } catch (reason) {
      if (current === generation && dialog.open) diagnosticError = safeError(reason, 'test');
    } finally {
      if (current === generation) { operation = ''; diagnosticRequest = undefined; }
    }
  }
</script>

<dialog class="settings-dialog" class:closing id="settings-dialog" bind:this={dialog} aria-labelledby="settings-context settings-title" onclick={dismissBackdrop} onclose={closed} oncancel={(event) => { event.preventDefault(); if (operation !== 'save' && operation !== 'close') void closeDrawer(); }}>
  <form onsubmit={save} aria-busy={loading || busy} inert={closing}>
    <header>
      <div class="profile-identity">
        <div class="profile-avatar" aria-hidden="true"><BrowserLogo engine={profile?.settings?.engine} browser={profile?.browser} /></div>
        <div class="profile-heading">
          <p class="drawer-kicker" id="settings-context">Profile settings</p>
          <h1 id="settings-title">{profile?.name}</h1>
        </div>
      </div>
      <button class="close-drawer" type="button" aria-label="Close profile settings" title="Close settings (Esc)" disabled={operation === 'save' || operation === 'close'} onclick={closeDrawer}>
        <svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" fill="currentColor" viewBox="0 0 256 256" aria-hidden="true"><path d="M205.66,194.34a8,8,0,0,1-11.32,11.32L128,139.31,61.66,205.66a8,8,0,0,1-11.32-11.32L116.69,128,50.34,61.66A8,8,0,0,1,61.66,50.34L128,116.69l66.34-66.35a8,8,0,0,1,11.32,11.32L139.31,128Z" /></svg>
      </button>
    </header>
    <div class="settings-content">
      {#if loading}
        <div class="settings-loading" role="status" aria-label="Loading settings">
          <span class="loader" aria-hidden="true"></span>
        </div>
      {/if}
      {#if loaded}
        <div class="running-state">
          {#if running}
            <p>Close this profile’s browser before saving changes. Its sessions and saved data will stay with the profile.</p>
            <button class="secondary" type="button" disabled={busy} onclick={closeBrowser}>{operation === 'close' ? 'Closing browser…' : 'Close browser'}</button>
          {:else}<p>Changes apply the next time you open this profile’s browser.</p>{/if}
        </div>
        <fieldset disabled={busy}>
          <legend><span class="section-icon connection-icon" aria-hidden="true"></span>Connection</legend>
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
          <legend><span class="section-icon browser-icon" aria-hidden="true"></span>Browser environment</legend>
          <label for="settings-engine">Browser</label>
          <select id="settings-engine" bind:value={engine}>
            {#each engines as available (available.id)}
              <option value={available.id} disabled={!available.available}>{available.name}{available.available ? '' : ' — not installed'}</option>
            {/each}
          </select>
          {#if engine === 'custom'}
            <label for="settings-browser-path">Browser executable</label>
            <input id="settings-browser-path" bind:value={browserPath} placeholder="Full path to the browser executable" autocomplete="off" required />
            <p class="hint">Choose the executable file, without command-line arguments. Use this for portable browsers and Chromium variants that are not detected automatically. Select the Firefox entry for Firefox. Safari is not supported.</p>
          {/if}
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
          <h2 id="settings-diagnostics-title"><span class="section-icon diagnostics-icon" aria-hidden="true"></span>Diagnostics</h2>
          <p class="hint">Test the connection entered above with a request to api.ipify.org. This does not save your settings or password, or check all browser traffic.</p>
          <button class="secondary" type="button" disabled={busy} onclick={testConnection}>{operation === 'test' ? 'Testing connection…' : 'Test connection'}</button>
          {#if diagnosticError && testedConnection === connectionDraft}<p class="error" role="alert">{diagnosticError}</p>{/if}
          {#if diagnostic && testedConnection === connectionDraft}
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
      <button class="secondary" type="button" disabled={operation === 'save' || operation === 'close'} onclick={closeDrawer}>Cancel</button>
      <button class="primary" type="submit" disabled={loading || !loaded || running || busy || !dirty}>{operation === 'save' ? 'Saving…' : 'Save changes'}</button>
    </footer>
  </form>
</dialog>

<style>
  .settings-loading { display: grid; place-items: center; min-height: 160px; }
  .loader {
    --color-1: var(--text);
    --size: 1px;
    width: calc(48 * var(--size));
    height: calc(48 * var(--size));
    border: calc(5 * var(--size)) dotted var(--color-1);
    border-radius: 50%;
    display: inline-block;
    position: relative;
    box-sizing: border-box;
    animation: rotation 2s linear infinite;
  }
  @keyframes rotation {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }
  @media (prefers-reduced-motion: reduce) {
    .loader { animation: none; }
  }
  dialog { --wails-draggable: no-drag; position: fixed; inset: 0 0 0 auto; width: min(460px, 100vw); max-width: 100vw; height: 100dvh; max-height: 100dvh; margin: 0; padding: 0; overflow: hidden; border: 0; border-left: 1px solid var(--border); border-radius: 0; background: var(--surface); color: var(--text); box-shadow: -16px 0 48px rgb(0 0 0 / 12%); }
  dialog[open] { animation: drawer-in 180ms ease-out; }
  dialog[open].closing { animation: drawer-out 180ms ease-in forwards; }
  @keyframes drawer-in {
    from { transform: translateX(100%); }
    to { transform: translateX(0); }
  }
  @keyframes drawer-out {
    from { transform: translateX(0); }
    to { transform: translateX(100%); }
  }
  @keyframes backdrop-in {
    from { opacity: 0; }
    to { opacity: 1; }
  }
  @keyframes backdrop-out {
    from { opacity: 1; }
    to { opacity: 0; }
  }
  dialog::backdrop { background: var(--backdrop); animation: backdrop-in 180ms ease-out; }
  dialog.closing::backdrop { animation: backdrop-out 180ms ease-in forwards; }
  form { display: flex; flex-direction: column; height: 100%; min-height: 0; }
  header { display: flex; align-items: center; justify-content: space-between; gap: 16px; flex-shrink: 0; padding: 24px; border-bottom: 1px solid var(--border); }
  .profile-identity { display: flex; align-items: center; gap: 12px; min-width: 0; }
  .profile-avatar { display: grid; place-items: center; flex-shrink: 0; width: 44px; height: 44px; border: 1px solid var(--border); background: var(--surface); }
  .profile-heading { min-width: 0; }
  .drawer-kicker { margin: 0 0 3px; color: var(--text-secondary); font-size: 11px; line-height: 1.4; }
  h1 { margin: 0; overflow-wrap: anywhere; font-size: 16px; line-height: 1.4; font-weight: 600; }
  .close-drawer { display: grid; place-items: center; flex-shrink: 0; width: 32px; height: 32px; min-height: 32px; padding: 6px; border: 0; background: transparent; color: var(--text-secondary); }
  .close-drawer:hover:enabled { background: var(--surface-hover); color: var(--text); }
  .close-drawer svg { display: block; width: 20px; height: 20px; }
  .settings-content { flex: 1; min-height: 0; padding: 20px 24px 24px; overflow: auto; overscroll-behavior: contain; }
  p { margin: 0; font-size: 13px; line-height: 1.6; }
  .running-state { margin-bottom: 24px; padding: 12px; border-left: 2px solid var(--border); background: var(--surface-hover); color: var(--text-secondary); }
  .running-state p { font-size: 12px; }
  .running-state button { margin-top: 12px; }
  fieldset { min-width: 0; margin: 0 0 22px; padding: 8px 16px 16px; border: 1px solid var(--border); }
  legend, h2 { display: flex; align-items: center; gap: 8px; margin: 0 0 4px; padding: 0 6px; color: var(--text); font-size: 13px; line-height: 1.4; font-weight: 600; }
  h2 { margin-bottom: 12px; padding: 0; }
  .section-icon { display: inline-block; flex-shrink: 0; width: 18px; height: 18px; background: currentColor; -webkit-mask: var(--section-icon) center / contain no-repeat; mask: var(--section-icon) center / contain no-repeat; }
  .connection-icon { --section-icon: url('/icons/settings-globe-simple.svg'); }
  .browser-icon { --section-icon: url('/icons/settings-browsers.svg'); }
  .diagnostics-icon { --section-icon: url('/icons/settings-pulse.svg'); }
  section[aria-labelledby='settings-diagnostics-title'] { padding: 16px; border: 1px solid var(--border); }
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
  footer .primary { flex: 1; }
  .error { margin-top: 14px; color: var(--danger); overflow-wrap: anywhere; }
  .diagnostic-result { margin-top: 16px; }
  dl { display: flex; flex-wrap: wrap; gap: 12px 24px; margin: 0; font-size: 13px; line-height: 1.5; }
  dt { color: var(--text-secondary); }
  dd { margin: 2px 0 0; overflow-wrap: anywhere; font-weight: 550; font-variant-numeric: tabular-nums; }
  @media (max-width: 520px) { dialog { width: 100vw; } header { padding: 16px; } .settings-content { padding: 16px; } footer { padding: 12px 16px; } .two-columns { grid-template-columns: minmax(0, 1fr); gap: 0; } }
  @media (prefers-reduced-motion: reduce) {
    dialog[open], dialog[open].closing, dialog::backdrop, dialog.closing::backdrop { animation: none; }
  }
</style>
