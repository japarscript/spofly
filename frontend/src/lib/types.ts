export interface TrackState {
  running: boolean;
  playing: boolean;
  trackId: string;
  trackKey: string;
  title: string;
  artist: string;
  album: string;
  durationMs: number;
  positionMs: number;
  artworkUrl: string;
  updatedAtMs: number;
  source: string;
}

export interface LyricWord {
  text: string;
  startMs: number;
  endMs: number;
}

export interface LyricLine {
  startMs: number;
  endMs: number;
  text: string;
  words?: LyricWord[];
}

export interface LyricsPayload {
  trackKey: string;
  trackName: string;
  artistName: string;
  source: 'lrclib-word' | 'lrclib-synced' | 'lrclib-plain' | 'instrumental' | 'none';
  synced: boolean;
  wordSynced: boolean;
  instrumental: boolean;
  lines: LyricLine[];
  plainText: string;
  error?: string;
}

export interface LyricsSearchResult {
  id: number;
  trackName: string;
  artistName: string;
  albumName: string;
  duration: number;
  wordSynced: boolean;
  synced: boolean;
  instrumental: boolean;
}

export type WindowMode = 'pill' | 'card' | 'full';
export type ThemeMode = 'adaptive' | 'obsidian' | 'minimal';

export interface WindowSize {
  width: number;
  height: number;
}

export interface Settings {
  mode: WindowMode;
  theme: ThemeMode;
  fontScale: number;
  opacity: number;
  showAlbumArt: boolean;
  showNextInPill: boolean;
  globalOffsetMs: number;
  trackOffsets?: Record<string, number>;
  modeSizes?: Record<string, WindowSize>;
  locked: boolean;
}

export interface AppSnapshot {
  track: TrackState;
  lyrics: LyricsPayload | null;
  settings: Settings;
  platform: string;
}

export interface AmbientPalette {
  primary: string;   // "r, g, b"
  secondary: string; // "r, g, b"
  accent: string;    // "r, g, b"
}

