package main

// TrackState represents the current playback state reported by Spotify.
type TrackState struct {
	Running     bool   `json:"running"`
	Playing     bool   `json:"playing"`
	TrackID     string `json:"trackId"`
	TrackKey    string `json:"trackKey"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
	DurationMs  int64  `json:"durationMs"`
	PositionMs  int64  `json:"positionMs"`
	ArtworkURL  string `json:"artworkUrl"`
	UpdatedAtMs int64  `json:"updatedAtMs"`
	Source      string `json:"source"`
}

// LyricWord represents a single timed word inside a lyric line.
type LyricWord struct {
	Text    string `json:"text"`
	StartMs int64  `json:"startMs"`
	EndMs   int64  `json:"endMs"`
}

// LyricLine represents a single line of lyrics, optionally word-timed.
type LyricLine struct {
	StartMs int64       `json:"startMs"`
	EndMs   int64       `json:"endMs"`
	Text    string      `json:"text"`
	Words   []LyricWord `json:"words,omitempty"`
}

// LyricsPayload holds the resolved lyrics for a track.
type LyricsPayload struct {
	TrackKey     string      `json:"trackKey"`
	TrackName    string      `json:"trackName"`
	ArtistName   string      `json:"artistName"`
	Source       string      `json:"source"` // "lrclib-word", "lrclib-synced", "lrclib-plain", "instrumental", "none"
	Synced       bool        `json:"synced"`
	WordSynced   bool        `json:"wordSynced"`
	Instrumental bool        `json:"instrumental"`
	Lines        []LyricLine `json:"lines"`
	PlainText    string      `json:"plainText"`
	Error        string      `json:"error,omitempty"`
}

// LyricsSearchResult represents a candidate lyrics entry from LRCLIB search.
type LyricsSearchResult struct {
	ID           int     `json:"id"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	WordSynced   bool    `json:"wordSynced"`
	Synced       bool    `json:"synced"`
	Instrumental bool    `json:"instrumental"`
}

// WindowSize stores custom width and height for a layout mode.
type WindowSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Settings holds persisted user preferences for the floating window.
type Settings struct {
	Mode           string                `json:"mode"`  // "pill" | "card" | "full"
	Theme          string                `json:"theme"` // "adaptive" | "obsidian" | "minimal"
	FontScale      float64               `json:"fontScale"`
	Opacity        float64               `json:"opacity"`
	ShowAlbumArt   bool                  `json:"showAlbumArt"`
	ShowNextInPill bool                  `json:"showNextInPill"`
	GlobalOffsetMs int64                 `json:"globalOffsetMs"`
	TrackOffsets   map[string]int64      `json:"trackOffsets,omitempty"`
	ModeSizes      map[string]WindowSize `json:"modeSizes,omitempty"`
	Locked         bool                  `json:"locked"`
}

// AppSnapshot is returned to the frontend on initial mount.
type AppSnapshot struct {
	Track    TrackState     `json:"track"`
	Lyrics   *LyricsPayload `json:"lyrics"`
	Settings Settings       `json:"settings"`
	Platform string         `json:"platform"`
}

