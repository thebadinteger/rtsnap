package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/thebadinteger/rtsnap"
	"github.com/thebadinteger/rtsnap/pkg/h264"
)

func TestUDPSnapshotH264(t *testing.T) {
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	server, err := newMockServer(&mockServer{
		udp:     true,
		codec:   "h264",
		packets: makeRTPPacketsH264(nalus),
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, server.URL(), rtsnap.WithTransport(rtsnap.TransportUDP))
	if err != nil {
		t.Fatalf("udp snapshot failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestUDPSnapshotMJPEG(t *testing.T) {
	scanData, w, h := makeTestJPEGScanData()
	server, err := newMockServer(&mockServer{
		udp:     true,
		codec:   "mjpeg",
		packets: makeMJPEGPackets(scanData, w, h, 3),
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, server.URL(), rtsnap.WithTransport(rtsnap.TransportUDP))
	if err != nil {
		t.Fatalf("udp mjpeg snapshot failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestAutoFallbackTCP(t *testing.T) {
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

	img, err := rtsnap.Snapshot(ctx, server.URL(), rtsnap.WithTransport(rtsnap.TransportAuto))
	if err != nil {
		t.Fatalf("auto fallback snapshot failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestAutoPrefersUDP(t *testing.T) {
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	server, err := newMockServer(&mockServer{
		udp:     true,
		codec:   "h264",
		packets: makeRTPPacketsH264(nalus),
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, server.URL(), rtsnap.WithTransport(rtsnap.TransportAuto))
	if err != nil {
		t.Fatalf("auto udp snapshot failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestUDPForcedFailsWithoutUDP(t *testing.T) {
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

	_, err = rtsnap.Snapshot(ctx, server.URL(), rtsnap.WithTransport(rtsnap.TransportUDP))
	if err == nil {
		t.Fatal("expected error for forced udp against tcp-only server, got nil")
	}
}

func TestUDPLossRecovery(t *testing.T) {
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	first := makeRTPPacketsH264(nalus)
	second := makeRTPPacketsH264(nalus)
	for i := range second {
		seq := uint16(len(first) + i + 1)
		second[i][2] = byte(seq >> 8)
		second[i][3] = byte(seq)
	}
	packets := append(append([][]byte{}, first...), second...)

	server, err := newMockServer(&mockServer{
		udp:     true,
		codec:   "h264",
		packets: packets,
		drop:    map[int]bool{1: true},
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, server.URL(), rtsnap.WithTransport(rtsnap.TransportUDP))
	if err != nil {
		t.Fatalf("udp loss recovery snapshot failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}
