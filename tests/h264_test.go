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

	img, err := rtsnap.Snapshot(ctx, server.URL())
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestH264SnapshotBasicAuth(t *testing.T) {
	data, err := os.ReadFile("testdata/black_idr.264")
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
	data, err := os.ReadFile("testdata/black_idr.264")
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
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	var pkts [][]byte

	for i := 0; i < len(nalus)-1; i++ {
		hdr := []byte{0x80, 0x60, 0x00, byte(i + 1), 0x00, 0x00, 0x01, 0x00, 0x11, 0x22, 0x33, 0x44}
		pkts = append(pkts, append(hdr, nalus[i]...))
	}

	idr := nalus[len(nalus)-1]
	idrHeader := idr[0]
	idrPayload := idr[1:]
	half := len(idrPayload) / 2

	fuaHdr1 := []byte{
		(idrHeader & 0x60) | 28,
		0x80 | (idrHeader & 0x1f),
	}
	seq1 := byte(len(nalus))
	rtpHdr1 := []byte{0x80, 0x60, 0x00, seq1, 0x00, 0x00, 0x01, 0x00, 0x11, 0x22, 0x33, 0x44}
	pkt1 := append(rtpHdr1, fuaHdr1...)
	pkt1 = append(pkt1, idrPayload[:half]...)
	pkts = append(pkts, pkt1)

	fuaHdr2 := []byte{
		(idrHeader & 0x60) | 28,
		0x40 | (idrHeader & 0x1f),
	}
	seq2 := byte(len(nalus) + 1)
	rtpHdr2 := []byte{0x80, 0xe0, 0x00, seq2, 0x00, 0x00, 0x01, 0x00, 0x11, 0x22, 0x33, 0x44} // marker bit
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
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	var pkts [][]byte

	var stapPayload []byte
	stapPayload = append(stapPayload, 24)
	for i := 0; i < len(nalus)-1; i++ {
		size := len(nalus[i])
		stapPayload = append(stapPayload, byte(size>>8), byte(size))
		stapPayload = append(stapPayload, nalus[i]...)
	}
	hdr1 := []byte{0x80, 0x60, 0x00, 0x01, 0x00, 0x00, 0x01, 0x00, 0x11, 0x22, 0x33, 0x44}
	pkts = append(pkts, append(hdr1, stapPayload...))

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

func TestH264GapKeepsParamSets(t *testing.T) {
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	var sps, pps, idr []byte
	for _, nalu := range h264.ExtractNalusFromByteStream(data) {
		switch h264.NaluType(nalu[0] & 0x1f) {
		case h264.NALU_SPS:
			if sps == nil {
				sps = nalu
			}
		case h264.NALU_PPS:
			if pps == nil {
				pps = nalu
			}
		case h264.NALU_IDR:
			if idr == nil {
				idr = nalu
			}
		}
	}
	if sps == nil || pps == nil || idr == nil {
		t.Fatal("fixture lacks sps/pps/idr units")
	}

	mk := func(seq byte, mark bool, payload []byte) []byte {
		hdr := []byte{0x80, 0x60, 0x00, seq, 0x00, 0x00, 0x01, 0x00, 0x11, 0x22, 0x33, 0x44}
		if mark {
			hdr[1] |= 0x80
		}
		return append(hdr, payload...)
	}
	pkts := [][]byte{
		mk(1, false, sps),
		mk(2, false, pps),
		mk(4, true, idr),
	}

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
		t.Fatalf("snapshot after gap failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestH264SpropTrackOnly(t *testing.T) {
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	var idr []byte
	for _, nalu := range h264.ExtractNalusFromByteStream(data) {
		if h264.NaluType(nalu[0]&0x1f) == h264.NALU_IDR {
			idr = nalu
			break
		}
	}
	if idr == nil {
		t.Fatal("fixture lacks idr unit")
	}

	hdr := []byte{0x80, 0xe0, 0x00, 0x01, 0x00, 0x00, 0x01, 0x00, 0x11, 0x22, 0x33, 0x44}
	server, err := newMockServer(&mockServer{
		codec:   "h264sprop",
		packets: [][]byte{append(hdr, idr...)},
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, server.URL())
	if err != nil {
		t.Fatalf("snapshot from sprop track failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestH264SnapshotJPEG(t *testing.T) {
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

	jpegBytes, err := rtsnap.SnapshotJPEG(ctx, server.URL(), 90)
	if err != nil {
		t.Fatalf("snapshot jpeg failed: %v", err)
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
