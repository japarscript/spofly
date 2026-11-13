//go:build !darwin && !windows

package main

import "context"

// SpotifyClient is a no-op stub on unsupported platforms so unit tests and cross-platform checks compile cleanly.
type SpotifyClient struct{}

// NewSpotifyClient returns a stub SpotifyClient.
func NewSpotifyClient() *SpotifyClient { return &SpotifyClient{} }

// Start is a no-op on unsupported platforms.
func (s *SpotifyClient) Start(_ context.Context) {}

// GetState returns an inactive TrackState on unsupported platforms.
func (s *SpotifyClient) GetState() TrackState { return TrackState{Source: "none"} }

// PlayPause is a no-op on unsupported platforms.
func (s *SpotifyClient) PlayPause() error { return nil }

// NextTrack is a no-op on unsupported platforms.
func (s *SpotifyClient) NextTrack() error { return nil }

// PrevTrack is a no-op on unsupported platforms.
func (s *SpotifyClient) PrevTrack() error { return nil }

// SeekTo is a no-op on unsupported platforms.
func (s *SpotifyClient) SeekTo(_ int64) error { return nil }

// ConfigureFloatingWindow is a no-op on unsupported platforms.
func ConfigureFloatingWindow() {}

// SetWindowAppearance is a no-op on unsupported platforms.
func SetWindowAppearance(_ bool, _ float64) {}

// SetClickThrough is a no-op on unsupported platforms.
func SetClickThrough(_ bool) {}

// SetupPlatformShell is a no-op on unsupported platforms.
func SetupPlatformShell(_ func(string), _ func(bool)) {}

