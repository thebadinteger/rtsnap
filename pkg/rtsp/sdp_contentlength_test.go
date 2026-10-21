package rtsp

import (
	"bufio"
	"strings"
	"testing"
)

func TestParseSDPMalformedMediaLines(t *testing.T) {
	// short m lines must be skipped without panicking
	sdp := []byte("v=0\n" +
		"m=video\n" +
		"m=video 0\n" +
		"m=video 0 RTP/AVP\n" +
		"m=video 0 RTP/AVP 96\n" +
		"a=rtpmap:96 H264/90000\n" +
		"a=control:track1\n")

	tracks, err := ParseSDP(sdp, "rtsp://127.0.0.1/live")
	if err != nil {
		t.Fatalf("parse sdp: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(tracks))
	}
	if tracks[0].Control != "rtsp://127.0.0.1/live/track1" {
		t.Errorf("unexpected control: %s", tracks[0].Control)
	}
}

func TestSDPControlJoinVariants(t *testing.T) {
	cases := []struct {
		base    string
		control string
		want    string
	}{
		{"rtsp://h/live", "track1", "rtsp://h/live/track1"},
		{"rtsp://h/live", "rtsp://h/abs", "rtsp://h/abs"},
		{"rtsp://h/live", "/root1", "rtsp://h/root1"},
		{"rtsp://h/live?q=1", "track1", "rtsp://h/live?q=1/track1"},
		{"rtsp://h/live/", "track1", "rtsp://h/live/track1"},
		{"rtsp://h/live", "*", "rtsp://h/live"},
	}

	for _, c := range cases {
		if got := joinControlURL(c.base, c.control); got != c.want {
			t.Errorf("joinControlURL(%q, %q) = %q, want %q", c.base, c.control, got, c.want)
		}
	}
}

func TestSDPSessionControlIsBase(t *testing.T) {
	sdp := []byte("v=0\n" +
		"a=control:session1\n" +
		"m=video 0 RTP/AVP 96\n" +
		"a=rtpmap:96 H264/90000\n" +
		"a=control:track1\n")

	tracks, err := ParseSDP(sdp, "rtsp://127.0.0.1/live")
	if err != nil {
		t.Fatalf("parse sdp: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(tracks))
	}
	if tracks[0].Control != "rtsp://127.0.0.1/session1/track1" {
		t.Errorf("unexpected control: %s", tracks[0].Control)
	}
}

func TestReadResponseContentLengthValidation(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		fail bool
	}{
		{"negative", "RTSP/1.0 200 OK\r\nContent-Length: -5\r\n\r\n", true},
		{"garbage", "RTSP/1.0 200 OK\r\nContent-Length: abc\r\n\r\n", true},
		{"duplicate", "RTSP/1.0 200 OK\r\nContent-Length: 2\r\nContent-Length: 3\r\n\r\n", true},
		{"zero", "RTSP/1.0 200 OK\r\nContent-Length: 0\r\n\r\n", false},
		{"body", "RTSP/1.0 200 OK\r\nContent-Length: 4\r\n\r\ntest", false},
	}

	for _, c := range cases {
		res, _, err := readResponse(bufio.NewReader(strings.NewReader(c.raw)))
		if c.fail && err == nil {
			t.Errorf("%s: expected error, got nil", c.name)
		}
		if !c.fail && err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
		}
		if !c.fail && res != nil && c.name == "body" && string(res.Body) != "test" {
			t.Errorf("%s: wrong body %q", c.name, res.Body)
		}
	}
}
