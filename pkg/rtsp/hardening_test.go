package rtsp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func readTestRequest(br *bufio.Reader) (string, error) {
	var b strings.Builder
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return "", err
		}
		b.WriteString(line)
		if strings.TrimSpace(line) == "" {
			return b.String(), nil
		}
	}
}

// read a request including a content-length body
func readTestRequestWithBody(br *bufio.Reader) (string, string, error) {
	head, err := readTestRequest(br)
	if err != nil {
		return "", "", err
	}
	length := 0
	for _, line := range strings.Split(head, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			if v, err := strconv.Atoi(strings.TrimSpace(line[len("content-length:"):])); err == nil {
				length = v
			}
		}
	}
	if length == 0 {
		return head, "", nil
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(br, body); err != nil {
		return "", "", err
	}
	return head, string(body), nil
}

func startScriptedServer(t *testing.T, respond func(n int, req string) string) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var count int32
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				br := bufio.NewReader(conn)
				for {
					req, err := readTestRequest(br)
					if err != nil {
						return
					}
					n := int(atomic.AddInt32(&count, 1))
					resp := respond(n, req)
					if resp == "" {
						return
					}
					if _, err := conn.Write([]byte(resp)); err != nil {
						return
					}
				}
			}(conn)
		}
	}()
	return ln.Addr().String(), func() { _ = ln.Close() }
}

func sdpResponse() string {
	sdp := "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=x\r\nt=0 0\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=control:track0\r\n"
	return "RTSP/1.0 200 OK\r\nContent-Type: application/sdp\r\nContent-Length: " + strconv.Itoa(len(sdp)) + "\r\n\r\n" + sdp
}

func TestResponseKeepsDuplicateHeaders(t *testing.T) {
	raw := "RTSP/1.0 401 Unauthorized\r\n" +
		"WWW-Authenticate: Basic realm=\"cam\"\r\n" +
		"WWW-Authenticate: Digest realm=\"cam\", nonce=\"abc\"\r\n" +
		"\r\n"
	res, _, err := readResponse(bufio.NewReader(strings.NewReader(raw)))
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	values := res.Values("www-authenticate")
	if len(values) != 2 {
		t.Fatalf("expected 2 challenges, got %d", len(values))
	}
	if !strings.HasPrefix(values[0], "Basic") || !strings.HasPrefix(values[1], "Digest") {
		t.Fatalf("unexpected challenges: %v", values)
	}
	if got := res.Header("WWW-Authenticate"); got != values[0] {
		t.Fatalf("header mismatch: %q", got)
	}
}

func TestResponseRejectsNonRTSP(t *testing.T) {
	for _, raw := range []string{
		"HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n",
		"SIP/2.0 200 OK\r\n\r\n",
	} {
		if _, _, err := readResponse(bufio.NewReader(strings.NewReader(raw))); err == nil {
			t.Fatalf("accepted non rtsp response: %q", raw)
		}
	}
}

func TestDescribeRawStatus(t *testing.T) {
	var requests int32
	addr, closeFn := startScriptedServer(t, func(n int, req string) string {
		atomic.AddInt32(&requests, 1)
		return "RTSP/1.0 401 Unauthorized\r\n" +
			"WWW-Authenticate: Basic realm=\"cam\"\r\n" +
			"WWW-Authenticate: Digest realm=\"cam\", nonce=\"abc\"\r\n" +
			"\r\n"
	})
	defer closeFn()

	ctx := context.Background()
	client, err := Dial(ctx, "rtsp://"+addr+"/live", "user", "pass", true)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	res, err := client.DescribeRaw(ctx)
	if err != nil {
		t.Fatalf("describe raw: %v", err)
	}
	if res.StatusCode != 401 {
		t.Fatalf("status %d, want 401", res.StatusCode)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("raw describe sent %d requests, want 1", got)
	}
	if len(res.Values("WWW-Authenticate")) != 2 {
		t.Fatalf("challenges lost: %v", res.HeaderValues)
	}

	authRes, err := client.DescribeAuth(ctx)
	if err != nil {
		t.Fatalf("describe auth: %v", err)
	}
	if authRes.StatusCode != 401 {
		t.Fatalf("auth status %d, want 401", authRes.StatusCode)
	}
	if got := atomic.LoadInt32(&requests); got != 3 {
		t.Fatalf("auth describe sent %d requests, want 3", got)
	}

	if err := client.Describe(ctx); err == nil {
		t.Fatal("expected describe error")
	} else {
		var statusErr *StatusError
		if !errors.As(err, &statusErr) {
			t.Fatalf("expected StatusError, got %T: %v", err, err)
		}
		if statusErr.Code != 401 {
			t.Fatalf("status error code %d", statusErr.Code)
		}
		if len(statusErr.Values("WWW-Authenticate")) != 2 {
			t.Fatalf("status error challenges lost")
		}
	}
}

