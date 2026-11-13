<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import {
    GetCurrentState,
    GetWindowSize,
    MinimizeToTray,
    NextTrack,
    PlayPause,
    PrevTrack,
    QuitApp,
    RefreshLyrics,
    ResizeWindow,
    SaveSettings,
    SearchLyrics,
    SeekTo,
    SetClickThrough,
    SetWindowMode,
    UseLyricsCandidate,
  } from '../wailsjs/go/main/App.js';
  import { main } from '../wailsjs/go/models';
  import { BrowserOpenURL, EventsOff, EventsOn } from '../wailsjs/runtime/runtime.js';
  import { extractAmbientPalette } from './lib/palette';
  import type {
    AmbientPalette,
    LyricsPayload,
    LyricsSearchResult,
    Settings,
    ThemeMode,
    TrackState,
    WindowMode,
  } from './lib/types';

  let track = $state<TrackState>({
    running: false,
    playing: false,
    trackId: '',
    trackKey: '',
    title: '',
    artist: '',
    album: '',
    durationMs: 0,
    positionMs: 0,
    artworkUrl: '',
    updatedAtMs: 0,
    source: '',
  });

  let lyrics = $state<LyricsPayload | null>(null);
  let lyricsLoading = $state(false);
  let platform = $state('darwin');

  let settings = $state<Settings>({
    mode: 'card',
    theme: 'adaptive',
    fontScale: 1,
    opacity: 0.82,
    showAlbumArt: true,
    showNextInPill: true,
    globalOffsetMs: 0,
    trackOffsets: {},
    modeSizes: {},
    locked: false,
  });

  let palette = $state<AmbientPalette>({
    primary: '42, 62, 118',
    secondary: '18, 86, 82',
    accent: '126, 214, 255',
  });

  // High-precision playback clock synced from backend + rAF interpolation
  let basePosMs = $state(0);
  let basePerfMs = $state(0);
  let livePosMs = $state(0);

  // UI panels & micro-interactions
  let activeDrawer = $state<'none' | 'settings' | 'search' | 'about'>('none');
  let searchQuery = $state('');
  let searchResults = $state<LyricsSearchResult[]>([]);
  let searching = $state(false);
  let lockToast = $state(false);
  let lockPeek = $state(false);
  let lockToastTimer: number | null = null;
  let userScrollingUntil = 0;

  let scrollContainer: HTMLDivElement | null = $state(null);
  let lineElements = $state<Array<HTMLElement | null>>([]);
  let rafClockId: number | null = null;
  let rafScrollId: number | null = null;
  let targetScrollTop = 0;

  // Interactive edge/corner resize state
  type ResizeAxis = 'se' | 'e' | 's';
  let resizingAxis: ResizeAxis | null = null;
  let resizeStartX = 0;
  let resizeStartY = 0;
  let resizeStartW = 460;
  let resizeStartH = 316;
  let pendingResizeW = 460;
  let pendingResizeH = 316;
  let rafResizeId: number | null = null;

  const trackOffsetMs = $derived.by(() => {
    if (!track.trackKey || !settings.trackOffsets) return 0;
    return settings.trackOffsets[track.trackKey] ?? 0;
  });

  const effectivePosMs = $derived(
    Math.max(0, livePosMs + settings.globalOffsetMs + trackOffsetMs)
  );

  const progressRatio = $derived.by(() => {
    if (!track.durationMs || track.durationMs <= 0) return 0;
    return Math.max(0, Math.min(1, livePosMs / track.durationMs));
  });

  const activeLineIndex = $derived.by(() => {
    if (!lyrics || !lyrics.synced || !lyrics.lines?.length) return -1;
    const pos = effectivePosMs + 60;
    let idx = -1;
    for (let i = 0; i < lyrics.lines.length; i++) {
      if (lyrics.lines[i].startMs <= pos) {
        idx = i;
      } else {
        break;
      }
    }
    return idx;
  });

  const inInterlude = $derived.by(() => {
    if (!lyrics || !lyrics.synced || !lyrics.lines?.length) return false;
    const lines = lyrics.lines;
    if (activeLineIndex < 0) {
      return lines[0].startMs - effectivePosMs > 2200;
    }
    const cur = lines[activeLineIndex];
    const next = lines[activeLineIndex + 1];
    if (next && effectivePosMs > cur.endMs + 400 && next.startMs - effectivePosMs > 3200) {
      return true;
    }
    return false;
  });

  const pillCurrentLine = $derived.by(() => {
    if (!lyrics || !lyrics.lines?.length) return null;
    if (activeLineIndex >= 0) return lyrics.lines[activeLineIndex];
    return lyrics.lines[0];
  });

  const pillNextLine = $derived.by(() => {
    if (!lyrics || !lyrics.lines?.length) return null;
    const nextIdx = activeLineIndex < 0 ? 1 : activeLineIndex + 1;
    return nextIdx < lyrics.lines.length ? lyrics.lines[nextIdx] : null;
  });

  function syncClockFromTrack(next: TrackState) {
    const perfNow = performance.now();
    const isSameTrack = next.trackKey && next.trackKey === track.trackKey;
    const drift = Math.abs(next.positionMs - livePosMs);

    if (!isSameTrack || !next.playing || drift > 450) {
      basePosMs = next.positionMs;
      basePerfMs = perfNow;
      livePosMs = next.positionMs;
    } else if (drift > 140) {
      basePosMs = livePosMs + (next.positionMs - livePosMs) * 0.45;
      basePerfMs = perfNow;
    }
  }

  function startClockLoop() {
    if (rafClockId !== null) return;
    const step = (now: number) => {
      if (!track.playing) {
        rafClockId = null;
        return;
      }
      const nextPos = basePosMs + (now - basePerfMs);
      livePosMs =
        track.durationMs > 0 ? Math.min(track.durationMs, nextPos) : nextPos;
      rafClockId = requestAnimationFrame(step);
    };
    rafClockId = requestAnimationFrame(step);
  }

  function stopClockLoop() {
    if (rafClockId !== null) {
      cancelAnimationFrame(rafClockId);
      rafClockId = null;
    }
  }

  function smoothScrollToActive(instant = false) {
    if (!scrollContainer || activeLineIndex < 0 || settings.mode === 'pill') return;
    if (performance.now() < userScrollingUntil && !instant) return;

    const el = lineElements[activeLineIndex];
    if (!el) return;

    const containerRect = scrollContainer.getBoundingClientRect();
    const elOffset = el.offsetTop - scrollContainer.offsetTop;
    const desired =
      elOffset - containerRect.height * 0.42 + el.offsetHeight * 0.5;
    targetScrollTop = Math.max(
      0,
      Math.min(scrollContainer.scrollHeight - containerRect.height, desired)
    );

    if (instant) {
      scrollContainer.scrollTop = targetScrollTop;
      return;
    }

    if (rafScrollId !== null) return;
    const animateScroll = () => {
      if (!scrollContainer) {
        rafScrollId = null;
        return;
      }
      const diff = targetScrollTop - scrollContainer.scrollTop;
      if (Math.abs(diff) < 0.75) {
        scrollContainer.scrollTop = targetScrollTop;
        rafScrollId = null;
        return;
      }
      scrollContainer.scrollTop += diff * 0.14;
      rafScrollId = requestAnimationFrame(animateScroll);
    };
    rafScrollId = requestAnimationFrame(animateScroll);
  }

  $effect(() => {
    const idx = activeLineIndex;
    const mode = settings.mode;
    if (idx >= 0 && mode !== 'pill') {
      tick().then(() => smoothScrollToActive(false));
    }
  });

  $effect(() => {
    const art = track.artworkUrl;
    if (art) {
      extractAmbientPalette(art).then((p) => {
        palette = p;
      });
    } else {
      palette = {
        primary: '42, 62, 118',
        secondary: '18, 86, 82',
        accent: '126, 214, 255',
      };
    }
  });

  function applyTrackEvent(next: TrackState) {
    const trackChanged = next.trackKey !== track.trackKey;
    syncClockFromTrack(next);
    track = next;
    if (trackChanged) {
      lineElements = [];
      if (scrollContainer) scrollContainer.scrollTop = 0;
      if (activeDrawer === 'search') {
        searchQuery = `${next.artist} ${next.title}`.trim();
      }
    }
    if (next.playing) startClockLoop();
    else stopClockLoop();
  }

  function handleKeydown(e: KeyboardEvent) {
    const tag = (e.target as HTMLElement | null)?.tagName;
    if (tag === 'INPUT' || tag === 'TEXTAREA') return;
    if (e.code === 'Space') {
      e.preventDefault();
      PlayPause();
    } else if (e.code === 'ArrowRight' && track.durationMs > 0) {
      e.preventDefault();
      SeekTo(Math.min(track.durationMs, livePosMs + 5000));
    } else if (e.code === 'ArrowLeft' && track.durationMs > 0) {
      e.preventDefault();
      SeekTo(Math.max(0, livePosMs - 5000));
    }
  }

  onMount(async () => {
    const snap = await GetCurrentState();
    platform = snap.platform || 'darwin';
    if (snap.settings) {
      settings = {
        ...settings,
        ...(snap.settings as unknown as Settings),
        trackOffsets: snap.settings.trackOffsets || {},
        modeSizes: snap.settings.modeSizes || {},
      };
    }
    if (snap.track) {
      applyTrackEvent(snap.track as unknown as TrackState);
    }
    if (snap.lyrics) {
      lyrics = snap.lyrics as unknown as LyricsPayload;
    }

    EventsOn('spotify:state', (next: TrackState) => {
      applyTrackEvent(next);
    });

    EventsOn('lyrics:loading', (key: string) => {
      if (!track.trackKey || key === track.trackKey) {
        lyricsLoading = true;
      }
    });

    EventsOn('lyrics:loaded', (payload: LyricsPayload) => {
      lyricsLoading = false;
      lyrics = payload;
      lineElements = [];
      tick().then(() => smoothScrollToActive(true));
    });

    EventsOn('settings:updated', (next: Settings) => {
      settings = {
        ...settings,
        ...next,
        trackOffsets: next.trackOffsets || settings.trackOffsets || {},
        modeSizes: next.modeSizes || settings.modeSizes || {},
      };
      tick().then(() => smoothScrollToActive(true));
    });

    EventsOn('window:lock', (locked: boolean) => {
      settings.locked = locked;
      if (!locked) lockPeek = false;
      if (locked) {
        activeDrawer = 'none';
        triggerLockToast();
      }
    });

    EventsOn('window:lock-peek', (peeking: boolean) => {
      lockPeek = peeking;
    });

    window.addEventListener('keydown', handleKeydown);
  });

  onDestroy(() => {
    stopClockLoop();
    if (rafScrollId !== null) cancelAnimationFrame(rafScrollId);
    if (rafResizeId !== null) cancelAnimationFrame(rafResizeId);
    if (lockToastTimer !== null) clearTimeout(lockToastTimer);
    window.removeEventListener('keydown', handleKeydown);
    EventsOff('spotify:state');
    EventsOff('lyrics:loading');
    EventsOff('lyrics:loaded');
    EventsOff('settings:updated');
    EventsOff('window:lock');
    EventsOff('window:lock-peek');
  });

  function triggerLockToast() {
    lockToast = true;
    if (lockToastTimer !== null) clearTimeout(lockToastTimer);
    lockToastTimer = window.setTimeout(() => {
      lockToast = false;
    }, 2400);
  }

  function persistSettings(patch: Partial<Settings>) {
    settings = { ...settings, ...patch };
    const payload = main.Settings.createFrom({
      ...settings,
      trackOffsets: { ...(settings.trackOffsets || {}) },
      modeSizes: { ...(settings.modeSizes || {}) },
    });
    SaveSettings(payload).catch(() => {});
  }

  async function switchMode(mode: WindowMode) {
    activeDrawer = 'none';
    const updated = await SetWindowMode(mode);
    settings = {
      ...settings,
      ...(updated as unknown as Settings),
      trackOffsets: updated.trackOffsets || settings.trackOffsets || {},
      modeSizes: updated.modeSizes || settings.modeSizes || {},
    };
    tick().then(() => smoothScrollToActive(true));
  }

  function cycleMode() {
    const order: WindowMode[] = ['card', 'full', 'pill'];
    const next = order[(order.indexOf(settings.mode) + 1) % order.length];
    switchMode(next);
  }

  function handleHeaderDblClick(e: MouseEvent) {
    if ((e.target as HTMLElement).closest('.no-drag')) return;
    switchMode(settings.mode === 'full' ? 'card' : 'full');
  }

  function setTheme(theme: ThemeMode) {
    persistSettings({ theme });
  }

  function adjustTrackOffset(deltaMs: number) {
    if (!track.trackKey) return;
    const map = { ...(settings.trackOffsets || {}) };
    const nextVal = Math.max(-10000, Math.min(10000, (map[track.trackKey] ?? 0) + deltaMs));
    if (nextVal === 0) delete map[track.trackKey];
    else map[track.trackKey] = nextVal;
    persistSettings({ trackOffsets: map });
  }

  function resetTrackOffset() {
    if (!track.trackKey || !settings.trackOffsets) return;
    const map = { ...settings.trackOffsets };
    delete map[track.trackKey];
    persistSettings({ trackOffsets: map });
  }

  async function toggleLock() {
    const next = !settings.locked;
    await SetClickThrough(next);
  }

  // Interactive Corner & Edge Resize
  async function startResize(axis: ResizeAxis, e: PointerEvent) {
    if (settings.locked) return;
    e.preventDefault();
    e.stopPropagation();
    const target = e.currentTarget as HTMLElement;
    target.setPointerCapture(e.pointerId);

    resizingAxis = axis;
    resizeStartX = e.screenX;
    resizeStartY = e.screenY;

    try {
      const sz = await GetWindowSize();
      resizeStartW = sz.width || window.innerWidth;
      resizeStartH = sz.height || window.innerHeight;
    } catch {
      resizeStartW = window.innerWidth;
      resizeStartH = window.innerHeight;
    }
    pendingResizeW = resizeStartW;
    pendingResizeH = resizeStartH;
  }

  function moveResize(e: PointerEvent) {
    if (!resizingAxis) return;
    const dx = e.screenX - resizeStartX;
    const dy = e.screenY - resizeStartY;

    const nextW = resizingAxis === 's' ? resizeStartW : Math.max(340, Math.min(1200, Math.round(resizeStartW + dx)));
    const nextH = resizingAxis === 'e' ? resizeStartH : Math.max(96, Math.min(1000, Math.round(resizeStartH + dy)));
    pendingResizeW = nextW;
    pendingResizeH = nextH;

    if (rafResizeId === null) {
      rafResizeId = requestAnimationFrame(() => {
        rafResizeId = null;
        ResizeWindow(pendingResizeW, pendingResizeH, false).catch(() => {});
      });
    }
  }

  async function endResize(e: PointerEvent) {
    if (!resizingAxis) return;
    const target = e.currentTarget as HTMLElement;
    if (target.hasPointerCapture(e.pointerId)) {
      target.releasePointerCapture(e.pointerId);
    }
    resizingAxis = null;
    if (rafResizeId !== null) {
      cancelAnimationFrame(rafResizeId);
      rafResizeId = null;
    }
    await ResizeWindow(pendingResizeW, pendingResizeH, true).catch(() => {});
    const map = { ...(settings.modeSizes || {}) };
    map[settings.mode] = { width: pendingResizeW, height: pendingResizeH };
    settings = { ...settings, modeSizes: map };
    tick().then(() => smoothScrollToActive(true));
  }

  function resetCurrentModeSize() {
    const map = { ...(settings.modeSizes || {}) };
    delete map[settings.mode];
    settings = { ...settings, modeSizes: map };
    persistSettings({ modeSizes: map });
    SetWindowMode(settings.mode);
  }

  function handleProgressClick(e: MouseEvent) {
    if (!track.durationMs) return;
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
    const ratio = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
    const targetMs = Math.round(ratio * track.durationMs);
    basePosMs = targetMs;
    basePerfMs = performance.now();
    livePosMs = targetMs;
    SeekTo(targetMs);
  }

  function handleLineClick(startMs: number) {
    if (!lyrics?.synced) return;
    const target = Math.max(0, startMs - settings.globalOffsetMs - trackOffsetMs);
    basePosMs = target;
    basePerfMs = performance.now();
    livePosMs = target;
    userScrollingUntil = 0;
    SeekTo(target);
  }

  function toggleDrawer(name: 'settings' | 'search' | 'about') {
    if (activeDrawer === name) {
      activeDrawer = 'none';
      return;
    }
    activeDrawer = name;
    if (name === 'search') {
      searchQuery = `${track.artist} ${track.title}`.trim();
      if (searchQuery) runSearch();
    }
  }

  async function runSearch() {
    const q = searchQuery.trim();
    if (!q) return;
    searching = true;
    try {
      const res = await SearchLyrics(q);
      searchResults = (res || []) as unknown as LyricsSearchResult[];
    } catch {
      searchResults = [];
    } finally {
      searching = false;
    }
  }

  async function selectCandidate(id: number) {
    if (!track.trackKey) return;
    lyricsLoading = true;
    activeDrawer = 'none';
    try {
      const res = await UseLyricsCandidate(track.trackKey, id);
      lyrics = res as unknown as LyricsPayload;
    } finally {
      lyricsLoading = false;
    }
  }

  function wordProgress(startMs: number, endMs: number): number {
    const now = effectivePosMs + 50;
    if (now <= startMs) return 0;
    if (now >= endMs) return 1;
    const span = Math.max(90, endMs - startMs);
    return (now - startMs) / span;
  }

  function formatTime(ms: number): string {
    const totalSec = Math.max(0, Math.floor(ms / 1000));
    const m = Math.floor(totalSec / 60);
    const s = totalSec % 60;
    return `${m}:${s.toString().padStart(2, '0')}`;
  }

  function formatDurationSec(sec: number): string {
    return formatTime(sec * 1000);
  }
