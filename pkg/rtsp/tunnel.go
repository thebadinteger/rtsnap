package rtsp

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
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
	encoded := base64.StdEncoding.EncodeToString(p)
	if _, err := c.writeConn.Write([]byte(encoded)); err != nil {
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

// open the get and post channels of an http tunnel
func dialHTTPTunnel(ctx context.Context, addr string, secure bool, cfg *tls.Config, path string) (net.Conn, error) {
	readConn, err := dialSocket(ctx, addr, secure, cfg)
	if err != nil {
		return nil, err
	}
	if d, ok := ctx.Deadline(); ok {
		_ = readConn.SetDeadline(d)
	}
	stopRead := context.AfterFunc(ctx, func() { _ = readConn.Close() })
	defer stopRead()

	cookie := tunnelCookie()
	getReq := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"X-Sessioncookie: " + cookie + "\r\n" +
		"Accept: application/x-rtsp-tunnelled\r\n" +
		"Cache-Control: no-cache\r\n" +
		"Pragma: no-cache\r\n" +
		"\r\n"
	if _, err := readConn.Write([]byte(getReq)); err != nil {
		_ = readConn.Close()
		return nil, fmt.Errorf("tunnel get request: %w", err)
	}

	reader := bufio.NewReaderSize(readConn, tunnelReadSize)
	code, err := readHTTPHead(reader)
	if err != nil {
		_ = readConn.Close()
		return nil, fmt.Errorf("tunnel get response: %w", err)
	}
	if code != 200 {
		_ = readConn.Close()
		return nil, fmt.Errorf("tunnel get rejected: %d", code)
	}

	writeConn, err := dialSocket(ctx, addr, secure, cfg)
	if err != nil {
		_ = readConn.Close()
		return nil, err
	}
	if d, ok := ctx.Deadline(); ok {
		_ = writeConn.SetDeadline(d)
	}
	stopWrite := context.AfterFunc(ctx, func() { _ = writeConn.Close() })
	defer stopWrite()

	postReq := "POST " + path + " HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"X-Sessioncookie: " + cookie + "\r\n" +
		"Content-Type: application/x-rtsp-tunnelled\r\n" +
		fmt.Sprintf("Content-Length: %d\r\n", tunnelContentLength) +
		"\r\n"
	if _, err := writeConn.Write([]byte(postReq)); err != nil {
		_ = readConn.Close()
		_ = writeConn.Close()
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

// read http status line and headers, return the status code
func readHTTPHead(br *bufio.Reader) (int, error) {
	line, err := readLine(br)
	if err != nil {
		return 0, err
	}
	parts := strings.SplitN(strings.TrimRight(line, "\r\n"), " ", 3)
	if len(parts) < 2 || !strings.HasPrefix(parts[0], "HTTP/") {
		return 0, fmt.Errorf("not an http response: %s", strings.TrimSpace(line))
	}
	var code int
	if _, err := fmt.Sscanf(parts[1], "%d", &code); err != nil {
		return 0, fmt.Errorf("invalid http status code: %s", parts[1])
	}
	for n := 0; ; n++ {
		if n > maxHeaderLines {
			return 0, fmt.Errorf("too many http response headers")
		}
		hline, err := readLine(br)
		if err != nil {
			return 0, err
		}
		if strings.TrimSpace(hline) == "" {
			return code, nil
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
	dec []byte
}

func newBase64Reader(src *bufio.Reader) io.Reader {
	return &base64Reader{src: src}
}

func (r *base64Reader) Read(p []byte) (int, error) {
	for len(r.dec) == 0 {
		b, err := r.src.ReadByte()
		if err != nil {
			return 0, err
		}
		if b == '\r' || b == '\n' || b == ' ' || b == '\t' {
			continue
		}
		r.enc = append(r.enc, b)
		if len(r.enc) < 4 {
			continue
		}
		out, err := base64.StdEncoding.DecodeString(string(r.enc[:4]))
		if err != nil {
			return 0, err
		}
		r.enc = r.enc[:0]
		r.dec = append(r.dec, out...)
	}
	n := copy(p, r.dec)
	r.dec = r.dec[n:]
	return n, nil
}
