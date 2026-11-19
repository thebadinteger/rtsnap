package rtsp

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	maxRedirects   = 3
	maxEarlyBytes  = 4 << 20 // rtp kept while a response is pending
	udpReadBuffer  = 2 << 20
	udpProbeWait   = 2 * time.Second
	teardownWait   = 500 * time.Millisecond
	readBufferSize = maxFrameSize
)

type Client struct {
	conn          net.Conn
	reader        *bufio.Reader
	cleanURL      string
	controlURL    string
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
	udpSeen       bool
	remoteIP      net.IP
	tunnel        bool
	secure        bool
	dial          DialConfig

	// bytes of the last peeked frame still sitting in the reader
	pending int
	// rtp packets that arrived ahead of a response
	early      [][]byte
	earlyBytes int

	armed       bool
	armDone     <-chan struct{}
	armDeadline time.Time
	disarmFn    func()
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
	u, err := parseURL(rtspURL)
	if err != nil {
		return nil, err
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

	c := &Client{user: user, pass: pass, dial: *cfg}
	if err := c.open(ctx, u); err != nil {
		return nil, err
	}
	return c, nil
}

// parse url, tolerating reserved characters in the credentials
func parseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" {
		if lu := parseLooseURL(raw); lu != nil {
			u, err = lu, nil
		}
	}
	if err != nil {
		// the wrapped error would repeat the url together with its password
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("invalid rtsp url: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "rtsp", "rtsps", "http", "https":
	default:
		return nil, fmt.Errorf("unsupported url scheme: %s", u.Scheme)
	}
	return u, nil
}

// split credentials at the last @ before the path, like most players do
func parseLooseURL(raw string) *url.URL {
	sep := strings.Index(raw, "://")
	if sep < 0 {
		return nil
	}
	rest := raw[sep+3:]
	authority, path := rest, ""
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		authority, path = rest[:slash], rest[slash:]
	}
	at := strings.LastIndexByte(authority, '@')
	if at < 0 {
		return nil
	}
	u, err := url.Parse(raw[:sep+3] + authority[at+1:] + path)
	if err != nil {
		return nil
	}
	user, pass, hasPass := strings.Cut(authority[:at], ":")
	if v, err := url.PathUnescape(user); err == nil {
		user = v
	}
	if v, err := url.PathUnescape(pass); err == nil {
		pass = v
	}
	if hasPass {
		u.User = url.UserPassword(user, pass)
	} else {
		u.User = url.User(user)
	}
	return u
}

// connect to the server behind u and reset every per connection state
func (c *Client) open(ctx context.Context, u *url.URL) error {
	scheme := strings.ToLower(u.Scheme)
	secure := scheme == "rtsps" || scheme == "https"
	tunnel := c.dial.Tunnel || scheme == "http" || scheme == "https"

	reqScheme := scheme
	switch scheme {
	case "http":
		reqScheme = "rtsp"
	case "https":
		reqScheme = "rtsps"
	}

	addr := dialAddr(u, scheme)
	tlsCfg := tlsClientConfig(c.dial.TLSConfig, u.Hostname(), secure)

	var conn net.Conn
	var err error
	if tunnel {
		conn, err = dialHTTPTunnel(ctx, addr, secure, tlsCfg, tunnelPath(u), c.user, c.pass)
	} else {
		conn, err = dialSocket(ctx, addr, secure, tlsCfg)
	}
	if err != nil {
		return err
	}

	c.conn = conn
	c.reader = bufio.NewReaderSize(conn, readBufferSize)
	c.cleanURL = requestURL(u, reqScheme)
	c.controlURL = c.cleanURL
	c.tunnel = tunnel
	c.secure = secure
	c.cseq = 0
	c.session = ""
	c.auth = nil
	c.authTries = 0
	c.rtpChannel = 0
	c.pending = 0
	c.early, c.earlyBytes = nil, 0
	c.armed = false
	c.remoteIP = nil
	if ta, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
		c.remoteIP = ta.IP
	}
	return nil
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

