package rtsp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	tunnelContentLength = 30000
	tunnelReadSize      = 8 << 10
)

// rtsp over http tunnel connection
type tunnelConn struct {
	readConn  net.Conn
	writeConn net.Conn
	reader    *bufio.Reader
	body      io.Reader
	sniffed   bool
	enc       []byte
}

func (c *tunnelConn) Read(p []byte) (int, error) {
	if !c.sniffed {
		c.sniffed = true
		head, err := c.reader.Peek(5)
		if err != nil && len(head) == 0 {
			return 0, err
		}
		if len(head) > 0 && head[0] == magicByte {
			c.body = c.reader
		} else if string(head) == "RTSP/" {
			c.body = c.reader
		} else {
			c.body = newBase64Reader(c.reader)
		}
	}
	return c.body.Read(p)
}

func (c *tunnelConn) Write(p []byte) (int, error) {
	c.enc = base64.StdEncoding.AppendEncode(c.enc[:0], p)
	if _, err := c.writeConn.Write(c.enc); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *tunnelConn) Close() error {
	err := c.readConn.Close()
	if err := c.writeConn.Close(); err != nil {
		return err
	}
	return err
}

func (c *tunnelConn) LocalAddr() net.Addr {
	return c.readConn.LocalAddr()
}

func (c *tunnelConn) RemoteAddr() net.Addr {
	return c.readConn.RemoteAddr()
}

func (c *tunnelConn) SetDeadline(t time.Time) error {
	_ = c.readConn.SetDeadline(t)
	return c.writeConn.SetDeadline(t)
}

func (c *tunnelConn) SetReadDeadline(t time.Time) error {
	return c.readConn.SetReadDeadline(t)
}

func (c *tunnelConn) SetWriteDeadline(t time.Time) error {
	return c.writeConn.SetWriteDeadline(t)
}

// open one tunnel channel and send its http request head
func tunnelRequest(ctx context.Context, addr string, secure bool, cfg *tls.Config, head string) (net.Conn, error) {
	conn, err := dialSocket(ctx, addr, secure, cfg)
	if err != nil {
		return nil, err
	}
	if d, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(d)
	}
	if _, err := conn.Write([]byte(head)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// open the get and post channels of an http tunnel
func dialHTTPTunnel(ctx context.Context, addr string, secure bool, cfg *tls.Config, path, user, pass string) (net.Conn, error) {
	cookie := tunnelCookie()
	common := " " + path + " HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"X-Sessioncookie: " + cookie + "\r\n"

	var auth *Auth
	authLine := func(method string) string {
		if auth == nil {
			return ""
		}
		return "Authorization: " + auth.Generate(method, path) + "\r\n"
	}

	var readConn net.Conn
	var reader *bufio.Reader
	for try := 0; ; try++ {
		conn, err := tunnelRequest(ctx, addr, secure, cfg, "GET"+common+
			"Accept: application/x-rtsp-tunnelled\r\n"+
			"Cache-Control: no-cache\r\n"+
			"Pragma: no-cache\r\n"+
			authLine("GET")+
			"\r\n")
		if err != nil {
			return nil, fmt.Errorf("tunnel get request: %w", err)
		}
		stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
		br := bufio.NewReaderSize(conn, tunnelReadSize)
		code, challenges, err := readHTTPHead(br)
		stop()
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("tunnel get response: %w", err)
		}
		if code == 200 {
			readConn, reader = conn, br
			break
		}
		_ = conn.Close()

		// some cameras guard the tunnel itself with http auth
		if code == 401 && try == 0 && (user != "" || pass != "") {
			if auth = NewAuth(pickAuthChallenge(challenges), user, pass); auth != nil {
				continue
			}
		}
		return nil, fmt.Errorf("tunnel get rejected: %d", code)
	}

	writeConn, err := tunnelRequest(ctx, addr, secure, cfg, "POST"+common+
		"Content-Type: application/x-rtsp-tunnelled\r\n"+
		"Content-Length: "+strconv.Itoa(tunnelContentLength)+"\r\n"+
		authLine("POST")+
		"\r\n")
	if err != nil {
		_ = readConn.Close()
		return nil, fmt.Errorf("tunnel post request: %w", err)
	}

	_ = readConn.SetDeadline(time.Time{})
	_ = writeConn.SetDeadline(time.Time{})

	return &tunnelConn{
		readConn:  readConn,
		writeConn: writeConn,
		reader:    reader,
	}, nil
}

// read http status line and headers, return the status code and auth challenges
func readHTTPHead(br *bufio.Reader) (int, []string, error) {
	line, err := readLine(br)
	if err != nil {
		return 0, nil, err
	}
	parts := strings.SplitN(strings.TrimRight(line, "\r\n"), " ", 3)
	if len(parts) < 2 || !strings.HasPrefix(parts[0], "HTTP/") {
		return 0, nil, fmt.Errorf("not an http response: %q", strings.TrimSpace(line))
	}
	code, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, nil, fmt.Errorf("invalid http status code: %s", parts[1])
	}
	var challenges []string
	for n := 0; ; n++ {
		if n > maxHeaderLines {
			return 0, nil, fmt.Errorf("too many http response headers")
		}
		hline, err := readLine(br)
		if err != nil {
			return 0, nil, err
		}
		if strings.TrimSpace(hline) == "" {
			return code, challenges, nil
		}
		if k, v, ok := strings.Cut(hline, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "WWW-Authenticate") {
			challenges = append(challenges, strings.TrimSpace(v))
		}
	}
}

func tunnelCookie() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// streaming base64 decoder that tolerates line breaks and padding
type base64Reader struct {
	src *bufio.Reader
	enc []byte
	buf []byte
	dec []byte
}

func newBase64Reader(src *bufio.Reader) io.Reader {
	return &base64Reader{src: src}
}

func (r *base64Reader) Read(p []byte) (int, error) {
	for len(r.dec) == 0 {
		if _, err := r.src.Peek(1); err != nil {
			return 0, err
		}
		chunk, _ := r.src.Peek(r.src.Buffered())
		for _, b := range chunk {
			if b != '\r' && b != '\n' && b != ' ' && b != '\t' {
				r.enc = append(r.enc, b)
			}
		}
		_, _ = r.src.Discard(len(chunk))

		whole := len(r.enc) &^ 3
		if need := base64.StdEncoding.DecodedLen(whole); cap(r.buf) < need {
			r.buf = make([]byte, need)
		}
		out := 0
		for enc := r.enc[:whole]; len(enc) > 0; {
			// every write of the peer is padded on its own
			end := len(enc)
			if pad := bytes.IndexByte(enc, '='); pad >= 0 {
				end = pad&^3 + 4
			}
			n, err := base64.StdEncoding.Decode(r.buf[out:cap(r.buf)], enc[:end])
			if err != nil {
				return 0, err
			}
			out += n
			enc = enc[end:]
		}
		r.dec = r.buf[:out]
		r.enc = r.enc[:copy(r.enc, r.enc[whole:])]
	}
	n := copy(p, r.dec)
	r.dec = r.dec[n:]
	return n, nil
}