func TestUnsupportedChallengeKeepsStatus(t *testing.T) {
	addr, closeFn := startScriptedServer(t, func(n int, req string) string {
		return "RTSP/1.0 401 Unauthorized\r\nWWW-Authenticate: Negotiate\r\n\r\n"
	})
	defer closeFn()

	client, err := Dial(context.Background(), "rtsp://"+addr+"/live", "user", "pass", true)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	res, err := client.DescribeRaw(context.Background())
	if err != nil {
		t.Fatalf("raw describe must not fail on unsupported challenge: %v", err)
	}
	if res.StatusCode != 401 {
		t.Fatalf("status %d, want 401", res.StatusCode)
	}
	if got := res.Header("WWW-Authenticate"); !strings.Contains(got, "Negotiate") {
		t.Fatalf("challenge missing: %q", got)
	}

	authRes, err := client.DescribeAuth(context.Background())
	if err != nil {
		t.Fatalf("auth describe must not fail on unsupported challenge: %v", err)
	}
	if authRes.StatusCode != 401 {
		t.Fatalf("auth status %d, want 401", authRes.StatusCode)
	}
}

func TestDigestFallsBackToBasic(t *testing.T) {
	addr, closeFn := startScriptedServer(t, func(n int, req string) string {
		if n == 1 {
			return "RTSP/1.0 401 Unauthorized\r\n" +
				"WWW-Authenticate: Digest realm=\"cam\", nonce=\"n\", algorithm=SHA-512-256\r\n" +
				"WWW-Authenticate: Basic realm=\"cam\"\r\n" +
				"\r\n"
		}
		for _, line := range strings.Split(req, "\r\n") {
			if strings.HasPrefix(strings.ToLower(line), "authorization:") {
				if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "authorization: basic ") {
					return sdpResponse()
				}
				return "RTSP/1.0 401 Unauthorized\r\nWWW-Authenticate: Basic realm=\"cam\"\r\n\r\n"
			}
		}
		return "RTSP/1.0 401 Unauthorized\r\nWWW-Authenticate: Basic realm=\"cam\"\r\n\r\n"
	})
	defer closeFn()

	client, err := Dial(context.Background(), "rtsp://"+addr+"/live", "user", "pass", true)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	res, err := client.DescribeAuth(context.Background())
	if err != nil {
		t.Fatalf("describe auth: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("status %d, want 200", res.StatusCode)
	}
}

func TestDigestStaleRetry(t *testing.T) {
	var lastAuth atomic.Value
	addr, closeFn := startScriptedServer(t, func(n int, req string) string {
		if n == 1 {
			return "RTSP/1.0 401 Unauthorized\r\nWWW-Authenticate: Digest realm=\"cam\", nonce=\"n1\", qop=\"auth\"\r\n\r\n"
		}
		if n == 3 {
			return "RTSP/1.0 401 Unauthorized\r\nWWW-Authenticate: Digest realm=\"cam\", nonce=\"n2\", qop=\"auth\", stale=true\r\n\r\n"
		}
		for _, line := range strings.Split(req, "\r\n") {
			if strings.HasPrefix(strings.ToLower(line), "authorization:") {
				lastAuth.Store(line)
			}
		}
		return sdpResponse()
	})
	defer closeFn()

	ctx := context.Background()
	client, err := Dial(ctx, "rtsp://"+addr+"/live", "user", "pass", true)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	if err := client.Describe(ctx); err != nil {
		t.Fatalf("first describe: %v", err)
	}
	if err := client.Describe(ctx); err != nil {
		t.Fatalf("describe after stale: %v", err)
	}
	auth, _ := lastAuth.Load().(string)
	if auth == "" {
		t.Fatal("no authorization header seen")
	}
	if !strings.Contains(auth, `nonce="n2"`) {
		t.Fatalf("stale nonce not refreshed: %s", auth)
	}
}

