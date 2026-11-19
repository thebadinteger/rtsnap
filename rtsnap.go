package rtsnap

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"time"

	"github.com/thebadinteger/rtsnap/pkg/h264"
	"github.com/thebadinteger/rtsnap/pkg/h265"
	"github.com/thebadinteger/rtsnap/pkg/mjpeg"
	"github.com/thebadinteger/rtsnap/pkg/rtp"
	"github.com/thebadinteger/rtsnap/pkg/rtsp"
)

// upper bound of decoded frame size
const maxFramePixels = 7680 * 4320

// run decode and convert panic to error
func safely[T any](fn func() (T, error)) (out T, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("decode panic: %v", r)
		}
	}()
	return fn()
}

// capture single frame from rtsp stream
func Snapshot(ctx context.Context, rtspURL string, opts ...Option) (image.Image, error) {
	img, _, err := snapshot(ctx, rtspURL, false, opts)
	return img, err
}

// capture single frame as jpeg bytes
func SnapshotJPEG(ctx context.Context, rtspURL string, quality int, opts ...Option) ([]byte, error) {
	img, raw, err := snapshot(ctx, rtspURL, true, opts)
	if err != nil || raw != nil {
		return raw, err
	}

	if quality <= 0 || quality > 100 {
		quality = 85
	}

	// the stream is already closed here, encoding does not keep the camera busy
	var buf bytes.Buffer
	size := img.Bounds().Size()
	buf.Grow(size.X*size.Y/8 + 4096)
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return buf.Bytes(), nil
}

// short video track description
type TrackInfo struct {
	Codec       Codec
	PayloadType uint8
	ClockRate   int
	Control     string
}

// stream description without playback
type StreamInfo struct {
	URL    string
	Tracks []TrackInfo
}

func buildOptions(opts []Option) Options {
	o := Options{
		Timeout: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// capture one frame, as raw jpeg bytes when the stream allows it and rawJPEG is set
func snapshot(ctx context.Context, rtspURL string, rawJPEG bool, opts []Option) (image.Image, []byte, error) {
	o := buildOptions(opts)
	if o.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.Timeout)
		defer cancel()
	}

	img, raw, err := capture(ctx, rtspURL, o, rawJPEG)
	// udp was negotiated but nothing came through, typical for nat and firewalls
	if o.Transport == TransportAuto && errors.Is(err, rtsp.ErrUDPTimeout) {
		o.Transport = TransportTCP
		img, raw, err = capture(ctx, rtspURL, o, rawJPEG)
	}
	return img, raw, err
}

// run one rtsp session until a frame is captured
func capture(ctx context.Context, rtspURL string, o Options, rawJPEG bool) (image.Image, []byte, error) {
	client, err := connect(ctx, rtspURL, o)
	if err != nil {
		return nil, nil, err
	}
	defer client.Close()

	track, err := client.SelectTrack(string(o.Codec))
	if err != nil {
		return nil, nil, err
	}
	if err := client.Setup(ctx); err != nil {
		return nil, nil, err
	}
	// registered before play, a play cut short still leaves a session on the server
	defer func() { _ = client.Teardown(ctx) }()
	if err := client.Play(ctx); err != nil {
		return nil, nil, err
	}

	r := &packetReader{client: client, payloadType: track.PayloadType, reorder: client.UDP()}

	switch track.Codec {
	case "mjpeg":
		if rawJPEG && !o.Transcode {
			raw, err := captureMJPEG(ctx, r, nil)
			return nil, raw, err
		}
		var img image.Image
		_, err := captureMJPEG(ctx, r, &img)
		return img, nil, err
	case "h264":
		img, err := captureH264(ctx, r, track, o.Fast)
		return img, nil, err
	case "h265":
		img, err := captureH265(ctx, r, track, o.Fast)
		return img, nil, err
	default:
		return nil, nil, fmt.Errorf("unsupported video codec: %s", track.Codec)
	}
}

