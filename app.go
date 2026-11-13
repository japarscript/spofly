package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App coordinates Spotify state polling, lyrics resolution, settings, and floating window behavior.
type App struct {
	ctx       context.Context
	cancel    context.CancelFunc
	spotify   *SpotifyClient
	lyrics    *LyricsEngine
	mu        sync.RWMutex
	track     TrackState
	curLyrics *LyricsPayload
	settings  Settings
	visible   bool
}

// NewApp creates a new App instance with default settings.
func NewApp() *App {
	return &App{
		spotify: NewSpotifyClient(),
		lyrics:  NewLyricsEngine(),
		settings: Settings{
			Mode:           "card",
			Theme:          "adaptive",
			FontScale:      1.0,
			Opacity:        0.82,
			ShowAlbumArt:   true,
			ShowNextInPill: true,
			TrackOffsets:   make(map[string]int64),
			ModeSizes:      make(map[string]WindowSize),
		},
		visible: true,
	}
}

func (a *App) startup(ctx context.Context) {
	pollCtx, cancel := context.WithCancel(ctx)
	a.ctx = ctx
	a.cancel = cancel

	a.loadSettings()
	a.settings.Locked = false // Never boot locked so the user can always drag/interact immediately.

	ConfigureFloatingWindow()
	SetupPlatformShell(a.handleShellAction, a.handleLockPeek)

	w, h := a.dimensionsForSettingsMode(a.settings.Mode)
	runtime.WindowSetSize(ctx, w, h)
	SetWindowAppearance(a.settings.Theme != "minimal", cornerRadiusForMode(a.settings.Mode))

	a.spotify.Start(pollCtx)
	go a.pollLoop(pollCtx)
}

func (a *App) shutdown(_ context.Context) {
	if a.cancel != nil {
		a.cancel()
	}
	a.saveSettings()
}