func TestUnsupportedDigestAlgorithm(t *testing.T) {
	cases := []string{
		`Digest realm="r", nonce="n", algorithm=SHA-512-256`,
		`Digest realm="r", nonce="n", algorithm=SHA-512`,
		`Digest realm="r", nonce="n", algorithm=SHA-256-512`,
	}
	for _, header := range cases {
		if NewAuth(header, "u", "p") != nil {
			t.Fatalf("accepted unsupported challenge: %s", header)
		}
	}
	if NewAuth(`Digest realm="r", nonce="n", algorithm=SHA-256`, "u", "p") == nil {
		t.Fatal("rejected sha-256")
	}
	if NewAuth(`Digest realm="r", nonce="n", algorithm=MD5-sess`, "u", "p") == nil {
		t.Fatal("rejected md5-sess")
	}
	if NewAuth(`Digest realm="r", nonce="n", qop="auth,auth-int"`, "u", "p") == nil {
		t.Fatal("rejected challenge offering auth")
	}

	// unsupported qop degrades to a plain digest instead of being refused
	auth := NewAuth(`Digest realm="r", nonce="n", qop="auth-int"`, "u", "p")
	if auth == nil {
		t.Fatal("rejected digest with unsupported qop")
	}
	if auth.Qop != "" {
		t.Fatalf("qop not cleared: %q", auth.Qop)
	}

	addr, closeFn := startScriptedServer(t, func(n int, req string) string {
		return "RTSP/1.0 401 Unauthorized\r\nWWW-Authenticate: Digest realm=\"r\", nonce=\"n\", algorithm=SHA-512-256\r\n\r\n"
	})
	defer closeFn()

	client, err := Dial(context.Background(), "rtsp://"+addr+"/live", "u", "p", true)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	res, err := client.DescribeAuth(context.Background())
	if err != nil {
		t.Fatalf("auth describe must not fail on unsupported algorithm: %v", err)
	}
	if res.StatusCode != 401 {
		t.Fatalf("status %d, want 401", res.StatusCode)
	}
}

func TestBasicIgnoresDigestParams(t *testing.T) {
	auth := NewAuth(`Basic realm="cam", algorithm=SHA-512-256, qop="auth-int"`, "u", "p")
	if auth == nil {
		t.Fatal("rejected basic with digest parameters")
	}
	if auth.Method != "basic" {
		t.Fatalf("method %q, want basic", auth.Method)
	}
}

func TestGenerateQopCaseInsensitive(t *testing.T) {
	auth := NewAuth(`Digest realm="r", nonce="n", qop="AUTH"`, "u", "p")
	if auth == nil {
		t.Fatal("rejected qop AUTH")
	}
	header := auth.Generate("DESCRIBE", "rtsp://host/live")
	if !strings.Contains(header, "qop=auth") {
		t.Fatalf("missing qop in %s", header)
	}
	if !strings.Contains(header, "nc=") || !strings.Contains(header, "cnonce=") {
		t.Fatalf("missing qop fields in %s", header)
	}
}

func TestDialAddrDefaults(t *testing.T) {
	cases := []struct {
		raw    string
		scheme string
		want   string
	}{
		{"rtsp://host/live", "rtsp", "host:554"},
		{"rtsps://host/live", "rtsps", "host:322"},
		{"http://host/live", "http", "host:80"},
		{"https://host/live", "https", "host:443"},
		{"rtsp://[::1]/live", "rtsp", "[::1]:554"},
		{"rtsps://[::1]:8554/live", "rtsps", "[::1]:8554"},
	}
	for _, tc := range cases {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatalf("parse %s: %v", tc.raw, err)
		}
		if got := dialAddr(u, tc.scheme); got != tc.want {
			t.Fatalf("%s: got %s, want %s", tc.raw, got, tc.want)
		}
	}
}

func TestTransportSource(t *testing.T) {
	if ip := transportSource("RTP/AVP;unicast;source=192.168.1.5;server_port=4000-4001"); !ip.Equal(net.ParseIP("192.168.1.5")) {
		t.Fatalf("unexpected source: %v", ip)
	}
	if ip := transportSource("RTP/AVP;unicast;source=::1"); !ip.Equal(net.ParseIP("::1")) {
		t.Fatalf("unexpected v6 source: %v", ip)
	}
	if ip := transportSource("RTP/AVP;unicast"); ip != nil {
		t.Fatalf("expected nil source, got %v", ip)
	}
}