// open connection and read stream description
func connect(ctx context.Context, rtspURL string, o Options) (*rtsp.Client, error) {
	client, err := rtsp.DialWithConfig(ctx, rtspURL, o.Username, o.Password, &rtsp.DialConfig{
		TLSConfig: connectTLSConfig(o),
		Tunnel:    o.Tunnel,
	})
	if err != nil {
		return nil, err
	}

	client.Transport = o.Transport
	client.UserAgent = o.UserAgent
	client.DebugFunc = o.DebugFunc

	if err := client.Describe(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

// tls settings derived from options
func connectTLSConfig(o Options) *tls.Config {
	if o.TLSConfig != nil {
		return o.TLSConfig
	}
	if !o.TLSVerify {
		return &tls.Config{InsecureSkipVerify: true}
	}
	return nil
}

// list video tracks without playback
func Query(ctx context.Context, rtspURL string, opts ...Option) (*StreamInfo, error) {
	o := buildOptions(opts)
	if o.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.Timeout)
		defer cancel()
	}

	client, err := connect(ctx, rtspURL, o)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	info := &StreamInfo{
		URL: rtspURL,
	}
	for _, t := range client.Tracks {
		info.Tracks = append(info.Tracks, TrackInfo{
			Codec:       Codec(t.Codec),
			PayloadType: t.PayloadType,
			ClockRate:   t.ClockRate,
			Control:     t.Control,
		})
	}

	return info, nil
}

const (
	// how far ahead of a missing packet udp delivery is followed
	reorderWindow = 16
	// later packets that may overtake one before it counts as lost
	reorderDepth = 4
)

// in order rtp packets of the selected track
type packetReader struct {
	client      *rtsp.Client
	payloadType uint8
	// udp may deliver out of order, tcp never does
	reorder bool

	pkt     rtp.Packet
	next    uint16
	started bool
	lost    bool
	late    int

	// packets waiting for a missing predecessor, slot is seq modulo window
	held  [reorderWindow][]byte
	nheld int
	// packet read from the wire that has to wait for the held ones
	cur []byte
}

// next packet in sequence, gap reports that packets before it are missing
// the packet is only valid until the following call
func (r *packetReader) read(ctx context.Context) (pkt *rtp.Packet, gap bool, err error) {
	for {
		if r.nheld > 0 {
			if raw := r.held[r.next%reorderWindow]; raw != nil {
				r.held[r.next%reorderWindow] = nil
				r.nheld--
				if r.pkt.Unmarshal(raw) == nil {
					return r.deliver()
				}
			}
		}

		raw := r.cur
		r.cur = nil
		if raw == nil {
			if raw, err = r.client.ReadPacket(ctx); err != nil {
				return nil, false, err
			}
		}
		// other payload types run on their own sequence numbers
		if r.pkt.Unmarshal(raw) != nil || r.pkt.PayloadType != r.payloadType {
			continue
		}

		seq := r.pkt.SequenceNumber
		if !r.started {
			r.started, r.next = true, seq
		}

		switch ahead := int16(seq - r.next); {
		case ahead == 0:
			return r.deliver()

		case !r.reorder:
			if ahead == -1 {
				continue
			}

		case ahead < 0:
			// duplicate or too late, a long run of them means the sender restarted
			if r.late++; r.late <= reorderWindow {
				continue
			}

		case ahead < reorderWindow && r.nheld+1 < reorderDepth:
			if slot := &r.held[seq%reorderWindow]; *slot == nil {
				*slot = append([]byte(nil), raw...)
				r.nheld++
			}
			continue
		}

		r.lost = true
		if r.nheld > 0 {
			// release what is held first, this packet follows it
			for r.held[r.next%reorderWindow] == nil {
				r.next++
			}
			r.cur = raw
			continue
		}
		r.next = seq
		return r.deliver()
	}
}

func (r *packetReader) deliver() (*rtp.Packet, bool, error) {
	gap := r.lost
	r.next = r.pkt.SequenceNumber + 1
	r.lost, r.late = false, 0
	return &r.pkt, gap, nil
}

// read mjpeg packets until a full frame, decoded into img when it is set
func captureMJPEG(ctx context.Context, r *packetReader, img *image.Image) ([]byte, error) {
	depack := rtp.NewMJPEGDepacketizer()
	dec := mjpeg.NewDecoder()

	for {
		pkt, _, err := r.read(ctx)
		if err != nil {
			return nil, err
		}

		jpegBytes, err := depack.Decode(pkt)
		if err != nil || len(jpegBytes) == 0 {
			continue
		}
		if img == nil {
			return jpegBytes, nil
		}
		decoded, err := safely(func() (image.Image, error) { return dec.Decode(jpegBytes) })
		if err != nil {
			continue
		}
		*img = decoded
		return nil, nil
	}
}

