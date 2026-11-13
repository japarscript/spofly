package main

import (
	"testing"
)

func TestParseLRC(t *testing.T) {
	raw := `
[00:05.27] Dihentak sunyi
[00:08.73][01:02.10] Geram-gusarmu mulai
[00:14.310] Gerayangi kupingku
`
	lines := parseLRC(raw, 232000)
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines (including repeated tag), got %d", len(lines))
	}
	if lines[0].StartMs != 5270 || lines[0].Text != "Dihentak sunyi" {
		t.Fatalf("unexpected first line: %+v", lines[0])
	}
	if lines[0].EndMs != 8730 {
		t.Fatalf("expected line 0 EndMs=8730, got %d", lines[0].EndMs)
	}
	if lines[2].StartMs != 14310 {
		t.Fatalf("expected 3-digit fractional ms 14310, got %d", lines[2].StartMs)
	}
	if lines[3].StartMs != 62100 || lines[3].EndMs != 232000 {
		t.Fatalf("unexpected last line: %+v", lines[3])
	}
}

func TestParseWordSyncYAML(t *testing.T) {
	raw := `
version: '1.0'
lines:
- text: Dihentak sunyi
  start_ms: 4880
  end_ms: 8400
  words:
  - text: 'Dihentak '
    start_ms: 4880
  - text: sunyi
    start_ms: 6447
`
	lines := parseWordSyncYAML(raw, 232000)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if len(lines[0].Words) != 2 {
		t.Fatalf("expected 2 words, got %d", len(lines[0].Words))
	}
	if lines[0].Words[0].EndMs != 6447 {
		t.Fatalf("expected word 0 EndMs=6447, got %d", lines[0].Words[0].EndMs)
	}
	if lines[0].Words[1].EndMs != 8400 {
		t.Fatalf("expected word 1 EndMs=8400, got %d", lines[0].Words[1].EndMs)
	}
}

func TestCleanTrackTitleAndArtist(t *testing.T) {
	if got := cleanTrackTitle("Bohemian Rhapsody - Remastered 2011"); got != "Bohemian Rhapsody" {
		t.Fatalf("unexpected cleaned title: %q", got)
	}
	if got := cleanTrackTitle("Stay (feat. Justin Bieber)"); got != "Stay" {
		t.Fatalf("unexpected cleaned title: %q", got)
	}
	if got := primaryArtist("Perunggu, Hindia"); got != "Perunggu" {
		t.Fatalf("unexpected primary artist: %q", got)
	}
}

func TestDimensionsForModeLocked(t *testing.T) {
	app := NewApp()
	app.settings.ModeSizes["pill"] = WindowSize{Width: 640, Height: 140}

	app.mu.Lock()
	w, h := app.dimensionsForModeLocked("pill")
	cw, ch := app.dimensionsForModeLocked("card")
	app.mu.Unlock()

	if w != 640 || h != 140 {
		t.Fatalf("expected custom pill size 640x140, got %dx%d", w, h)
	}
	if cw != 460 || ch != 316 {
		t.Fatalf("expected default card size 460x316, got %dx%d", cw, ch)
	}
}