func (a *App) pollLoop(ctx context.Context) {
	lastTrackKey := ""
	var lastEmitted TrackState

	for {
		state := a.spotify.GetState()

		a.mu.Lock()
		a.track = state
		trackChanged := state.TrackKey != "" && state.TrackKey != lastTrackKey
		if state.TrackKey == "" && lastTrackKey != "" {
			lastTrackKey = ""
			a.curLyrics = nil
		}
		if trackChanged {
			lastTrackKey = state.TrackKey
			a.curLyrics = nil
		}
		a.mu.Unlock()

		if trackChanged {
			runtime.EventsEmit(a.ctx, "lyrics:loading", state.TrackKey)
			go a.resolveLyricsForTrack(state, false)
		}

		// Emit playback state on any meaningful change or heartbeat while playing.
		if state.Playing ||
			state.Running != lastEmitted.Running ||
			state.TrackKey != lastEmitted.TrackKey ||
			state.Playing != lastEmitted.Playing ||
			state.PositionMs != lastEmitted.PositionMs {
			runtime.EventsEmit(a.ctx, "spotify:state", state)
			lastEmitted = state
		}

		delay := 1100 * time.Millisecond
		if state.Playing {
			delay = 350 * time.Millisecond
		} else if state.Running {
			delay = 650 * time.Millisecond
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func (a *App) resolveLyricsForTrack(track TrackState, bypassCache bool) {
	payload := a.lyrics.FetchForTrack(track, bypassCache)

	a.mu.Lock()
	currentKey := a.track.TrackKey
	if currentKey == track.TrackKey {
		a.curLyrics = payload
	}
	a.mu.Unlock()

	if currentKey == track.TrackKey {
		runtime.EventsEmit(a.ctx, "lyrics:loaded", payload)
	}
}

// GetCurrentState returns the full initial snapshot for the frontend.
func (a *App) GetCurrentState() AppSnapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return AppSnapshot{
		Track:    a.track,
		Lyrics:   a.curLyrics,
		Settings: a.settings,
		Platform: goruntime.GOOS,
	}
}

// PlayPause toggles Spotify playback.
func (a *App) PlayPause() error {
	return a.spotify.PlayPause()
}

// NextTrack skips to the next Spotify track.
func (a *App) NextTrack() error {
	return a.spotify.NextTrack()
}

// PrevTrack goes back to the previous Spotify track.
func (a *App) PrevTrack() error {
	return a.spotify.PrevTrack()
}

// SeekTo jumps Spotify playback to the specified millisecond timestamp.
func (a *App) SeekTo(ms int64) error {
	err := a.spotify.SeekTo(ms)
	if err == nil {
		a.mu.Lock()
		a.track.PositionMs = ms
		a.track.UpdatedAtMs = time.Now().UnixMilli()
		snapshot := a.track
		a.mu.Unlock()
		runtime.EventsEmit(a.ctx, "spotify:state", snapshot)
	}
	return err
}

// GetWindowSize returns the current floating window size.
func (a *App) GetWindowSize() WindowSize {
	w, h := runtime.WindowGetSize(a.ctx)
	return WindowSize{Width: w, Height: h}
}

// ResizeWindow resizes the floating window and optionally persists the size for the current mode.
func (a *App) ResizeWindow(w int, h int, persist bool) WindowSize {
	if w < 340 {
		w = 340
	} else if w > 1200 {
		w = 1200
	}
	if h < 96 {
		h = 96
	} else if h > 1000 {
		h = 1000
	}

	runtime.WindowSetSize(a.ctx, w, h)

	a.mu.Lock()
	mode := a.settings.Mode
	theme := a.settings.Theme
	if persist {
		if a.settings.ModeSizes == nil {
			a.settings.ModeSizes = make(map[string]WindowSize)
		}
		a.settings.ModeSizes[mode] = WindowSize{Width: w, Height: h}
	}
	a.mu.Unlock()

	SetWindowAppearance(theme != "minimal", cornerRadiusForMode(mode))
	if persist {
		a.saveSettings()
	}
	return WindowSize{Width: w, Height: h}
}

// SetWindowMode switches between "pill", "card", and "full" floating layouts.
func (a *App) SetWindowMode(mode string) Settings {
	norm := normalizeMode(mode)

	a.mu.Lock()
	oldW, _ := a.dimensionsForModeLocked(a.settings.Mode)
	w, h := a.dimensionsForModeLocked(norm)
	a.settings.Mode = norm
	settingsCopy := a.settings
	a.mu.Unlock()

	x, y := runtime.WindowGetPosition(a.ctx)
	newX := x + (oldW-w)/2
	if newX < 0 {
		newX = 0
	}
	runtime.WindowSetSize(a.ctx, w, h)
	runtime.WindowSetPosition(a.ctx, newX, y)
	SetWindowAppearance(settingsCopy.Theme != "minimal", cornerRadiusForMode(norm))

	a.saveSettings()
	runtime.EventsEmit(a.ctx, "settings:updated", settingsCopy)
	return settingsCopy
}

// SetClickThrough locks or unlocks mouse click-through on the floating window.
func (a *App) SetClickThrough(locked bool) bool {
	a.mu.Lock()
	a.settings.Locked = locked
	a.mu.Unlock()

	SetClickThrough(locked)
	runtime.EventsEmit(a.ctx, "window:lock", locked)
	return locked
}

// ToggleVisibility shows or hides the floating window.
func (a *App) ToggleVisibility() {
	a.mu.Lock()
	a.visible = !a.visible
	vis := a.visible
	a.mu.Unlock()

	if vis {
		runtime.WindowShow(a.ctx)
		ConfigureFloatingWindow()
	} else {
		runtime.WindowHide(a.ctx)
	}
}

// MinimizeToTray hides the floating window (re-open via menu bar / tray).
func (a *App) MinimizeToTray() {
	a.mu.Lock()
	a.visible = false
	a.mu.Unlock()
	runtime.WindowHide(a.ctx)
}

// QuitApp exits the application cleanly.
func (a *App) QuitApp() {
	runtime.Quit(a.ctx)
}

// SaveSettings persists user visual and timing preferences.
func (a *App) SaveSettings(next Settings) Settings {
	a.mu.Lock()
	next.Mode = normalizeMode(next.Mode)
	if next.FontScale < 0.8 {
		next.FontScale = 0.8
	} else if next.FontScale > 1.45 {
		next.FontScale = 1.45
	}
	if next.Opacity < 0.25 {
		next.Opacity = 0.25
	} else if next.Opacity > 1.0 {
		next.Opacity = 1.0
	}
	if next.TrackOffsets == nil {
		next.TrackOffsets = make(map[string]int64)
	}
	if next.ModeSizes == nil {
		next.ModeSizes = a.settings.ModeSizes
	}
	a.settings = next
	a.mu.Unlock()

	SetWindowAppearance(next.Theme != "minimal", cornerRadiusForMode(next.Mode))
	a.saveSettings()
	return next
}

// RefreshLyrics clears cache for the current track and fetches fresh lyrics.
func (a *App) RefreshLyrics() {
	a.mu.RLock()
	track := a.track
	a.mu.RUnlock()
	if track.TrackKey == "" {
		return
	}
	a.lyrics.ClearTrackCache(track.TrackKey)
	runtime.EventsEmit(a.ctx, "lyrics:loading", track.TrackKey)
	go a.resolveLyricsForTrack(track, true)
}

// SearchLyrics queries LRCLIB for manual lyrics selection.
func (a *App) SearchLyrics(query string) ([]LyricsSearchResult, error) {
	return a.lyrics.SearchCandidates(query)
}

// UseLyricsCandidate loads a specific LRCLIB entry by ID for the active track.
func (a *App) UseLyricsCandidate(trackKey string, id int) (*LyricsPayload, error) {
	a.mu.RLock()
	durMs := a.track.DurationMs
	a.mu.RUnlock()

	payload, err := a.lyrics.FetchByID(trackKey, id, durMs)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	if a.track.TrackKey == trackKey {
		a.curLyrics = payload
	}
	a.mu.Unlock()
	runtime.EventsEmit(a.ctx, "lyrics:loaded", payload)
	return payload, nil
}

func (a *App) handleLockPeek(peeking bool) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "window:lock-peek", peeking)
	}
}

