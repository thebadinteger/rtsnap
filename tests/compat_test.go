package tests

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/thebadinteger/rtsnap"
	"github.com/thebadinteger/rtsnap/pkg/h264"
)

func h264Frame(t *testing.T) [][]byte {
	t.Helper()
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}
	nalus := h264.ExtractNalusFromByteStream(data)
	if len(nalus) != 3 {
		t.Fatalf("fixture has %d nal units, want sps, pps and idr", len(nalus))
	}
	return makeRTPPacketsH264(nalus)
}

func TestEarlyRTPBeforePlayResponse(t *testing.T) {
	// the only key frame is sent before play is answered and must not be lost
	server, err := newMockServer(&mockServer{codec: "h264", packets: h264Frame(t), earlyRTP: true})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if _, err := rtsnap.Snapshot(ctx, server.URL()); err != nil {
		t.Fatalf("snapshot with rtp ahead of the play response: %v", err)
	}
}

func TestForeignPayloadTypeIgnored(t *testing.T) {
	data, err := os.ReadFile("testdata/testpic_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}
	frame := fragmentRTP(h264.ExtractNalusFromByteStream(data), false, 200)
	if len(frame) < 8 {
		t.Fatalf("fixture split into %d packets only", len(frame))
	}

	var packets [][]byte
	for i, pkt := range frame {
		packets = append(packets,
			[]byte{0x80, 107, 0x70, byte(i), 0, 0, 0, 9, 0xAA, 0xBB, 0xCC, 0xDD, '<', 'x', '/', '>'},
			pkt)
	}

	server, err := newMockServer(&mockServer{codec: "h264", packets: packets})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if _, err := rtsnap.Snapshot(ctx, server.URL()); err != nil {
		t.Fatalf("snapshot with a foreign payload type in between: %v", err)
	}
}

func TestUDPReorderRecovers(t *testing.T) {
	frame := h264Frame(t)
	// a single key frame whose last two packets swap places on the way
	server, err := newMockServer(&mockServer{
		udp:     true,
		codec:   "h264",
		packets: [][]byte{frame[0], frame[2], frame[1]},
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if _, err := rtsnap.Snapshot(ctx, server.URL(), rtsnap.WithTransport(rtsnap.TransportUDP)); err != nil {
		t.Fatalf("snapshot with reordered udp packets: %v", err)
	}
}

func TestAutoFallsBackWhenUDPSilent(t *testing.T) {
	var transcript []string
	server, err := newMockServer(&mockServer{udp: true, udpSilent: true, codec: "h264", packets: h264Frame(t)})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	start := time.Now()
	_, err = rtsnap.Snapshot(ctx, server.URL(),
		rtsnap.WithTransport(rtsnap.TransportAuto),
		rtsnap.WithDebugFunc(func(s string) { transcript = append(transcript, s) }),
	)
	if err != nil {
		t.Fatalf("auto transport did not recover from a silent udp path: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("fallback took %v", elapsed)
	}

	joined := strings.Join(transcript, "\n")
	udp := strings.Index(joined, "client_port=")
	tcp := strings.Index(joined, "interleaved=0-1")
	if udp < 0 || tcp < udp {
		t.Fatalf("expected a udp setup followed by a tcp one:\n%s", joined)
	}
	if !strings.Contains(joined, "> TEARDOWN") {
		t.Fatalf("udp session was not torn down before the retry:\n%s", joined)
	}
}

func TestForcedUDPDoesNotFallBack(t *testing.T) {
	server, err := newMockServer(&mockServer{udp: true, udpSilent: true, codec: "h264", packets: h264Frame(t)})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	_, err = rtsnap.Snapshot(ctx, server.URL(), rtsnap.WithTransport(rtsnap.TransportUDP))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the deadline error for forced udp, got %v", err)
	}
}

func TestCredentialsWithReservedCharacters(t *testing.T) {
	server, err := newMockServer(&mockServer{
		authMode: "digest",
		username: "admin",
		password: "P@ss#1?x",
		codec:    "h264",
		packets:  h264Frame(t),
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// the password is given the way people paste it, without percent encoding
	url := fmt.Sprintf("rtsp://admin:P@ss#1?x@127.0.0.1:%d/live", server.port)
	if _, err := rtsnap.Snapshot(ctx, url); err != nil {
		t.Fatalf("snapshot with reserved characters in the password: %v", err)
	}
}
