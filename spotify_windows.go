//go:build windows

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const windowsSMTCScript = `
$ErrorActionPreference = 'SilentlyContinue'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
Add-Type -AssemblyName System.Runtime.WindowsRuntime

$asTaskGeneric = ([System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object {
    $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation` + "`1" + `'
})[0]

function Await($WinRtTask, $ResultType) {
    $netTask = $asTaskGeneric.MakeGenericMethod($ResultType).Invoke($null, @($WinRtTask))
    $netTask.Wait(-1) | Out-Null
    return $netTask.Result
}

[Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager, Windows.Media.Control, ContentType=WindowsRuntime] | Out-Null
[Windows.Storage.Streams.DataReader, Windows.Storage.Streams, ContentType=WindowsRuntime] | Out-Null

$mgr = Await ([Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager]::RequestAsync()) ([Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager])

$lastThumbKey = ""
$lastThumbData = ""

function Get-SpotifySession {
    $sessions = $mgr.GetSessions()
    foreach ($s in $sessions) {
        if ($s.SourceAppUserModelId -match 'Spotify') { return $s }
    }
    return $null
}

while ($true) {
    while ([Console]::In.Peek() -ge 0) {
        $cmd = [Console]::In.ReadLine()
        $s = Get-SpotifySession
        if ($null -ne $s -and -not [string]::IsNullOrWhiteSpace($cmd)) {
            if ($cmd -eq "PLAYPAUSE") { $null = $s.TryTogglePlayPauseAsync() }
            elseif ($cmd -eq "NEXT") { $null = $s.TrySkipNextAsync() }
            elseif ($cmd -eq "PREV") { $null = $s.TrySkipPreviousAsync() }
            elseif ($cmd.StartsWith("SEEK:")) {
                $ms = [int64]($cmd.Substring(5))
                $ticks = $ms * 10000
                $null = $s.TryChangePlaybackPositionAsync($ticks)
            }
        }
    }

    $session = Get-SpotifySession
    if ($null -eq $session) {
        $proc = Get-Process -Name "Spotify" -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($null -ne $proc) {
            [Console]::Out.WriteLine('{"running":true,"playing":false,"title":"","artist":""}')
        } else {
            [Console]::Out.WriteLine('{"running":false}')
        }
        [Console]::Out.Flush()
        Start-Sleep -Milliseconds 700
        continue
    }

    $pb = $session.GetPlaybackInfo()
    $tl = $session.GetTimelineProperties()
    $props = Await ($session.TryGetMediaPropertiesAsync()) ([Windows.Media.Control.GlobalSystemMediaTransportControlsSessionMediaProperties])

    $playing = ($pb.PlaybackStatus -eq [Windows.Media.Control.GlobalSystemMediaTransportControlsSessionPlaybackStatus]::Playing)
    $title = if ($props) { [string]$props.Title } else { "" }
    $artist = if ($props) { [string]$props.Artist } else { "" }
    $album = if ($props) { [string]$props.AlbumTitle } else { "" }
    $durMs = [int64]$tl.EndTime.TotalMilliseconds
    $posMs = [int64]$tl.Position.TotalMilliseconds
    if ($playing -and $tl.LastUpdatedTime.Year -gt 2000) {
        $elapsed = ([DateTimeOffset]::UtcNow - $tl.LastUpdatedTime).TotalMilliseconds
        if ($elapsed -gt 0 -and $elapsed -lt 15000) {
            $posMs = [int64]($posMs + $elapsed)
        }
    }
    if ($durMs -gt 0 -and $posMs -gt $durMs) { $posMs = $durMs }

    $thumbKey = "$artist::$title"
    if ($thumbKey -ne $lastThumbKey) {
        $lastThumbKey = $thumbKey
        $lastThumbData = ""
        if ($props -and $props.Thumbnail) {
            try {
                $stream = Await ($props.Thumbnail.OpenReadAsync()) ([Windows.Storage.Streams.IRandomAccessStreamWithContentType])
                if ($stream -and $stream.Size -gt 0 -and $stream.Size -lt 2097152) {
                    $reader = [Windows.Storage.Streams.DataReader]::new($stream.GetInputStreamAt(0))
                    $null = Await ($reader.LoadAsync([uint32]$stream.Size)) ([uint32])
                    $bytes = New-Object byte[] $stream.Size
                    $reader.ReadBytes($bytes)
                    $ct = if ($stream.ContentType) { $stream.ContentType } else { "image/jpeg" }
                    $lastThumbData = "data:$ct;base64," + [Convert]::ToBase64String($bytes)
                    $reader.Dispose()
                    $stream.Dispose()
                }
            } catch {}
        }
    }

    $obj = @{
        running    = $true
        playing    = $playing
        title      = $title
        artist     = $artist
        album      = $album
        durationMs = $durMs
        positionMs = $posMs
        artworkUrl = $lastThumbData
    }
    [Console]::Out.WriteLine(($obj | ConvertTo-Json -Compress))
    [Console]::Out.Flush()
    Start-Sleep -Milliseconds 350
}
`