type fakePacket struct {
	data []byte
	addr *net.UDPAddr
}

type fakePacketConn struct {
	packets []fakePacket
	index   int
}

func (c *fakePacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	if c.index >= len(c.packets) {
		return 0, nil, io.EOF
	}
	pkt := c.packets[c.index]
	c.index++
	return copy(p, pkt.data), pkt.addr, nil
}

func (c *fakePacketConn) WriteTo(p []byte, addr net.Addr) (int, error) { return len(p), nil }
func (c *fakePacketConn) Close() error                                 { return nil }
func (c *fakePacketConn) LocalAddr() net.Addr                          { return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)} }
func (c *fakePacketConn) SetDeadline(t time.Time) error                { return nil }
func (c *fakePacketConn) SetReadDeadline(t time.Time) error            { return nil }
func (c *fakePacketConn) SetWriteDeadline(t time.Time) error           { return nil }

func TestReadUDPSourceCheck(t *testing.T) {
	conn := &fakePacketConn{
		packets: []fakePacket{
			{data: []byte("evil"), addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 5000}},
			{data: []byte("good"), addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5000}},
		},
	}
	client := &Client{
		udpRTP:   conn,
		udpRTPIP: net.IPv4(127, 0, 0, 1),
	}
	frame, err := client.readUDP(context.Background())
	if err != nil {
		t.Fatalf("read udp: %v", err)
	}
	if string(frame.Payload) != "good" {
		t.Fatalf("accepted spoofed packet: %q", frame.Payload)
	}
}

func TestTunnelDialHonorsContext(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			defer conn.Close()
			_, _ = io.Copy(io.Discard, conn)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = DialWithConfig(ctx, "rtsp://"+ln.Addr().String()+"/live", "", "", &DialConfig{Tunnel: true})
	if err == nil {
		t.Fatal("expected tunnel dial to fail")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("tunnel dial ignored context for %v", elapsed)
	}
}

func TestRequestURLStripsUserAndFragment(t *testing.T) {
	u, err := url.Parse("rtsp://admin:secret@host:554/live?a=1#frag")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := requestURL(u, "rtsp"); got != "rtsp://host:554/live?a=1" {
		t.Fatalf("request url %q", got)
	}
}

func TestRequestURLAddsRootPath(t *testing.T) {
	u, err := url.Parse("rtsp://host:554")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := requestURL(u, "rtsp"); got != "rtsp://host:554/" {
		t.Fatalf("request url %q", got)
	}
}

func TestSetupTCPRequiresChannelPair(t *testing.T) {
	addr, closeFn := startScriptedServer(t, func(n int, req string) string {
		if strings.HasPrefix(req, "SETUP") {
			return "RTSP/1.0 200 OK\r\nTransport: RTP/AVP/TCP;unicast;interleaved=4-9\r\n\r\n"
		}
		return sdpResponse()
	})
	defer closeFn()

	client, err := Dial(context.Background(), "rtsp://"+addr+"/live", "", "", true)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	if err := client.Describe(context.Background()); err != nil {
		t.Fatalf("describe: %v", err)
	}
	if err := client.Setup(context.Background()); err == nil {
		t.Fatal("expected setup to reject a non adjacent interleaved pair")
	}
}

func TestSetupTCPAcceptsSAVP(t *testing.T) {
	addr, closeFn := startScriptedServer(t, func(n int, req string) string {
		if strings.HasPrefix(req, "SETUP") {
			return "RTSP/1.0 200 OK\r\nTransport: RTP/SAVP/TCP;unicast;interleaved=2-3\r\n\r\n"
		}
		return sdpResponse()
	})
	defer closeFn()

	client, err := Dial(context.Background(), "rtsp://"+addr+"/live", "", "", true)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	if err := client.Describe(context.Background()); err != nil {
		t.Fatalf("describe: %v", err)
	}
	if err := client.Setup(context.Background()); err != nil {
		t.Fatalf("setup with savp profile: %v", err)
	}
	if client.RTPChannel() != 2 {
		t.Fatalf("rtp channel %d, want 2", client.RTPChannel())
	}
}

