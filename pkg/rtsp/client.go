package rtsp

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	conn          net.Conn
	reader        *bufio.Reader
	rawURL        string
	cleanURL      string
	user          string
	pass          string
	cseq          int
	session       string
	auth          *Auth
	authTries     int
	Tracks        []*MediaTrack
	Track         *MediaTrack
	Transport     Transport
	UserAgent     string
	DebugFunc     func(string)
	rtpChannel    int
	udp           bool
	udpRTP        net.PacketConn
	udpRTCP       net.PacketConn
	udpBuf        []byte
	udpRTPIP      net.IP
	udpServerRTP  int
	udpServerRTCP int
	remoteIP      net.IP
	tunnel        bool
}

// dial settings overriding defaults
type DialConfig struct {
	TLSConfig *tls.Config
	Tunnel    bool
}

// open tcp connection to rtsp server
func Dial(ctx context.Context, rtspURL string, user, pass string, tlsSkipVerify bool) (*Client, error) {
	return DialWithConfig(ctx, rtspURL, user, pass, &DialConfig{
		TLSConfig: &tls.Config{InsecureSkipVerify: tlsSkipVerify},
	})
}

// open connection with explicit tls and tunnel settings
func DialWithConfig(ctx context.Context, rtspURL string, user, pass string, cfg *DialConfig) (*Client, error) {
	u, err := url.Parse(rtspURL)
	if err != nil {
		return nil, fmt.Errorf("invalid rtsp url: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "rtsp", "rtsps", "http", "https":
	default:
		return nil, fmt.Errorf("unsupported url scheme: %s", u.Scheme)
	}
	if cfg == nil {
		cfg = &DialConfig{}
	}

	if u.User != nil {
		if user == "" {
			user = u.User.Username()
		}
		if pass == "" {
			pass, _ = u.User.Password()
		}
	}

	secure := scheme == "rtsps" || scheme == "https"
	tunnel := cfg.Tunnel || scheme == "http" || scheme == "https"

	reqScheme := scheme
	switch scheme {
	case "http":
		reqScheme = "rtsp"
	case "https":
		reqScheme = "rtsps"
	}

	cleanURL := requestURL(u, reqScheme)

	addr := dialAddr(u, scheme)
	tlsCfg := tlsClientConfig(cfg.TLSConfig, u.Hostname(), secure)

	var conn net.Conn
	if tunnel {
		conn, err = dialHTTPTunnel(ctx, addr, secure, tlsCfg, tunnelPath(u))
	} else {
		conn, err = dialSocket(ctx, addr, secure, tlsCfg)
	}
	if err != nil {
		return nil, err
	}

	c := &Client{
		conn:       conn,
		reader:     bufio.NewReaderSize(conn, 64*1024),
		rawURL:     rtspURL,
		cleanURL:   cleanURL,
		user:       user,
		pass:       pass,
		tunnel:     tunnel,
		rtpChannel: 0,
	}
	if ta, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
		c.remoteIP = ta.IP
	}
	return c, nil
}

// rtsp request uri without credentials or fragment
func requestURL(u *url.URL, scheme string) string {
	clean := *u
	clean.User = nil
	clean.Fragment = ""
	clean.Scheme = scheme
	if clean.Path == "" {
		clean.Path = "/"
	}
	return clean.String()
}

// address with default port for every supported scheme
func dialAddr(u *url.URL, scheme string) string {
	port := u.Port()
	if port == "" {
		switch scheme {
		case "rtsps":
			port = "322"
		case "http":
			port = "80"
		case "https":
			port = "443"
		default:
			port = "554"
		}
	}
	return net.JoinHostPort(u.Hostname(), port)
}

func tlsClientConfig(base *tls.Config, serverName string, secure bool) *tls.Config {
	if !secure {
		return nil
	}
	var cfg *tls.Config
	if base != nil {
		cfg = base.Clone()
	} else {
		cfg = &tls.Config{}
	}
	if cfg.ServerName == "" {
		cfg.ServerName = serverName
	}
	return cfg
}

func tunnelPath(u *url.URL) string {
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	return path
}

