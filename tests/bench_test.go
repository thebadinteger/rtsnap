package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/thebadinteger/rtsnap"
	"github.com/thebadinteger/rtsnap/pkg/h264"
)

// split nal units into fu sized rtp packets the way cameras send key frames
func fragmentRTP(nalus [][]byte, h265 bool, mtu int) [][]byte {
	var pkts [][]byte
	seq := uint16(0)
	add := func(marker bool, payload ...[]byte) {
		seq++
		pkt := []byte{0x80, 0x60, byte(seq >> 8), byte(seq), 0, 0, 1, 0, 0x11, 0x22, 0x33, 0x44}
		if marker {
			pkt[1] |= 0x80
		}
		for _, p := range payload {
			pkt = append(pkt, p...)
		}
		pkts = append(pkts, pkt)
	}

	for i, nal := range nalus {
		last := i == len(nalus)-1
		if len(nal) <= mtu {
			add(last, nal)
			continue
		}

		hdr := 1
		if h265 {
			hdr = 2
		}
		body := nal[hdr:]
		for off := 0; off < len(body); off += mtu {
			end := min(off+mtu, len(body))
			var flags byte
			if off == 0 {
				flags |= 0x80
			}
			if end == len(body) {
				flags |= 0x40
			}
			if h265 {
				fu := []byte{(nal[0] & 0x81) | 49<<1, nal[1], flags | (nal[0]>>1)&0x3f}
				add(last && end == len(body), fu, body[off:end])
			} else {
				fu := []byte{(nal[0] & 0xe0) | 28, flags | nal[0]&0x1f}
				add(last && end == len(body), fu, body[off:end])
			}
		}
	}
	return pkts
}

func benchSnapshot(b *testing.B, codec string, pkts [][]byte, jpeg bool, opts ...rtsnap.Option) {
	server, err := newMockServer(&mockServer{codec: codec, packets: pkts})
	if err != nil {
		b.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if jpeg {
			_, err = rtsnap.SnapshotJPEG(ctx, server.URL(), 85, opts...)
		} else {
			_, err = rtsnap.Snapshot(ctx, server.URL(), opts...)
		}
		cancel()
		if err != nil {
			b.Fatalf("snapshot: %v", err)
		}
	}
}

// stream to benchmark, an annex b capture named by the environment wins over the tiny fixture
func benchFile(env, fallback string) string {
	if path := os.Getenv(env); path != "" {
		return path
	}
	return fallback
}

func BenchmarkSnapshotH264(b *testing.B) {
	data, err := os.ReadFile(benchFile("RTSNAP_BENCH_H264", "testdata/testpic_idr.264"))
	if err != nil {
		b.Skip(err)
	}
	benchSnapshot(b, "h264", fragmentRTP(h264.ExtractNalusFromByteStream(data), false, 1400), false)
}

func BenchmarkSnapshotH265(b *testing.B) {
	data, err := os.ReadFile(benchFile("RTSNAP_BENCH_H265", "testdata/1080p.h265"))
	if err != nil {
		b.Skip(err)
	}
	benchSnapshot(b, "h265", fragmentRTP(splitAnnexBNALBytes(data), true, 1400), false)
}

func BenchmarkSnapshotH265Fast(b *testing.B) {
	data, err := os.ReadFile(benchFile("RTSNAP_BENCH_H265", "testdata/1080p.h265"))
	if err != nil {
		b.Skip(err)
	}
	benchSnapshot(b, "h265", fragmentRTP(splitAnnexBNALBytes(data), true, 1400), false, rtsnap.WithFast())
}

func BenchmarkSnapshotJPEGH265(b *testing.B) {
	data, err := os.ReadFile(benchFile("RTSNAP_BENCH_H265", "testdata/1080p.h265"))
	if err != nil {
		b.Skip(err)
	}
	benchSnapshot(b, "h265", fragmentRTP(splitAnnexBNALBytes(data), true, 1400), true)
}

func BenchmarkSnapshotMJPEG(b *testing.B) {
	scanData, w, h := makeTestJPEGScanData()
	benchSnapshot(b, "mjpeg", makeMJPEGPackets(scanData, w, h, 3), true)
}