func TestPickAuthChallengePrefersSHA256(t *testing.T) {
	values := []string{
		`Digest realm="r", nonce="n1", algorithm=MD5`,
		`Basic realm="r"`,
		`Digest realm="r", nonce="n2", algorithm=SHA-256`,
	}
	got := pickAuthChallenge(values, "u", "p")
	if !strings.Contains(got, "SHA-256") || !strings.Contains(got, `nonce="n2"`) {
		t.Fatalf("did not prefer sha-256: %s", got)
	}

	got = pickAuthChallenge([]string{`Digest realm="r", nonce="n1", algorithm=MD5`, `Basic realm="r"`}, "u", "p")
	if !strings.Contains(got, "MD5") {
		t.Fatalf("did not prefer digest over basic: %s", got)
	}
}

func TestSessDigestKeepsCnonce(t *testing.T) {
	auth := NewAuth(`Digest realm="r", nonce="n", algorithm=MD5-sess`, "u", "p")
	if auth == nil {
		t.Fatal("rejected md5-sess")
	}
	header := auth.Generate("DESCRIBE", "rtsp://host/live")
	if !strings.Contains(header, "cnonce=") {
		t.Fatalf("md5-sess without cnonce: %s", header)
	}
}

func TestOpenUDPPortsSendsToServer(t *testing.T) {
	rtp := &countingPacketConn{}
	rtcp := &countingPacketConn{}
	client := &Client{
		udp:           true,
		udpRTP:        rtp,
		udpRTCP:       rtcp,
		udpRTPIP:      net.IPv4(127, 0, 0, 1),
		udpServerRTP:  9000,
		udpServerRTCP: 9001,
	}
	client.openUDPPorts()

	if rtp.writes != 1 || rtcp.writes != 1 {
		t.Fatalf("expected one datagram each, got rtp=%d rtcp=%d", rtp.writes, rtcp.writes)
	}
	if rtp.lastPort != 9000 || rtcp.lastPort != 9001 {
		t.Fatalf("wrong ports: rtp=%d rtcp=%d", rtp.lastPort, rtcp.lastPort)
	}
}

type countingPacketConn struct {
	writes   int
	lastPort int
}

func (c *countingPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	return 0, nil, io.EOF
}

func (c *countingPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	c.writes++
	if ua, ok := addr.(*net.UDPAddr); ok {
		c.lastPort = ua.Port
	}
	return len(p), nil
}

func (c *countingPacketConn) Close() error                       { return nil }
func (c *countingPacketConn) LocalAddr() net.Addr                { return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)} }
func (c *countingPacketConn) SetDeadline(t time.Time) error      { return nil }
func (c *countingPacketConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *countingPacketConn) SetWriteDeadline(t time.Time) error { return nil }

func TestTunnelPathEscaped(t *testing.T) {
	u, err := url.Parse("rtsp://host/a%2Fb%20c?x=1#frag")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := tunnelPath(u); got != "/a%2Fb%20c?x=1" {
		t.Fatalf("tunnel path %q", got)
	}
}

func TestSetupTCPRejectsNonInterleaved(t *testing.T) {
	addr, closeFn := startScriptedServer(t, func(n int, req string) string {
		if strings.HasPrefix(req, "SETUP") {
			return "RTSP/1.0 200 OK\r\nTransport: RTP/AVP;unicast;client_port=8000-8001\r\n\r\n"
		}
		return sdpResponse()
	})
	defer closeFn()

	client, err := Dial(context.Background(), "rtsp://"+addr+"/live", "", "", true)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	if err := client.Describe(context.Background()); err != nil {
		t.Fatalf("describe: %v", err)
	}
	if err := client.Setup(context.Background()); err == nil {
		t.Fatal("expected setup to reject non interleaved transport")
	}
}

func TestSetupTCPRejectsOutOfRangeChannel(t *testing.T) {
	addr, closeFn := startScriptedServer(t, func(n int, req string) string {
		if strings.HasPrefix(req, "SETUP") {
			return "RTSP/1.0 200 OK\r\nTransport: RTP/AVP/TCP;unicast;interleaved=999-1000\r\n\r\n"
		}
		return sdpResponse()
	})
	defer closeFn()

	client, err := Dial(context.Background(), "rtsp://"+addr+"/live", "", "", true)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	if err := client.Describe(context.Background()); err != nil {
		t.Fatalf("describe: %v", err)
	}
	if err := client.Setup(context.Background()); err == nil {
		t.Fatal("expected setup to reject out of range channel")
	}
}