// read h264 packets until first decodable frame
func captureH264(ctx context.Context, r *packetReader, track *rtsp.MediaTrack, fast bool) (image.Image, error) {
	depack := rtp.NewH264Depacketizer()
	dec := h264.New()
	dec.SkipDeblock = fast
	dec.FrameSizeLimit(maxFramePixels)

	// one unit at a time so a broken one does not hide the units after it
	var one [1][]byte
	var lastErr error
	decode := func(nalu []byte) *h264.Frame {
		one[0] = nalu
		f, err := safely(func() (*h264.Frame, error) { return dec.DecodeNALUs(one[:]) })
		if err != nil {
			lastErr = err
		}
		return f
	}
	for _, sps := range track.SPS {
		decode(sps)
	}
	for _, pps := range track.PPS {
		decode(pps)
	}

	for {
		pkt, gap, err := r.read(ctx)
		if err != nil {
			return nil, withDecodeError(err, lastErr)
		}
		if gap {
			// parameter sets survive the reset, in band ones are newer than the sdp
			depack = rtp.NewH264Depacketizer()
			dec.Reset()
		}

		nalus, err := depack.Decode(pkt)
		if err != nil {
			continue
		}
		for _, nalu := range nalus {
			if f := decode(nalu); f != nil {
				if img := f.Image(); img != nil {
					return img, nil
				}
			}
		}
	}
}

// say why nothing could be decoded when the stream ran out of time
func withDecodeError(err, decodeErr error) error {
	if decodeErr == nil {
		return err
	}
	return fmt.Errorf("%w (last decode error: %v)", err, decodeErr)
}

// keep newest parameter set of each type
func stashParam(stash []h265.NALUnit, u h265.NALUnit) []h265.NALUnit {
	for i := range stash {
		if stash[i].Type == u.Type {
			stash[i] = u
			return stash
		}
	}
	return append(stash, u)
}

// read h265 packets until first decodable frame
func captureH265(ctx context.Context, r *packetReader, track *rtsp.MediaTrack, fast bool) (image.Image, error) {
	var depack *rtp.H265Depacketizer
	var dec h265.Decoder
	defer dec.Close()
	var stash []h265.NALUnit
	var seenIRAP bool
	var fallback image.Image
	var lastErr error

	decode := func(u h265.NALUnit) ([]*h265.Picture, error) {
		pics, err := safely(func() ([]*h265.Picture, error) { return dec.DecodeNAL(u) })
		if err != nil {
			lastErr = err
		}
		return pics, err
	}
	reset := func() {
		seenIRAP = false
		fallback = nil
		depack = rtp.NewH265Depacketizer()
		dec.Reset()
		dec.SkipLoop = fast
		dec.FrameSizeLimit(maxFramePixels)
		for _, sets := range [][][]byte{track.VPS, track.SPS, track.PPS} {
			for _, set := range sets {
				if u, ok := h265.ParseNAL(set); ok {
					_, _ = decode(u)
				}
			}
		}
		for _, u := range stash {
			_, _ = decode(u)
		}
	}
	reset()

	for {
		pkt, gap, err := r.read(ctx)
		if err != nil {
			// prefer a clean frame, fall back to a partly corrupted one
			if fallback != nil {
				return fallback, nil
			}
			return nil, withDecodeError(err, lastErr)
		}
		if gap {
			reset()
		}

		nalus, err := depack.Decode(pkt)
		if err != nil {
			continue
		}

		for _, nal := range nalus {
			u, ok := h265.ParseNAL(nal)
			if !ok {
				continue
			}

			switch u.Type {
			case h265.NALVPS, h265.NALSPS, h265.NALPPS:
				stash = stashParam(stash, u)
			}

			if u.Type.IsIRAP() {
				seenIRAP = true
			}

			if u.Type.IsVCL() && !seenIRAP {
				continue
			}

			pics, err := decode(u)
			if err != nil {
				reset()
				continue
			}
			if img := pickImage(pics, &fallback, seenIRAP); img != nil {
				return img, nil
			}

			if u.Type.IsIRAP() && !dec.Pending() {
				flush, _ := safely(func() ([]*h265.Picture, error) { return dec.Flush(), nil })
				if img := pickImage(flush, &fallback, seenIRAP); img != nil {
					return img, nil
				}
			}
		}
	}
}

// first clean picture as an image, a corrupt one is only remembered as a fallback
func pickImage(pics []*h265.Picture, fallback *image.Image, emit bool) image.Image {
	var out image.Image

	for _, p := range pics {
		if emit && out == nil && (!p.Corrupt || *fallback == nil) {
			img, _ := safely(func() (image.Image, error) { return p.ImageCopy(), nil })
			if !p.Corrupt {
				out = img
			} else {
				*fallback = img
			}
		}

		p.Release()
	}

	return out
}
