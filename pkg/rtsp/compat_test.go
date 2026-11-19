package rtsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func interleaved(channel byte, payload string) string {
	return string([]byte{magicByte, channel, byte(len(payload) >> 8), byte(len(payload))}) + payload
}

func dialTest(t *testing.T, addr string) *Client {
	t.Helper()
	client, err := Dial(context.Background(), "rtsp://"+addr+"/live", "", "", true)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestPlayKeepsEarlyRTP(t *testing.T) {
	addr, closeFn := startScriptedServer(t, func(n int, req string) string {
		switch {
		case strings.HasPrefix(req, "SETUP"):
			return "RTSP/1.0 200 OK\r\nSession: s1\r\nTransport: RTP/AVP/TCP;unicast;interleaved=0-1\r\n\r\n"
		case strings.HasPrefix(req, "PLAY"):
			// media and a report race ahead of the play response
			return interleaved(0, "first") + interleaved(1, "rtcp") + interleaved(0, "second") +
				"RTSP/1.0 200 OK\r\nSession: s1\r\n\r\n" + interleaved(0, "third")
		}
		return sdpResponse()
	})
	defer closeFn()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client := dialTest(t, addr)

	if err := client.Describe(ctx); err != nil {
		t.Fatalf("describe: %v", err)
	}
	if err := client.Setup(ctx); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := client.Play(ctx); err != nil {
		t.Fatalf("play with early interleaved data: %v", err)
	}
	for _, want := range []string{"first", "second", "third"} {
		got, err := client.ReadPacket(ctx)
		if err != nil {
			t.Fatalf("read %s: %v", want, err)
		}
		if string(got) != want {
			t.Fatalf("got packet %q, want %q", got, want)
		}
	}
}

func TestSendSkipsServerRequest(t *testing.T) {
	addr, closeFn := startScriptedServer(t, func(n int, req string) string {
		return "\r\nOPTIONS * RTSP/1.0\r\nCSeq: 7\r\n\r\n" + sdpResponse()
	})
	defer closeFn()

	client := dialTest(t, addr)
	if err := client.Describe(context.Background()); err != nil {
		t.Fatalf("describe behind a server request: %v", err)
	}
}

func TestReadPacketResync(t *testing.T) {
	stream := "junk\x00\x01" +
		interleaved(0, "one") +
		"RTSP/1.0 200 OK\r\nCSeq: 9\r\nContent-Length: 3\r\n\r\n$$$" +
		"\r\nmore junk" +
		interleaved(1, "rtcp") +
		interleaved(0, "two")
	client := &Client{reader: bufio.NewReader(strings.NewReader(stream))}

	for _, want := range []string{"one", "two"} {
		got, err := client.ReadPacket(context.Background())
		if err != nil {
			t.Fatalf("read %s: %v", want, err)
		}
		if string(got) != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

func TestReadFrameLargerThanBuffer(t *testing.T) {
	payload := strings.Repeat("x", 9000)
	client := &Client{reader: bufio.NewReaderSize(strings.NewReader(interleaved(0, payload)+interleaved(0, "tail")), 4096)}

	frame, err := client.ReadFrame(context.Background())
	if err != nil {
		t.Fatalf("read large frame: %v", err)
	}
	if string(frame.Payload) != payload {
		t.Fatalf("large frame damaged, %d bytes", len(frame.Payload))
	}
	frame, err = client.ReadFrame(context.Background())
	if err != nil || string(frame.Payload) != "tail" {
		t.Fatalf("frame after large one: %v %q", err, frame)
	}
}

func TestReadFrameOwnsPayload(t *testing.T) {
	client := &Client{reader: bufio.NewReader(strings.NewReader(interleaved(0, "aaaa") + interleaved(0, "bbbb")))}

	first, err := client.ReadFrame(context.Background())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, err := client.ReadFrame(context.Background()); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(first.Payload) != "aaaa" {
		t.Fatalf("frame payload changed by a later read: %q", first.Payload)
	}
}

func TestSetupTCPLenientTransport(t *testing.T) {
	cases := []struct {
		header  string
		channel int
	}{
		{"", 0},
		{"Transport: RTP/AVP/TCP;unicast\r\n", 0},
		{"Transport: RTP/AVP/TCP;unicast;interleaved=4\r\n", 4},
		{"Transport: rtp/avp/tcp;unicast;Interleaved=2-3\r\n", 2},
	}
	for _, tc := range cases {
		addr, closeFn := startScriptedServer(t, func(n int, req string) string {
			if strings.HasPrefix(req, "SETUP") {
				return "RTSP/1.0 200 OK\r\nSession: s1\r\n" + tc.header + "\r\n"
			}
			return sdpResponse()
		})

		client := dialTest(t, addr)
		if err := client.Describe(context.Background()); err != nil {
			t.Fatalf("describe: %v", err)
		}
		if err := client.Setup(context.Background()); err != nil {
			t.Fatalf("setup with %q: %v", tc.header, err)
		}
		if client.RTPChannel() != tc.channel {
			t.Fatalf("%q: channel %d, want %d", tc.header, client.RTPChannel(), tc.channel)
		}
		closeFn()
	}
}

func TestPlayUsesContentBase(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	addr, closeFn := startScriptedServer(t, func(n int, req string) string {
		mu.Lock()
		lines = append(lines, strings.SplitN(req, "\r\n", 2)[0])
		mu.Unlock()
		switch {
		case strings.HasPrefix(req, "DESCRIBE"):
			return strings.Replace(sdpResponse(), "\r\n", "\r\nContent-Base: /base/stream/\r\n", 1)
		case strings.HasPrefix(req, "SETUP"):
			return "RTSP/1.0 200 OK\r\nSession: s1;timeout=60\r\nTransport: RTP/AVP/TCP;unicast;interleaved=0-1\r\n\r\n"
		}
		return "RTSP/1.0 200 OK\r\n\r\n"
	})
	defer closeFn()

	ctx := context.Background()
	client := dialTest(t, addr)
	if err := client.Describe(ctx); err != nil {
		t.Fatalf("describe: %v", err)
	}
	if err := client.Setup(ctx); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := client.Play(ctx); err != nil {
		t.Fatalf("play: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{
		"DESCRIBE rtsp://" + addr + "/live RTSP/1.0",
		"SETUP rtsp://" + addr + "/base/stream/track0 RTSP/1.0",
		"PLAY rtsp://" + addr + "/base/stream/ RTSP/1.0",
	}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("request lines:\n got %q\nwant %q", lines, want)
	}
}

func TestDescribeFollowsRedirect(t *testing.T) {
	target, closeTarget := startScriptedServer(t, func(n int, req string) string {
		if !strings.HasPrefix(req, "DESCRIBE rtsp://") || !strings.Contains(req, "/moved RTSP/1.0") {
			return "RTSP/1.0 404 Not Found\r\n\r\n"
		}
		return sdpResponse()
	})
	defer closeTarget()

	addr, closeFn := startScriptedServer(t, func(n int, req string) string {
		return "RTSP/1.0 302 Moved Temporarily\r\nLocation: rtsp://" + target + "/moved\r\n\r\n"
	})
	defer closeFn()

	client := dialTest(t, addr)
	if err := client.Describe(context.Background()); err != nil {
		t.Fatalf("describe through redirect: %v", err)
	}
	if len(client.Tracks) != 1 || client.Tracks[0].Control != "rtsp://"+target+"/moved/track0" {
		t.Fatalf("tracks after redirect: %+v", client.Tracks)
	}

	// the low level call still reports the redirect itself
	raw := dialTest(t, addr)
	res, err := raw.DescribeAuth(context.Background())
	if err != nil || res.StatusCode != 302 {
		t.Fatalf("raw describe: %v %+v", err, res)
	}
}

func TestDescribeRedirectLoopEnds(t *testing.T) {
	addr, closeFn := startScriptedServer(t, func(n int, req string) string {
		return "RTSP/1.0 301 Moved\r\nLocation: /again\r\n\r\n"
	})
	defer closeFn()

	client := dialTest(t, addr)
	var statusErr *StatusError
	if err := client.Describe(context.Background()); !errors.As(err, &statusErr) || statusErr.Code != 301 {
		t.Fatalf("expected the redirect status after too many hops, got %v", err)
	}
}

func TestParseURLCredentials(t *testing.T) {
	cases := []struct {
		raw, user, pass, host, path string
	}{
		{"rtsp://admin:secret@cam:554/live", "admin", "secret", "cam:554", "/live"},
		{"rtsp://admin:p%40ss@cam/live", "admin", "p@ss", "cam", "/live"},
		{"rtsp://admin:P@ss#1@192.168.1.64:554/Streaming/Channels/101", "admin", "P@ss#1", "192.168.1.64:554", "/Streaming/Channels/101"},
		{"rtsp://admin:a?b@cam/live?x=1", "admin", "a?b", "cam", "/live"},
		{"rtsp://admin:50%off@cam/live", "admin", "50%off", "cam", "/live"},
		{"rtsp://ad min:p w@[::1]:8554/live", "ad min", "p w", "[::1]:8554", "/live"},
		{"rtsp://cam/live#frag", "", "", "cam", "/live"},
		{"rtsp://cam?user=a@b", "", "", "cam", ""},
	}
	for _, tc := range cases {
		u, err := parseURL(tc.raw)
		if err != nil {
			t.Fatalf("%s: %v", tc.raw, err)
		}
		pass, _ := u.User.Password()
		if u.User.Username() != tc.user || pass != tc.pass || u.Host != tc.host || u.Path != tc.path {
			t.Fatalf("%s: got user=%q pass=%q host=%q path=%q", tc.raw, u.User.Username(), pass, u.Host, u.Path)
		}
	}

	_, err := parseURL("rtsp://admin:topsecret@cam:port/live")
	if err == nil {
		t.Fatal("expected error for a bad port")
	}
	if strings.Contains(err.Error(), "topsecret") {
		t.Fatalf("password leaked into the error: %v", err)
	}
	if _, err := parseURL("ftp://cam/live"); err == nil {
		t.Fatal("expected error for an unsupported scheme")
	}
}

func TestTeardownDoesNotWait(t *testing.T) {
	got := make(chan string, 8)
	addr, closeFn := startScriptedServer(t, func(n int, req string) string {
		got <- strings.SplitN(req, " ", 2)[0]
		switch {
		case strings.HasPrefix(req, "SETUP"):
			return "RTSP/1.0 200 OK\r\nSession: s1\r\nTransport: RTP/AVP/TCP;unicast;interleaved=0-1\r\n\r\n"
		case strings.HasPrefix(req, "TEARDOWN"):
			// never answered
			time.Sleep(3 * time.Second)
			return ""
		}
		return sdpResponse()
	})
	defer closeFn()

	ctx, cancel := context.WithCancel(context.Background())
	client := dialTest(t, addr)
	if err := client.Describe(ctx); err != nil {
		t.Fatalf("describe: %v", err)
	}
	if err := client.Setup(ctx); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// an expired context must not keep the session open on the server
	cancel()
	start := time.Now()
	if err := client.Teardown(ctx); err != nil {
		t.Fatalf("teardown: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("teardown waited %v for a response", elapsed)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case method := <-got:
			if method == "TEARDOWN" {
				return
			}
		case <-deadline:
			t.Fatal("server never received teardown")
		}
	}
}

func TestRequestBytesOrder(t *testing.T) {
	req := &Request{
		Method:  "SETUP",
		URI:     "rtsp://h/live/track0",
		Headers: map[string]string{"User-Agent": "ua", "Transport": "t", "CSeq": "3", "Authorization": "a"},
		Body:    []byte("xy"),
	}
	want := "SETUP rtsp://h/live/track0 RTSP/1.0\r\nCSeq: 3\r\nAuthorization: a\r\nTransport: t\r\nUser-Agent: ua\r\nContent-Length: 2\r\n\r\nxy"
	if got := string(req.Bytes()); got != want {
		t.Fatalf("request bytes:\n got %q\nwant %q", got, want)
	}
}

func TestContextDeadlineError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, _ = io.Copy(io.Discard, conn)
				_ = conn.Close()
			}()
		}
	}()

	client := dialTest(t, ln.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := client.Describe(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}

	// the expired deadline must not leak into a call with a fresh context
	fresh, cancelFresh := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancelFresh()
	start := time.Now()
	_ = client.Describe(fresh)
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("stale deadline cut the next call short after %v", elapsed)
	}
}

