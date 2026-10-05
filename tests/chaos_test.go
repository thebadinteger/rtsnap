package tests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/draw"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/thebadinteger/rtsnap"
	"github.com/thebadinteger/rtsnap/pkg/h264"
	"github.com/thebadinteger/rtsnap/pkg/h265"
)

func hashImage(img image.Image) string {
	b := img.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	sum := sha256.Sum256(dst.Pix)
	return hex.EncodeToString(sum[:])
}

func snapshotHash(t *testing.T, url string, opts ...rtsnap.Option) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, url, opts...)
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
	return hashImage(img)
}

func servePackets(t *testing.T, codec string, packets [][]byte) *mockServer {
	t.Helper()
	server, err := newMockServer(&mockServer{
		codec:   codec,
		packets: packets,
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	return server
}

func dropSeeded(pkts [][]byte, seed int64, pct int) [][]byte {
	rng := rand.New(rand.NewSource(seed))
	var out [][]byte
	for _, p := range pkts {
		if rng.Intn(100) < pct {
			continue
		}
		out = append(out, p)
	}
	return out
}

func dupEach(pkts [][]byte) [][]byte {
	var out [][]byte
	for _, p := range pkts {
		out = append(out, p, p)
	}
	return out
}

func flipSeeded(pkts [][]byte, seed int64, pct int, from int) [][]byte {
	rng := rand.New(rand.NewSource(seed))
	var out [][]byte
	for _, p := range pkts {
		q := append([]byte{}, p...)
		for i := from; i < len(q); i++ {
			if rng.Intn(100) < pct {
				q[i] ^= byte(1 + rng.Intn(255))
			}
		}
		out = append(out, q)
	}
	return out
}

func fragmentFU(nal []byte) [][]byte {
	indicator := (nal[0] & 0x60) | 28
	sub := nal[0] & 0x1f
	half := (len(nal) - 1) / 2
	return [][]byte{
		append([]byte{indicator, 0x80 | sub}, nal[1:1+half]...),
		append([]byte{indicator, 0x40 | sub}, nal[1+half:]...),
	}
}

func TestChaosH264GlitchyStart(t *testing.T) {
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	clean := makeRTPPacketsH264(nalus)
	refServer := servePackets(t, "h264", clean)
	defer refServer.Close()
	ref := snapshotHash(t, refServer.URL())

	var first [][]byte
	for i, p := range clean {
		if i == 1 {
			continue
		}
		first = append(first, p)
	}
	first = append(first, clean...)
	first = append(first, clean...)
	server := servePackets(t, "h264", first)
	defer server.Close()

	got := snapshotHash(t, server.URL())
	if got != ref {
		t.Fatal("glitchy start produced different image")
	}
}

func TestChaosH264DupStorm(t *testing.T) {
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	clean := makeRTPPacketsH264(nalus)
	refServer := servePackets(t, "h264", clean)
	defer refServer.Close()
	ref := snapshotHash(t, refServer.URL())

	duped := dupEach(clean)
	duped = append(duped, clean...)
	server := servePackets(t, "h264", duped)
	defer server.Close()

	got := snapshotHash(t, server.URL())
	if got != ref {
		t.Fatal("duplicate storm produced different image")
	}
}

func TestChaosH264HeavyLoss(t *testing.T) {
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	var all [][]byte
	for c := 0; c < 2; c++ {
		all = append(all, makeRTPPacketsH264(nalus)...)
	}
	mangled := dropSeeded(all, 99, 40)

	server := servePackets(t, "h264", mangled)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, server.URL())
	if err != nil {
		return
	}
	refServer := servePackets(t, "h264", makeRTPPacketsH264(nalus))
	defer refServer.Close()
	if hashImage(img) != snapshotHash(t, refServer.URL()) {
		t.Fatal("corrupt image returned instead of clean one")
	}
}

func TestChaosH264TruncatedFU(t *testing.T) {
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	clean := makeRTPPacketsH264(nalus)
	refServer := servePackets(t, "h264", clean)
	defer refServer.Close()
	ref := snapshotHash(t, refServer.URL())

	frags := fragmentFU(nalus[len(nalus)-1])
	stream := append([][]byte{frags[0]}, clean...)
	server := servePackets(t, "h264", stream)
	defer server.Close()

	got := snapshotHash(t, server.URL())
	if got != ref {
		t.Fatal("truncated fragment poisoned next frame")
	}
}

func TestChaosH264LateJoin(t *testing.T) {
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	clean := makeRTPPacketsH264(nalus)
	refServer := servePackets(t, "h264", clean)
	defer refServer.Close()
	ref := snapshotHash(t, refServer.URL())

	late := append([][]byte{}, clean...)
	late = append(late, clean...)
	server := servePackets(t, "h264", late)
	defer server.Close()

	got := snapshotHash(t, server.URL())
	if got != ref {
		t.Fatal("late join produced different image")
	}
}

func TestChaosH264CorruptBytes(t *testing.T) {
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	clean := makeRTPPacketsH264(nalus)
	refServer := servePackets(t, "h264", clean)
	defer refServer.Close()
	ref := snapshotHash(t, refServer.URL())

	var stream [][]byte
	stream = append(stream, flipSeeded(clean, 21, 10, 12)...)
	stream = append(stream, clean...)
	server := servePackets(t, "h264", stream)
	defer server.Close()

	got := snapshotHash(t, server.URL())
	if got != ref {
		t.Fatal("corrupt bytes produced different image")
	}
}

func TestChaosH265CorruptBytes(t *testing.T) {
	data, err := os.ReadFile("testdata/tiny_intra.h265")
	if err != nil {
		t.Skip("testdata not found")
	}

	rawNALs := splitAnnexBNALBytes(data)
	clean := makeRTPPacketsH265(rawNALs)
	refServer := servePackets(t, "h265", clean)
	defer refServer.Close()
	ref := snapshotHash(t, refServer.URL())

	var stream [][]byte
	stream = append(stream, flipSeeded(clean, 22, 10, 14)...)
	stream = append(stream, clean...)
	server := servePackets(t, "h265", stream)
	defer server.Close()

	got := snapshotHash(t, server.URL())
	if got != ref {
		t.Fatal("corrupt bytes produced different image")
	}
}

func TestChaosMJPEGCorruptBytes(t *testing.T) {
	scanData, w, h := makeTestJPEGScanData()
	frame := makeMJPEGPackets(scanData, w, h, 3)
	refServer := servePackets(t, "mjpeg", frame)
	defer refServer.Close()
	ref := snapshotHash(t, refServer.URL())

	var stream [][]byte
	stream = append(stream, flipSeeded(frame, 23, 10, 20)...)
	stream = append(stream, frame...)
	server := servePackets(t, "mjpeg", stream)
	defer server.Close()

	got := snapshotHash(t, server.URL())
	if got != ref {
		t.Fatal("corrupt bytes produced different image")
	}
}

func TestChaosH265GlitchyStart(t *testing.T) {
	data, err := os.ReadFile("testdata/tiny_intra.h265")
	if err != nil {
		t.Skip("testdata not found")
	}

	rawNALs := splitAnnexBNALBytes(data)
	clean := makeRTPPacketsH265(rawNALs)
	refServer := servePackets(t, "h265", clean)
	defer refServer.Close()
	ref := snapshotHash(t, refServer.URL())

	var first [][]byte
	for i := len(clean) - 1; i >= 0; i-- {
		first = append(first, clean[i])
	}
	first = append(first, dupEach(clean)...)
	first = append(first, clean...)
	server := servePackets(t, "h265", first)
	defer server.Close()

	got := snapshotHash(t, server.URL())
	if got != ref {
		t.Fatal("glitchy start produced different image")
	}
}

func TestChaosMJPEGLoss(t *testing.T) {
	scanData, w, h := makeTestJPEGScanData()
	frame := makeMJPEGPackets(scanData, w, h, 3)
	refServer := servePackets(t, "mjpeg", frame)
	defer refServer.Close()
	ref := snapshotHash(t, refServer.URL())

	var stream [][]byte
	stream = append(stream, frame[:len(frame)-1]...)
	stream = append(stream, frame...)
	server := servePackets(t, "mjpeg", stream)
	defer server.Close()

	got := snapshotHash(t, server.URL())
	if got != ref {
		t.Fatal("lost fragment poisoned next frame")
	}
}

func TestChaosH265MidGOPJoin(t *testing.T) {
	data, err := os.ReadFile("testdata/pgop.h265")
	if err != nil {
		t.Skip("testdata not found")
	}

	rawNALs := splitAnnexBNALBytes(data)
	var irapIdx []int
	for i, raw := range rawNALs {
		if u, ok := h265.ParseNAL(raw); ok && u.Type.IsIRAP() {
			irapIdx = append(irapIdx, i)
		}
	}
	if len(irapIdx) < 2 {
		t.Fatal("fixture lacks two keyframes")
	}

	var joined [][]byte
	for i, raw := range rawNALs {
		if i == irapIdx[0] {
			continue
		}
		joined = append(joined, raw)
	}
	server := servePackets(t, "h265", makeRTPPacketsH265(joined))
	defer server.Close()

	got := snapshotHash(t, server.URL())

	var refNALs [][]byte
	refNALs = append(refNALs, rawNALs[:4]...)
	refNALs = append(refNALs, rawNALs[irapIdx[1]])
	refServer := servePackets(t, "h265", makeRTPPacketsH265(refNALs))
	defer refServer.Close()

	if got != snapshotHash(t, refServer.URL()) {
		t.Fatal("mid gop join did not return clean keyframe")
	}
}

func TestChaosH265NoKeyframe(t *testing.T) {
	data, err := os.ReadFile("testdata/pgop.h265")
	if err != nil {
		t.Skip("testdata not found")
	}

	rawNALs := splitAnnexBNALBytes(data)
	var first [][]byte
	for i, raw := range rawNALs {
		if u, ok := h265.ParseNAL(raw); ok && u.Type.IsIRAP() {
			first = rawNALs[:i]
			break
		}
	}
	if len(first) == 0 {
		t.Fatal("fixture lacks keyframe")
	}

	server := servePackets(t, "h265", makeRTPPacketsH265(first))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if _, err := rtsnap.Snapshot(ctx, server.URL()); err == nil {
		t.Fatal("expected timeout without keyframe, got image")
	}
}

func TestChaosH265FullPStream(t *testing.T) {
	data, err := os.ReadFile("testdata/pgop.h265")
	if err != nil {
		t.Skip("testdata not found")
	}

	rawNALs := splitAnnexBNALBytes(data)
	end := len(rawNALs)
	for i, raw := range rawNALs {
		if u, ok := h265.ParseNAL(raw); ok && u.Type.IsIRAP() {
			end = i + 1
			break
		}
	}

	ref := snapshotHash(t, servePackets(t, "h265", makeRTPPacketsH265(rawNALs[:end])).URL())
	server := servePackets(t, "h265", makeRTPPacketsH265(rawNALs))
	defer server.Close()

	got := snapshotHash(t, server.URL())
	if got != ref {
		t.Fatal("p stream changed keyframe pixels")
	}
}

func TestChaosH264MidGOPJoin(t *testing.T) {
	data, err := os.ReadFile("testdata/pgop.h264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	var firstIDR, secondIDR int = -1, -1
	for i, nalu := range nalus {
		if h264.NaluType(nalu[0]&0x1f) == h264.NALU_IDR {
			if firstIDR < 0 {
				firstIDR = i
			} else if secondIDR < 0 {
				secondIDR = i
			}
		}
	}
	if firstIDR < 0 || secondIDR < 0 {
		t.Fatal("fixture lacks two keyframes")
	}

	var refSPS, refPPS []byte
	for i := secondIDR - 1; i >= 0; i-- {
		switch h264.NaluType(nalus[i][0] & 0x1f) {
		case h264.NALU_SPS:
			if refSPS == nil {
				refSPS = nalus[i]
			}
		case h264.NALU_PPS:
			if refPPS == nil {
				refPPS = nalus[i]
			}
		}
	}
	if refSPS == nil || refPPS == nil {
		t.Fatal("fixture lacks param sets before second keyframe")
	}
	ref := snapshotHash(t, servePackets(t, "h264", makeRTPPacketsH264([][]byte{refSPS, refPPS, nalus[secondIDR]})).URL())

	var joined [][]byte
	for i, nalu := range nalus {
		if i == firstIDR {
			continue
		}
		joined = append(joined, nalu)
	}
	server := servePackets(t, "h264", makeRTPPacketsH264(joined))
	defer server.Close()

	got := snapshotHash(t, server.URL())
	if got != ref {
		t.Fatal("mid gop join did not return clean keyframe")
	}
}

func TestChaosH264NoKeyframe(t *testing.T) {
	data, err := os.ReadFile("testdata/pgop.h264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	var head [][]byte
	for _, nalu := range nalus {
		if h264.NaluType(nalu[0]&0x1f) == h264.NALU_IDR {
			break
		}
		head = append(head, nalu)
	}
	if len(head) == 0 {
		t.Fatal("fixture lacks leading non keyframe units")
	}

	server := servePackets(t, "h264", makeRTPPacketsH264(head))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if _, err := rtsnap.Snapshot(ctx, server.URL()); err == nil {
		t.Fatal("expected timeout without keyframe, got image")
	}
}
