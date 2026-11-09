<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { CreateProfileWithBrowser, DeleteProfile, ListBrowserEngines, ListProfiles, OpenProfile, UpdateProfile } from '../bindings/isolate/profileservice';
  import type { BrowserProfile, EngineInfo } from '../bindings/isolate/models';
  import WindowControls from './WindowControls.svelte';
  import ThemeToggle from './ThemeToggle.svelte';
  import ProfileSettings from './ProfileSettings.svelte';
  import BrowserLogo from './BrowserLogo.svelte';
  import EmptyWorkspace from './EmptyWorkspace.svelte';
  import { formatCreatedDate, formatExactTimestamp, formatLastOpened, formatTime } from './timestamps';
  let now = $state(Date.now());
  let controls: WindowControls;
  let settingsDialog: ProfileSettings;
  let message = $state('');
  let profileDialog: HTMLDialogElement;
  let profileName = $state('');
  let profileEngine = $state('chrome');
  let profileBrowserPath = $state('');
  let profileEngines = $state<EngineInfo[]>([]);
  let browserOptionsLoading = $state(false);
  let browserOptionsGeneration = 0;
  let profileToEdit = $state<BrowserProfile | null>(null);
  const browserSelectionChanged = $derived(Boolean(profileToEdit && (profileEngine !== (profileToEdit.settings?.engine || 'chrome') || (profileEngine === 'custom' && profileBrowserPath.trim() !== (profileToEdit.settings?.browserPath || '')))));
  const profileUnchanged = $derived(Boolean(profileToEdit && profileName.trim() === profileToEdit.name && !browserSelectionChanged));
  let profileDialogReturnID: string | null = null;
  let profiles = $state<BrowserProfile[]>([]);
  let loading = $state(true);
  let loadFailed = $state(false);
  let creating = $state(false);
  let profileError = $state('');
  let openingIDs = $state<string[]>([]);
  let announcement = $state('');
  let deleteDialog: HTMLDialogElement;
  let profileToDelete = $state<BrowserProfile | null>(null);
  let deleting = $state(false);
  let deleteError = $state('');

  function profileFailure(error: unknown, action: 'create' | 'rename' | 'open' | 'delete') {
    console.error(`Profile ${action} failed`, error);
    const detail = error instanceof Error ? error.message : String(error);
    if (detail.includes('A profile with this name already exists')) return 'A profile with this name already exists. Choose a different name.';
    if (detail.includes('Enter a profile name')) return 'Enter a profile name.';
    if (detail.includes('Isolate is closing')) return 'Isolate is closing. Reopen the app to try again.';
    if (detail.includes('Profile was not found')) return 'This profile is no longer available. Restart Isolate to refresh the list.';
    if (detail.includes('Close this profile browser before changing browsers')) return 'Close this profile’s browser before changing it. You can close it from Settings, then try again.';
    if (detail.includes('Choose a supported browser')) return 'Choose a supported browser.';
    if (detail.includes('Install Chrome or Chromium')) return 'Install Chrome or Chromium, then try opening the profile again.';
    if (detail.includes('Install Brave')) return 'Install Brave to open this profile, or choose Chrome in its settings.';
    if (detail.includes('Isolate will not substitute another browser')) return 'The selected browser is not installed. Install it, or select another browser in this profile’s settings.';
    if (detail.includes('browser executable')) return 'Enter the full path to an existing Chromium-based browser executable, without command-line arguments.';
    if (detail.includes('Firefox does not support the selected language or timezone settings')) return 'Update Firefox to use the selected language and timezone settings, or leave these fields blank in this profile’s settings.';
    if (detail.includes('keyring') || detail.includes('proxy password')) return 'Could not access the saved proxy password. Unlock your system keyring or update the password in profile settings.';
    if (detail.includes('Browser opened, but the last-open time could not be saved')) return 'The browser opened, but its activity time could not be saved. Check available disk space and folder permissions.';
    if (action === 'open') return 'Could not open the browser. Close any unresponsive browser windows and try again.';
    if (action === 'delete') return 'Could not finish deleting the profile. Check available disk space and folder permissions, then try again.';
    if (action === 'rename') return 'Could not save the profile. Check available disk space and folder permissions, then try again.';
    return 'Could not create the profile. Check available disk space and folder permissions, then try again.';
  }

  onMount(() => {
    const refreshTime = () => { now = Date.now(); };
    const clock = window.setInterval(refreshTime, 1000);
    window.addEventListener('focus', refreshTime);
    document.addEventListener('visibilitychange', refreshTime);
    void ListProfiles().then((saved) => profiles = saved ?? [])
      .catch(() => { loadFailed = true; message = 'Could not load profiles. Restart Isolate to try again.'; })
      .finally(() => loading = false);
    const handleShortcut = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && !event.altKey && !event.shiftKey && event.key.toLowerCase() === 'n') {
        event.preventDefault();
        if (!event.repeat && !loading && !document.querySelector('dialog[open]')) openProfileDialog();
      }
    };
    window.addEventListener('keydown', handleShortcut);
    return () => {
      window.clearInterval(clock);
      window.removeEventListener('focus', refreshTime);
      document.removeEventListener('visibilitychange', refreshTime);
      window.removeEventListener('keydown', handleShortcut);
    };
  });

  function openProfileDialog() {
    profileDialogReturnID = document.activeElement?.id === 'empty-create-profile' ? 'empty-create-profile' : null;
    profileToEdit = null;
    profileName = '';
    profileEngine = 'chrome';
    profileBrowserPath = '';
    profileError = '';
    profileDialog.showModal();
    void loadProfileBrowsers();
  }

  async function loadProfileBrowsers() {
    const generation = ++browserOptionsGeneration;
    browserOptionsLoading = true;
    profileEngines = [];
    try {
      const engines = await ListBrowserEngines();
      if (generation !== browserOptionsGeneration) return;
      profileEngines = engines ?? [];
      if (!profileToEdit && !profileEngines.find(item => item.id === profileEngine)?.available) {
        profileEngine = profileEngines.find(item => item.available && item.id !== 'custom')?.id || 'chrome';
      }
    } catch {
      if (generation === browserOptionsGeneration) profileError = 'Could not load browsers. Close this dialog and try again.';
    } finally {
      if (generation === browserOptionsGeneration) browserOptionsLoading = false;
    }
  }

  async function editProfile(profile: BrowserProfile) {
    profileDialogReturnID = `edit-${profile.id}`;
    profileToEdit = profile;
    profileName = profile.name;
    profileEngine = profile.settings?.engine || 'chrome';
    profileBrowserPath = profile.settings?.browserPath || '';
    profileError = '';
    await tick();
    profileDialog.showModal();
    void loadProfileBrowsers();
    document.querySelector<HTMLInputElement>('#profile-name')?.select();
  }

  function closeProfileDialog() {
    browserOptionsGeneration++;
    profileToEdit = null;
    if (profileDialogReturnID) document.getElementById(profileDialogReturnID)?.focus();
    else document.querySelector<HTMLButtonElement>('.add-profile')?.focus();
  }

  async function createProfile(event: SubmitEvent) {
    event.preventDefault();
    const name = profileName.trim();
    if (!name || creating || browserOptionsLoading || !profileEngines.length) return;
    creating = true;
    profileError = '';
    const edited = profileToEdit;
    try {
      const browserPath = profileEngine === 'custom' ? profileBrowserPath.trim() : '';
      const profile = edited ? await UpdateProfile(edited.id, name, profileEngine, browserPath) : await CreateProfileWithBrowser(name, profileEngine, browserPath);
      now = Date.now();
      profiles = edited ? profiles.map((item) => item.id === profile.id ? profile : item) : [profile, ...profiles];
      profileDialogReturnID = `${edited ? 'edit' : 'open'}-${profile.id}`;
      profileDialog.close();
      announcement = edited ? `Updated ${profile.name}` : `Created ${profile.name}`;
      await tick();
      if (!edited) document.querySelector('main')?.scrollTo({ top: 0 });
      document.getElementById(`${edited ? 'edit' : 'open'}-${profile.id}`)?.focus();
    } catch (error) {
      profileError = profileFailure(error, edited ? 'rename' : 'create');
    } finally {
      creating = false;
    }
  }

  async function openProfile(profile: BrowserProfile) {
    if (openingIDs.includes(profile.id)) return;
    openingIDs = [...openingIDs, profile.id];
    message = '';
    try {
      const updated = await OpenProfile(profile.id);
      now = Date.now();
      profiles = profiles.map((item) => item.id === updated.id ? updated : item);
      announcement = `Opened ${profile.name}`;
    } catch (error) {
      message = profileFailure(error, 'open');
    } finally {
      openingIDs = openingIDs.filter((id) => id !== profile.id);
    }
  }
  async function requestDelete(profile: BrowserProfile) {
    profileToDelete = profile;
    deleteError = '';
    await tick();
    deleteDialog.showModal();
  }

  async function deleteProfile(event: SubmitEvent) {
    event.preventDefault();
    const profile = profileToDelete;
    if (!profile || deleting) return;
    deleting = true;
    deleteError = '';
    try {
      await DeleteProfile(profile.id);
      profiles = profiles.filter((item) => item.id !== profile.id);
      deleteDialog.close();
      announcement = `Deleted ${profile.name}`;
      await tick();
      const nextButton = document.querySelector<HTMLButtonElement>('.open-profile') ?? document.querySelector<HTMLButtonElement>('.add-profile');
      nextButton?.focus();
    } catch (error) {
      deleteError = profileFailure(error, 'delete');
    } finally {
      deleting = false;
    }
  }