func (a *App) handleShellAction(action string) {
	switch action {
	case "toggle-visible":
		a.ToggleVisibility()
	case "toggle-lock":
		a.mu.RLock()
		locked := a.settings.Locked
		a.mu.RUnlock()
		a.SetClickThrough(!locked)
	case "mode:pill":
		a.SetWindowMode("pill")
	case "mode:card":
		a.SetWindowMode("card")
	case "mode:full":
		a.SetWindowMode("full")
	case "quit":
		a.QuitApp()
	}
}

func normalizeMode(mode string) string {
	switch mode {
	case "pill", "card", "full":
		return mode
	default:
		return "card"
	}
}

func dimensionsForMode(mode string) (int, int) {
	switch mode {
	case "pill":
		return 560, 122
	case "full":
		return 460, 540
	default:
		return 460, 316
	}
}

func cornerRadiusForMode(mode string) float64 {
	if mode == "pill" {
		return 20.0
	}
	return 22.0
}

func (a *App) dimensionsForModeLocked(mode string) (int, int) {
	if custom, ok := a.settings.ModeSizes[mode]; ok && custom.Width >= 340 && custom.Height >= 96 {
		return custom.Width, custom.Height
	}
	return dimensionsForMode(mode)
}

func (a *App) dimensionsForSettingsMode(mode string) (int, int) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.dimensionsForModeLocked(mode)
}

func settingsFilePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "spofly", "settings.json")
}

func (a *App) loadSettings() {
	raw, err := os.ReadFile(settingsFilePath())
	if err != nil {
		return
	}
	var s Settings
	if err := json.Unmarshal(raw, &s); err != nil {
		return
	}
	s.Mode = normalizeMode(s.Mode)
	if s.Theme == "" {
		s.Theme = "adaptive"
	}
	if s.FontScale == 0 {
		s.FontScale = 1.0
	}
	if s.Opacity == 0 {
		s.Opacity = 0.82
	}
	if s.TrackOffsets == nil {
		s.TrackOffsets = make(map[string]int64)
	}
	if s.ModeSizes == nil {
		s.ModeSizes = make(map[string]WindowSize)
	}
	a.mu.Lock()
	a.settings = s
	a.mu.Unlock()
}

func (a *App) saveSettings() {
	a.mu.RLock()
	copySettings := a.settings
	a.mu.RUnlock()
	copySettings.Locked = false

	path := settingsFilePath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if data, err := json.MarshalIndent(copySettings, "", "  "); err == nil {
		_ = os.WriteFile(path, data, 0o644)
	}
}
