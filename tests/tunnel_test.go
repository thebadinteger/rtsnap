package tests

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/thebadinteger/rtsnap"
	"github.com/thebadinteger/rtsnap/pkg/h264"
)

// rtsp requests encoded base64 over the post channel
type base64RequestReader struct {
	src *bufio.Reader
	enc []byte
	dec []byte
}

func (r *base64RequestReader) Read(p []byte) (int, error) {
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
		out, err := base64.StdEncoding.DecodeString(string(r.enc))
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

type tunnelServerConn struct {
	r        io.Reader
	w        io.Writer
	conn     net.Conn
	base64Wr bool
}

func (c *tunnelServerConn) Read(p []byte) (int, error) { return c.r.Read(p) }

func (c *tunnelServerConn) Write(p []byte) (int, error) {
	if c.base64Wr {
		encoded := base64.StdEncoding.EncodeToString(p)
		if _, err := c.w.Write([]byte(encoded)); err != nil {
			return 0, err
		}
		return len(p), nil
	}
	return c.w.Write(p)
}

func (c *tunnelServerConn) Close() error                       { return c.conn.Close() }
func (c *tunnelServerConn) LocalAddr() net.Addr                { return c.conn.LocalAddr() }
func (c *tunnelServerConn) RemoteAddr() net.Addr               { return c.conn.RemoteAddr() }
func (c *tunnelServerConn) SetDeadline(t time.Time) error      { return c.conn.SetDeadline(t) }
func (c *tunnelServerConn) SetReadDeadline(t time.Time) error  { return c.conn.SetReadDeadline(t) }
func (c *tunnelServerConn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }

// serve one http tunnel pair and run the rtsp mock over it
func serveTunnel(s *mockServer, ln net.Listener, base64Responses bool) {
	getConn, err := ln.Accept()
	if err != nil {
		return
	}
	defer getConn.Close()

	getReq, err := http.ReadRequest(bufio.NewReader(getConn))
	if err != nil || getReq.Method != "GET" {
		return
	}
	cookie := getReq.Header.Get("X-Sessioncookie")
	if cookie == "" {
		return
	}
	if _, err := getConn.Write([]byte("HTTP/1.1 200 OK\r\n" +
		"Content-Type: application/x-rtsp-tunnelled\r\n" +
		"Cache-Control: no-cache\r\n" +
		"Connection: close\r\n\r\n")); err != nil {
		return
	}

	postConn, err := ln.Accept()
	if err != nil {
		return
	}
	defer postConn.Close()

	postReq, err := http.ReadRequest(bufio.NewReader(postConn))
	if err != nil || postReq.Method != "POST" {
		return
	}
	if postReq.Header.Get("X-Sessioncookie") != cookie {
		return
	}

	conn := &tunnelServerConn{
		r:        &base64RequestReader{src: bufio.NewReader(postReq.Body)},
		w:        getConn,
		conn:     getConn,
		base64Wr: base64Responses,
	}
	s.handle(conn)
}

func newTunnelMock(t *testing.T, s *mockServer, ln net.Listener, base64Responses bool) {
	t.Helper()
	go serveTunnel(s, ln, base64Responses)
}

func h264Packets(t *testing.T) [][]byte {
	t.Helper()
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}
	return makeRTPPacketsH264(h264.ExtractNalusFromByteStream(data))
}

func TestSnapshotTunnelRTSP(t *testing.T) {
	server, err := newMockServer(&mockServer{codec: "h264", packets: h264Packets(t)})
	if err != nil {
		t.Fatalf("mock server: %v", err)
	}
	defer server.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("tunnel listen: %v", err)
	}
	defer ln.Close()
	newTunnelMock(t, server, ln, false)

	url := fmt.Sprintf("rtsp://%s/live", ln.Addr())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, url, rtsnap.WithTunnel())
	if err != nil {
		t.Fatalf("tunnel snapshot: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestSnapshotTunnelAutoHTTP(t *testing.T) {
	server, err := newMockServer(&mockServer{codec: "h264", packets: h264Packets(t)})
	if err != nil {
		t.Fatalf("mock server: %v", err)
	}
	defer server.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("tunnel listen: %v", err)
	}
	defer ln.Close()
	newTunnelMock(t, server, ln, false)

	url := fmt.Sprintf("http://%s/live", ln.Addr())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, url)
	if err != nil {
		t.Fatalf("http tunnel snapshot: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestSnapshotTunnelBase64Response(t *testing.T) {
	server, err := newMockServer(&mockServer{codec: "h264", packets: h264Packets(t)})
	if err != nil {
		t.Fatalf("mock server: %v", err)
	}
	defer server.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("tunnel listen: %v", err)
	}
	defer ln.Close()
	newTunnelMock(t, server, ln, true)

	url := fmt.Sprintf("rtsp://%s/live", ln.Addr())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, url, rtsnap.WithTunnel())
	if err != nil {
		t.Fatalf("base64 tunnel snapshot: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestSnapshotTunnelHTTPS(t *testing.T) {
	server, err := newMockServer(&mockServer{codec: "h264", packets: h264Packets(t)})
	if err != nil {
		t.Fatalf("mock server: %v", err)
	}
	defer server.Close()

	cert := selfSignedCert(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatalf("tls listen: %v", err)
	}
	defer ln.Close()
	newTunnelMock(t, server, ln, false)

	url := fmt.Sprintf("https://%s/live", ln.Addr())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, url, rtsnap.WithTLSConfig(&tls.Config{InsecureSkipVerify: true}))
	if err != nil {
		t.Fatalf("https tunnel snapshot: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}
