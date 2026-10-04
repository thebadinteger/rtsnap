package rtsnap

import (
	"time"

	"github.com/thebadinteger/rtsnap/pkg/rtsp"
)

// video codec selector
type Codec string

const (
	CodecAuto  Codec = ""      // pick best available codec
	CodecH264  Codec = "h264"  // force h264 stream
	CodecH265  Codec = "h265"  // force h265 stream
	CodecMJPEG Codec = "mjpeg" // force mjpeg stream
)

type Transport = rtsp.Transport

const (
	TransportTCP  = rtsp.TransportTCP  // tcp interleaved only
	TransportUDP  = rtsp.TransportUDP  // udp unicast only
	TransportAuto = rtsp.TransportAuto // try udp then fall back to tcp
)

// snapshot settings
type Options struct {
	Username  string
	Password  string
	Timeout   time.Duration
	Codec     Codec
	Transport Transport
	Fast      bool
}

type Option func(*Options)

// set stream credentials
func WithAuth(username, password string) Option {
	return func(o *Options) {
		o.Username = username
		o.Password = password
	}
}

// bound total operation time
func WithTimeout(d time.Duration) Option {
	return func(o *Options) {
		o.Timeout = d
	}
}

// choose video codec
func WithCodec(c Codec) Option {
	return func(o *Options) {
		o.Codec = c
	}
}

// skip loop filters for speed
func WithFast() Option {
	return func(o *Options) {
		o.Fast = true
	}
}

// choose rtp transport
func WithTransport(t Transport) Option {
	return func(o *Options) {
		o.Transport = t
	}
}
