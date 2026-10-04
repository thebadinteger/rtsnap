package tests

import (
	"testing"

	"github.com/thebadinteger/rtsnap/pkg/rtsp"
)

func TestSDPNoVideoTrack(t *testing.T) {
	sdp := []byte(`v=0
o=- 1 1 IN IP4 127.0.0.1
s=Audio Only
t=0 0
m=audio 0 RTP/AVP 0
a=rtpmap:0 PCMU/8000
a=control:track0
`)

	_, err := rtsp.ParseSDP(sdp, "rtsp://127.0.0.1/live")
	if err == nil {
		t.Fatal("expected error for sdp without video, got nil")
	}
}

func TestSDPH265Parameters(t *testing.T) {
	sdp := []byte(`v=0
o=- 1 1 IN IP4 127.0.0.1
s=HEVC Stream
t=0 0
m=video 0 RTP/AVP 96
a=rtpmap:96 H265/90000
a=fmtp:96 sprop-vps=QAEM//4BAAUABAAAAMAIAAMAAAMAAAMAAA+WmA==;sprop-sps=QgEBAWAAAAMAsAAAAwAAAwB7jAk=;sprop-pps=RAHBcrA=
a=control:trackID=1
`)

	tracks, err := rtsp.ParseSDP(sdp, "rtsp://127.0.0.1:554/stream1")
	if err != nil {
		t.Fatalf("parse sdp: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(tracks))
	}
	track := tracks[0]

	if track.Codec != "h265" {
		t.Errorf("expected h265, got %s", track.Codec)
	}
	if len(track.VPS) == 0 {
		t.Error("expected parsed vps")
	}
	if len(track.SPS) == 0 {
		t.Error("expected parsed sps")
	}
	if len(track.PPS) == 0 {
		t.Error("expected parsed pps")
	}
	if track.Control != "rtsp://127.0.0.1:554/trackID=1" {
		t.Errorf("unexpected resolved control url: %s", track.Control)
	}
}

func TestSDPRelativeControls(t *testing.T) {
	// test wildcard control
	sdp1 := []byte(`v=0
m=video 0 RTP/AVP 26
a=control:*
`)
	tracks1, err := rtsp.ParseSDP(sdp1, "rtsp://192.168.1.50/cam/realmonitor")
	if err != nil {
		t.Fatalf("parse sdp1: %v", err)
	}
	if tracks1[0].Control != "rtsp://192.168.1.50/cam/realmonitor" {
		t.Errorf("wildcard control resolution failed: %s", tracks1[0].Control)
	}

	// test empty control with global control
	sdp2 := []byte(`v=0
a=control:rtsp://192.168.1.50/cam/realmonitor
m=video 0 RTP/AVP 26
`)
	tracks2, err := rtsp.ParseSDP(sdp2, "rtsp://192.168.1.50/cam/realmonitor")
	if err != nil {
		t.Fatalf("parse sdp2: %v", err)
	}
	if tracks2[0].Control != "rtsp://192.168.1.50/cam/realmonitor" {
		t.Errorf("global control resolution failed: %s", tracks2[0].Control)
	}
}

func TestSDPMultiTrackAndSelect(t *testing.T) {
	sdp := []byte(`v=0
o=- 1 1 IN IP4 127.0.0.1
s=Multi Stream
t=0 0
m=video 0 RTP/AVP 26
a=control:trackMJPEG
m=video 0 RTP/AVP 96
a=rtpmap:96 H264/90000
a=control:trackH264
m=video 0 RTP/AVP 97
a=rtpmap:97 H265/90000
a=control:trackH265
`)

	tracks, err := rtsp.ParseSDP(sdp, "rtsp://127.0.0.1:554/live")
	if err != nil {
		t.Fatalf("parse sdp: %v", err)
	}
	if len(tracks) != 3 {
		t.Fatalf("expected 3 tracks, got %d", len(tracks))
	}

	// auto should prioritize h264
	autoTrack, err := rtsp.SelectTrack(tracks, "auto")
	if err != nil {
		t.Fatalf("select auto: %v", err)
	}
	if autoTrack.Codec != "h264" {
		t.Errorf("expected auto to pick h264, got %s", autoTrack.Codec)
	}

	// manual selection of mjpeg
	mjpegTrack, err := rtsp.SelectTrack(tracks, "mjpeg")
	if err != nil {
		t.Fatalf("select mjpeg: %v", err)
	}
	if mjpegTrack.Codec != "mjpeg" {
		t.Errorf("expected mjpeg, got %s", mjpegTrack.Codec)
	}

	// manual selection of h265
	h265Track, err := rtsp.SelectTrack(tracks, "h265")
	if err != nil {
		t.Fatalf("select h265: %v", err)
	}
	if h265Track.Codec != "h265" {
		t.Errorf("expected h265, got %s", h265Track.Codec)
	}

	// non-existing codec
	_, err = rtsp.SelectTrack(tracks, "vp9")
	if err == nil {
		t.Fatal("expected error for non-existent codec, got nil")
	}
}