// bind socket deadlines to ctx, free when ctx did not change since the last call
func (c *Client) arm(ctx context.Context) {
	done := ctx.Done()
	deadline, _ := ctx.Deadline()
	if c.armed && done == c.armDone && deadline.Equal(c.armDeadline) {
		return
	}
	c.disarm()
	c.armed, c.armDone, c.armDeadline = true, done, deadline

	conn, udp := c.conn, c.udpRTP
	set := func(t time.Time) {
		if conn != nil {
			_ = conn.SetDeadline(t)
		}
		if udp != nil {
			_ = udp.SetReadDeadline(t)
		}
	}
	set(deadline)
	if done == nil {
		return
	}

	ran := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		set(time.Now())
		close(ran)
	})
	c.disarmFn = func() {
		// a callback already running must not outlive the next deadline
		if !stop() {
			<-ran
		}
	}
}

func (c *Client) disarm() {
	if c.disarmFn != nil {
		c.disarmFn()
		c.disarmFn = nil
	}
	c.armed = false
}

// prefer the context error over the socket error it caused
func ioError(ctx context.Context, err error) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if d, ok := ctx.Deadline(); ok && !time.Now().Before(d) && errors.Is(err, os.ErrDeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return err
}

// drop the frame handed out by the previous read
func (c *Client) consume() {
	if c.pending > 0 {
		_, _ = c.reader.Discard(c.pending)
		c.pending = 0
	}
}

// keep rtp that shows up before the response it was racing with
func (c *Client) stash(channel int, payload []byte) {
	if channel != c.rtpChannel || c.earlyBytes+len(payload) > maxEarlyBytes {
		return
	}
	c.early = append(c.early, append([]byte(nil), payload...))
	c.earlyBytes += len(payload)
}

func (c *Client) send(ctx context.Context, req *Request) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.arm(ctx)

	rawReq := c.prepare(req)
	if c.DebugFunc != nil {
		c.DebugFunc("> " + string(rawReq))
	}
	if _, err := c.conn.Write(rawReq); err != nil {
		return nil, ioError(ctx, err)
	}

	for {
		c.consume()
		b, err := c.reader.Peek(1)
		if err != nil {
			return nil, ioError(ctx, err)
		}

		switch b[0] {
		case magicByte:
			// servers may start streaming before they answer play
			channel, payload, size, err := peekFrame(c.reader)
			if err != nil {
				return nil, ioError(ctx, err)
			}
			c.pending = size
			c.stash(channel, payload)
			continue
		case '\r', '\n':
			_, _ = c.reader.Discard(1)
			continue
		}

		res, rawRes, err := readResponse(c.reader)
		if c.DebugFunc != nil && rawRes != "" {
			c.DebugFunc("< " + rawRes)
		}
		if err == errServerRequest {
			continue
		}
		if err != nil {
			return nil, ioError(ctx, err)
		}
		return res, nil
	}
}

// fill session headers and serialize
func (c *Client) prepare(req *Request) []byte {
	if req.Headers == nil {
		req.Headers = make(map[string]string)
	}
	c.cseq++
	req.Headers["CSeq"] = strconv.Itoa(c.cseq)
	ua := c.UserAgent
	if ua == "" {
		ua = "rtsnap"
	}
	req.Headers["User-Agent"] = ua

	if c.session != "" {
		req.Headers["Session"] = c.session
	}
	if c.auth != nil {
		req.Headers["Authorization"] = c.auth.GenerateBody(req.Method, req.URI, req.Body)
	}
	return req.Bytes()
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
	// without any credentials a retry can only fail again
	if c.user == "" && c.pass == "" {
		return unauth, nil
	}
	auth := NewAuth(challenge, c.user, c.pass)
	if auth == nil {
		return unauth, nil
	}
	c.auth = auth
	c.authTries++

	return c.send(ctx, req)
}

