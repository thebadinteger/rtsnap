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
	Transcode bool
	UserAgent string
	DebugFunc func(string)
	TLSVerify bool
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

// force mjpeg decode and reencode
func WithTranscode() Option {
	return func(o *Options) {
		o.Transcode = true
	}
}

// override user agent header
func WithUserAgent(ua string) Option {
	return func(o *Options) {
		o.UserAgent = ua
	}
}

// receive raw rtsp exchange
func WithDebugFunc(fn func(string)) Option {
	return func(o *Options) {
		o.DebugFunc = fn
	}
}

// choose rtp transport
func WithTransport(t Transport) Option {
	return func(o *Options) {
		o.Transport = t
	}
}

// enable tls certificate verification for rtsps
func WithTLSVerify() Option {
	return func(o *Options) {
		o.TLSVerify = true
	}
}
