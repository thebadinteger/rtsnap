package tests

import (
	"context"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/thebadinteger/rtsnap"
)

func snapshotOnce(t *testing.T, pkts [][]byte) {
	t.Helper()

	server, err := newMockServer(&mockServer{codec: "h265", packets: pkts})
	if err != nil {
		return
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = rtsnap.SnapshotJPEG(ctx, server.URL(), 85)
}

func TestSnapshotNoGoroutineLeak(t *testing.T) {
	data, err := os.ReadFile("testdata/1080p.h265")
	if err != nil {
		t.Skip(err)
	}
	pkts := makeRTPPacketsH265(splitAnnexBNALBytes(data))

	snapshotOnce(t, pkts)
	time.Sleep(100 * time.Millisecond)

	base := runtime.NumGoroutine()

	for range 40 {
		snapshotOnce(t, pkts)
	}
	time.Sleep(300 * time.Millisecond)

	if after := runtime.NumGoroutine(); after > base+5 {
		t.Fatalf("goroutine leak: %d -> %d", base, after)
	}
}

func TestSnapshotHeapStable(t *testing.T) {
	data, err := os.ReadFile("testdata/1080p.h265")
	if err != nil {
		t.Skip(err)
	}
	pkts := makeRTPPacketsH265(splitAnnexBNALBytes(data))

	snapshotOnce(t, pkts)
	runtime.GC()

	var base runtime.MemStats
	runtime.ReadMemStats(&base)

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 4 {
				snapshotOnce(t, pkts)
			}
		}()
	}
	wg.Wait()

	runtime.GC()

	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	grown := int64(after.HeapAlloc)/1024/1024 - int64(base.HeapAlloc)/1024/1024
	t.Logf("heap after 64 concurrent snapshots: %d MB", after.HeapAlloc/1024/1024)
	if grown > 300 {
		t.Fatalf("retained %d MB after snapshots", grown)
	}
}