func TestBase64ReaderPaddedChunks(t *testing.T) {
	parts := []string{"RTSP/1.0 200 OK\r\n", "CSeq: 1\r\n\r\n", "x", "yz", strings.Repeat("payload", 900)}
	var encoded, plain bytes.Buffer
	for i, p := range parts {
		plain.WriteString(p)
		encoded.WriteString(base64.StdEncoding.EncodeToString([]byte(p)))
		if i%2 == 0 {
			encoded.WriteString("\r\n")
		}
	}

	r := newBase64Reader(bufio.NewReaderSize(&encoded, 64))
	var got bytes.Buffer
	buf := make([]byte, 50)
	for got.Len() < plain.Len() {
		n, err := r.Read(buf)
		if err != nil {
			t.Fatalf("read after %d bytes: %v", got.Len(), err)
		}
		got.Write(buf[:n])
	}
	if !bytes.Equal(got.Bytes(), plain.Bytes()) {
		t.Fatal("decoded tunnel data differs")
	}
}

func TestTransportParams(t *testing.T) {
	trans := "RTP/AVP;unicast;client_port=5000-5001; Server_Port = 6970 ;source=\"10.0.0.7\";ssrc=1234"
	if got := udpServerPort(trans); got != 6970 {
		t.Fatalf("server port %d", got)
	}
	if ip := transportSource(trans); !ip.Equal(net.ParseIP("10.0.0.7")) {
		t.Fatalf("source %v", ip)
	}
	if ip := transportSource("RTP/AVP;unicast;source=0.0.0.0"); ip != nil {
		t.Fatalf("unspecified source must be ignored, got %v", ip)
	}
	if a, b, ok := transportPair("8-9"); !ok || a != 8 || b != 9 {
		t.Fatalf("pair %d %d %v", a, b, ok)
	}
	if _, _, ok := transportPair("x-9"); ok {
		t.Fatal("accepted a broken pair")
	}
}
