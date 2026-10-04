package tests

import (
	"bufio"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"sync"
)

type mockServer struct {
	listener net.Listener
	port     int
	mu       sync.Mutex
	closed   bool

	authMode string // none, basic, digest, digest-sha256
	username string
	password string
	realm    string
	nonce    string

	codec   string // h264, h265, mjpeg
	packets [][]byte
	noVideo bool
}

func newMockServer(s *mockServer) (*mockServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	s.listener = ln
	s.port = ln.Addr().(*net.TCPAddr).Port

	if s.realm == "" {
		s.realm = "rtsnap-test"
	}
	if s.nonce == "" {
		s.nonce = "123456789abcdef"
	}

	go s.serve()
	return s, nil
}

func (s *mockServer) URL() string {
	return fmt.Sprintf("rtsp://127.0.0.1:%d/live", s.port)
}

func (s *mockServer) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		s.listener.Close()
	}
}

func (s *mockServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *mockServer) verifyAuth(method, uri, authHeader string) bool {
	if s.authMode == "" || s.authMode == "none" {
		return true
	}

	if s.authMode == "basic" {
		expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(s.username+":"+s.password))
		return authHeader == expected
	}

	if strings.HasPrefix(authHeader, "Digest ") {
		raw := strings.TrimPrefix(authHeader, "Digest ")
		params := make(map[string]string)
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			eq := strings.IndexByte(part, '=')
			if eq > 0 {
				k := strings.TrimSpace(part[:eq])
				v := strings.Trim(strings.TrimSpace(part[eq+1:]), `"`)
				params[k] = v
			}
		}

		if params["username"] != s.username {
			return false
		}

		if s.authMode == "digest-sha256" {
			h1 := sha256.Sum256([]byte(s.username + ":" + s.realm + ":" + s.password))
			ha1 := hex.EncodeToString(h1[:])
			h2 := sha256.Sum256([]byte(method + ":" + uri))
			ha2 := hex.EncodeToString(h2[:])
			h3 := sha256.Sum256([]byte(ha1 + ":" + s.nonce + ":" + params["nc"] + ":" + params["cnonce"] + ":auth:" + ha2))
			expected := hex.EncodeToString(h3[:])
			return params["response"] == expected
		}

		// md5 digest
		h1 := md5.Sum([]byte(s.username + ":" + s.realm + ":" + s.password))
		ha1 := hex.EncodeToString(h1[:])
		h2 := md5.Sum([]byte(method + ":" + uri))
		ha2 := hex.EncodeToString(h2[:])
		if params["qop"] == "auth" {
			h3 := md5.Sum([]byte(ha1 + ":" + s.nonce + ":" + params["nc"] + ":" + params["cnonce"] + ":auth:" + ha2))
			expected := hex.EncodeToString(h3[:])
			return params["response"] == expected
		}

		h3 := md5.Sum([]byte(ha1 + ":" + s.nonce + ":" + ha2))
		expected := hex.EncodeToString(h3[:])
		return params["response"] == expected
	}

	return false
}

func (s *mockServer) handle(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	cseq := "1"

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		parts := strings.Split(strings.TrimSpace(line), " ")
		if len(parts) < 2 {
			continue
		}
		method := parts[0]
		uri := parts[1]

		authHeader := ""
		for {
			h, err := reader.ReadString('\n')
			if err != nil || strings.TrimSpace(h) == "" {
				break
			}
			if strings.HasPrefix(h, "CSeq:") {
				cseq = strings.TrimSpace(strings.TrimPrefix(h, "CSeq:"))
			}
			if strings.HasPrefix(h, "Authorization:") {
				authHeader = strings.TrimSpace(strings.TrimPrefix(h, "Authorization:"))
			}
		}

		// check auth for describe, setup, play
		if !s.verifyAuth(method, uri, authHeader) {
			var wwwAuth string
			if s.authMode == "basic" {
				wwwAuth = fmt.Sprintf(`Basic realm="%s"`, s.realm)
			} else if s.authMode == "digest-sha256" {
				wwwAuth = fmt.Sprintf(`Digest realm="%s", nonce="%s", qop="auth", algorithm=SHA-256`, s.realm, s.nonce)
			} else {
				wwwAuth = fmt.Sprintf(`Digest realm="%s", nonce="%s", qop="auth"`, s.realm, s.nonce)
			}
			resp := fmt.Sprintf("RTSP/1.0 401 Unauthorized\r\nCSeq: %s\r\nWWW-Authenticate: %s\r\n\r\n", cseq, wwwAuth)
			conn.Write([]byte(resp))
			continue
		}

		switch method {
		case "OPTIONS":
			resp := fmt.Sprintf("RTSP/1.0 200 OK\r\nCSeq: %s\r\nPublic: DESCRIBE, SETUP, TEARDOWN, PLAY\r\n\r\n", cseq)
			conn.Write([]byte(resp))

		case "DESCRIBE":
			var sdp string
			switch s.codec {
			case "h264":
				sdp = "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=Session\r\nt=0 0\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=control:track0\r\n"
			case "h265":
				sdp = "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=Session\r\nt=0 0\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 H265/90000\r\na=control:track0\r\n"
			case "mjpeg":
				sdp = "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=Session\r\nt=0 0\r\nm=video 0 RTP/AVP 26\r\na=rtpmap:26 JPEG/90000\r\na=control:track0\r\n"
			default:
				sdp = "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=Session\r\nt=0 0\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=control:track0\r\n"
			}
			resp := fmt.Sprintf("RTSP/1.0 200 OK\r\nCSeq: %s\r\nContent-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s", cseq, len(sdp), sdp)
			conn.Write([]byte(resp))

		case "SETUP":
			resp := fmt.Sprintf("RTSP/1.0 200 OK\r\nCSeq: %s\r\nSession: test1234\r\nTransport: RTP/AVP/TCP;unicast;interleaved=0-1\r\n\r\n", cseq)
			conn.Write([]byte(resp))

		case "PLAY":
			resp := fmt.Sprintf("RTSP/1.0 200 OK\r\nCSeq: %s\r\nSession: test1234\r\n\r\n", cseq)
			conn.Write([]byte(resp))

			if s.noVideo {
				continue
			}

			// write all configured packets
			for _, pkt := range s.packets {
				length := len(pkt)
				frameHdr := []byte{0x24, 0x00, byte(length >> 8), byte(length)}
				conn.Write(frameHdr)
				conn.Write(pkt)
			}

		case "TEARDOWN":
			resp := fmt.Sprintf("RTSP/1.0 200 OK\r\nCSeq: %s\r\n\r\n", cseq)
			conn.Write([]byte(resp))
			return
		}
	}
}
