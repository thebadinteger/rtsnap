package rtsp

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

const (
	rtspProto       = "RTSP/1.0"
	magicByte       = 0x24
	maxResponseBody = 16 << 20 // 16mb cap on response body
	maxHeaderLines  = 128
	maxHeaderLine   = 8 << 10 // single line cap
	maxFrameSize    = 4 + 0xffff
)

// a server request arrived where a response was expected
var errServerRequest = errors.New("rtsp request from server")

type Request struct {
	Method  string
	URI     string
	Headers map[string]string
	Body    []byte
}

// serialize request to bytes
func (r *Request) Bytes() []byte {
	b := make([]byte, 0, 256+len(r.Body))
	b = append(b, r.Method...)
	b = append(b, ' ')
	b = append(b, r.URI...)
	b = append(b, ' ')
	b = append(b, rtspProto...)
	b = append(b, '\r', '\n')

	// cseq goes first, some embedded servers only look at the leading headers
	if v, ok := r.Headers["CSeq"]; ok {
		b = appendHeader(b, "CSeq", v)
	}
	keys := make([]string, 0, len(r.Headers))
	for k := range r.Headers {
		if k != "CSeq" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		b = appendHeader(b, k, r.Headers[k])
	}
	if len(r.Body) > 0 {
		b = appendHeader(b, "Content-Length", strconv.Itoa(len(r.Body)))
	}
	b = append(b, '\r', '\n')
	return append(b, r.Body...)
}

func appendHeader(b []byte, key, value string) []byte {
	b = append(b, key...)
	b = append(b, ':', ' ')
	b = append(b, value...)
	return append(b, '\r', '\n')
}

// serialize request to wire
func (r *Request) Write(w io.Writer) error {
	_, err := w.Write(r.Bytes())
	return err
}

type Response struct {
	StatusCode    int
	StatusMessage string
	// first value of each header, kept for compatibility
	Headers map[string]string
	// every value of each header in arrival order
	HeaderValues map[string][]string
	Body         []byte
}