// open tcp or tls socket
func dialSocket(ctx context.Context, addr string, secure bool, cfg *tls.Config) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", addr, err)
	}
	if !secure {
		return conn, nil
	}
	tc := tls.Client(conn, cfg)
	if err := tc.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("tls handshake with %s: %w", addr, err)
	}
	return tc, nil
}

func (c *Client) nextCSeq() int {
	c.cseq++
	return c.cseq
}

func (c *Client) send(ctx context.Context, req *Request) (*Response, error) {
	if d, ok := ctx.Deadline(); ok {
		_ = c.conn.SetDeadline(d)
	}

	stop := context.AfterFunc(ctx, func() {
		_ = c.conn.SetDeadline(time.Now())
	})
	defer stop()

	if req.Headers == nil {
		req.Headers = make(map[string]string)
	}
	req.Headers["CSeq"] = strconv.Itoa(c.nextCSeq())
	ua := c.UserAgent
	if ua == "" {
		ua = "rtsnap"
	}
	req.Headers["User-Agent"] = ua

	if c.session != "" {
		req.Headers["Session"] = c.session
	}

	if c.auth != nil {
		req.Headers["Authorization"] = c.auth.Generate(req.Method, req.URI)
	}

	rawReq := req.Bytes()
	if c.DebugFunc != nil {
		c.DebugFunc("> " + string(rawReq))
	}
	if _, err := c.conn.Write(rawReq); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}

	res, rawRes, err := readResponse(c.reader)
	if c.DebugFunc != nil && rawRes != "" {
		c.DebugFunc("< " + rawRes)
	}
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return res, err
}