// fetch stream description, following redirects
func (c *Client) Describe(ctx context.Context) error {
	for hop := 0; ; hop++ {
		res, err := c.DescribeAuth(ctx)
		if err != nil {
			return err
		}
		if res.StatusCode == 200 {
			return nil
		}
		location := res.Header("Location")
		if res.StatusCode < 300 || res.StatusCode > 399 || location == "" || hop >= maxRedirects {
			return newStatusError("describe", res)
		}
		if err := c.redirect(ctx, location); err != nil {
			return err
		}
	}
}

// reconnect to the server named by a redirect
func (c *Client) redirect(ctx context.Context, location string) error {
	u, err := url.Parse(resolveURL(c.cleanURL, location))
	if err != nil {
		return fmt.Errorf("invalid redirect location: %q", location)
	}
	switch strings.ToLower(u.Scheme) {
	case "rtsps":
	case "rtsp":
		if c.secure {
			return fmt.Errorf("refusing redirect from rtsps to rtsp")
		}
	default:
		return fmt.Errorf("unsupported redirect scheme: %s", u.Scheme)
	}
	_ = c.Close()
	return c.open(ctx, u)
}

// describe returning the response even for error statuses, no auth retry
func (c *Client) DescribeRaw(ctx context.Context) (*Response, error) {
	res, err := c.send(ctx, c.describeRequest())
	if err != nil {
		return nil, fmt.Errorf("describe failed: %w", err)
	}
	return c.acceptDescribe(res)
}

// describe with credentials when challenged
func (c *Client) DescribeAuth(ctx context.Context) (*Response, error) {
	res, err := c.roundTrip(ctx, c.describeRequest())
	if err != nil {
		return nil, fmt.Errorf("describe failed: %w", err)
	}
	return c.acceptDescribe(res)
}

func (c *Client) describeRequest() *Request {
	return &Request{
		Method:  "DESCRIBE",
		URI:     c.cleanURL,
		Headers: map[string]string{"Accept": "application/sdp"},
	}
}

// parse sdp of a successful describe
func (c *Client) acceptDescribe(res *Response) (*Response, error) {
	if res.StatusCode != 200 {
		return res, nil
	}

	base := c.cleanURL
	if cb := res.Header("Content-Base"); cb != "" {
		base = resolveURL(c.cleanURL, cb)
	} else if cl := res.Header("Content-Location"); cl != "" {
		base = resolveURL(c.cleanURL, cl)
	}

	tracks, control, err := parseSDP(res.Body, base)
	if err != nil {
		return res, fmt.Errorf("parse sdp: %w", err)
	}
	c.Tracks = tracks
	c.controlURL = control
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
		if ctx.Err() != nil {
			return nil, ctx.Err()
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

// open udp port pair, adjacent with an even rtp port when the os allows it
func listenPair() (net.PacketConn, net.PacketConn, int, int, error) {
	for try := 0; ; try++ {
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
		if (pa%2 == 0 && pb == pa+1) || try >= 16 {
			return a, b, pa, pb, nil
		}
		_ = a.Close()
		_ = b.Close()
	}
}

// value of a transport header parameter
func transportParam(trans, key string) (string, bool) {
	for _, part := range strings.Split(trans, ";") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		if strings.EqualFold(strings.TrimSpace(k), key) {
			return strings.TrimSpace(v), true
		}
	}
	return "", false
}

// parse a port or channel pair, a lone value implies the next one
func transportPair(value string) (int, int, bool) {
	first, second, isPair := strings.Cut(value, "-")
	a, err := strconv.Atoi(strings.TrimSpace(first))
	if err != nil {
		return 0, 0, false
	}
	if !isPair {
		return a, a + 1, true
	}
	b, err := strconv.Atoi(strings.TrimSpace(second))
	if err != nil {
		return 0, 0, false
	}
	return a, b, true
}

func udpServerPort(trans string) int {
	v, _ := transportParam(trans, "server_port")
	port, _, _ := transportPair(v)
	return port
}

// source address announced by the server, nil if absent
func transportSource(trans string) net.IP {
	v, ok := transportParam(trans, "source")
	if !ok {
		return nil
	}
	ip := net.ParseIP(strings.Trim(strings.Trim(v, `"`), "[]"))
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
		return nil
	}
	return ip
}