</script>

<div
  class="shell mode-{settings.mode} theme-{settings.theme}"
  class:is-playing={track.playing}
  class:is-locked={settings.locked}
  class:drawer-open={activeDrawer !== 'none'}
  style="
    --p-rgb: {palette.primary};
    --s-rgb: {palette.secondary};
    --a-rgb: {palette.accent};
    --glass-alpha: {settings.opacity};
    --font-scale: {settings.fontScale};
  "
>
  <!-- Ambient Background Layer -->
  <div class="ambient-bg" aria-hidden="true">
    <div class="orb orb-a"></div>
    <div class="orb orb-b"></div>
    <div class="glass-scrim"></div>
    <div class="specular-sheen"></div>
  </div>

  <!-- Lock Reminder Toast -->
  {#if lockToast}
    <div class="lock-toast">
      <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2">
        <rect x="3" y="11" width="18" height="11" rx="2" />
        <path d="M7 11V7a5 5 0 0 1 10 0v4" />
      </svg>
      <span>
        {platform === 'darwin'
          ? 'Click-through locked · Hold ⌥ Option or use Menu Bar ♫ to unlock'
          : 'Click-through locked · Hold Alt or press Ctrl+Alt+L to unlock'}
      </span>
    </div>
  {/if}

  <!-- Temporary Interactive Unlock Pill while holding Option/Alt -->
  {#if settings.locked && lockPeek}
    <div class="unlock-peek no-drag">
      <button class="unlock-pill" onclick={toggleLock} title="Unlock click-through overlay">
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2">
          <rect x="4" y="11" width="16" height="10" rx="2" />
          <path d="M8 11V7a4 4 0 0 1 7.6-1.7" />
        </svg>
        <span>Click to Unlock</span>
      </button>
    </div>
  {/if}

  <!-- Top Drag & Control Header -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <header class="header drag-region" ondblclick={handleHeaderDblClick}>
    <div class="track-meta">
      {#if settings.showAlbumArt}
        <div class="art-wrap">
          {#if track.artworkUrl}
            <img class="art-img" src={track.artworkUrl} alt={track.album || track.title} />
          {:else}
            <div class="art-placeholder">
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M9 18V5l12-2v13" />
                <circle cx="6" cy="18" r="3" />
                <circle cx="18" cy="16" r="3" />
              </svg>
            </div>
          {/if}
          {#if track.playing}
            <div class="eq-badge" aria-hidden="true">
              <span></span><span></span><span></span>
            </div>
          {/if}
        </div>
      {/if}

      <div class="track-text">
        {#if !track.running}
          <div class="track-title muted">Spotify not detected</div>
          <div class="track-artist">Open Spotify & play any track</div>
        {:else if !track.title}
          <div class="track-title muted">Nothing playing</div>
          <div class="track-artist">Waiting for Spotify playback…</div>
        {:else}
          <div class="track-title" title={track.title}>{track.title}</div>
          <div class="track-artist" title={track.artist}>
            <span class="artist-name">{track.artist}</span>
            {#if lyrics?.wordSynced}
              <span class="sync-dot" title="Word-by-word sync">✦</span>
            {/if}
            {#if track.durationMs > 0 && settings.mode !== 'pill'}
              <span class="time-chip">{formatTime(livePosMs)} / {formatTime(track.durationMs)}</span>
            {/if}
          </div>
        {/if}
      </div>
    </div>

    <!-- Hover Floating Controls -->
    {#if !settings.locked}
      <div class="controls no-drag">
        <div class="pill-group">
          <button class="icon-btn" onclick={() => PrevTrack()} title="Previous track" aria-label="Previous track">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor">
              <path d="M6 6h2v12H6zm3.5 6 8.5 6V6z" />
            </svg>
          </button>
          <button class="icon-btn play-btn" onclick={() => PlayPause()} title={track.playing ? 'Pause' : 'Play'} aria-label={track.playing ? 'Pause' : 'Play'}>
            {#if track.playing}
              <svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor">
                <rect x="6" y="5" width="4" height="14" rx="1.2" />
                <rect x="14" y="5" width="4" height="14" rx="1.2" />
              </svg>
            {:else}
              <svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor">
                <path d="M7 5.2v13.6c0 .8.9 1.3 1.6.9l10.6-6.8c.6-.4.6-1.4 0-1.8L8.6 4.3C7.9 3.9 7 4.4 7 5.2z" />
              </svg>
            {/if}
          </button>
          <button class="icon-btn" onclick={() => NextTrack()} title="Next track" aria-label="Next track">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor">
              <path d="M16 6h2v12h-2zm-10 12 8.5-6L6 6z" />
            </svg>
          </button>
        </div>

        <div class="pill-group">
          <button
            class="icon-btn"
            onclick={cycleMode}
            title={`Switch layout (${settings.mode})`}
            aria-label="Switch layout mode"
          >
            {#if settings.mode === 'pill'}
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <rect x="3" y="8" width="18" height="8" rx="4" />
              </svg>
            {:else if settings.mode === 'card'}
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <rect x="4" y="5" width="16" height="14" rx="3" />
              </svg>
            {:else}
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <rect x="5" y="3" width="14" height="18" rx="3" />
                <line x1="9" y1="8" x2="15" y2="8" />
                <line x1="9" y1="12" x2="15" y2="12" />
              </svg>
            {/if}
          </button>

          <button
            class="icon-btn"
            class:active={activeDrawer === 'search'}
            onclick={() => toggleDrawer('search')}
            title="Match or search lyrics"
            aria-label="Search lyrics"
          >
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.1">
              <circle cx="11" cy="11" r="7" />
              <path d="m20 20-3.5-3.5" />
            </svg>
          </button>

          <button
            class="icon-btn"
            class:active={activeDrawer === 'settings'}
            onclick={() => toggleDrawer('settings')}
            title="Appearance & timing"
            aria-label="Settings"
          >
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <line x1="4" y1="7" x2="20" y2="7" />
              <circle cx="9" cy="7" r="2" fill="currentColor" />
              <line x1="4" y1="17" x2="20" y2="17" />
              <circle cx="15" cy="17" r="2" fill="currentColor" />
            </svg>
          </button>

          <button
            class="icon-btn"
            class:active={activeDrawer === 'about'}
            onclick={() => toggleDrawer('about')}
            title="About Spofly"
            aria-label="About Spofly"
          >
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="12" cy="12" r="9" />
              <line x1="12" y1="11" x2="12" y2="16" stroke-linecap="round" />
              <circle cx="12" cy="7.7" r="0.9" fill="currentColor" stroke="none" />
            </svg>
          </button>

          <button
            class="icon-btn"
            onclick={toggleLock}
            title="Lock click-through overlay"
            aria-label="Lock click-through"
          >
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="4" y="11" width="16" height="10" rx="2" />
              <path d="M8 11V7a4 4 0 0 1 7.6-1.7" />
            </svg>
          </button>

          <button
            class="icon-btn"
            onclick={() => MinimizeToTray()}
            title="Hide floating window"
            aria-label="Hide window"
          >
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2">
              <line x1="6" y1="12" x2="18" y2="12" />
            </svg>
          </button>
        </div>
      </div>
    {/if}
  </header>

  <!-- Popover Drawers (Settings / Search) -->
  {#if activeDrawer !== 'none' && !settings.locked}
    <div class="drawer no-drag">
      {#if activeDrawer === 'settings'}
        <div class="drawer-section">
          <div class="drawer-label">Layout</div>
          <div class="seg-control">
            {#each (['pill', 'card', 'full'] as const) as m}
              <button
                class="seg-btn"
                class:selected={settings.mode === m}
                onclick={() => switchMode(m)}
              >
                {m === 'pill' ? 'Pill' : m === 'card' ? 'Card' : 'Full'}
              </button>
            {/each}
          </div>
        </div>

        <div class="drawer-section">
          <div class="drawer-label">Surface Theme</div>
          <div class="seg-control">
            {#each (['adaptive', 'obsidian', 'minimal'] as const) as t}
              <button
                class="seg-btn"
                class:selected={settings.theme === t}
                onclick={() => setTheme(t)}
              >
                {t === 'adaptive' ? 'Adaptive' : t === 'obsidian' ? 'Obsidian' : 'Minimal'}
              </button>
            {/each}
          </div>
        </div>

        <div class="drawer-row">
          <div class="slider-group">
            <div class="drawer-label">Glass <span>{Math.round(settings.opacity * 100)}%</span></div>
            <input
              type="range"
              min="0.30"
              max="0.98"
              step="0.02"
              value={settings.opacity}
              oninput={(e) => persistSettings({ opacity: parseFloat((e.target as HTMLInputElement).value) })}
            />
          </div>
          <div class="slider-group">
            <div class="drawer-label">Text Size <span>{Math.round(settings.fontScale * 100)}%</span></div>
            <input
              type="range"
              min="0.82"
              max="1.34"
              step="0.02"
              value={settings.fontScale}
              oninput={(e) => persistSettings({ fontScale: parseFloat((e.target as HTMLInputElement).value) })}
            />
          </div>
        </div>

        <div class="drawer-row between">
          <div class="drawer-label">
            Sync Offset
            <span class="offset-val">{trackOffsetMs >= 0 ? '+' : ''}{(trackOffsetMs / 1000).toFixed(1)}s</span>
          </div>
          <div class="nudge-group">
            <button class="chip-btn" onclick={() => adjustTrackOffset(-200)}>-0.2s</button>
            <button class="chip-btn" onclick={() => adjustTrackOffset(-100)}>-0.1s</button>
            <button class="chip-btn" onclick={resetTrackOffset}>Reset</button>
            <button class="chip-btn" onclick={() => adjustTrackOffset(100)}>+0.1s</button>
            <button class="chip-btn" onclick={() => adjustTrackOffset(200)}>+0.2s</button>
          </div>
        </div>

        <div class="drawer-footer">
          <label class="toggle-check">
            <input
              type="checkbox"
              checked={settings.showAlbumArt}
              onchange={(e) => persistSettings({ showAlbumArt: (e.target as HTMLInputElement).checked })}
            />
            <span>Album art</span>
          </label>
          <div class="footer-actions">
            <button class="chip-btn" onclick={resetCurrentModeSize} title="Reset window size for current mode">Reset Size</button>
            <button class="chip-btn" onclick={() => RefreshLyrics()}>Refetch</button>
            <button class="chip-btn danger" onclick={() => QuitApp()}>Quit</button>
          </div>
        </div>
      {:else if activeDrawer === 'search'}
        <form
          class="search-bar"
          onsubmit={(e) => {
            e.preventDefault();
            runSearch();
          }}
        >
          <input
            class="search-input"
            type="text"
            placeholder="Search song & artist on LRCLIB…"
            bind:value={searchQuery}
          />
          <button class="chip-btn accent" type="submit" disabled={searching}>
            {searching ? '…' : 'Find'}
          </button>
        </form>

        <div class="search-list">
          {#if searching}
            <div class="search-empty">Searching LRCLIB…</div>
          {:else if searchResults.length === 0}
            <div class="search-empty">No matches yet. Try editing the title above.</div>
          {:else}
            {#each searchResults as item}
              <button class="search-item" onclick={() => selectCandidate(item.id)}>
                <div class="search-item-main">
                  <div class="search-item-title">{item.trackName}</div>
                  <div class="search-item-sub">
                    {item.artistName}
                    {#if item.albumName} · {item.albumName}{/if}
                  </div>
                </div>
                <div class="search-item-badges">
                  {#if item.wordSynced}
                    <span class="badge word">Word</span>
                  {:else if item.synced}
                    <span class="badge sync">Synced</span>
                  {:else if item.instrumental}
                    <span class="badge">Inst</span>
                  {:else}
                    <span class="badge">Plain</span>
                  {/if}
                  <span class="dur">{formatDurationSec(item.duration)}</span>
                </div>
              </button>
            {/each}
          {/if}
        </div>
      {:else if activeDrawer === 'about'}
        <div class="about-card">
          <div class="about-head">
            <div class="about-title-row">
              <span class="about-name">Spofly</span>
              <span class="about-ver">v1.0.0</span>
            </div>
            <div class="about-sub">Floating Spotify Lyrics</div>
          </div>

          <p class="about-desc">
            Lightweight, zero-config synced & word-by-word lyrics overlay for Spotify on macOS and Windows.
          </p>

          <div class="about-meta">
            <div class="about-copy">Copyright © 2026 <strong>japarscript</strong></div>
            <button
              type="button"
              class="chip-btn accent"
              onclick={() => BrowserOpenURL('https://japarscript.com')}
            >
              japarscript.com ↗
            </button>
          </div>
        </div>
      {/if}
    </div>
  {/if}

  <!-- Main Lyrics Stage -->
  <main class="stage" class:drag-region={settings.mode === 'pill'}>
    {#if !track.running || !track.title}
      <div class="empty-state drag-region">
        <div class="pulse-ring"></div>
        <div class="empty-title">
          {!track.running ? 'Waiting for Spotify' : 'Ready for your next track'}
        </div>
        <div class="empty-sub">
          Play any song on Spotify and synced lyrics will glide in automatically.
        </div>
      </div>
    {:else if lyricsLoading}
      <div class="empty-state">
        <div class="skeleton-line wide"></div>
        <div class="skeleton-line"></div>
        <div class="skeleton-line narrow"></div>
      </div>
    {:else if lyrics?.instrumental}
      <div class="empty-state">
        <div class="interlude-dots big">
          <span></span><span></span><span></span>
        </div>
        <div class="empty-title">Instrumental</div>
        <div class="empty-sub">{track.title} · {track.artist}</div>
      </div>
    {:else if !lyrics || lyrics.source === 'none' || !lyrics.lines?.length}
      <div class="empty-state">
        <div class="empty-title">No lyrics found</div>
        <div class="empty-sub">Couldn't match "{track.title}" automatically.</div>
        {#if !settings.locked}
          <button class="chip-btn accent no-drag" onclick={() => toggleDrawer('search')}>
            Search LRCLIB manually
          </button>
        {/if}
      </div>
    {:else if settings.mode === 'pill'}
      <!-- PILL / SUBTITLE MODE -->
      <div class="pill-stage">
        {#if inInterlude && activeLineIndex < 0}
          <div class="interlude-dots">
            <span></span><span></span><span></span>
          </div>
        {:else if pillCurrentLine}
          <div class="pill-current">
            {#if pillCurrentLine.words?.length}
              {#each pillCurrentLine.words as w}
                {@const wp = wordProgress(w.startMs, w.endMs)}
                <span
                  class="word"
                  class:active={wp > 0 && wp < 1}
                  class:sung={wp >= 1}
                  style="--wp: {wp};"
                >{w.text}</span>
              {/each}
            {:else}
              {pillCurrentLine.text || '♪'}
            {/if}
          </div>
          {#if settings.showNextInPill && pillNextLine && pillNextLine.text}
            <div class="pill-next">{pillNextLine.text}</div>
          {/if}
        {/if}
      </div>
    {:else}
      <!-- CARD & FULL SYNCED / PLAIN LYRICS STAGE -->
      <div
        class="lyrics-scroll"
        role="region"
        aria-label="Synced lyrics"
        bind:this={scrollContainer}
        onwheel={() => { userScrollingUntil = performance.now() + 2800; }}
        ontouchmove={() => { userScrollingUntil = performance.now() + 2800; }}
      >
        <div class="lyrics-spacer"></div>

        {#if inInterlude && activeLineIndex < 0}
          <div class="interlude-row">
            <div class="interlude-dots">
              <span></span><span></span><span></span>
            </div>
          </div>
        {/if}

        {#each lyrics.lines as line, idx}
          {@const isActive = lyrics.synced && idx === activeLineIndex}
          {@const dist = lyrics.synced ? Math.abs(idx - activeLineIndex) : 0}
          {@const isPast = lyrics.synced && idx < activeLineIndex}
          <button
            bind:this={lineElements[idx]}
            type="button"
            class="lyric-line no-drag"
            class:active={isActive}
            class:past={isPast}
            class:near={lyrics.synced && dist === 1}
            class:far={lyrics.synced && dist >= 2}
            class:plain={!lyrics.synced}
            onclick={() => handleLineClick(line.startMs)}
            tabindex={lyrics.synced ? 0 : -1}
          >
            {#if isActive && line.words?.length}
              {#each line.words as w}
                {@const wp = wordProgress(w.startMs, w.endMs)}
                <span
                  class="word"
                  class:active={wp > 0 && wp < 1}
                  class:sung={wp >= 1}
                  style="--wp: {wp};"
                >{w.text}</span>
              {/each}
            {:else}
              {line.text || '♪'}
            {/if}
          </button>

          {#if isActive && inInterlude}
            <div class="interlude-row">
              <div class="interlude-dots">
                <span></span><span></span><span></span>
              </div>
            </div>
          {/if}
        {/each}

        <div class="lyrics-spacer"></div>
      </div>
    {/if}
  </main>

  <!-- Subtle Bottom Progress Bar -->
  {#if track.title && track.durationMs > 0 && settings.theme !== 'minimal'}
    <div
      class="progress-track no-drag"
      role="slider"
      tabindex="0"
      aria-label="Track progress"
      aria-valuemin={0}
      aria-valuemax={track.durationMs}
      aria-valuenow={Math.round(livePosMs)}
      onclick={handleProgressClick}
      onkeydown={(e) => {
        if (e.key === 'ArrowRight') SeekTo(Math.min(track.durationMs, livePosMs + 5000));
        if (e.key === 'ArrowLeft') SeekTo(Math.max(0, livePosMs - 5000));
      }}
      title={`${formatTime(livePosMs)} / ${formatTime(track.durationMs)}`}
    >
      <div class="progress-fill" style="width: {progressRatio * 100}%"></div>
    </div>
  {/if}

  <!-- Edge & Corner Interactive Resize Handles -->
  {#if !settings.locked}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="resize-edge-e no-drag"
      onpointerdown={(e) => startResize('e', e)}
      onpointermove={moveResize}
      onpointerup={endResize}
      onpointercancel={endResize}
    ></div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="resize-edge-s no-drag"
      onpointerdown={(e) => startResize('s', e)}
      onpointermove={moveResize}
      onpointerup={endResize}
      onpointercancel={endResize}
    ></div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="resize-grip-se no-drag"
      title="Drag to resize"
      onpointerdown={(e) => startResize('se', e)}
      onpointermove={moveResize}
      onpointerup={endResize}
      onpointercancel={endResize}
    >
      <svg width="11" height="11" viewBox="0 0 12 12" fill="none">
        <path d="M10.5 4.5C10.5 7.81371 7.81371 10.5 4.5 10.5" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
      </svg>
    </div>
  {/if}
</div>

<style>
  .shell {
    position: relative;
    width: 100vw;
    height: 100vh;
    margin: 0;
    border-radius: 22px;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.22),
      inset 0 -1px 0 rgba(0, 0, 0, 0.26);
    border: 1px solid rgba(255, 255, 255, 0.16);
    transition: border-color 0.3s ease, background 0.3s ease;
  }

  .shell.mode-pill {
    border-radius: 20px;
  }

  .shell.theme-minimal {
    border-color: transparent;
    box-shadow: none;
  }
  .shell.theme-minimal:hover:not(.is-locked),
  .shell.theme-minimal.drawer-open {
    border-color: rgba(255, 255, 255, 0.16);
    box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.18);
  }

  /* Ambient Mesh Glass Layer */
  .ambient-bg {
    position: absolute;
    inset: 0;
    z-index: 0;
    overflow: hidden;
    border-radius: inherit;
    pointer-events: none;
  }

  .glass-scrim {
    position: absolute;
    inset: 0;
    backdrop-filter: blur(32px) saturate(185%);
    -webkit-backdrop-filter: blur(32px) saturate(185%);
    background: linear-gradient(
      165deg,
      rgba(16, 19, 28, calc(var(--glass-alpha) * 0.66)) 0%,
      rgba(9, 11, 16, calc(var(--glass-alpha) * 0.80)) 100%
    );
    transition: background 0.45s ease;
  }

  .specular-sheen {
    position: absolute;
    inset: 0;
    background: linear-gradient(
      180deg,
      rgba(255, 255, 255, 0.08) 0%,
      rgba(255, 255, 255, 0.01) 34%,
      rgba(0, 0, 0, 0.14) 100%
    );
    pointer-events: none;
  }

  .theme-obsidian .glass-scrim {
    background: linear-gradient(
      170deg,
      rgba(15, 17, 22, calc(var(--glass-alpha) * 0.90)) 0%,
      rgba(8, 9, 13, calc(var(--glass-alpha) * 0.96)) 100%
    );
  }

  .theme-minimal .glass-scrim,
  .theme-minimal .specular-sheen {
    backdrop-filter: none;
    -webkit-backdrop-filter: none;
    background: transparent;
  }
  .theme-minimal:hover:not(.is-locked) .glass-scrim,
  .theme-minimal.drawer-open .glass-scrim {
    backdrop-filter: blur(22px) saturate(165%);
    -webkit-backdrop-filter: blur(22px) saturate(165%);
    background: rgba(10, 12, 18, 0.62);
  }

  .orb {
    position: absolute;
    border-radius: 50%;
    filter: blur(46px);
    opacity: 0.72;
    transition: background 1.2s cubic-bezier(0.22, 1, 0.36, 1), opacity 0.6s ease;
    will-change: transform;
  }

  .theme-obsidian .orb,
  .theme-minimal .orb {
    opacity: 0;
  }

  .orb-a {
    width: 82%;
    height: 98%;
    top: -34%;
    left: -18%;
    background: radial-gradient(circle, rgba(var(--p-rgb), 0.85) 0%, transparent 72%);
  }

  .orb-b {
    width: 78%;
    height: 94%;
    bottom: -38%;
    right: -16%;
    background: radial-gradient(circle, rgba(var(--s-rgb), 0.78) 0%, transparent 72%);
  }

  .is-playing .orb-a {
    animation: orbDriftA 18s ease-in-out infinite alternate;
  }
  .is-playing .orb-b {
    animation: orbDriftB 22s ease-in-out infinite alternate;
  }

  /* Header */
  .header {
    position: relative;
    z-index: 4;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 13px 15px 7px;
    min-height: 50px;
    cursor: grab;
  }

  .header:active {
    cursor: grabbing;
  }

  .mode-pill .header {
    padding: 8px 14px 2px;
    min-height: 32px;
  }

  .theme-minimal:not(:hover):not(.drawer-open) .header {
    opacity: 0;
    transition: opacity 0.25s ease;
  }
  .theme-minimal:hover .header,
  .theme-minimal.drawer-open .header {
    opacity: 1;
    transition: opacity 0.2s ease;
  }

  .track-meta {
    display: flex;
    align-items: center;
    gap: 11px;
    min-width: 0;
    flex: 1;
  }

  .art-wrap {
    position: relative;
    width: 40px;
    height: 40px;
    border-radius: 10px;
    overflow: hidden;
    flex-shrink: 0;
    box-shadow:
      0 8px 20px rgba(var(--p-rgb), 0.45),
      0 3px 10px rgba(0, 0, 0, 0.38);
    border: 1px solid rgba(255, 255, 255, 0.18);
    transition: box-shadow 1s ease;
  }

  .mode-pill .art-wrap {
    width: 24px;
    height: 24px;
    border-radius: 6px;
    box-shadow: 0 3px 8px rgba(0, 0, 0, 0.35);
  }

  .art-img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }

  .art-placeholder {
    width: 100%;
    height: 100%;
    display: grid;
    place-items: center;
    background: rgba(255, 255, 255, 0.08);
    color: rgba(255, 255, 255, 0.65);
  }

  .eq-badge {
    position: absolute;
    right: 3px;
    bottom: 3px;
    display: flex;
    align-items: flex-end;
    gap: 1.5px;
    height: 9px;
    padding: 2px 3px;
    border-radius: 4px;
    background: rgba(0, 0, 0, 0.62);
  }
  .mode-pill .eq-badge {
    display: none;
  }
  .eq-badge span {
    width: 2px;
    height: 100%;
    background: rgb(var(--a-rgb));
    border-radius: 2px;
    transform-origin: bottom;
    animation: eqBounce 0.9s ease-in-out infinite;
  }
  .eq-badge span:nth-child(2) {
    animation-delay: 0.2s;
  }
  .eq-badge span:nth-child(3) {
    animation-delay: 0.4s;
  }

  .track-text {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .mode-pill .track-text {
    flex-direction: row;
    align-items: baseline;
    gap: 7px;
  }

  .track-title {
    font-size: 13.5px;
    font-weight: 700;
    letter-spacing: -0.015em;
    color: rgba(255, 255, 255, 0.96);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .track-title.muted {
    color: rgba(255, 255, 255, 0.68);
  }

  .track-artist {
    font-size: 11.5px;
    font-weight: 500;
    color: rgba(255, 255, 255, 0.58);
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }

  .artist-name {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .sync-dot {
    font-size: 9px;
    color: rgb(var(--a-rgb));
    opacity: 0.9;
    flex-shrink: 0;
  }

  .time-chip {
    font-size: 10.5px;
    font-variant-numeric: tabular-nums;
    color: rgba(255, 255, 255, 0.45);
    opacity: 0;
    transition: opacity 0.2s ease;
    flex-shrink: 0;
  }
  .shell:hover .time-chip {
    opacity: 1;
  }

  /* Hover Controls */
  .controls {
    display: flex;
    align-items: center;
    gap: 5px;
    opacity: 0;
    transform: translateY(-2px);
    transition: opacity 0.22s ease, transform 0.22s ease;
    flex-shrink: 0;
  }

  .shell:hover .controls,
  .shell.drawer-open .controls {
    opacity: 1;
    transform: translateY(0);
  }

  .pill-group {
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 3px;
    border-radius: 999px;
    background: rgba(255, 255, 255, 0.09);
    border: 1px solid rgba(255, 255, 255, 0.11);
    backdrop-filter: blur(12px);
  }

  .icon-btn {
    width: 24px;
    height: 24px;
    border: none;
    border-radius: 999px;
    background: transparent;
    color: rgba(255, 255, 255, 0.8);
    display: grid;
    place-items: center;
    cursor: pointer;
    padding: 0;
    transition: background 0.16s ease, color 0.16s ease, transform 0.12s ease;
  }

  .icon-btn:hover {
    background: rgba(255, 255, 255, 0.16);
    color: #ffffff;
  }
  .icon-btn:active {
    transform: scale(0.92);
  }
  .icon-btn.active {
    background: rgba(var(--a-rgb), 0.24);
    color: rgb(var(--a-rgb));
  }
  .play-btn {
    background: rgba(255, 255, 255, 0.16);
    color: #ffffff;
  }

  /* Lock Toast & Peek Unlock */
  .lock-toast {
    position: absolute;
    top: 10px;
    left: 50%;
    transform: translateX(-50%);
    z-index: 20;
    display: flex;
    align-items: center;
    gap: 7px;
    padding: 6px 12px;
    border-radius: 999px;
    background: rgba(12, 14, 20, 0.9);
    border: 1px solid rgba(255, 255, 255, 0.18);
    font-size: 11px;
    font-weight: 550;
    color: rgba(255, 255, 255, 0.92);
    white-space: nowrap;
    pointer-events: none;
  }

  .unlock-peek {
    position: absolute;
    top: 10px;
    right: 12px;
    z-index: 22;
  }

  .unlock-pill {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 12px;
    border-radius: 999px;
    border: 1px solid rgba(var(--a-rgb), 0.65);
    background: rgba(14, 18, 28, 0.92);
    color: #ffffff;
    font-size: 11.5px;
    font-weight: 650;
    cursor: pointer;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.45);
    transition: transform 0.14s ease, background 0.15s ease;
  }

  .unlock-pill:hover {
    background: rgba(var(--a-rgb), 0.28);
    transform: scale(1.03);
  }

  /* Popover Drawer */
  .drawer {
    position: absolute;
    top: 56px;
    left: 14px;
    right: 14px;
    z-index: 15;
    padding: 12px 14px;
    border-radius: 16px;
    background: rgba(14, 16, 24, 0.94);
    border: 1px solid rgba(255, 255, 255, 0.15);
    backdrop-filter: blur(24px);
    box-shadow: 0 18px 40px rgba(0, 0, 0, 0.45);
    display: flex;
    flex-direction: column;
    gap: 10px;
    max-height: calc(100% - 70px);
    overflow-y: auto;
  }

  .mode-pill .drawer {
    top: 38px;
  }

  .drawer-section {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
  }

  .drawer-row {
    display: flex;
    gap: 12px;
    align-items: center;
  }
  .drawer-row.between {
    justify-content: space-between;
  }

  .drawer-label {
    font-size: 11px;
    font-weight: 600;
    color: rgba(255, 255, 255, 0.65);
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 6px;
  }
  .drawer-label span,
  .offset-val {
    color: rgb(var(--a-rgb));
    font-variant-numeric: tabular-nums;
  }

  .seg-control {
    display: flex;
    background: rgba(255, 255, 255, 0.06);
    padding: 2px;
    border-radius: 9px;
    border: 1px solid rgba(255, 255, 255, 0.08);
  }
  .seg-btn {
    border: none;
    background: transparent;
    color: rgba(255, 255, 255, 0.65);
    font-size: 11px;
    font-weight: 600;
    padding: 4px 10px;
    border-radius: 7px;
    cursor: pointer;
    transition: all 0.16s ease;
  }
  .seg-btn.selected {
    background: rgba(255, 255, 255, 0.16);
    color: #ffffff;
  }

  .slider-group {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .slider-group input[type='range'] {
    width: 100%;
    accent-color: rgb(var(--a-rgb));
    height: 4px;
    cursor: pointer;
  }

  .nudge-group {
    display: flex;
    gap: 4px;
  }

  .chip-btn {
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(255, 255, 255, 0.07);
    color: rgba(255, 255, 255, 0.88);
    font-size: 11px;
    font-weight: 600;
    padding: 4px 8px;
    border-radius: 8px;
    cursor: pointer;
    transition: background 0.15s ease;
  }
  .chip-btn:hover {
    background: rgba(255, 255, 255, 0.15);
  }
  .chip-btn.accent {
    background: rgba(var(--a-rgb), 0.2);
    border-color: rgba(var(--a-rgb), 0.45);
    color: #ffffff;
  }
  .chip-btn.danger:hover {
    background: rgba(239, 68, 68, 0.28);
    border-color: rgba(239, 68, 68, 0.5);
  }

  .drawer-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding-top: 6px;
    border-top: 1px solid rgba(255, 255, 255, 0.08);
  }

  .toggle-check {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 11.5px;
    color: rgba(255, 255, 255, 0.78);
    cursor: pointer;
  }
  .footer-actions {
    display: flex;
    gap: 6px;
  }

  /* Search */
  .search-bar {
    display: flex;
    gap: 6px;
  }
  .search-input {
    flex: 1;
    background: rgba(255, 255, 255, 0.08);
    border: 1px solid rgba(255, 255, 255, 0.14);
    border-radius: 9px;
    padding: 6px 10px;
    font-size: 12px;
    color: #ffffff;
    outline: none;
  }
  .search-input:focus {
    border-color: rgba(var(--a-rgb), 0.65);
  }
  .search-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
    max-height: 185px;
    overflow-y: auto;
  }
  .search-empty {
    font-size: 12px;
    color: rgba(255, 255, 255, 0.55);
    text-align: center;
    padding: 14px 6px;
  }
  .search-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    text-align: left;
    padding: 7px 9px;
    border-radius: 10px;
    border: 1px solid transparent;
    background: rgba(255, 255, 255, 0.04);
    color: #ffffff;
    cursor: pointer;
  }
  .search-item:hover {
    background: rgba(255, 255, 255, 0.1);
    border-color: rgba(255, 255, 255, 0.12);
  }
  .search-item-main {
    min-width: 0;
  }
  .search-item-title {
    font-size: 12px;
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .search-item-sub {
    font-size: 11px;
    color: rgba(255, 255, 255, 0.55);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .search-item-badges {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
  }
  .badge {
    font-size: 10px;
    font-weight: 650;
    padding: 2px 6px;
    border-radius: 6px;
    background: rgba(255, 255, 255, 0.12);
  }
  .badge.word {
    background: rgba(var(--a-rgb), 0.24);
    color: rgb(var(--a-rgb));
  }
  .badge.sync {
    background: rgba(52, 211, 153, 0.2);
    color: #6ee7b7;
  }
  .dur {
    font-size: 10.5px;
    color: rgba(255, 255, 255, 0.5);
    font-variant-numeric: tabular-nums;
  }

  /* About Drawer */
  .about-card {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .about-head {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .about-title-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .about-name {
    font-size: 15px;
    font-weight: 750;
    letter-spacing: -0.02em;
    color: #ffffff;
  }
  .about-ver {
    font-size: 10.5px;
    font-weight: 650;
    padding: 2px 7px;
    border-radius: 999px;
    background: rgba(var(--a-rgb), 0.2);
    color: rgb(var(--a-rgb));
  }
  .about-sub {
    font-size: 11.5px;
    color: rgba(255, 255, 255, 0.58);
  }
  .about-desc {
    margin: 0;
    font-size: 12px;
    line-height: 1.45;
    color: rgba(255, 255, 255, 0.78);
  }
  .about-meta {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding-top: 8px;
    border-top: 1px solid rgba(255, 255, 255, 0.08);
  }
  .about-copy {
    font-size: 11.5px;
    color: rgba(255, 255, 255, 0.72);
  }
  .about-copy strong {
    color: #ffffff;
    font-weight: 650;
  }

  /* Main Lyrics Stage */
  .stage {
    position: relative;
    z-index: 2;
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  .lyrics-scroll {
    flex: 1;
    overflow-y: auto;
    padding: 0 22px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    -webkit-mask-image: linear-gradient(
      to bottom,
      transparent 0%,
      black 14%,
      black 86%,
      transparent 100%
    );
    mask-image: linear-gradient(
      to bottom,
      transparent 0%,
      black 14%,
      black 86%,
      transparent 100%
    );
  }

  .lyrics-spacer {
    min-height: 44%;
    flex-shrink: 0;
  }

  .lyric-line {
    border: none;
    background: transparent;
    text-align: left;
    padding: 5px 8px;
    margin: 0 -8px;
    border-radius: 11px;
    font-family: inherit;
    font-size: calc(clamp(16px, 4.1vw, 28px) * var(--font-scale));
    line-height: 1.38;
    font-weight: 650;
    letter-spacing: -0.022em;
    color: rgba(255, 255, 255, 0.34);
    cursor: pointer;
    transform-origin: left center;
    transform: scale(0.955);
    transition:
      color 0.35s cubic-bezier(0.22, 1, 0.36, 1),
      transform 0.42s cubic-bezier(0.22, 1, 0.36, 1),
      opacity 0.35s ease,
      filter 0.35s ease,
      background 0.2s ease;
  }

  .lyric-line:hover {
    background: rgba(255, 255, 255, 0.06);
    color: rgba(255, 255, 255, 0.82);
    filter: none !important;
  }

  .lyric-line.past {
    color: rgba(255, 255, 255, 0.24);
  }

  .lyric-line.near {
    color: rgba(255, 255, 255, 0.50);
    transform: scale(0.98);
  }

  .lyric-line.far {
    color: rgba(255, 255, 255, 0.22);
    filter: blur(0.55px);
  }

  .lyric-line.active {
    color: #ffffff;
    transform: scale(1.03);
    font-weight: 760;
    filter: none;
    text-shadow: 0 2px 24px rgba(var(--a-rgb), 0.38);
  }

  .lyric-line.plain {
    color: rgba(255, 255, 255, 0.88);
    font-size: calc(clamp(14px, 3.3vw, 22px) * var(--font-scale));
    font-weight: 550;
    transform: none;
    cursor: default;
  }

  .theme-minimal .lyric-line.active {
    text-shadow:
      0 2px 14px rgba(0, 0, 0, 0.88),
      0 0 28px rgba(0, 0, 0, 0.72);
  }
  .theme-minimal .lyric-line:not(.active) {
    text-shadow: 0 2px 10px rgba(0, 0, 0, 0.78);
  }

  /* Word-by-word karaoke sweep */
  .word {
    display: inline-block;
    white-space: pre-wrap;
    color: transparent;
    -webkit-background-clip: text;
    background-clip: text;
    background-image: linear-gradient(
      90deg,
      #ffffff 0%,
      #ffffff calc(var(--wp, 0) * 100%),
      rgba(255, 255, 255, 0.44) calc(var(--wp, 0) * 100% + 10%),
      rgba(255, 255, 255, 0.38) 100%
    );
    transition: transform 0.24s cubic-bezier(0.22, 1, 0.36, 1), filter 0.24s ease;
  }

  .word.active {
    transform: translateY(-1.5px);
    filter: drop-shadow(0 0 10px rgba(var(--a-rgb), 0.6));
  }

  .word.sung {
    background-image: none;
    color: #ffffff;
  }

  /* Pill / Subtitle Mode */
  .pill-stage {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 0 24px 10px;
    text-align: center;
    gap: 3px;
  }

  .pill-current {
    font-size: calc(clamp(17px, 3.4vw, 26px) * var(--font-scale));
    font-weight: 760;
    letter-spacing: -0.02em;
    line-height: 1.24;
    color: #ffffff;
    text-shadow: 0 2px 18px rgba(0, 0, 0, 0.55);
    max-width: 100%;
    overflow: hidden;
    display: -webkit-box;
    line-clamp: 2;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
  }

  .pill-next {
    font-size: calc(12px * var(--font-scale));
    font-weight: 500;
    color: rgba(255, 255, 255, 0.50);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 92%;
  }

  .theme-minimal .pill-current {
    text-shadow:
      0 2px 12px rgba(0, 0, 0, 0.9),
      0 0 26px rgba(0, 0, 0, 0.75);
  }

  /* Interlude Dots */
  .interlude-row {
    padding: 4px 0;
  }
  .interlude-dots {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .interlude-dots span {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: rgb(var(--a-rgb));
    animation: dotPulse 1.5s ease-in-out infinite;
  }
  .interlude-dots span:nth-child(2) {
    animation-delay: 0.25s;
  }
  .interlude-dots span:nth-child(3) {
    animation-delay: 0.5s;
  }
  .interlude-dots.big span {
    width: 9px;
    height: 9px;
  }

  /* Empty / Loading States */
  .empty-state {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    text-align: center;
    padding: 16px 24px;
    gap: 7px;
  }

  .mode-pill .empty-state {
    padding: 4px 20px;
    gap: 3px;
  }

  .empty-title {
    font-size: 14.5px;
    font-weight: 650;
    color: rgba(255, 255, 255, 0.9);
  }
  .empty-sub {
    font-size: 12px;
    color: rgba(255, 255, 255, 0.52);
    max-width: 280px;
  }
  .mode-pill .empty-sub {
    max-width: 420px;
  }

  .pulse-ring {
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: rgb(var(--a-rgb));
    box-shadow: 0 0 0 6px rgba(var(--a-rgb), 0.18);
    margin-bottom: 4px;
  }
  .mode-pill .pulse-ring {
    display: none;
  }

  .skeleton-line {
    height: 15px;
    width: 68%;
    border-radius: 999px;
    background: linear-gradient(
      90deg,
      rgba(255, 255, 255, 0.06) 25%,
      rgba(255, 255, 255, 0.16) 50%,
      rgba(255, 255, 255, 0.06) 75%
    );
    background-size: 200% 100%;
    animation: shimmer 1.4s infinite linear;
  }
  .skeleton-line.wide {
    width: 82%;
  }
  .skeleton-line.narrow {
    width: 52%;
  }

  /* Bottom Progress Bar */
  .progress-track {
    position: relative;
    z-index: 5;
    height: 4px;
    width: 100%;
    background: rgba(255, 255, 255, 0.07);
    cursor: pointer;
    transition: height 0.16s ease;
    flex-shrink: 0;
  }

  .progress-track:hover {
    height: 6px;
  }

  .progress-fill {
    height: 100%;
    background: linear-gradient(90deg, rgba(var(--a-rgb), 0.85), #ffffff);
    box-shadow: 0 0 10px rgba(var(--a-rgb), 0.55);
    border-radius: 0 999px 999px 0;
    transition: width 0.08s linear;
  }

  /* Interactive Edge & Corner Resize Handles */
  .resize-edge-e {
    position: absolute;
    top: 16px;
    right: 0;
    bottom: 18px;
    width: 6px;
    z-index: 24;
    cursor: ew-resize;
  }

  .resize-edge-s {
    position: absolute;
    left: 16px;
    right: 18px;
    bottom: 0;
    height: 6px;
    z-index: 24;
    cursor: ns-resize;
  }

  .resize-grip-se {
    position: absolute;
    right: 4px;
    bottom: 4px;
    width: 16px;
    height: 16px;
    z-index: 25;
    display: grid;
    place-items: center;
    color: rgba(255, 255, 255, 0.48);
    cursor: nwse-resize;
    opacity: 0;
    transition: opacity 0.2s ease, color 0.15s ease;
  }

  .shell:hover .resize-grip-se {
    opacity: 0.72;
  }

  .resize-grip-se:hover {
    opacity: 1 !important;
    color: #ffffff;
  }
</style>
