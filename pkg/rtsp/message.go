package rtsp

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	rtspProto = "RTSP/1.0"
	magicByte = 0x24
)

// request represents an rtsp request
type Request struct {
	Method  string
	URI     string
	Headers map[string]string
	Body    []byte
}

func (r *Request) Write(w io.Writer) error {
	var b bytes.Buffer
	b.WriteString(fmt.Sprintf("%s %s %s\r\n", r.Method, r.URI, rtspProto))
	for k, v := range r.Headers {
		b.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	if len(r.Body) > 0 {
		b.WriteString(fmt.Sprintf("Content-Length: %d\r\n", len(r.Body)))
	}
	b.WriteString("\r\n")
	if len(r.Body) > 0 {
		b.Write(r.Body)
	}
	_, err := w.Write(b.Bytes())
	return err
}

// response represents an rtsp response
type Response struct {
	StatusCode    int
	StatusMessage string
	Headers       map[string]string
	Body          []byte
}

func (r *Response) Header(key string) string {
	if r.Headers == nil {
		return ""
	}
	for k, v := range r.Headers {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

func readResponse(br *bufio.Reader) (*Response, error) {
	line, err := br.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 2 {
		return nil, fmt.Errorf("malformed status line: %s", line)
	}
	code, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid status code: %s", parts[1])
	}
	msg := ""
	if len(parts) == 3 {
		msg = parts[2]
	}

	headers := make(map[string]string)
	for {
		hline, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		hline = strings.TrimRight(hline, "\r\n")
		if hline == "" {
			break
		}
		colon := strings.IndexByte(hline, ':')
		if colon > 0 {
			k := strings.TrimSpace(hline[:colon])
			v := strings.TrimSpace(hline[colon+1:])
			headers[k] = v
		}
	}

	res := &Response{
		StatusCode:    code,
		StatusMessage: msg,
		Headers:       headers,
	}

	clStr := res.Header("Content-Length")
	if clStr != "" {
		cl, err := strconv.Atoi(clStr)
		if err == nil && cl > 0 {
			body := make([]byte, cl)
			_, err = io.ReadFull(br, body)
			if err != nil {
				return nil, err
			}
			res.Body = body
		}
	}

	return res, nil
}

// frame represents an interleaved binary data frame
type Frame struct {
	Channel int
	Payload []byte
}

func readFrame(br *bufio.Reader) (*Frame, error) {
	var hdr [4]byte
	_, err := io.ReadFull(br, hdr[:])
	if err != nil {
		return nil, err
	}
	if hdr[0] != magicByte {
		return nil, fmt.Errorf("invalid interleaved frame magic: 0x%02x", hdr[0])
	}
	length := int(uint16(hdr[2])<<8 | uint16(hdr[3]))
	payload := make([]byte, length)
	_, err = io.ReadFull(br, payload)
	if err != nil {
		return nil, err
	}
	return &Frame{
		Channel: int(hdr[1]),
		Payload: payload,
	}, nil
}