// send request with auth retry on 401 and stale nonce
func (c *Client) roundTrip(ctx context.Context, req *Request) (*Response, error) {
	res, err := c.send(ctx, req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode == 401 {
		return c.authRetry(ctx, req, res)
	}
	c.authTries = 0
	return res, nil
}

// raw round trip without automatic auth retry
func (c *Client) rawRoundTrip(ctx context.Context, req *Request) (*Response, error) {
	return c.send(ctx, req)
}

// rebuild request with credentials when challenged or stale
func (c *Client) authRetry(ctx context.Context, req *Request, unauth *Response) (*Response, error) {
	values := unauth.Values("WWW-Authenticate")
	if len(values) == 0 {
		return unauth, nil
	}

	stale := false
	for _, v := range values {
		if staleChallenge(v) {
			stale = true
			break
		}
	}
	if c.auth != nil && !stale {
		return unauth, nil
	}
	if c.authTries >= 2 {
		return unauth, nil
	}

	challenge := pickAuthChallenge(values)
	if challenge == "" {
		return unauth, nil
	}
	auth := NewAuth(challenge, c.user, c.pass)
	if auth == nil {
		return unauth, nil
	}
	c.auth = auth
	c.authTries++

	// body was already sent once, send it again on the retry
	if len(req.Body) > 0 {
		req = &Request{Method: req.Method, URI: req.URI, Headers: req.Headers, Body: append([]byte(nil), req.Body...)}
	}
	return c.send(ctx, req)
}

// fetch stream description
func (c *Client) Describe(ctx context.Context) error {
	res, err := c.DescribeAuth(ctx)
	if err != nil {
		return err
	}
	if res.StatusCode != 200 {
		return newStatusError("describe", res)
	}
	return nil
}

// describe returning the response even for error statuses, no auth retry
func (c *Client) DescribeRaw(ctx context.Context) (*Response, error) {
	res, err := c.sendMethod(ctx, "DESCRIBE", c.cleanURL, map[string]string{"Accept": "application/sdp"})
	if err != nil {
		return nil, fmt.Errorf("describe failed: %w", err)
	}
	return c.acceptDescribe(res)
}

// describe with credentials when challenged
func (c *Client) DescribeAuth(ctx context.Context) (*Response, error) {
	res, err := c.Do(ctx, "DESCRIBE", c.cleanURL, map[string]string{"Accept": "application/sdp"}, nil)
	if err != nil {
		return nil, fmt.Errorf("describe failed: %w", err)
	}
	return c.acceptDescribe(res)
}

// parse sdp of a successful describe
func (c *Client) acceptDescribe(res *Response) (*Response, error) {
	if res.StatusCode != 200 {
		return res, nil
	}

	tracks, err := ParseSDP(res.Body, c.cleanURL)
	if err != nil {
		return res, fmt.Errorf("parse sdp: %w", err)
	}
	c.Tracks = tracks
	c.Track, _ = SelectTrack(tracks, "")
	return res, nil
}

// send a request with auth and session handling
func (c *Client) Do(ctx context.Context, method, uri string, headers map[string]string, body []byte) (*Response, error) {
	req := &Request{
		Method:  method,
		URI:     uri,
		Headers: headers,
		Body:    body,
	}
	return c.roundTrip(ctx, req)
}

// send a request without automatic auth retry
func (c *Client) sendMethod(ctx context.Context, method, uri string, headers map[string]string) (*Response, error) {
	req := &Request{
		Method:  method,
		URI:     uri,
		Headers: headers,
	}
	return c.rawRoundTrip(ctx, req)
}

// negotiate media transport
func (c *Client) Setup(ctx context.Context) error {
	res, err := c.SetupRaw(ctx)
	if err != nil {
		return err
	}
	if res.StatusCode != 200 {
		return newStatusError("setup", res)
	}
	return nil
}

// setup returning the final response even for error statuses
func (c *Client) SetupRaw(ctx context.Context) (*Response, error) {
	if c.Track == nil {
		return nil, fmt.Errorf("no track available for setup")
	}
	if c.tunnel {
		return c.setupTCP(ctx)
	}

	t := c.Transport
	if t == "" {
		t = TransportTCP
	}

	if t == TransportUDP || t == TransportAuto {
		res, err := c.setupUDP(ctx)
		if err == nil && res.StatusCode == 200 {
			return res, nil
		}
		if t == TransportUDP {
			if err != nil {
				return nil, err
			}
			return res, nil
		}
	}

	return c.setupTCP(ctx)
}

func (c *Client) storeSession(res *Response) {
	sess := res.Header("Session")
	if sess == "" {
		return
	}
	if semi := strings.IndexByte(sess, ';'); semi != -1 {
		sess = sess[:semi]
	}
	c.session = strings.TrimSpace(sess)
}

// open adjacent udp port pair
func listenPair() (net.PacketConn, net.PacketConn, int, int, error) {
	for range 16 {
		a, err := net.ListenPacket("udp", ":0")
		if err != nil {
			return nil, nil, 0, 0, err
		}
		b, err := net.ListenPacket("udp", ":0")
		if err != nil {
			_ = a.Close()
			return nil, nil, 0, 0, err
		}
		pa := a.LocalAddr().(*net.UDPAddr).Port
		pb := b.LocalAddr().(*net.UDPAddr).Port
		if pa%2 == 0 && pb == pa+1 {
			return a, b, pa, pb, nil
		}
		_ = a.Close()
		_ = b.Close()
	}

	a, err := net.ListenPacket("udp", ":0")
	if err != nil {
		return nil, nil, 0, 0, err
	}
	b, err := net.ListenPacket("udp", ":0")
	if err != nil {
		_ = a.Close()
		return nil, nil, 0, 0, err
	}
	pa := a.LocalAddr().(*net.UDPAddr).Port
	pb := b.LocalAddr().(*net.UDPAddr).Port
	return a, b, pa, pb, nil
}

func udpServerPort(trans string) int {
	for _, part := range strings.Split(trans, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "server_port=") {
			ports := strings.TrimPrefix(part, "server_port=")
			if dash := strings.IndexByte(ports, '-'); dash != -1 {
				ports = ports[:dash]
			}
			if p, err := strconv.Atoi(strings.TrimSpace(ports)); err == nil {
				return p
			}
		}
	}
	return 0
}

// source address announced by the server, nil if absent
func transportSource(trans string) net.IP {
	for _, part := range strings.Split(trans, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "source=") {
			value := strings.Trim(strings.TrimSpace(strings.TrimPrefix(part, "source=")), `"`)
			value = strings.Trim(value, "[]")
			return net.ParseIP(value)
		}
	}
	return nil
}