// accept rtp only from the negotiated peer address
func (c *Client) udpSourceOK(addr net.Addr) bool {
	if c.udpRTPIP == nil {
		return true
	}
	ua, ok := addr.(*net.UDPAddr)
	if !ok {
		return false
	}
	return ua.IP.Equal(c.udpRTPIP) || (c.remoteIP != nil && ua.IP.Equal(c.remoteIP))
}

// drop udp sockets from a previous setup so repeated setups do not leak them
func (c *Client) closeUDP() {
	if c.udpRTP != nil {
		_ = c.udpRTP.Close()
		c.udpRTP = nil
	}
	if c.udpRTCP != nil {
		_ = c.udpRTCP.Close()
		c.udpRTCP = nil
	}
	c.udp = false
	c.udpSeen = false
	c.udpRTPIP = nil
	c.udpServerRTP, c.udpServerRTCP = 0, 0
	c.armed = false
}

// negotiate udp unicast transport
func (c *Client) setupUDP(ctx context.Context) (*Response, error) {
	c.closeUDP()

	rtpConn, rtcpConn, rtpPort, rtcpPort, err := listenPair()
	if err != nil {
		return nil, fmt.Errorf("setup udp listen: %w", err)
	}
	// a key frame arrives as one burst, the default buffer drops part of it
	if uc, ok := rtpConn.(*net.UDPConn); ok {
		_ = uc.SetReadBuffer(udpReadBuffer)
	}
	closePair := func() {
		_ = rtpConn.Close()
		_ = rtcpConn.Close()
	}

	req := &Request{
		Method: "SETUP",
		URI:    c.Track.Control,
		Headers: map[string]string{
			"Transport": "RTP/AVP;unicast;client_port=" + strconv.Itoa(rtpPort) + "-" + strconv.Itoa(rtcpPort),
		},
	}

	res, err := c.roundTrip(ctx, req)
	if err != nil {
		closePair()
		return nil, fmt.Errorf("setup failed: %w", err)
	}
	if res.StatusCode != 200 {
		closePair()
		return res, nil
	}

	trans := res.Header("Transport")
	if _, interleaved := transportParam(trans, "interleaved"); interleaved || strings.Contains(strings.ToUpper(trans), "/TCP") {
		closePair()
		return res, fmt.Errorf("setup udp: server did not accept udp transport: %s", trans)
	}

	c.storeSession(res)
	c.udpRTP = rtpConn
	c.udpRTCP = rtcpConn
	c.udp = true
	c.armed = false
	if v, ok := transportParam(trans, "server_port"); ok {
		c.udpServerRTP, c.udpServerRTCP, _ = transportPair(v)
	}
	c.udpRTPIP = transportSource(trans)
	if c.udpRTPIP == nil {
		c.udpRTPIP = c.remoteIP
	}
	return res, nil
}

// send a first datagram so stateful firewalls let the server stream back
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
	c.closeUDP()

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

	// a missing header or channel pair means the requested one was taken as is
	trans := res.Header("Transport")
	if trans != "" && !strings.Contains(strings.ToUpper(trans), "/TCP") {
		return res, fmt.Errorf("setup tcp: server did not accept interleaved transport: %s", trans)
	}

	rtpCh := 0
	if v, ok := transportParam(trans, "interleaved"); ok {
		a, b, ok := transportPair(v)
		// rtp and rtcp channels must be an adjacent pair within the 0-255 range
		if !ok || a < 0 || b != a+1 || a > 254 {
			return res, fmt.Errorf("setup tcp: invalid interleaved channels: %s", trans)
		}
		rtpCh = a
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
	if c.udp {
		// punch before play so the very first packets are not filtered
		c.openUDPPorts()
	}
	res, err := c.Do(ctx, "PLAY", c.controlURL, map[string]string{"Range": "npt=0.000-"}, nil)
	if err != nil {
		return nil, fmt.Errorf("play failed: %w", err)
	}
	return res, nil
}

