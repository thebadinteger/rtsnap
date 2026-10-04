package tests

import (
	"bufio"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
)

type mockServer struct {
	listener net.Listener
	port     int
	mu       sync.Mutex
	closed   bool

	authMode string
	username string
	password string
	realm    string
	nonce    string
	authLate bool

	codec   string
	packets [][]byte
	noVideo bool
	udp     bool
	drop    map[int]bool

	udpConn   net.PacketConn
	udpTarget *net.UDPAddr
}

func udpClientPort(trans string) (string, int) {
	i := strings.Index(trans, "client_port=")
	if i == -1 {
		return "", 0
	}
	rest := trans[i+len("client_port="):]
	if end := strings.IndexAny(rest, "; \r\n"); end != -1 {
		rest = rest[:end]
	}
	first := rest
	if dash := strings.IndexByte(rest, '-'); dash != -1 {
		first = rest[:dash]
	}
	p, err := strconv.Atoi(strings.TrimSpace(first))
	if err != nil || p <= 0 {
		return "", 0
	}
	return strings.TrimSpace(rest), p
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
		if s.udpConn != nil {
			_ = s.udpConn.Close()
		}
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
		transportHeader := ""
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
			if strings.HasPrefix(h, "Transport:") {
				transportHeader = strings.TrimSpace(strings.TrimPrefix(h, "Transport:"))
			}
		}

		if !(s.authLate && method == "DESCRIBE") && !s.verifyAuth(method, uri, authHeader) {
			var wwwAuth string
			if s.authMode == "basic" {
				wwwAuth = fmt.Sprintf(`Basic realm="%s"`, s.realm)
			} else if s.authMode == "digest-sha256" {
				wwwAuth = fmt.Sprintf(`Digest realm="%s", nonce="%s", qop="auth", algorithm=SHA-256`, s.realm, s.nonce)
			} else {
				wwwAuth = fmt.Sprintf(`Digest realm="%s", nonce="%s", qop="auth"`, s.realm, s.nonce)
			}
			resp := fmt.Sprintf("RTSP/1.0 401 Unauthorized\r\nCSeq: %s\r\nWWW-Authenticate: %s\r\n\r\n", cseq, wwwAuth)
			_, _ = conn.Write([]byte(resp))
			continue
		}

		switch method {
		case "OPTIONS":
			resp := fmt.Sprintf("RTSP/1.0 200 OK\r\nCSeq: %s\r\nPublic: DESCRIBE, SETUP, TEARDOWN, PLAY\r\n\r\n", cseq)
			_, _ = conn.Write([]byte(resp))

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
			_, _ = conn.Write([]byte(resp))

		case "SETUP":
			if s.udp {
				if pair, clientPort := udpClientPort(transportHeader); clientPort > 0 {
					host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
					target, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, strconv.Itoa(clientPort)))
					if err == nil {
						udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
						if err == nil {
							serverPort := udpConn.LocalAddr().(*net.UDPAddr).Port
							s.mu.Lock()
							s.udpConn = udpConn
							s.udpTarget = target
							s.mu.Unlock()
							resp := fmt.Sprintf("RTSP/1.0 200 OK\r\nCSeq: %s\r\nSession: test1234\r\nTransport: RTP/AVP;unicast;client_port=%s;server_port=%d-%d;ssrc=11223344\r\n\r\n", cseq, pair, serverPort, serverPort)
							_, _ = conn.Write([]byte(resp))
							continue
						}
					}
				}
			}
			resp := fmt.Sprintf("RTSP/1.0 200 OK\r\nCSeq: %s\r\nSession: test1234\r\nTransport: RTP/AVP/TCP;unicast;interleaved=0-1\r\n\r\n", cseq)
			_, _ = conn.Write([]byte(resp))

		case "PLAY":
			resp := fmt.Sprintf("RTSP/1.0 200 OK\r\nCSeq: %s\r\nSession: test1234\r\n\r\n", cseq)
			_, _ = conn.Write([]byte(resp))

			if s.noVideo {
				continue
			}

			s.mu.Lock()
			udpConn, udpTarget := s.udpConn, s.udpTarget
			s.mu.Unlock()

			for i, pkt := range s.packets {
				if s.drop[i] {
					continue
				}
				if udpConn != nil && udpTarget != nil {
					_, _ = udpConn.WriteTo(pkt, udpTarget)
					continue
				}
				length := len(pkt)
				frameHdr := []byte{0x24, 0x00, byte(length >> 8), byte(length)}
				_, _ = conn.Write(frameHdr)
				_, _ = conn.Write(pkt)
			}

		case "TEARDOWN":
			resp := fmt.Sprintf("RTSP/1.0 200 OK\r\nCSeq: %s\r\n\r\n", cseq)
			_, _ = conn.Write([]byte(resp))
			return
		}
	}
}
