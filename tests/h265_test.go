package tests

import (
	"bytes"
	"context"
	"image/jpeg"
	"os"
	"testing"
	"time"

	"github.com/thebadinteger/rtsnap"
)

func makeRTPPacketsH265(rawNALs [][]byte) [][]byte {
	var pkts [][]byte
	for i, nal := range rawNALs {
		hdr := []byte{
			0x80, 0x60,
			0x00, byte(i + 1),
			0x00, 0x00, 0x01, 0x00,
			0x11, 0x22, 0x33, 0x44,
		}
		if i == len(rawNALs)-1 {
			hdr[1] |= 0x80 // marker bit
		}
		pkts = append(pkts, append(hdr, nal...))
	}
	return pkts
}

func splitAnnexBNALBytes(data []byte) [][]byte {
	var nals [][]byte
	start := -1
	for i := 0; i < len(data)-2; i++ {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			if start != -1 {
				end := i
				if end > 0 && data[end-1] == 0 {
					end--
				}
				nals = append(nals, data[start:end])
			}
			start = i + 3
		}
	}
	if start != -1 && start < len(data) {
		nals = append(nals, data[start:])
	}
	return nals
}

func TestH265Snapshot(t *testing.T) {
	data, err := os.ReadFile("../src/h265/hevc/testdata/tiny_intra.h265")
	if err != nil {
		t.Skip("testdata not found")
	}

	rawNALs := splitAnnexBNALBytes(data)
	server, err := newMockServer(&mockServer{
		codec:   "h265",
		packets: makeRTPPacketsH265(rawNALs),
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, server.URL())
	if err != nil {
		t.Fatalf("h265 snapshot failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestH265SnapshotDigestAuth(t *testing.T) {
	data, err := os.ReadFile("../src/h265/hevc/testdata/tiny_intra.h265")
	if err != nil {
		t.Skip("testdata not found")
	}

	rawNALs := splitAnnexBNALBytes(data)
	server, err := newMockServer(&mockServer{
		authMode: "digest",
		username: "hevcuser",
		password: "hevcpassword",
		codec:    "h265",
		packets:  makeRTPPacketsH265(rawNALs),
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, server.URL(), rtsnap.WithAuth("hevcuser", "hevcpassword"))
	if err != nil {
		t.Fatalf("h265 snapshot with auth failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestH265Snapshot1080p(t *testing.T) {
	data, err := os.ReadFile("../src/h265/hevc/testdata/1080p.h265")
	if err != nil {
		t.Skip("testdata not found")
	}

	rawNALs := splitAnnexBNALBytes(data)
	server, err := newMockServer(&mockServer{
		codec:   "h265",
		packets: makeRTPPacketsH265(rawNALs),
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, server.URL())
	if err != nil {
		t.Fatalf("1080p h265 snapshot failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}

	b := img.Bounds()
	if b.Dx() != 1920 || b.Dy() != 1080 {
		t.Fatalf("expected 1920x1080, got %dx%d", b.Dx(), b.Dy())
	}
}

func TestH265SnapshotJPEG(t *testing.T) {
	data, err := os.ReadFile("../src/h265/hevc/testdata/tiny_intra.h265")
	if err != nil {
		t.Skip("testdata not found")
	}

	rawNALs := splitAnnexBNALBytes(data)
	server, err := newMockServer(&mockServer{
		codec:   "h265",
		packets: makeRTPPacketsH265(rawNALs),
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	jpegBytes, err := rtsnap.SnapshotJPEG(ctx, server.URL(), 85)
	if err != nil {
		t.Fatalf("h265 snapshot jpeg failed: %v", err)
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