</script>

<div class="app-shell">
  <header class="title-bar">
    <div class="profile-actions">
      <button class="add-profile" type="button" disabled={loading} title="Add new profile (Ctrl+N / ⌘N)" aria-keyshortcuts="Control+n Meta+n" onclick={openProfileDialog}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>
        Add new profile
      </button>
    </div>
    <div class="window-caption" role="presentation" ondblclick={() => controls?.toggleMaximise()}></div>
    <ThemeToggle />
    <WindowControls bind:this={controls} onerror={(error) => message = error} />
  </header>
  <main class:workspace-empty={!loading && !profiles.length && !loadFailed} aria-label="Profile workspace" aria-busy={loading}>
    {#if loading}
      <p class="loading" role="status">Loading profiles…</p>
    {:else if profiles.length}
      <div class="profile-grid">
        {#each profiles as profile (profile.id)}
          {@const createdDate = formatCreatedDate(profile.createdAt, now)}
          {@const lastOpened = formatLastOpened(profile.lastOpenedAt, now)}
          <article class="profile-card" aria-labelledby={`profile-${profile.id}`}>
            <div class="profile-card-header">
              <div class="profile-brand" aria-hidden="true">
                <BrowserLogo engine={profile.settings?.engine} browser={profile.browser} />
              </div>
            </div>
            <div class="profile-details">
              <h2 id={`profile-${profile.id}`}>{profile.name}</h2>
              <div class="profile-badges">
                <span class="browser-name">{profile.browser === 'Chrome' ? 'Google Chrome' : profile.browser}</span>
                <span class="connection-name">{!profile.settings?.proxy?.mode || profile.settings.proxy.mode === 'direct' ? 'Direct connection' : `${profile.settings.proxy.mode.toUpperCase()} proxy`}</span>
              </div>
            </div>
            <div class="profile-footer">
              <div class="profile-dates">
              <div class="creation-date">
                {#if createdDate}
                  <span>Created</span>
                  <time datetime={profile.createdAt} title={formatExactTimestamp(profile.createdAt)} aria-label={formatExactTimestamp(profile.createdAt)}>
                    <span>{createdDate}</span>
                    <span class="time-part">{formatTime(profile.createdAt)}</span>
                  </time>
                {:else}
                  <span>Creation date unavailable</span>
                {/if}
              </div>
              <div class="last-opened">
                <span>Last opened</span>
                {#if lastOpened.label}
                  <time datetime={profile.lastOpenedAt} title={formatExactTimestamp(profile.lastOpenedAt)} aria-label={formatExactTimestamp(profile.lastOpenedAt)}>
                    <span>{lastOpened.label}</span>
                    {#if lastOpened.time}<span class="time-part">{lastOpened.time}</span>{/if}
                  </time>
                {:else}
                  <span class="no-open-date">Not recorded</span>
                {/if}
              </div>
              </div>
              <div class="profile-card-actions">
              <button id={`open-${profile.id}`} class="open-profile" type="button" disabled={openingIDs.includes(profile.id)} aria-label={`Open ${profile.name}`} onclick={() => openProfile(profile)}>
                {openingIDs.includes(profile.id) ? 'Opening…' : 'Open'}
              </button>
              <button id={`edit-${profile.id}`} class="edit-profile" type="button" disabled={openingIDs.includes(profile.id) || deleting} aria-label={`Edit ${profile.name}`} onclick={() => editProfile(profile)}>Edit</button>
              <button id={`settings-${profile.id}`} class="settings-profile" type="button" disabled={openingIDs.includes(profile.id) || deleting} aria-label={`Settings for ${profile.name}`} onclick={() => settingsDialog.open(profile)}>Settings</button>
              <button class="delete-profile" type="button" disabled={openingIDs.includes(profile.id) || deleting} aria-label={`Delete ${profile.name}`} onclick={() => requestDelete(profile)}>Delete</button>
              </div>
            </div>
          </article>
        {/each}
      </div>
    {:else if !loadFailed}
      <EmptyWorkspace oncreate={openProfileDialog} onerror={(error) => message = error} />
    {/if}
  </main>
  <span class="sr-only" role="status">{announcement}</span>
  {#if message}
    <div class="notice" role="status">
      <span>{message}</span>
      <button type="button" aria-label="Dismiss message" onclick={() => message = ''}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true"><path d="m6 6 12 12M6 18 18 6" /></svg>
      </button>
    </div>
  {/if}
</div>


<dialog bind:this={profileDialog} aria-labelledby="profile-dialog-title" onclose={closeProfileDialog} oncancel={(event) => { if (creating) event.preventDefault(); }}>
  <form onsubmit={createProfile} aria-busy={creating}>
    <h1 id="profile-dialog-title">{profileToEdit ? 'Edit profile' : 'Add new profile'}</h1>
    <p id="profile-name-hint" class="dialog-hint">{profileToEdit ? 'Update the name or browser. Your saved browser data stays with this profile.' : 'Each profile keeps its browser sessions, cookies, saved data, and downloads separate. Choose a name and browser to get started.'}</p>
    <label for="profile-name">Profile name</label>
    <input id="profile-name" name="profileName" type="text" bind:value={profileName} placeholder="e.g. Personal" autocomplete="off" disabled={creating} aria-invalid={Boolean(profileError)} aria-describedby={profileError ? 'profile-name-hint profile-error' : 'profile-name-hint'} required />
    <label class="browser-label" for="profile-engine">Browser</label>
    <select id="profile-engine" bind:value={profileEngine} disabled={creating || browserOptionsLoading} aria-describedby="profile-browser-hint" required>
      {#if browserOptionsLoading}<option value={profileEngine}>Loading browsers…</option>{/if}
      {#each profileEngines as available (available.id)}
        <option value={available.id} disabled={!available.available}>{available.name}{available.available ? '' : ' — not installed'}</option>
      {/each}
    </select>
    {#if profileEngine === 'custom'}
      <label class="browser-label" for="profile-browser-path">Browser executable</label>
      <input id="profile-browser-path" type="text" bind:value={profileBrowserPath} placeholder="Full path to the browser executable" autocomplete="off" disabled={creating} required />
    {/if}
    <p id="profile-browser-hint" class="browser-hint">{profileEngine === 'custom' ? 'Use a Chromium-based executable, without command-line arguments. Choose the Firefox entry for Firefox. ' : ''}Each browser keeps its own sessions; switching browsers does not transfer logins.{profileToEdit ? ' Close the running browser before changing it.' : ''}</p>
    {#if profileError}<p id="profile-error" class="profile-error" role="alert">{profileError}</p>{/if}
    <div class="dialog-actions">
      <button class="secondary" type="button" disabled={creating} onclick={() => profileDialog.close()}>Cancel</button>
      <button class="primary" type="submit" disabled={!profileName.trim() || creating || browserOptionsLoading || !profileEngines.length || profileUnchanged || (profileEngine === 'custom' && !profileBrowserPath.trim())}>{creating ? (profileToEdit ? 'Saving…' : 'Creating…') : (profileToEdit ? 'Save changes' : 'Create')}</button>
    </div>
  </form>
</dialog>

<dialog class="delete-dialog" bind:this={deleteDialog} aria-labelledby="delete-dialog-title" aria-describedby="delete-dialog-description" oncancel={(event) => { if (deleting) event.preventDefault(); }} onclose={() => profileToDelete = null}>
  <form onsubmit={deleteProfile} aria-busy={deleting}>
    <h1 id="delete-dialog-title">Delete profile?</h1>
    <p id="delete-dialog-description">Delete <strong>{profileToDelete?.name}</strong>? Its browser will close, and its cookies, sessions, saved data, and downloaded files will be permanently removed.</p>
    {#if deleteError}<p class="profile-error" role="alert">{deleteError}</p>{/if}
    <div class="dialog-actions">
      <button class="secondary" type="button" disabled={deleting} onclick={() => deleteDialog.close()}>Cancel</button>
      <button class="danger" type="submit" disabled={deleting}>{deleting ? 'Deleting…' : 'Delete profile'}</button>
    </div>
  </form>
</dialog>

<ProfileSettings bind:this={settingsDialog} onupdated={(updated) => { profiles = profiles.map((profile) => profile.id === updated.id ? updated : profile); announcement = `Updated settings for ${updated.name}`; }} />

<style>
  .app-shell { display: flex; flex-direction: column; height: 100%; border: 2px solid var(--accent); }
  .title-bar { display: flex; flex: 0 0 48px; min-width: 0; background: var(--surface); border-bottom: 1px solid var(--border); user-select: none; }
  .profile-actions { --wails-draggable: no-drag; display: flex; align-items: center; padding: 0 10px; }
  .add-profile { display: inline-flex; align-items: center; gap: 8px; min-height: 32px; padding: 0 12px; border: 1px solid var(--border); border-radius: 0; background: var(--surface); color: var(--text); font-size: 13px; font-weight: 550; white-space: nowrap; cursor: pointer; }
  .add-profile:hover { background: var(--surface-hover); }
  .add-profile:active { background: var(--surface-active); }
  .add-profile svg { width: 16px; height: 16px; }
  .window-caption { --wails-draggable: drag; flex: 1; min-width: 32px; }
  main { flex: 1; min-height: 0; overflow: auto; padding: 24px; }
  main.workspace-empty { display: grid; place-items: center; }
  .profile-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(400px, 100%), 1fr)); gap: 16px; align-items: start; }
  .profile-card { display: grid; grid-template-columns: 40px minmax(0, 1fr); align-content: space-between; align-items: start; gap: 20px 16px; min-width: 0; min-height: 180px; padding: 20px; border-radius: 0; background: var(--surface); box-shadow: var(--card-shadow); }
  .profile-card-header { display: flex; align-items: center; justify-content: space-between; gap: 16px; width: 100%; margin: 0; }
  .edit-profile, .settings-profile, .delete-profile { min-width: 64px; min-height: 34px; padding: 8px 16px; border: 1px solid var(--border); border-radius: 0; background: transparent; color: var(--text-secondary); font-size: 12px; line-height: 1.5; font-weight: 550; cursor: pointer; }
  .edit-profile:hover:enabled, .settings-profile:hover:enabled { border-color: var(--text-secondary); background: var(--surface-hover); color: var(--text); }
  .delete-profile:hover:enabled { border-color: var(--danger); color: var(--danger); background: var(--danger-bg); }
  .profile-brand { display: grid; place-items: center; flex-shrink: 0; width: 40px; height: 40px; border: 1px solid var(--border); border-radius: 0; background: var(--surface); color: var(--text); }
  .profile-details { min-width: 0; width: 100%; margin: 0; }
  h2 { max-width: 100%; margin: 0 0 8px; overflow-wrap: anywhere; font-size: 18px; line-height: 1.4; font-weight: 600; letter-spacing: .02em; text-transform: uppercase; }
  .browser-name { display: inline-block; max-width: 100%; padding: 4px 8px; overflow-wrap: anywhere; border-radius: 0; background: var(--surface-hover); color: var(--text); font-size: 11px; line-height: 1.5; font-weight: 550; }
  .profile-badges { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
  .connection-name { color: var(--text-secondary); font-size: 12px; line-height: 1.5; }
  .profile-footer { grid-column: 1 / -1; display: flex; flex-wrap: wrap; align-items: flex-end; gap: 12px 16px; width: 100%; margin-top: auto; padding-top: 14px; border-top: 1px solid var(--border); }
  .profile-dates { flex: 1 0 180px; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; min-width: 0; }
  .profile-card-actions { display: flex; align-items: center; justify-content: flex-end; flex-wrap: wrap; gap: 8px; margin-left: auto; }
  .creation-date, .last-opened { display: flex; flex-direction: column; gap: 2px; min-width: 0; color: var(--text-secondary); font-size: 12px; line-height: 1.5; }
  .no-open-date { color: var(--text); font-size: 13px; }
  .time-part { white-space: nowrap; }
  time { display: flex; flex-direction: column; overflow-wrap: anywhere; color: var(--text); font-size: 13px; font-weight: 550; }
  .open-profile { display: inline-flex; align-items: center; justify-content: center; flex-shrink: 0; min-width: 64px; min-height: 34px; padding: 8px 16px; border: 0; border-radius: 0; background: var(--text); color: var(--surface); font-size: 12px; line-height: 1.5; font-weight: 550; cursor: pointer; }
  .open-profile:hover:enabled { background: var(--text-secondary); }
  .dialog-hint { margin: -12px 0 20px; color: var(--text-secondary); font-size: 13px; line-height: 1.6; text-wrap: pretty; }
  .loading { margin: 0; font-size: 13px; line-height: 1.5; color: var(--text-secondary); }
  button:disabled { cursor: default; opacity: .5; }
  .profile-error { margin: 12px 0 0; font-size: 13px; line-height: 1.5; overflow-wrap: anywhere; }
  .sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; border: 0; }
  .notice { position: fixed; bottom: 20px; left: 20px; right: 20px; max-width: 400px; display: flex; align-items: center; gap: 16px; padding: 12px 16px; border: 1px solid var(--border); border-radius: 0; background: var(--surface); font-size: 13px; line-height: 1.5; overflow-wrap: anywhere; }
  .notice button { display: grid; place-items: center; flex-shrink: 0; width: 28px; height: 28px; margin-left: auto; padding: 0; border: 0; border-radius: 0; background: transparent; color: var(--text-secondary); cursor: pointer; }
  .notice button:hover { background: var(--surface-hover); }
  .notice svg { width: 16px; height: 16px; }
  dialog { --wails-draggable: no-drag; width: min(420px, calc(100% - 40px)); max-height: calc(100% - 40px); margin: auto; padding: 24px; overflow: auto; border: 1px solid var(--border); border-radius: 0; background: var(--surface); color: var(--text); }
  dialog::backdrop { background: var(--backdrop); }
  h1 { margin: 0 0 24px; font-size: 18px; line-height: 1.4; font-weight: 600; }
  label { display: block; margin-bottom: 8px; font-size: 13px; line-height: 1.5; font-weight: 550; }
  input, select { width: 100%; min-height: 40px; padding: 9px 12px; border: 1px solid var(--input-border); border-radius: 0; background: var(--surface); color: var(--text); caret-color: var(--accent); font: inherit; font-size: 13px; line-height: 1.5; }
  .browser-label { margin-top: 16px; }
  .browser-hint { margin: 8px 0 0; color: var(--text-secondary); font-size: 12px; line-height: 1.6; }
  input::placeholder { color: var(--text-secondary); }
  #profile-name { text-transform: uppercase; font-weight: 550; }
  #profile-name::placeholder { text-transform: none; font-weight: 400; }
  input:focus-visible, select:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }
  .dialog-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 24px; }
  .dialog-actions button { min-height: 36px; padding: 8px 16px; border: 1px solid var(--border); border-radius: 0; font-size: 13px; line-height: 1.4; font-weight: 550; cursor: pointer; }
  .secondary { background: var(--surface); color: var(--text); }
  .secondary:hover { background: var(--surface-hover); }
  .primary { background: var(--accent); border-color: var(--accent); color: var(--on-accent); }
  .primary:hover:enabled { background: var(--accent-hover); }
  #delete-dialog-description { margin: 0; font-size: 13px; line-height: 1.7; color: var(--text-secondary); overflow-wrap: anywhere; }
  #delete-dialog-description strong { color: var(--text); text-transform: uppercase; font-weight: 600; }
  .dialog-actions .danger { border-color: var(--danger-solid); background: var(--danger-solid); color: var(--on-danger); }
  .dialog-actions .danger:hover:enabled { background: #b33533; border-color: #b33533; }
  .primary:disabled { opacity: .5; cursor: default; }
</style>
