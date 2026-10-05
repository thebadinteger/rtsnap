package rtsp

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestAuthBasic(t *testing.T) {
	auth := NewAuth("Basic realm=\"test\"", "admin", "12345")
	if auth == nil {
		t.Fatal("auth is nil")
	}
	header := auth.Generate("DESCRIBE", "rtsp://127.0.0.1/live")
	if !strings.HasPrefix(header, "Basic ") {
		t.Fatalf("unexpected basic header: %s", header)
	}
}

func TestAuthDigest(t *testing.T) {
	wwwAuth := `Digest realm="IP Camera", nonce="dcd98b7102dd2f0e8b11d0f600bfb0c093", qop="auth"`
	auth := NewAuth(wwwAuth, "admin", "admin123")
	if auth == nil {
		t.Fatal("auth is nil")
	}
	header := auth.Generate("DESCRIBE", "rtsp://192.168.1.100/cam")
	if !strings.HasPrefix(header, "Digest ") {
		t.Fatalf("unexpected digest header: %s", header)
	}
	if !strings.Contains(header, `username="admin"`) {
		t.Errorf("missing username in %s", header)
	}
	if !strings.Contains(header, `response="`) {
		t.Errorf("missing response in %s", header)
	}
}

func TestParseSDP(t *testing.T) {
	sdp := []byte(`v=0
o=- 1600000000 1600000000 IN IP4 192.168.1.100
s=RTSP Session
t=0 0
m=video 0 RTP/AVP 96
a=rtpmap:96 H264/90000
a=fmtp:96 packetization-mode=1;sprop-parameter-sets=Z0LADJWoLA9puAgICgAAAwACAAA4g8OHyw==,aM48gA==
a=control:trackID=1
m=audio 0 RTP/AVP 97
a=rtpmap:97 PCMU/8000
a=control:trackID=2
`)

	tracks, err := ParseSDP(sdp, "rtsp://192.168.1.100:554/live")
	if err != nil {
		t.Fatalf("parse sdp: %v", err)
	}
	if len(tracks) == 0 {
		t.Fatalf("expected at least 1 track")
	}
	track := tracks[0]

	if track.Codec != "h264" {
		t.Errorf("expected h264, got %s", track.Codec)
	}
	if track.PayloadType != 96 {
		t.Errorf("expected pt 96, got %d", track.PayloadType)
	}
	if len(track.SPS) == 0 {
		t.Errorf("expected sps, got none")
	}
	if len(track.PPS) == 0 {
		t.Errorf("expected pps, got none")
	}
	if track.Control != "rtsp://192.168.1.100:554/trackID=1" {
		t.Errorf("unexpected control: %s", track.Control)
	}
}

func TestRequestResponse(t *testing.T) {
	req := &Request{
		Method: "DESCRIBE",
		URI:    "rtsp://127.0.0.1/live",
		Headers: map[string]string{
			"CSeq":   "1",
			"Accept": "application/sdp",
		},
	}

	var buf bytes.Buffer
	if err := req.Write(&buf); err != nil {
		t.Fatalf("write request: %v", err)
	}

	rawResponse := "RTSP/1.0 200 OK\r\nCSeq: 1\r\nContent-Type: application/sdp\r\nContent-Length: 4\r\n\r\ntest"
	res, raw, err := readResponse(bufio.NewReader(strings.NewReader(rawResponse)))
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if raw != rawResponse {
		t.Fatalf("raw mismatch: %q", raw)
	}

	if res.StatusCode != 200 {
		t.Errorf("status code: %d", res.StatusCode)
	}
	if string(res.Body) != "test" {
		t.Errorf("body mismatch: %s", string(res.Body))
	}
}
