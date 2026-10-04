package tests

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/thebadinteger/rtsnap"
	"github.com/thebadinteger/rtsnap/pkg/h264"
	"github.com/thebadinteger/rtsnap/pkg/h265"
)

func BenchmarkH264DecodeHD(b *testing.B) {
	data, err := os.ReadFile("../src/hi264/testdata/golden/res_hd.264")
	if err != nil {
		b.Skip("testdata not found")
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		d := h264.New()
		d.SkipDeblock = true
		f, err := d.DecodeAnnexB(data)
		if err != nil || f == nil {
			b.Fatalf("decode failed: %v", err)
		}
	}
}

func BenchmarkH265Decode1080p(b *testing.B) {
	data, err := os.ReadFile("../src/h265/hevc/testdata/1080p.h265")
	if err != nil {
		b.Skip("testdata not found")
	}

	nals := h265.SplitAnnexB(data)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var d h265.Decoder
		for _, nal := range nals {
			_, _ = d.DecodeNAL(nal)
		}
		_ = d.Flush()
	}
}

func BenchmarkConcurrentH264Snapshots(b *testing.B) {
	data, err := os.ReadFile("../src/hi264/testdata/testpic_idr.264")
	if err != nil {
		b.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	server, err := newMockServer(&mockServer{
		codec:   "h264",
		packets: makeRTPPacketsH264(nalus),
	})
	if err != nil {
		b.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	url := server.URL()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx := context.Background()
			_, err := rtsnap.Snapshot(ctx, url)
			if err != nil {
				b.Fatalf("snapshot failed: %v", err)
			}
		}
	})
}

func BenchmarkBatchConcurrent50(b *testing.B) {
	data, err := os.ReadFile("../src/hi264/testdata/testpic_idr.264")
	if err != nil {
		b.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	server, err := newMockServer(&mockServer{
		codec:   "h264",
		packets: makeRTPPacketsH264(nalus),
	})
	if err != nil {
		b.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	url := server.URL()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		const concurrency = 50
		wg.Add(concurrency)
		for j := 0; j < concurrency; j++ {
			go func() {
				defer wg.Done()
				ctx := context.Background()
				_, _ = rtsnap.Snapshot(ctx, url)
			}()
		}
		wg.Wait()
	}
}