func TestSetupUDPRejectsTCPResponse(t *testing.T) {
	addr, closeFn := startScriptedServer(t, func(n int, req string) string {
		if strings.HasPrefix(req, "SETUP") {
			return "RTSP/1.0 200 OK\r\nTransport: RTP/AVP/TCP;unicast;server_port=9000-9001\r\n\r\n"
		}
		return sdpResponse()
	})
	defer closeFn()

	client, err := Dial(context.Background(), "rtsp://"+addr+"/live", "", "", true)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()
	client.Transport = TransportUDP

	if err := client.Describe(context.Background()); err != nil {
		t.Fatalf("describe: %v", err)
	}
	if err := client.Setup(context.Background()); err == nil {
		t.Fatal("expected setup to reject tcp transport on udp request")
	}
}

func TestReadRTPFrameSkipsOtherChannels(t *testing.T) {
	rtcp := []byte{magicByte, 1, 0, 4, 1, 2, 3, 4}
	rtp := []byte{magicByte, 0, 0, 3, 9, 9, 9}
	client := &Client{
		reader:     bufio.NewReader(bytes.NewReader(append(rtcp, rtp...))),
		rtpChannel: 0,
	}

	frame, err := client.ReadRTPFrame(context.Background())
	if err != nil {
		t.Fatalf("read rtp frame: %v", err)
	}
	if frame.Channel != 0 {
		t.Fatalf("channel %d, want 0", frame.Channel)
	}
	if !bytes.Equal(frame.Payload, []byte{9, 9, 9}) {
		t.Fatalf("payload %v", frame.Payload)
	}
}

func TestDoResendsBodyOnAuthRetry(t *testing.T) {
	bodies := make(chan string, 4)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		br := bufio.NewReader(conn)
		for i := 0; ; i++ {
			head, body, err := readTestRequestWithBody(br)
			if err != nil {
				return
			}
			bodies <- body
			if i == 0 {
				_, _ = conn.Write([]byte("RTSP/1.0 401 Unauthorized\r\n" +
					"WWW-Authenticate: Basic realm=\"cam\"\r\n\r\n"))
				continue
			}
			if !strings.Contains(head, "Authorization:") {
				_, _ = conn.Write([]byte("RTSP/1.0 401 Unauthorized\r\nWWW-Authenticate: Basic realm=\"cam\"\r\n\r\n"))
				continue
			}
			_, _ = conn.Write([]byte("RTSP/1.0 200 OK\r\n\r\n"))
		}
	}()

	client, err := Dial(context.Background(), "rtsp://"+ln.Addr().String()+"/live", "user", "pass", true)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	res, err := client.Do(context.Background(), "ANNOUNCE", client.cleanURL,
		map[string]string{"Content-Type": "application/sdp"}, []byte("v=0-body"))
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("status %d, want 200", res.StatusCode)
	}

	first := <-bodies
	second := <-bodies
	if first != "v=0-body" || second != "v=0-body" {
		t.Fatalf("body not resent: first=%q second=%q", first, second)
	}
}

func TestDescribeErrNoMedia(t *testing.T) {
	addr, closeFn := startScriptedServer(t, func(n int, req string) string {
		sdp := "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=x\r\nt=0 0\r\nm=audio 0 RTP/AVP 97\r\na=rtpmap:97 PCMU/8000\r\na=control:track0\r\n"
		return "RTSP/1.0 200 OK\r\nContent-Type: application/sdp\r\nContent-Length: " + strconv.Itoa(len(sdp)) + "\r\n\r\n" + sdp
	})
	defer closeFn()

	client, err := Dial(context.Background(), "rtsp://"+addr+"/live", "", "", true)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	_, err = client.DescribeAuth(context.Background())
	if !errors.Is(err, ErrNoMedia) {
		t.Fatalf("expected ErrNoMedia, got %v", err)
	}
}

func TestReadRTPFrameUDP(t *testing.T) {
	conn := &fakePacketConn{
		packets: []fakePacket{
			{data: []byte("payload"), addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5000}},
		},
	}
	client := &Client{
		udp:        true,
		udpRTP:     conn,
		udpRTPIP:   net.IPv4(127, 0, 0, 1),
		rtpChannel: 0,
	}

	frame, err := client.ReadRTPFrame(context.Background())
	if err != nil {
		t.Fatalf("read rtp frame: %v", err)
	}
	if string(frame.Payload) != "payload" {
		t.Fatalf("payload %q", frame.Payload)
	}
}
