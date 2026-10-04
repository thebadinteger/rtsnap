package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/thebadinteger/rtsnap"
	"github.com/thebadinteger/rtsnap/pkg/h264"
)

func TestWithCodecSelection(t *testing.T) {
	data, err := os.ReadFile("../src/hi264/testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	pkts := makeRTPPacketsH264(nalus)

	srv, err := newMockServer(&mockServer{
		codec:   "h264",
		packets: pkts,
	})
	if err != nil {
		t.Fatalf("start mock server: %v", err)
	}
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, srv.URL(), rtsnap.WithCodec(rtsnap.CodecH264))
	if err != nil {
		t.Fatalf("snapshot with h264 failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected decoded image, got nil")
	}

	_, err = rtsnap.Snapshot(ctx, srv.URL(), rtsnap.WithCodec(rtsnap.CodecH265))
	if err == nil {
		t.Fatal("expected error requesting unavailable h265 from h264 stream, got nil")
	}
}

func TestQueryStream(t *testing.T) {
	srv, err := newMockServer(&mockServer{
		codec: "h264",
	})
	if err != nil {
		t.Fatalf("start mock server: %v", err)
	}
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	info, err := rtsnap.Query(ctx, srv.URL())
	if err != nil {
		t.Fatalf("query stream failed: %v", err)
	}

	if len(info.Tracks) == 0 {
		t.Fatal("expected at least one track in query result")
	}

	if info.Tracks[0].Codec != rtsnap.CodecH264 {
		t.Errorf("expected track codec h264, got %s", info.Tracks[0].Codec)
	}
}