type smtcJSON struct {
	Running    bool   `json:"running"`
	Playing    bool   `json:"playing"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	DurationMs int64  `json:"durationMs"`
	PositionMs int64  `json:"positionMs"`
	ArtworkURL string `json:"artworkUrl"`
}

// SpotifyClient reads Windows SMTC playback state via a single persistent hidden PowerShell session.
type SpotifyClient struct {
	mu           sync.RWMutex
	latest       TrackState
	stdin        io.WriteCloser
	artCache     map[string]string
	artFetching  map[string]bool
	httpClient   *http.Client
}

// NewSpotifyClient initializes the Windows Spotify SMTC bridge.
func NewSpotifyClient() *SpotifyClient {
	return &SpotifyClient{
		latest:      TrackState{Source: "smtc"},
		artCache:    make(map[string]string),
		artFetching: make(map[string]bool),
		httpClient:  &http.Client{Timeout: 5 * time.Second},
	}
}

// Start launches the persistent SMTC reader loop.
func (s *SpotifyClient) Start(ctx context.Context) {
	go func() {
		for {
			if ctx.Err() != nil {
				return
			}
			s.runSession(ctx)
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}()
}

func (s *SpotifyClient) runSession(ctx context.Context) {
	cmd := exec.CommandContext(ctx, "powershell.exe",
		"-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", "-")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}

	s.mu.Lock()
	s.stdin = stdin
	s.mu.Unlock()

	// Feed the script and keep stdin open for playback commands.
	_, _ = io.WriteString(stdin, windowsSMTCScript+"\n")

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var msg smtcJSON
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		key := ""
		if msg.Title != "" {
			key = strings.ToLower(strings.TrimSpace(msg.Artist) + "::" + strings.TrimSpace(msg.Title))
		}
		art := msg.ArtworkURL
		if art == "" && key != "" {
			art = s.lookupOrFetchArtwork(key, msg.Title, msg.Artist)
		}

		s.mu.Lock()
		s.latest = TrackState{
			Running:     msg.Running,
			Playing:     msg.Playing,
			TrackID:     key,
			TrackKey:    key,
			Title:       msg.Title,
			Artist:      msg.Artist,
			Album:       msg.Album,
			DurationMs:  msg.DurationMs,
			PositionMs:  msg.PositionMs,
			ArtworkURL:  art,
			UpdatedAtMs: time.Now().UnixMilli(),
			Source:      "smtc",
		}
		s.mu.Unlock()
	}

	s.mu.Lock()
	s.stdin = nil
	s.mu.Unlock()
	_ = cmd.Wait()
}

func (s *SpotifyClient) lookupOrFetchArtwork(key, title, artist string) string {
	s.mu.RLock()
	if cached, ok := s.artCache[key]; ok {
		s.mu.RUnlock()
		return cached
	}
	fetching := s.artFetching[key]
	s.mu.RUnlock()

	if fetching || title == "" {
		return ""
	}
	s.mu.Lock()
	s.artFetching[key] = true
	s.mu.Unlock()

	go func() {
		q := url.QueryEscape(strings.TrimSpace(artist + " " + title))
		resp, err := s.httpClient.Get("https://itunes.apple.com/search?term=" + q + "&entity=song&limit=1")
		if err == nil {
			defer resp.Body.Close()
			var data struct {
				Results []struct {
					ArtworkUrl100 string `json:"artworkUrl100"`
				} `json:"results"`
			}
			if json.NewDecoder(resp.Body).Decode(&data) == nil && len(data.Results) > 0 {
				artURL := strings.ReplaceAll(data.Results[0].ArtworkUrl100, "100x100bb", "600x600bb")
				s.mu.Lock()
				s.artCache[key] = artURL
				s.mu.Unlock()
				return
			}
		}
		s.mu.Lock()
		s.artCache[key] = ""
		s.mu.Unlock()
	}()
	return ""
}

// GetState returns the most recent SMTC playback snapshot.
func (s *SpotifyClient) GetState() TrackState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest
}

func (s *SpotifyClient) sendCommand(cmd string) error {
	s.mu.RLock()
	w := s.stdin
	s.mu.RUnlock()
	if w == nil {
		return fmt.Errorf("smtc session not active")
	}
	_, err := io.WriteString(w, cmd+"\n")
	return err
}

// PlayPause toggles Spotify playback.
func (s *SpotifyClient) PlayPause() error { return s.sendCommand("PLAYPAUSE") }

// NextTrack skips to the next Spotify track.
func (s *SpotifyClient) NextTrack() error { return s.sendCommand("NEXT") }

// PrevTrack returns to the previous Spotify track.
func (s *SpotifyClient) PrevTrack() error { return s.sendCommand("PREV") }

// SeekTo jumps Spotify playback to the given position in milliseconds.
func (s *SpotifyClient) SeekTo(ms int64) error {
	if ms < 0 {
		ms = 0
	}
	return s.sendCommand(fmt.Sprintf("SEEK:%d", ms))
}

