package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type lrclibResponse struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	HasWordSync  bool    `json:"hasWordSync"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
	LyricsFile   string  `json:"lyricsfile"`
}

type lyricsFileYAML struct {
	Version  string `yaml:"version"`
	Metadata struct {
		Instrumental bool `yaml:"instrumental"`
	} `yaml:"metadata"`
	Lines []struct {
		Text    string `yaml:"text"`
		StartMs int64  `yaml:"start_ms"`
		EndMs   int64  `yaml:"end_ms"`
		Words   []struct {
			Text    string `yaml:"text"`
			StartMs int64  `yaml:"start_ms"`
			EndMs   int64  `yaml:"end_ms"`
		} `yaml:"words"`
	} `yaml:"lines"`
}

var (
	lrcTagRe     = regexp.MustCompile(`\[(\d{1,3}):(\d{2})(?:[.:](\d{1,3}))?\]`)
	parenFeatRe  = regexp.MustCompile(`(?i)\s*[\(\[](?:feat\.?|ft\.?|with|remaster(?:ed)?|live|deluxe|radio edit|acoustic|version|mono|stereo)[^\)\]]*[\)\]]`)
	dashSuffixRe = regexp.MustCompile(`(?i)\s+-\s+(?:remaster(?:ed)?|live|acoustic|radio edit|single version|album version|deluxe|bonus track|mono|stereo).*$`)
)

// LyricsEngine fetches, parses, and caches lyrics from LRCLIB.
type LyricsEngine struct {
	client   *http.Client
	cacheDir string
	mu       sync.RWMutex
	mem      map[string]*LyricsPayload
}

// NewLyricsEngine creates a LyricsEngine with disk and in-memory caching.
func NewLyricsEngine() *LyricsEngine {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	cacheDir := filepath.Join(dir, "spofly", "cache")
	_ = os.MkdirAll(cacheDir, 0o755)

	return &LyricsEngine{
		client: &http.Client{
			Timeout: 9 * time.Second,
		},
		cacheDir: cacheDir,
		mem:      make(map[string]*LyricsPayload),
	}
}

func cacheFileKey(trackKey string) string {
	sum := sha1.Sum([]byte(trackKey))
	return hex.EncodeToString(sum[:]) + ".json"
}

func (e *LyricsEngine) getCached(trackKey string) *LyricsPayload {
	e.mu.RLock()
	if p, ok := e.mem[trackKey]; ok {
		e.mu.RUnlock()
		return p
	}
	e.mu.RUnlock()

	path := filepath.Join(e.cacheDir, cacheFileKey(trackKey))
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var p LyricsPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil
	}
	e.mu.Lock()
	e.mem[trackKey] = &p
	e.mu.Unlock()
	return &p
}

func (e *LyricsEngine) setCached(trackKey string, p *LyricsPayload) {
	if p == nil || trackKey == "" {
		return
	}
	e.mu.Lock()
	e.mem[trackKey] = p
	e.mu.Unlock()

	if data, err := json.Marshal(p); err == nil {
		path := filepath.Join(e.cacheDir, cacheFileKey(trackKey))
		_ = os.WriteFile(path, data, 0o644)
	}
}

// ClearTrackCache removes the cached lyrics for a specific track key.
func (e *LyricsEngine) ClearTrackCache(trackKey string) {
	e.mu.Lock()
	delete(e.mem, trackKey)
	e.mu.Unlock()
	_ = os.Remove(filepath.Join(e.cacheDir, cacheFileKey(trackKey)))
}

// FetchForTrack resolves lyrics for the given track, using cache unless bypassCache is true.
func (e *LyricsEngine) FetchForTrack(track TrackState, bypassCache bool) *LyricsPayload {
	if track.Title == "" {
		return &LyricsPayload{TrackKey: track.TrackKey, Source: "none"}
	}
	if !bypassCache {
		if cached := e.getCached(track.TrackKey); cached != nil {
			return cached
		}
	}

	durSec := int(math.Round(float64(track.DurationMs) / 1000.0))
	resp, err := e.fetchExact(track.Title, track.Artist, track.Album, durSec)
	if err != nil || resp == nil {
		cleanTitle := cleanTrackTitle(track.Title)
		cleanArtist := primaryArtist(track.Artist)
		if cleanTitle != track.Title || cleanArtist != track.Artist {
			if alt, altErr := e.fetchExact(cleanTitle, cleanArtist, "", durSec); altErr == nil && alt != nil {
				resp = alt
			}
		}
		if resp == nil {
			resp = e.searchBestMatch(cleanTitle, cleanArtist, durSec)
		}
	}

	if resp == nil {
		payload := &LyricsPayload{
			TrackKey:   track.TrackKey,
			TrackName:  track.Title,
			ArtistName: track.Artist,
			Source:     "none",
		}
		return payload
	}

	payload := convertResponse(track.TrackKey, resp, track.DurationMs)
	if payload.Source != "none" {
		e.setCached(track.TrackKey, payload)
	}
	return payload
}

// FetchByID retrieves a specific LRCLIB entry by ID and caches it for trackKey.
func (e *LyricsEngine) FetchByID(trackKey string, id int, durationMs int64) (*LyricsPayload, error) {
	endpoint := fmt.Sprintf("https://lrclib.net/api/get/%d", id)
	body, status, err := e.doGet(endpoint)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("lrclib returned status %d", status)
	}
	var resp lrclibResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	payload := convertResponse(trackKey, &resp, durationMs)
	e.setCached(trackKey, payload)
	return payload, nil
}

// SearchCandidates searches LRCLIB for candidate tracks matching a user query.
func (e *LyricsEngine) SearchCandidates(query string) ([]LyricsSearchResult, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	endpoint := "https://lrclib.net/api/search?q=" + url.QueryEscape(q)
	body, status, err := e.doGet(endpoint)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("search returned status %d", status)
	}
	var items []lrclibResponse
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, err
	}
	out := make([]LyricsSearchResult, 0, len(items))
	for _, item := range items {
		out = append(out, LyricsSearchResult{
			ID:           item.ID,
			TrackName:    item.TrackName,
			ArtistName:   item.ArtistName,
			AlbumName:    item.AlbumName,
			Duration:     item.Duration,
			WordSynced:   strings.TrimSpace(item.LyricsFile) != "",
			Synced:       strings.TrimSpace(item.SyncedLyrics) != "",
			Instrumental: item.Instrumental,
		})
	}
	return out, nil
}

func (e *LyricsEngine) fetchExact(title, artist, album string, durSec int) (*lrclibResponse, error) {
	params := url.Values{}
	params.Set("track_name", title)
	if artist != "" {
		params.Set("artist_name", artist)
	}
	if album != "" {
		params.Set("album_name", album)
	}
	if durSec > 0 {
		params.Set("duration", strconv.Itoa(durSec))
	}
	endpoint := "https://lrclib.net/api/get?" + params.Encode()
	body, status, err := e.doGet(endpoint)
	if err != nil || status != http.StatusOK {
		return nil, err
	}
	var resp lrclibResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (e *LyricsEngine) searchBestMatch(title, artist string, durSec int) *lrclibResponse {
	params := url.Values{}
	params.Set("track_name", title)
	if artist != "" {
		params.Set("artist_name", artist)
	}
	endpoint := "https://lrclib.net/api/search?" + params.Encode()
	body, status, err := e.doGet(endpoint)
	if err != nil || status != http.StatusOK {
		return nil
	}
	var candidates []lrclibResponse
	if err := json.Unmarshal(body, &candidates); err != nil || len(candidates) == 0 {
		return nil
	}

	bestIdx := -1
	bestScore := -1e9
	for i, c := range candidates {
		score := 0.0
		if c.HasWordSync {
			score += 50
		}
		if strings.TrimSpace(c.SyncedLyrics) != "" || strings.TrimSpace(c.LyricsFile) != "" {
			score += 30
		} else if strings.TrimSpace(c.PlainLyrics) != "" {
			score += 10
		}
		if c.Instrumental {
			score += 5
		}
		if durSec > 0 && c.Duration > 0 {
			diff := math.Abs(c.Duration - float64(durSec))
			score -= diff * 1.5
		}
		if strings.EqualFold(strings.TrimSpace(c.TrackName), strings.TrimSpace(title)) {
			score += 15
		}
		if artist != "" && strings.Contains(strings.ToLower(c.ArtistName), strings.ToLower(artist)) {
			score += 15
		}
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		return nil
	}
	best := candidates[bestIdx]
	return &best
}

func (e *LyricsEngine) doGet(endpoint string) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "Spofly/1.0.0 (Floating Spotify Lyrics)")
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	return body, resp.StatusCode, err
}

func convertResponse(trackKey string, resp *lrclibResponse, durationMs int64) *LyricsPayload {
	payload := &LyricsPayload{
		TrackKey:     trackKey,
		TrackName:    resp.TrackName,
		ArtistName:   resp.ArtistName,
		Instrumental: resp.Instrumental,
		PlainText:    resp.PlainLyrics,
		Source:       "none",
	}
	if resp.Instrumental {
		payload.Source = "instrumental"
		return payload
	}

	if strings.TrimSpace(resp.LyricsFile) != "" {
		if lines := parseWordSyncYAML(resp.LyricsFile, durationMs); len(lines) > 0 {
			hasWords := false
			for _, l := range lines {
				if len(l.Words) > 0 {
					hasWords = true
					break
				}
			}
			payload.Lines = lines
			payload.Synced = true
			payload.WordSynced = hasWords
			if hasWords {
				payload.Source = "lrclib-word"
			} else {
				payload.Source = "lrclib-synced"
			}
			return payload
		}
	}

	if strings.TrimSpace(resp.SyncedLyrics) != "" {
		if lines := parseLRC(resp.SyncedLyrics, durationMs); len(lines) > 0 {
			payload.Lines = lines
			payload.Synced = true
			payload.Source = "lrclib-synced"
			return payload
		}
	}

	if strings.TrimSpace(resp.PlainLyrics) != "" {
		rawLines := strings.Split(strings.ReplaceAll(resp.PlainLyrics, "\r\n", "\n"), "\n")
		lines := make([]LyricLine, 0, len(rawLines))
		for _, rl := range rawLines {
			lines = append(lines, LyricLine{Text: strings.TrimSpace(rl)})
		}
		payload.Lines = lines
		payload.Source = "lrclib-plain"
		return payload
	}

	return payload
}

func parseWordSyncYAML(raw string, durationMs int64) []LyricLine {
	var parsed lyricsFileYAML
	if err := yaml.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}
	lines := make([]LyricLine, 0, len(parsed.Lines))
	for _, l := range parsed.Lines {
		text := strings.TrimSpace(l.Text)
		if text == "" && len(l.Words) == 0 {
			continue
		}
		line := LyricLine{
			StartMs: l.StartMs,
			EndMs:   l.EndMs,
			Text:    text,
		}
		if len(l.Words) > 0 {
			words := make([]LyricWord, 0, len(l.Words))
			for idx, w := range l.Words {
				end := w.EndMs
				if end <= w.StartMs {
					if idx+1 < len(l.Words) {
						end = l.Words[idx+1].StartMs
					} else if l.EndMs > w.StartMs {
						end = l.EndMs
					} else {
						end = w.StartMs + 500
					}
				}
				words = append(words, LyricWord{
					Text:    w.Text,
					StartMs: w.StartMs,
					EndMs:   end,
				})
			}
			line.Words = words
			if line.Text == "" {
				var b strings.Builder
				for _, w := range words {
					b.WriteString(w.Text)
				}
				line.Text = strings.TrimSpace(b.String())
			}
		}
		lines = append(lines, line)
	}
	finalizeLineEndTimes(lines, durationMs)
	return lines
}

func parseLRC(raw string, durationMs int64) []LyricLine {
	rawLines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	lines := make([]LyricLine, 0, len(rawLines))

	for _, line := range rawLines {
		matches := lrcTagRe.FindAllStringSubmatch(line, -1)
		if len(matches) == 0 {
			continue
		}
		text := strings.TrimSpace(lrcTagRe.ReplaceAllString(line, ""))
		for _, m := range matches {
			min, _ := strconv.ParseInt(m[1], 10, 64)
			sec, _ := strconv.ParseInt(m[2], 10, 64)
			ms := int64(0)
			if m[3] != "" {
				frac := m[3]
				switch len(frac) {
				case 1:
					v, _ := strconv.ParseInt(frac, 10, 64)
					ms = v * 100
				case 2:
					v, _ := strconv.ParseInt(frac, 10, 64)
					ms = v * 10
				default:
					v, _ := strconv.ParseInt(frac[:3], 10, 64)
					ms = v
				}
			}
			startMs := (min*60+sec)*1000 + ms
			lines = append(lines, LyricLine{
				StartMs: startMs,
				Text:    text,
			})
		}
	}

	sort.SliceStable(lines, func(i, j int) bool {
		return lines[i].StartMs < lines[j].StartMs
	})
	finalizeLineEndTimes(lines, durationMs)
	return lines
}

func finalizeLineEndTimes(lines []LyricLine, durationMs int64) {
	for i := range lines {
		if lines[i].EndMs > lines[i].StartMs {
			continue
		}
		if i+1 < len(lines) && lines[i+1].StartMs > lines[i].StartMs {
			lines[i].EndMs = lines[i+1].StartMs
		} else if durationMs > lines[i].StartMs {
			lines[i].EndMs = durationMs
		} else {
			lines[i].EndMs = lines[i].StartMs + 5000
		}
	}
}

func cleanTrackTitle(title string) string {
	s := parenFeatRe.ReplaceAllString(title, "")
	s = dashSuffixRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

func primaryArtist(artist string) string {
	for _, sep := range []string{", ", " & ", " feat. ", " ft. "} {
		if idx := strings.Index(strings.ToLower(artist), sep); idx > 0 {
			return strings.TrimSpace(artist[:idx])
		}
	}
	return strings.TrimSpace(artist)
}

