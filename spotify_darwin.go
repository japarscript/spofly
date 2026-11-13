//go:build darwin

package main

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const appleScriptReadState = `
if application "Spotify" is running then
	tell application "Spotify"
		set pState to player state as string
		if pState is "stopped" then
			return "stopped"
		end if
		set tId to id of current track as string
		set tName to name of current track as string
		set tArtist to artist of current track as string
		set tAlbum to album of current track as string
		set tDur to duration of current track as string
		set tPos to player position as string
		set tArt to ""
		try
			set rawArt to artwork url of current track
			if rawArt is not missing value then
				set tArt to rawArt as string
			end if
		end try
		return pState & "||~||" & tId & "||~||" & tName & "||~||" & tArtist & "||~||" & tAlbum & "||~||" & tDur & "||~||" & tPos & "||~||" & tArt
	end tell
else
	return "not_running"
end if
`

// SpotifyClient bridges playback state and controls to the Spotify desktop client.
type SpotifyClient struct{}

// NewSpotifyClient initializes the platform-specific Spotify client.
func NewSpotifyClient() *SpotifyClient {
	return &SpotifyClient{}
}

// Start begins any background listeners required by the platform client.
func (s *SpotifyClient) Start(_ context.Context) {}

// GetState reads the current playback state from Spotify via AppleScript.
func (s *SpotifyClient) GetState() TrackState {
	now := time.Now().UnixMilli()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "osascript", "-e", appleScriptReadState).Output()
	if err != nil {
		return TrackState{Running: false, UpdatedAtMs: now, Source: "applescript"}
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" || raw == "not_running" {
		return TrackState{Running: false, UpdatedAtMs: now, Source: "applescript"}
	}
	if raw == "stopped" {
		return TrackState{Running: true, Playing: false, UpdatedAtMs: now, Source: "applescript"}
	}

	parts := strings.Split(raw, "||~||")
	if len(parts) < 8 {
		return TrackState{Running: true, UpdatedAtMs: now, Source: "applescript"}
	}

	stateStr := strings.ToLower(strings.TrimSpace(parts[0]))
	trackID := strings.TrimSpace(parts[1])
	title := strings.TrimSpace(parts[2])
	artist := strings.TrimSpace(parts[3])
	album := strings.TrimSpace(parts[4])
	durMs := parseAppleScriptDurationMs(parts[5])
	posMs := parseAppleScriptSecondsToMs(parts[6])
	artwork := strings.TrimSpace(parts[7])
	if artwork == "missing value" {
		artwork = ""
	}

	trackKey := trackID
	if trackKey == "" {
		trackKey = strings.ToLower(artist + "::" + title)
	}

	return TrackState{
		Running:     true,
		Playing:     stateStr == "playing",
		TrackID:     trackID,
		TrackKey:    trackKey,
		Title:       title,
		Artist:      artist,
		Album:       album,
		DurationMs:  durMs,
		PositionMs:  posMs,
		ArtworkURL:  artwork,
		UpdatedAtMs: now,
		Source:      "applescript",
	}
}

// PlayPause toggles Spotify playback.
func (s *SpotifyClient) PlayPause() error {
	return runAppleScript(`if application "Spotify" is running then tell application "Spotify" to playpause`)
}

// NextTrack skips to the next Spotify track.
func (s *SpotifyClient) NextTrack() error {
	return runAppleScript(`if application "Spotify" is running then tell application "Spotify" to next track`)
}

// PrevTrack returns to the previous Spotify track.
func (s *SpotifyClient) PrevTrack() error {
	return runAppleScript(`if application "Spotify" is running then tell application "Spotify" to previous track`)
}

// SeekTo jumps Spotify playback to the given position in milliseconds.
func (s *SpotifyClient) SeekTo(ms int64) error {
	if ms < 0 {
		ms = 0
	}
	sec := float64(ms) / 1000.0
	script := fmt.Sprintf(`if application "Spotify" is running then tell application "Spotify" to set player position to %.3f`, sec)
	return runAppleScript(script)
}

func runAppleScript(script string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "osascript", "-e", script).Run()
}

func parseAppleScriptDurationMs(raw string) int64 {
	cleaned := strings.ReplaceAll(strings.TrimSpace(raw), ",", ".")
	val, err := strconv.ParseFloat(cleaned, 64)
	if err != nil || val < 0 {
		return 0
	}
	return int64(val)
}

func parseAppleScriptSecondsToMs(raw string) int64 {
	cleaned := strings.ReplaceAll(strings.TrimSpace(raw), ",", ".")
	val, err := strconv.ParseFloat(cleaned, 64)
	if err != nil || val < 0 {
		return 0
	}
	return int64(val * 1000.0)
}