// accept rtp only from the negotiated peer
func (c *Client) udpSourceOK(addr net.Addr) bool {
	if c.udpRTPIP == nil {
		return true
	}
	ua, ok := addr.(*net.UDPAddr)
	if !ok {
		return false
	}
	return ua.IP.Equal(c.udpRTPIP)
}

// negotiate udp unicast transport
func (c *Client) setupUDP(ctx context.Context) (*Response, error) {
	rtpConn, rtcpConn, rtpPort, rtcpPort, err := listenPair()
	if err != nil {
		return nil, fmt.Errorf("setup udp listen: %w", err)
	}

	req := &Request{
		Method: "SETUP",
		URI:    c.Track.Control,
		Headers: map[string]string{
			"Transport": fmt.Sprintf("RTP/AVP;unicast;client_port=%d-%d", rtpPort, rtcpPort),
		},
	}

	res, err := c.roundTrip(ctx, req)
	if err != nil {
		_ = rtpConn.Close()
		_ = rtcpConn.Close()
		return nil, fmt.Errorf("setup failed: %w", err)
	}
	if res.StatusCode != 200 {
		_ = rtpConn.Close()
		_ = rtcpConn.Close()
		return res, nil
	}

	trans := res.Header("Transport")
	if strings.Contains(trans, "RTP/AVP/TCP") {
		_ = rtpConn.Close()
		_ = rtcpConn.Close()
		return res, fmt.Errorf("setup udp: server did not accept udp transport: %s", trans)
	}
	if udpServerPort(trans) == 0 {
		_ = rtpConn.Close()
		_ = rtcpConn.Close()
		return nil, fmt.Errorf("setup udp: server did not provide server_port")
	}

	c.storeSession(res)
	c.udpRTP = rtpConn
	c.udpRTCP = rtcpConn
	c.udp = true
	c.udpServerRTP = udpServerPort(trans)
	c.udpServerRTCP = c.udpServerRTP + 1
	c.udpRTPIP = transportSource(trans)
	if c.udpRTPIP == nil {
		c.udpRTPIP = c.remoteIP
	}
	return res, nil
}

// send a first datagram so stateful firewalls let the camera stream back
func (c *Client) openUDPPorts() {
	if c.udpRTP == nil || c.udpRTCP == nil || c.udpRTPIP == nil || c.udpServerRTP == 0 {
		return
	}
	rtp := &net.UDPAddr{IP: c.udpRTPIP, Port: c.udpServerRTP}
	rtcp := &net.UDPAddr{IP: c.udpRTPIP, Port: c.udpServerRTCP}
	_, _ = c.udpRTP.WriteTo([]byte{0x80, 0x00, 0x00, 0x00, 0, 0, 0, 0, 0, 0, 0, 0}, rtp)
	_, _ = c.udpRTCP.WriteTo([]byte{0x80, 0xc9, 0x00, 0x01, 0, 0, 0, 0}, rtcp)
}

// negotiate tcp interleaved transport
func (c *Client) setupTCP(ctx context.Context) (*Response, error) {
	req := &Request{
		Method: "SETUP",
		URI:    c.Track.Control,
		Headers: map[string]string{
			"Transport": "RTP/AVP/TCP;unicast;interleaved=0-1",
		},
	}

	res, err := c.roundTrip(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("setup failed: %w", err)
	}
	if res.StatusCode != 200 {
		return res, nil
	}

	trans := res.Header("Transport")
	if !strings.Contains(trans, "/TCP") {
		return res, fmt.Errorf("setup tcp: server did not accept interleaved transport: %s", trans)
	}

	rtpCh, rtcpCh := -1, -1
	for _, part := range strings.Split(trans, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "interleaved=") {
			chans := strings.TrimPrefix(part, "interleaved=")
			if dash := strings.IndexByte(chans, '-'); dash != -1 {
				a, errA := strconv.Atoi(strings.TrimSpace(chans[:dash]))
				b, errB := strconv.Atoi(strings.TrimSpace(chans[dash+1:]))
				if errA == nil && errB == nil {
					rtpCh, rtcpCh = a, b
				}
			}
		}
	}
	// rtp and rtcp channels must be an adjacent pair inside a single byte
	if rtpCh < 0 || rtcpCh != rtpCh+1 || rtpCh > 254 {
		return res, fmt.Errorf("setup tcp: invalid interleaved channels: %s", trans)
	}

	c.storeSession(res)
	c.rtpChannel = rtpCh

	return res, nil
}

