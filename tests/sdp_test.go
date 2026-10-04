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

func TestSDPQuirkyFoldedFmtp(t *testing.T) {
	sdp := []byte(`v=0
o=- 1 1 IN IP4 127.0.0.1
s=Quirky Camera
m=video 0 RTP/AVP 96
a=RTPMAP:96 H264/90000
a=FMTP:96 packetization-mode=1; sprop-parameter-sets=Z2QAHqyyAWhf8uAiAAAD
 AAIAAAMAZB4sXJA=,aOvMsiw=
a=Control:track1
`)

	tracks, err := rtsp.ParseSDP(sdp, "rtsp://127.0.0.1:554/live")
	if err != nil {
		t.Fatalf("parse sdp: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(tracks))
	}
	track := tracks[0]
	if track.Codec != "h264" {
		t.Errorf("expected h264, got %s", track.Codec)
	}
	if len(track.SPS) != 1 {
		t.Errorf("expected 1 sps from folded line, got %d", len(track.SPS))
	}
	if len(track.PPS) != 1 {
		t.Errorf("expected 1 pps from folded line, got %d", len(track.PPS))
	}
	if track.Control != "rtsp://127.0.0.1:554/track1" {
		t.Errorf("unexpected control: %s", track.Control)
	}
}

func TestSDPMissingRtpmap(t *testing.T) {
	sdp := []byte(`v=0
m=video 0 RTP/AVP 96
a=fmtp:96 packetization-mode=1; sprop-parameter-sets=Z2QAHqyyAWhf8uAiAAADAAIAAAMAZB4sXJA=,aOvMsiw=
a=control:track0
`)

	tracks, err := rtsp.ParseSDP(sdp, "rtsp://127.0.0.1/live")
	if err != nil {
		t.Fatalf("parse sdp: %v", err)
	}
	if len(tracks) != 1 || tracks[0].Codec != "h264" {
		t.Fatalf("expected h264 fallback from fmtp, got %v", tracks)
	}

	sdp265 := []byte(`v=0
m=video 0 RTP/AVP 96
a=fmtp:96 sprop-sps=QgEBAWAAAAMAsAAAAwAAAwB7jAk=;sprop-pps=RAHBcrA=
a=control:track0
`)

	tracks265, err := rtsp.ParseSDP(sdp265, "rtsp://127.0.0.1/live")
	if err != nil {
		t.Fatalf("parse sdp265: %v", err)
	}
	if len(tracks265) != 1 || tracks265[0].Codec != "h265" {
		t.Fatalf("expected h265 fallback from fmtp, got %v", tracks265)
	}
}

func TestSDPCROnlyEndings(t *testing.T) {
	sdp := []byte("v=0\ro=- 1 1 IN IP4 127.0.0.1\rm=video 0 RTP/AVP 96\ra=rtpmap:96 H264/90000\ra=control:track0\r")

	tracks, err := rtsp.ParseSDP(sdp, "rtsp://127.0.0.1/live")
	if err != nil {
		t.Fatalf("parse sdp: %v", err)
	}
	if len(tracks) != 1 || tracks[0].Codec != "h264" {
		t.Fatalf("expected h264 track, got %v", tracks)
	}
}

func TestSDPUnpaddedBase64(t *testing.T) {
	sdp := []byte(`v=0
m=video 0 RTP/AVP 96
a=rtpmap:96 H264/90000
a=fmtp:96 sprop-parameter-sets=Z2QAHqyyAWhf8uAiAAADAAIAAAMAZB4sXJA,aOvMsiw
a=control:track0
`)

	tracks, err := rtsp.ParseSDP(sdp, "rtsp://127.0.0.1/live")
	if err != nil {
		t.Fatalf("parse sdp: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(tracks))
	}
	if len(tracks[0].SPS) != 1 || len(tracks[0].PPS) != 1 {
		t.Errorf("expected sps+pps from unpadded base64, got sps=%d pps=%d",
			len(tracks[0].SPS), len(tracks[0].PPS))
	}
}
