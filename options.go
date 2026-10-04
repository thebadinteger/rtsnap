package rtsnap

import (
	"time"

	"github.com/thebadinteger/rtsnap/pkg/rtsp"
)

type Codec string

const (
	CodecAuto  Codec = ""
	CodecH264  Codec = "h264"
	CodecH265  Codec = "h265"
	CodecMJPEG Codec = "mjpeg"
)

type Transport = rtsp.Transport

const (
	TransportTCP  = rtsp.TransportTCP
	TransportUDP  = rtsp.TransportUDP
	TransportAuto = rtsp.TransportAuto
)

type Options struct {
	Username  string
	Password  string
	Timeout   time.Duration
	Codec     Codec
	Transport Transport
	Fast      bool
}

type Option func(*Options)

func WithAuth(username, password string) Option {
	return func(o *Options) {
		o.Username = username
		o.Password = password
	}
}

func WithTimeout(d time.Duration) Option {
	return func(o *Options) {
		o.Timeout = d
	}
}

func WithCodec(c Codec) Option {
	return func(o *Options) {
		o.Codec = c
	}
}

func WithFast() Option {
	return func(o *Options) {
		o.Fast = true
	}
}

func WithTransport(t Transport) Option {
	return func(o *Options) {
		o.Transport = t
	}
}