// start stream playback
func (c *Client) Play(ctx context.Context) error {
	res, err := c.PlayRaw(ctx)
	if err != nil {
		return err
	}
	if res.StatusCode != 200 {
		return newStatusError("play", res)
	}
	return nil
}

// play returning the response even for error statuses
func (c *Client) PlayRaw(ctx context.Context) (*Response, error) {
	res, err := c.Do(ctx, "PLAY", c.cleanURL, map[string]string{"Range": "npt=0.000-"}, nil)
	if err != nil {
		return nil, fmt.Errorf("play failed: %w", err)
	}
	if res.StatusCode == 200 && c.udp {
		c.openUDPPorts()
	}
	return res, nil
}

// read next interleaved frame
func (c *Client) ReadFrame(ctx context.Context) (*Frame, error) {
	if c.udp {
		return c.readUDP(ctx)
	}

	if d, ok := ctx.Deadline(); ok {
		_ = c.conn.SetReadDeadline(d)
	}

	stop := context.AfterFunc(ctx, func() {
		_ = c.conn.SetReadDeadline(time.Now())
	})
	defer stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		b, err := c.reader.Peek(1)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}

		if b[0] == magicByte {
			frame, err := readFrame(c.reader)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, err
			}
			return frame, nil
		}

		_, err = readLine(c.reader)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
	}
}

func (c *Client) ReadRTPFrame(ctx context.Context) (*Frame, error) {
	for {
		frame, err := c.ReadFrame(ctx)
		if err != nil {
			return nil, err
		}
		if frame.Channel == c.rtpChannel {
			return frame, nil
		}
	}
}

// read next udp datagram
func (c *Client) readUDP(ctx context.Context) (*Frame, error) {
	if d, ok := ctx.Deadline(); ok {
		_ = c.udpRTP.SetReadDeadline(d)
	}

	stop := context.AfterFunc(ctx, func() {
		_ = c.udpRTP.SetReadDeadline(time.Now())
	})
	defer stop()

	if c.udpBuf == nil {
		c.udpBuf = make([]byte, 64*1024)
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		n, addr, err := c.udpRTP.ReadFrom(c.udpBuf)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		if n <= 0 || !c.udpSourceOK(addr) {
			continue
		}

		payload := make([]byte, n)
		copy(payload, c.udpBuf[:n])
		return &Frame{Channel: c.rtpChannel, Payload: payload}, nil
	}
}

// stop playback on server
func (c *Client) Teardown(ctx context.Context) error {
	if c.session == "" {
		return nil
	}
	req := &Request{
		Method: "TEARDOWN",
		URI:    c.cleanURL,
	}
	_, _ = c.send(ctx, req)
	return nil
}

// active rtp channel number
func (c *Client) RTPChannel() int {
	return c.rtpChannel
}

// close all connections
func (c *Client) Close() error {
	if c.udpRTP != nil {
		_ = c.udpRTP.Close()
	}
	if c.udpRTCP != nil {
		_ = c.udpRTCP.Close()
	}
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// pick video track by codec
func (c *Client) SelectTrack(codec string) (*MediaTrack, error) {
	t, err := SelectTrack(c.Tracks, codec)
	if err != nil {
		return nil, err
	}
	c.Track = t
	return t, nil
}

// list codecs present in stream
func (c *Client) AvailableCodecs() []string {
	var res []string
	seen := make(map[string]bool)
	for _, t := range c.Tracks {
		if !seen[t.Codec] {
			seen[t.Codec] = true
			res = append(res, t.Codec)
		}
	}
	return res
}
