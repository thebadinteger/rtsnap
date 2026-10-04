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
	conn       net.Conn
	reader     *bufio.Reader
	rawURL     string
	cleanURL   string
	user       string
	pass       string
	cseq       int
	session    string
	auth       *Auth
	Tracks     []*MediaTrack
	Track      *MediaTrack
	Transport  Transport
	rtpChannel int
	udp        bool
	udpRTP     net.PacketConn
	udpRTCP    net.PacketConn
	udpBuf     []byte
}

// open tcp connection to rtsp server
func Dial(ctx context.Context, rtspURL string, user, pass string) (*Client, error) {
	u, err := url.Parse(rtspURL)
	if err != nil {
		return nil, fmt.Errorf("invalid rtsp url: %w", err)
	}

	if u.User != nil {
		if user == "" {
			user = u.User.Username()
		}
		if pass == "" {
			pass, _ = u.User.Password()
		}
	}

	cleanU := *u
	cleanU.User = nil
	cleanURL := cleanU.String()

	host := u.Host
	if !strings.Contains(host, ":") {
		if strings.EqualFold(u.Scheme, "rtsps") {
			host += ":322"
		} else {
			host += ":554"
		}
	}

	var d net.Dialer
	var conn net.Conn
	if strings.EqualFold(u.Scheme, "rtsps") {
		conn, err = tls.DialWithDialer(&d, "tcp", host, &tls.Config{
			InsecureSkipVerify: true,
		})
	} else {
		conn, err = d.DialContext(ctx, "tcp", host)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", host, err)
	}

	return &Client{
		conn:       conn,
		reader:     bufio.NewReaderSize(conn, 64*1024),
		rawURL:     rtspURL,
		cleanURL:   cleanURL,
		user:       user,
		pass:       pass,
		rtpChannel: 0,
	}, nil
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
	req.Headers["User-Agent"] = "rtsnap"

	if c.session != "" {
		req.Headers["Session"] = c.session
	}

	if c.auth != nil {
		req.Headers["Authorization"] = c.auth.Generate(req.Method, req.URI)
	}

	if err := req.Write(c.conn); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}

	res, err := readResponse(c.reader)
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return res, err
}

// send request with auth retry on 401
func (c *Client) roundTrip(ctx context.Context, req *Request) (*Response, error) {
	res, err := c.send(ctx, req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode == 401 {
		return c.authRetry(ctx, req, res)
	}
	return res, nil
}

// rebuild request with credentials once
func (c *Client) authRetry(ctx context.Context, req *Request, unauth *Response) (*Response, error) {
	if c.auth != nil {
		return unauth, nil
	}
	wwwAuth := unauth.Header("WWW-Authenticate")
	if wwwAuth == "" {
		return nil, fmt.Errorf("unauthorized without www-authenticate header")
	}
	c.auth = NewAuth(wwwAuth, c.user, c.pass)
	if c.auth == nil {
		return nil, fmt.Errorf("unsupported auth header: %s", wwwAuth)
	}
	return c.send(ctx, req)
}

// fetch stream description
func (c *Client) Describe(ctx context.Context) error {
	req := &Request{
		Method: "DESCRIBE",
		URI:    c.cleanURL,
		Headers: map[string]string{
			"Accept": "application/sdp",
		},
	}

	res, err := c.roundTrip(ctx, req)
	if err != nil {
		return fmt.Errorf("describe failed: %w", err)
	}

	if res.StatusCode != 200 {
		return fmt.Errorf("describe rejected: %d %s", res.StatusCode, res.StatusMessage)
	}

	tracks, err := ParseSDP(res.Body, c.cleanURL)
	if err != nil {
		return fmt.Errorf("parse sdp: %w", err)
	}
	c.Tracks = tracks
	c.Track, _ = SelectTrack(tracks, "")
	return nil
}

// negotiate transport with fallback
func (c *Client) Setup(ctx context.Context) error {
	if c.Track == nil {
		return fmt.Errorf("no track available for setup")
	}

	t := c.Transport
	if t == "" {
		t = TransportTCP
	}

	if t == TransportUDP || t == TransportAuto {
		if err := c.setupUDP(ctx); err != nil {
			if t != TransportAuto {
				return err
			}
		} else {
			return nil
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

// negotiate udp unicast transport
func (c *Client) setupUDP(ctx context.Context) error {
	rtpConn, rtcpConn, rtpPort, rtcpPort, err := listenPair()
	if err != nil {
		return fmt.Errorf("setup udp listen: %w", err)
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
		return fmt.Errorf("setup failed: %w", err)
	}
	if res.StatusCode != 200 {
		_ = rtpConn.Close()
		_ = rtcpConn.Close()
		return fmt.Errorf("setup rejected: %d %s", res.StatusCode, res.StatusMessage)
	}
	if udpServerPort(res.Header("Transport")) == 0 {
		_ = rtpConn.Close()
		_ = rtcpConn.Close()
		return fmt.Errorf("setup udp: server did not provide server_port")
	}

	c.storeSession(res)
	c.udpRTP = rtpConn
	c.udpRTCP = rtcpConn
	c.udp = true
	return nil
}

// negotiate tcp interleaved transport
func (c *Client) setupTCP(ctx context.Context) error {
	req := &Request{
		Method: "SETUP",
		URI:    c.Track.Control,
		Headers: map[string]string{
			"Transport": "RTP/AVP/TCP;unicast;interleaved=0-1",
		},
	}

	res, err := c.roundTrip(ctx, req)
	if err != nil {
		return fmt.Errorf("setup failed: %w", err)
	}
	if res.StatusCode != 200 {
		return fmt.Errorf("setup rejected: %d %s", res.StatusCode, res.StatusMessage)
	}

	c.storeSession(res)

	trans := res.Header("Transport")
	for _, part := range strings.Split(trans, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "interleaved=") {
			chans := strings.TrimPrefix(part, "interleaved=")
			if dash := strings.IndexByte(chans, '-'); dash != -1 {
				chans = chans[:dash]
			}
			ch, err := strconv.Atoi(chans)
			if err == nil {
				c.rtpChannel = ch
			}
		}
	}

	return nil
}

// start stream playback
func (c *Client) Play(ctx context.Context) error {
	req := &Request{
		Method: "PLAY",
		URI:    c.cleanURL,
		Headers: map[string]string{
			"Range": "npt=0.000-",
		},
	}

	res, err := c.roundTrip(ctx, req)
	if err != nil {
		return fmt.Errorf("play failed: %w", err)
	}
	if res.StatusCode != 200 {
		return fmt.Errorf("play rejected: %d %s", res.StatusCode, res.StatusMessage)
	}
	return nil
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

		_, err = c.reader.ReadString('\n')
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
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

		n, _, err := c.udpRTP.ReadFrom(c.udpBuf)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		if n <= 0 {
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
