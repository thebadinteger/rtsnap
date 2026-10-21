package rtsp

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// sdp carries no media tracks
	ErrNoMedia = errors.New("no video tracks found in sdp")
)

// server answered with a non success status
type StatusError struct {
	Method  string
	Code    int
	Status  string
	Headers map[string][]string
	Body    []byte
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s rejected: %d %s", e.Method, e.Code, e.Status)
}

// first value of a response header
func (e *StatusError) Header(key string) string {
	values := e.Values(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// every value of a response header
func (e *StatusError) Values(key string) []string {
	for k, v := range e.Headers {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return nil
}

func newStatusError(method string, res *Response) *StatusError {
	return &StatusError{
		Method:  method,
		Code:    res.StatusCode,
		Status:  res.StatusMessage,
		Headers: res.HeaderValues,
		Body:    res.Body,
	}
}
