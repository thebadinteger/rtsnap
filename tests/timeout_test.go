package tests

import (
	"context"
	"testing"
	"time"

	"github.com/thebadinteger/rtsnap"
)

func TestTimeoutDial(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := rtsnap.Snapshot(ctx, "rtsp://127.0.0.1:54321/live", rtsnap.WithTimeout(200*time.Millisecond))
	duration := time.Since(start)

	if err == nil {
		t.Fatal("expected dial error, got nil")
	}

	if duration > 2*time.Second {
		t.Fatalf("timeout took too long: %v", duration)
	}
}

func TestTimeoutNoVideo(t *testing.T) {
	server, err := newMockServer(&mockServer{
		codec:   "h264",
		noVideo: true,
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = rtsnap.Snapshot(ctx, server.URL(), rtsnap.WithTimeout(300*time.Millisecond))
	duration := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	if duration > 2*time.Second {
		t.Fatalf("stream timeout took too long: %v", duration)
	}
}

func TestContextCancellation(t *testing.T) {
	server, err := newMockServer(&mockServer{
		codec:   "h264",
		noVideo: true,
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err = rtsnap.Snapshot(ctx, server.URL())
	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
}

func TestInvalidURL(t *testing.T) {
	ctx := context.Background()
	_, err := rtsnap.Snapshot(ctx, "::bad url::")
	if err == nil {
		t.Fatal("expected error on malformed url, got nil")
	}
}
