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
	rtpChannel int
}

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

func (c *Client) Describe(ctx context.Context) error {
	req := &Request{
		Method: "DESCRIBE",
		URI:    c.cleanURL,
		Headers: map[string]string{
			"Accept": "application/sdp",
		},
	}

	res, err := c.send(ctx, req)
	if err != nil {
		return fmt.Errorf("describe failed: %w", err)
	}

	if res.StatusCode == 401 {
		wwwAuth := res.Header("WWW-Authenticate")
		if wwwAuth == "" {
			return fmt.Errorf("unauthorized without www-authenticate header")
		}
		c.auth = NewAuth(wwwAuth, c.user, c.pass)
		if c.auth == nil {
			return fmt.Errorf("unsupported auth header: %s", wwwAuth)
		}

		// retry with authorization
		res, err = c.send(ctx, req)
		if err != nil {
			return fmt.Errorf("describe retry failed: %w", err)
		}
		if res.StatusCode != 200 {
			return fmt.Errorf("describe rejected: %d %s", res.StatusCode, res.StatusMessage)
		}
	} else if res.StatusCode != 200 {
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

func (c *Client) Setup(ctx context.Context) error {
	if c.Track == nil {
		return fmt.Errorf("no track available for setup")
	}

	req := &Request{
		Method: "SETUP",
		URI:    c.Track.Control,
		Headers: map[string]string{
			"Transport": "RTP/AVP/TCP;unicast;interleaved=0-1",
		},
	}

	res, err := c.send(ctx, req)
	if err != nil {
		return fmt.Errorf("setup failed: %w", err)
	}
	if res.StatusCode != 200 {
		return fmt.Errorf("setup rejected: %d %s", res.StatusCode, res.StatusMessage)
	}

	sess := res.Header("Session")
	if sess != "" {
		if semi := strings.IndexByte(sess, ';'); semi != -1 {
			sess = sess[:semi]
		}
		c.session = strings.TrimSpace(sess)
	}

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

func (c *Client) Play(ctx context.Context) error {
	req := &Request{
		Method: "PLAY",
		URI:    c.cleanURL,
		Headers: map[string]string{
			"Range": "npt=0.000-",
		},
	}

	res, err := c.send(ctx, req)
	if err != nil {
		return fmt.Errorf("play failed: %w", err)
	}
	if res.StatusCode != 200 {
		return fmt.Errorf("play rejected: %d %s", res.StatusCode, res.StatusMessage)
	}
	return nil
}

func (c *Client) ReadFrame(ctx context.Context) (*Frame, error) {
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

func (c *Client) RTPChannel() int {
	return c.rtpChannel
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) SelectTrack(codec string) (*MediaTrack, error) {
	t, err := SelectTrack(c.Tracks, codec)
	if err != nil {
		return nil, err
	}
	c.Track = t
	return t, nil
}

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
