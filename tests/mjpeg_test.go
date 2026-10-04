package tests

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
	"time"

	"github.com/thebadinteger/rtsnap"
)

func makeTestJPEGScanData() ([]byte, int, int) {
	const w, h = 32, 32
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			src.Set(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 7), B: 120, A: 255})
		}
	}

	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, src, &jpeg.Options{Quality: 50})
	data := buf.Bytes()

	sosIdx := bytes.Index(data, []byte{0xFF, 0xDA})
	if sosIdx == -1 {
		return data, w, h
	}
	sosLen := int(data[sosIdx+2])<<8 | int(data[sosIdx+3])
	scanStart := sosIdx + 2 + sosLen
	scanEnd := len(data)
	if bytes.HasSuffix(data, []byte{0xFF, 0xD9}) {
		scanEnd -= 2
	}
	return data[scanStart:scanEnd], w, h
}

func makeMJPEGPackets(scanData []byte, width, height int, fragments int) [][]byte {
	var pkts [][]byte
	chunkSize := (len(scanData) + fragments - 1) / fragments

	for i := 0; i < fragments; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(scanData) {
			end = len(scanData)
		}
		if start >= end {
			break
		}

		offset := start
		hdr := []byte{
			0x80, 0x1A,
			0x00, byte(i + 1),
			0x00, 0x00, 0x02, 0x00,
			0x11, 0x22, 0x33, 0x44,
		}
		if end == len(scanData) {
			hdr[1] |= 0x80 // marker bit
		}

		jpegHdr := []byte{
			0x00,
			byte(offset >> 16), byte(offset >> 8), byte(offset),
			0x01,
			50,
			byte(width / 8),
			byte(height / 8),
		}

		pkt := append(hdr, jpegHdr...)
		pkt = append(pkt, scanData[start:end]...)
		pkts = append(pkts, pkt)
	}
	return pkts
}

func TestMJPEGSnapshot(t *testing.T) {
	scanData, w, h := makeTestJPEGScanData()
	pkts := makeMJPEGPackets(scanData, w, h, 1)

	server, err := newMockServer(&mockServer{
		codec:   "mjpeg",
		packets: pkts,
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, server.URL())
	if err != nil {
		t.Fatalf("mjpeg snapshot failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}

	b := img.Bounds()
	if b.Dx() != w || b.Dy() != h {
		t.Fatalf("expected %dx%d, got %dx%d", w, h, b.Dx(), b.Dy())
	}
}

func TestMJPEGSnapshotMultiFragment(t *testing.T) {
	scanData, w, h := makeTestJPEGScanData()
	pkts := makeMJPEGPackets(scanData, w, h, 3)

	server, err := newMockServer(&mockServer{
		codec:   "mjpeg",
		packets: pkts,
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, server.URL())
	if err != nil {
		t.Fatalf("mjpeg multi-fragment snapshot failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestMJPEGSnapshotDigestAuth(t *testing.T) {
	scanData, w, h := makeTestJPEGScanData()
	pkts := makeMJPEGPackets(scanData, w, h, 1)

	server, err := newMockServer(&mockServer{
		authMode: "digest",
		username: "jpeguser",
		password: "jpegpassword",
		codec:    "mjpeg",
		packets:  pkts,
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, server.URL(), rtsnap.WithAuth("jpeguser", "jpegpassword"))
	if err != nil {
		t.Fatalf("mjpeg snapshot with auth failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestMJPEGSnapshotJPEG(t *testing.T) {
	scanData, w, h := makeTestJPEGScanData()
	pkts := makeMJPEGPackets(scanData, w, h, 2)

	server, err := newMockServer(&mockServer{
		codec:   "mjpeg",
		packets: pkts,
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	jpegBytes, err := rtsnap.SnapshotJPEG(ctx, server.URL(), 75)
	if err != nil {
		t.Fatalf("mjpeg snapshot jpeg failed: %v", err)
	}
	if len(jpegBytes) == 0 {
		t.Fatal("expected non-empty jpeg bytes")
	}

	img, err := jpeg.Decode(bytes.NewReader(jpegBytes))
	if err != nil {
		t.Fatalf("decode jpeg bytes failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil decoded image")
	}
}

func TestMJPEGTranscode(t *testing.T) {
	scanData, w, h := makeTestJPEGScanData()
	pkts := makeMJPEGPackets(scanData, w, h, 2)

	server, err := newMockServer(&mockServer{
		codec:   "mjpeg",
		packets: pkts,
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	jpegBytes, err := rtsnap.SnapshotJPEG(ctx, server.URL(), 10, rtsnap.WithTranscode())
	if err != nil {
		t.Fatalf("mjpeg transcode failed: %v", err)
	}
	if len(jpegBytes) == 0 {
		t.Fatal("expected non-empty jpeg bytes")
	}

	img, err := jpeg.Decode(bytes.NewReader(jpegBytes))
	if err != nil {
		t.Fatalf("decode transcoded jpeg failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil decoded image")
	}
}
