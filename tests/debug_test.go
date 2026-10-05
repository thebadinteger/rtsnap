package tests

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/thebadinteger/rtsnap"
	"github.com/thebadinteger/rtsnap/pkg/h264"
)

func TestUserAgentDefault(t *testing.T) {
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	server, err := newMockServer(&mockServer{
		codec:   "h264",
		packets: makeRTPPacketsH264(nalus),
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if _, err := rtsnap.Snapshot(ctx, server.URL()); err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}

	server.mu.Lock()
	ua := server.lastUA
	server.mu.Unlock()
	if ua != "rtsnap" {
		t.Fatalf("expected default ua rtsnap, got %q", ua)
	}
}

func TestUserAgentOverride(t *testing.T) {
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	server, err := newMockServer(&mockServer{
		codec:   "h264",
		packets: makeRTPPacketsH264(nalus),
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if _, err := rtsnap.Snapshot(ctx, server.URL(), rtsnap.WithUserAgent("VLC/3.0.16")); err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}

	server.mu.Lock()
	ua := server.lastUA
	server.mu.Unlock()
	if ua != "VLC/3.0.16" {
		t.Fatalf("expected custom ua, got %q", ua)
	}
}

func TestDebugTranscript(t *testing.T) {
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	server, err := newMockServer(&mockServer{
		codec:   "h264",
		packets: makeRTPPacketsH264(nalus),
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	var lines []string
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if _, err := rtsnap.Snapshot(ctx, server.URL(), rtsnap.WithDebugFunc(func(s string) {
		lines = append(lines, s)
	})); err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}

	joined := strings.Join(lines, "\n")
	for _, want := range []string{"> DESCRIBE", "< RTSP/1.0 200", "> SETUP", "> PLAY"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("transcript missing %q:\n%s", want, joined)
		}
	}
}
