package tests

import (
	"bytes"
	"context"
	"image/jpeg"
	"os"
	"testing"
	"time"

	"github.com/thebadinteger/rtsnap"
	"github.com/thebadinteger/rtsnap/pkg/h264"
)

func makeRTPPacketsH264(nalus [][]byte) [][]byte {
	var pkts [][]byte
	for i, nal := range nalus {
		hdr := []byte{
			0x80, 0x60,
			0x00, byte(i + 1),
			0x00, 0x00, 0x01, 0x00,
			0x11, 0x22, 0x33, 0x44,
		}
		if i == len(nalus)-1 {
			hdr[1] |= 0x80 // marker bit
		}
		pkts = append(pkts, append(hdr, nal...))
	}
	return pkts
}

func TestH264Snapshot(t *testing.T) {
	data, err := os.ReadFile("../src/hi264/testdata/black_idr.264")
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

	img, err := rtsnap.Snapshot(ctx, server.URL())
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestH264SnapshotBasicAuth(t *testing.T) {
	data, err := os.ReadFile("../src/hi264/testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	server, err := newMockServer(&mockServer{
		authMode: "basic",
		username: "admin",
		password: "secretpassword",
		codec:    "h264",
		packets:  makeRTPPacketsH264(nalus),
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, server.URL(), rtsnap.WithAuth("admin", "secretpassword"))
	if err != nil {
		t.Fatalf("snapshot with basic auth failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestH264SnapshotDigestAuth(t *testing.T) {
	data, err := os.ReadFile("../src/hi264/testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	server, err := newMockServer(&mockServer{
		authMode: "digest",
		username: "camuser",
		password: "campassword",
		codec:    "h264",
		packets:  makeRTPPacketsH264(nalus),
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, server.URL(), rtsnap.WithAuth("camuser", "campassword"))
	if err != nil {
		t.Fatalf("snapshot with digest auth failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestH264SnapshotFUA(t *testing.T) {
	data, err := os.ReadFile("../src/hi264/testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	var pkts [][]byte

	// send sps and pps as single nalus
	for i := 0; i < len(nalus)-1; i++ {
		hdr := []byte{0x80, 0x60, 0x00, byte(i + 1), 0x00, 0x00, 0x01, 0x00, 0x11, 0x22, 0x33, 0x44}
		pkts = append(pkts, append(hdr, nalus[i]...))
	}

	// split idr into two fu-a fragments
	idr := nalus[len(nalus)-1]
	idrHeader := idr[0]
	idrPayload := idr[1:]
	half := len(idrPayload) / 2

	// fragment 1 (start)
	fuaHdr1 := []byte{
		(idrHeader & 0x60) | 28,   // FU indicator
		0x80 | (idrHeader & 0x1f), // FU header: start=1, end=0
	}
	rtpHdr1 := []byte{0x80, 0x60, 0x00, 0x10, 0x00, 0x00, 0x01, 0x00, 0x11, 0x22, 0x33, 0x44}
	pkt1 := append(rtpHdr1, fuaHdr1...)
	pkt1 = append(pkt1, idrPayload[:half]...)
	pkts = append(pkts, pkt1)

	// fragment 2 (end)
	fuaHdr2 := []byte{
		(idrHeader & 0x60) | 28,
		0x40 | (idrHeader & 0x1f), // FU header: start=0, end=1
	}
	rtpHdr2 := []byte{0x80, 0xe0, 0x00, 0x11, 0x00, 0x00, 0x01, 0x00, 0x11, 0x22, 0x33, 0x44} // marker bit set
	pkt2 := append(rtpHdr2, fuaHdr2...)
	pkt2 = append(pkt2, idrPayload[half:]...)
	pkts = append(pkts, pkt2)

	server, err := newMockServer(&mockServer{
		codec:   "h264",
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
		t.Fatalf("snapshot with fu-a failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestH264SnapshotSTAPA(t *testing.T) {
	data, err := os.ReadFile("../src/hi264/testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	var pkts [][]byte

	// pack sps and pps into stap-a
	var stapPayload []byte
	stapPayload = append(stapPayload, 24) // stap-a type
	for i := 0; i < len(nalus)-1; i++ {
		size := len(nalus[i])
		stapPayload = append(stapPayload, byte(size>>8), byte(size))
		stapPayload = append(stapPayload, nalus[i]...)
	}
	hdr1 := []byte{0x80, 0x60, 0x00, 0x01, 0x00, 0x00, 0x01, 0x00, 0x11, 0x22, 0x33, 0x44}
	pkts = append(pkts, append(hdr1, stapPayload...))

	// send idr
	idr := nalus[len(nalus)-1]
	hdr2 := []byte{0x80, 0xe0, 0x00, 0x02, 0x00, 0x00, 0x01, 0x00, 0x11, 0x22, 0x33, 0x44}
	pkts = append(pkts, append(hdr2, idr...))

	server, err := newMockServer(&mockServer{
		codec:   "h264",
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
		t.Fatalf("snapshot with stap-a failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestH264SnapshotJPEG(t *testing.T) {
	data, err := os.ReadFile("../src/hi264/testdata/black_idr.264")
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

	jpegBytes, err := rtsnap.SnapshotJPEG(ctx, server.URL(), 90)
	if err != nil {
		t.Fatalf("snapshot jpeg failed: %v", err)
	}
	if len(jpegBytes) == 0 {
		t.Fatal("expected non-empty jpeg bytes")
	}

	// verify decoded jpeg
	img, err := jpeg.Decode(bytes.NewReader(jpegBytes))
	if err != nil {
		t.Fatalf("decode jpeg bytes failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil decoded image")
	}
}