// first value of a header
func (r *Response) Header(key string) string {
	values := r.Values(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// every value of a header
func (r *Response) Values(key string) []string {
	if v, ok := r.HeaderValues[key]; ok {
		return v
	}
	for k, v := range r.HeaderValues {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	for k, v := range r.Headers {
		if strings.EqualFold(k, key) {
			return []string{v}
		}
	}
	return nil
}

// add header value preserving duplicates and original case
func addHeaderValue(values map[string][]string, key, value string) {
	if _, ok := values[key]; !ok {
		for k := range values {
			if strings.EqualFold(k, key) {
				key = k
				break
			}
		}
	}
	values[key] = append(values[key], value)
}

// read one crlf line with a hard size cap
func readLine(br *bufio.Reader) (string, error) {
	line, err := br.ReadSlice('\n')
	if err == bufio.ErrBufferFull {
		return "", fmt.Errorf("response line exceeds %d bytes", maxHeaderLine)
	}
	if err != nil {
		return "", err
	}
	if len(line) > maxHeaderLine {
		return "", fmt.Errorf("response line exceeds %d bytes", maxHeaderLine)
	}
	return string(line), nil
}

// parse status line, headers and body
func readResponse(br *bufio.Reader) (*Response, string, error) {
	var raw strings.Builder
	line, err := readLine(br)
	if err != nil {
		return nil, "", err
	}
	raw.WriteString(line)
	line = strings.TrimRight(line, "\r\n")

	// a request pushed by the server is consumed whole and reported
	request := !strings.HasPrefix(line, "RTSP/") && strings.HasSuffix(line, " "+rtspProto)

	code := 0
	msg := ""
	if !request {
		parts := strings.SplitN(line, " ", 3)
		if len(parts) < 2 {
			return nil, "", fmt.Errorf("malformed status line: %q", line)
		}
		if !strings.HasPrefix(parts[0], "RTSP/") {
			return nil, "", fmt.Errorf("not an rtsp response: %q", line)
		}
		code, err = strconv.Atoi(parts[1])
		if err != nil {
			return nil, "", fmt.Errorf("invalid status code: %s", parts[1])
		}
		if len(parts) == 3 {
			msg = parts[2]
		}
	}

	values := make(map[string][]string)
	for n := 0; ; n++ {
		if n > maxHeaderLines {
			return nil, "", fmt.Errorf("too many response headers")
		}
		hline, err := readLine(br)
		if err != nil {
			return nil, "", err
		}
		raw.WriteString(hline)
		hline = strings.TrimRight(hline, "\r\n")
		if hline == "" {
			break
		}
		colon := strings.IndexByte(hline, ':')
		if colon > 0 {
			k := strings.TrimSpace(hline[:colon])
			v := strings.TrimSpace(hline[colon+1:])
			addHeaderValue(values, k, v)
		}
	}

	headers := make(map[string]string, len(values))
	for k, v := range values {
		headers[k] = v[0]
	}

	res := &Response{
		StatusCode:    code,
		StatusMessage: msg,
		Headers:       headers,
		HeaderValues:  values,
	}

	clValues := res.Values("Content-Length")
	if len(clValues) > 1 {
		return nil, "", fmt.Errorf("multiple content-length headers")
	}
	if len(clValues) == 1 {
		cl, err := strconv.Atoi(strings.TrimSpace(clValues[0]))
		if err != nil {
			return nil, "", fmt.Errorf("invalid content-length: %s", clValues[0])
		}
		if cl < 0 {
			return nil, "", fmt.Errorf("negative content-length: %d", cl)
		}
		if cl > maxResponseBody {
			return nil, "", fmt.Errorf("response body too large: %d bytes", cl)
		}
		if cl > 0 {
			body := make([]byte, cl)
			_, err = io.ReadFull(br, body)
			if err != nil {
				return nil, "", err
			}
			res.Body = body
			raw.Write(body)
		}
	}

	if request {
		return nil, raw.String(), errServerRequest
	}
	return res, raw.String(), nil
}

type Frame struct {
	Channel int
	Payload []byte
}

// peek one interleaved frame, the payload stays valid until the next read
func peekFrame(br *bufio.Reader) (channel int, payload []byte, size int, err error) {
	hdr, err := br.Peek(4)
	if err != nil {
		return 0, nil, 0, err
	}
	if hdr[0] != magicByte {
		return 0, nil, 0, fmt.Errorf("invalid interleaved frame magic: 0x%02x", hdr[0])
	}
	channel = int(hdr[1])
	size = 4 + (int(hdr[2])<<8 | int(hdr[3]))
	if size > br.Size() {
		// reader smaller than a full frame, fall back to a copy
		payload = make([]byte, size-4)
		if _, err := br.Discard(4); err != nil {
			return 0, nil, 0, err
		}
		if _, err := io.ReadFull(br, payload); err != nil {
			return 0, nil, 0, err
		}
		return channel, payload, 0, nil
	}
	b, err := br.Peek(size)
	if err != nil {
		return 0, nil, 0, err
	}
	return channel, b[4:], size, nil
}

// drop bytes up to the next frame magic or rtsp message
func resync(br *bufio.Reader) error {
	for {
		b, err := br.Peek(1)
		if err != nil {
			return err
		}
		if b[0] == magicByte {
			return nil
		}
		if b[0] == 'R' {
			head, err := br.Peek(5)
			if err != nil {
				return err
			}
			if string(head) == "RTSP/" {
				return nil
			}
		}
		buf, _ := br.Peek(br.Buffered())
		n := len(buf)
		if i := bytes.IndexByte(buf[1:], magicByte); i >= 0 {
			n = i + 1
		}
		if i := bytes.IndexByte(buf[1:n], 'R'); i >= 0 {
			n = i + 1
		}
		if _, err := br.Discard(n); err != nil {
			return err
		}
	}
}
