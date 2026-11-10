<script lang="ts">
  import { untrack } from 'svelte';
  import { attachAndroidMedia, playAndroidScreen } from './androidMedia';
  let { url, muted = true, name, onerror, onblocked }: { url: string; muted?: boolean; name: string; onerror: (message: string) => void; onblocked: (blocked: boolean) => void } = $props();
  let video: HTMLVideoElement;
  // Device polling replaces the parent object, even when this URL is unchanged.
  // Compare URL values before deciding whether to tear down the media session.
  const streamURL = $derived(url);
  $effect(() => {
    const source = streamURL;
    if (!video) return;
    // Setup reports playback state synchronously. Parent callbacks read device
    // and UI state, which must not become dependencies of this media session.
    return untrack(() => attachAndroidMedia(video, source, message => onerror(message), blocked => onblocked(blocked)));
  });
  $effect(() => {
    if (!video) return;
    video.muted = muted;
    if (!muted) void playAndroidScreen(video, blocked => onblocked(blocked), message => onerror(message));
  });
</script>
<!-- svelte-ignore a11y_media_has_caption -->
<video bind:this={video} autoplay playsinline muted={muted} aria-label={`Live Android screen for ${name}`}></video>
<style>
  video { display: block; width: 100%; height: 100%; object-fit: contain; pointer-events: none; background: #111; }
</style>