// read next packet of any channel, valid until the next read
func (c *Client) read(ctx context.Context) (int, []byte, error) {
	if len(c.early) > 0 {
		payload := c.early[0]
		c.early[0] = nil
		c.early = c.early[1:]
		c.earlyBytes -= len(payload)
		return c.rtpChannel, payload, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}
	c.arm(ctx)

	if c.udp {
		payload, err := c.readUDP(ctx)
		return c.rtpChannel, payload, err
	}

	for {
		c.consume()
		b, err := c.reader.Peek(1)
		if err != nil {
			return 0, nil, ioError(ctx, err)
		}
		if b[0] == magicByte {
			channel, payload, size, err := peekFrame(c.reader)
			if err != nil {
				return 0, nil, ioError(ctx, err)
			}
			c.pending = size
			return channel, payload, nil
		}

		// late responses, server requests and stray bytes are not media
		if err := resync(c.reader); err != nil {
			return 0, nil, ioError(ctx, err)
		}
		if b, _ := c.reader.Peek(1); len(b) == 1 && b[0] != magicByte {
			_, _, _ = readResponse(c.reader)
		}
	}
}

// read next interleaved frame
func (c *Client) ReadFrame(ctx context.Context) (*Frame, error) {
	channel, payload, err := c.read(ctx)
	if err != nil {
		return nil, err
	}
	return &Frame{Channel: channel, Payload: append([]byte(nil), payload...)}, nil
}

// read next frame of the negotiated rtp channel
func (c *Client) ReadRTPFrame(ctx context.Context) (*Frame, error) {
	payload, err := c.ReadPacket(ctx)
	if err != nil {
		return nil, err
	}
	return &Frame{Channel: c.rtpChannel, Payload: append([]byte(nil), payload...)}, nil
}

// read next rtp packet without copying, the slice is only valid until the next read
func (c *Client) ReadPacket(ctx context.Context) ([]byte, error) {
	for {
		channel, payload, err := c.read(ctx)
		if err != nil {
			return nil, err
		}
		if channel == c.rtpChannel {
			return payload, nil
		}
	}
}

// read next udp datagram
func (c *Client) readUDP(ctx context.Context) ([]byte, error) {
	if c.udpBuf == nil {
		c.udpBuf = make([]byte, 64*1024)
	}

	// with auto transport a silent udp path must fail early enough to retry over tcp
	probing := c.Transport == TransportAuto && !c.udpSeen
	if probing {
		wait := udpProbeWait
		if d, ok := ctx.Deadline(); ok {
			wait = min(wait, time.Until(d)/3)
		}
		_ = c.udpRTP.SetReadDeadline(time.Now().Add(wait))
		c.armed = false
	}

	for {
		n, addr, err := c.udpRTP.ReadFrom(c.udpBuf)
		if err != nil {
			if e := ctx.Err(); e != nil {
				return nil, e
			}
			if probing && errors.Is(err, os.ErrDeadlineExceeded) {
				return nil, ErrUDPTimeout
			}
			return nil, ioError(ctx, err)
		}
		if n <= 0 || !c.udpSourceOK(addr) {
			continue
		}
		c.udpSeen = true
		return c.udpBuf[:n], nil
	}
}

// ask the server to stop, the reply is not awaited since the connection is closed right after
func (c *Client) Teardown(_ context.Context) error {
	if c.session == "" || c.conn == nil {
		return nil
	}
	c.disarm()
	rawReq := c.prepare(&Request{Method: "TEARDOWN", URI: c.controlURL})
	c.session = ""
	if c.DebugFunc != nil {
		c.DebugFunc("> " + string(rawReq))
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(teardownWait))
	_, err := c.conn.Write(rawReq)
	return err
}

// active rtp channel number
func (c *Client) RTPChannel() int {
	return c.rtpChannel
}

// true when rtp is delivered over udp
func (c *Client) UDP() bool {
	return c.udp
}

// close all connections
func (c *Client) Close() error {
	c.disarm()
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
	for _, t := range c.Tracks {
		seen := false
		for _, codec := range res {
			seen = seen || codec == t.Codec
		}
		if !seen {
			res = append(res, t.Codec)
		}
	}
	return res
}
