<div align="center">
  <img src="assets/logo.png" alt="Spofly Logo" width="96" height="96" />
  <h1>Spofly — Floating Spotify Lyrics</h1>
  <p>
    A lightweight, zero-config floating synced lyrics overlay for Spotify on <strong>macOS</strong> and <strong>Windows</strong>.
  </p>
  <p>
    Built with <strong>Wails v2 (Go + Native OS WebView)</strong> and <strong>Svelte 5 + TypeScript</strong> — ~10 MB binary, ~70–85 MB RAM, zero Electron bloat.
  </p>
</div>

---

## Features

- **Zero Spotify API Setup** — Reads playback state, track metadata, progress, and album artwork directly from your Spotify desktop app:
  - **macOS**: Native AppleScript integration (no API keys or OAuth tokens required).
  - **Windows**: Native Windows System Media Transport Controls (SMTC).
- **Word-by-Word Karaoke & Synced LRC** — Powered by [LRCLIB](https://lrclib.net/), prioritizing word-timed karaoke (`lyricsfile`), falling back to line-synced `.lrc` and plain lyrics, with local disk caching for instant reloads.
- **60/120fps Fluid Playback Clock** — Client-side `requestAnimationFrame` interpolation keeps word-by-word highlighting, progress bars, and smooth spring-like lyric scrolling completely fluid.
- **Adaptive Ambient Glass** — Extracts dominant colors from the active album cover to tint the frosted glass background smoothly as tracks change.
- **3 Floating Layouts + Free Resize**:
  - **Card** (`460×316`) — Compact album header + multi-line depth-blurred lyrics.
  - **Pill** (`560×122`) — Minimal floating subtitle bar with active + next line preview.
  - **Full** (`460×540`) — Expanded interactive lyrics sheet.
  - Drag the **right edge**, **bottom edge**, or **bottom-right corner grip** to resize freely; custom sizes are remembered per layout.
- **3 Surface Themes** — `Adaptive`, `Obsidian`, and `Minimal` (pure floating subtitle text).
- **Interactive Controls**:
  - Click any lyric line (or the bottom progress bar) to **seek Spotify** directly to that moment.
  - Per-track **Sync Offset** (`-0.2s` / `-0.1s` / `+0.1s` / `+0.2s`) saved automatically per song.
  - Built-in **Manual LRCLIB Search** to swap lyric matches on the fly.
  - **Click-Through Lock Overlay** so mouse clicks pass straight through to apps or games underneath.

---

## Controls & Shortcuts

| Action | macOS | Windows |
| --- | --- | --- |
| Move floating window | Drag header (or body in Pill mode) | Drag header (or body in Pill mode) |
| Resize window | Drag right/bottom edge or bottom-right grip | Drag right/bottom edge or bottom-right grip |
| Toggle `Card` ↔ `Full` | Double-click header | Double-click header |
| Play / Pause | `Space` (when focused) or hover controls | `Space` (when focused) or hover controls |
| Seek ±5 seconds | `←` / `→` (when focused) | `←` / `→` (when focused) |
| Peek & Unlock while Locked | Hold `⌥ Option` or Menu Bar `♫` icon | Hold `Alt` or press `Ctrl + Alt + L` |

---

## Architecture

```text
spofly/
├── main.go               # Wails window configuration (frameless, always-on-top, transparent)
├── app.go                # Polling loop, settings persistence, window mode & resize state
├── types.go              # Shared Go <-> TypeScript data models
├── lyrics.go             # LRCLIB client, word-sync YAML & LRC parser, local disk cache
├── spotify_darwin.go     # macOS AppleScript Spotify bridge
├── spotify_windows.go    # Windows SMTC PowerShell session bridge
├── window_darwin.go/.m   # macOS floating level, all-spaces, rounded clip, Menu Bar & Option-peek
├── window_windows.go     # Windows layered/click-through style, Ctrl+Alt+L hotkey & Alt-peek
├── frontend/             # Svelte 5 + TypeScript + Vite UI
└── build/                # App icons, Info.plist, Windows manifest & build outputs
```

---

## Getting Started (Development)

### Prerequisites

- **Go** `1.26+`
- **Bun** `1.1+` (or Node.js `20+`)
- **Wails CLI v2**:
  ```bash
  go install github.com/wailsapp/wails/v2/cmd/wails@latest
  ```
- **Platform requirements**:
  - **macOS**: Xcode Command Line Tools (`xcode-select --install`)
  - **Windows**: WebView2 Runtime (pre-installed on Windows 10/11)

### Run in Dev Mode

```bash
cd frontend && bun install && cd ..
wails dev
```

---

## Building for Production

### macOS

```bash
wails build -platform darwin/arm64
# Or universal binary (Apple Silicon + Intel):
wails build -platform darwin/universal
```

Output: `build/bin/Spofly.app`

> **Note on macOS Permissions**: On first launch, macOS will ask for permission for Spofly to control Spotify via Apple Events so it can read the active track and playback position.

### Windows

Build natively on Windows or cross-compile from macOS:

```bash
# On macOS (cross-compile):
wails build -platform windows/amd64 -skipbindings

# On Windows:
wails build -platform windows/amd64
```

Output: `build/bin/Spofly.exe`

---

## Customizing the Logo & App Icon

- **README Logo**: Place your logo image at `assets/logo.png` (recommended `512×512` PNG with transparent background).
- **App Icon (macOS `.icns` & Windows `.ico`)**: Replace `build/appicon.png` with a `1024×1024` PNG, remove `build/windows/icon.ico` if you want Wails to regenerate the Windows icon automatically, then run `wails build`.

---

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT License — Copyright © 2026 [japarscript](https://japarscript.com). See [LICENSE](LICENSE) for details.
