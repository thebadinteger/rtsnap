package rtsnap

import (
	"bytes"
	"context"
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
	ctx, client, o, cancel, err := connect(ctx, rtspURL, opts...)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer client.Close()

	if err := playTrack(ctx, client, o.Codec); err != nil {
		return nil, err
	}
	defer func() { _ = client.Teardown(ctx) }()

	switch client.Track.Codec {
	case "mjpeg":
		return captureMJPEG(ctx, client)
	case "h264":
		return captureH264(ctx, client, o.Fast)
	case "h265":
		return captureH265(ctx, client, o.Fast)
	default:
		return nil, fmt.Errorf("unsupported video codec: %s", client.Track.Codec)
	}
}

// capture single frame as jpeg bytes
func SnapshotJPEG(ctx context.Context, rtspURL string, quality int, opts ...Option) ([]byte, error) {
	ctx, client, o, cancel, err := connect(ctx, rtspURL, opts...)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer client.Close()

	if err := playTrack(ctx, client, o.Codec); err != nil {
		return nil, err
	}
	defer func() { _ = client.Teardown(ctx) }()

	if client.Track.Codec == "mjpeg" && !o.Transcode {
		return captureMJPEGRaw(ctx, client)
	}

	var img image.Image
	switch client.Track.Codec {
	case "mjpeg":
		img, err = captureMJPEG(ctx, client)
	case "h264":
		img, err = captureH264(ctx, client, o.Fast)
	case "h265":
		img, err = captureH265(ctx, client, o.Fast)
	default:
		return nil, fmt.Errorf("unsupported video codec: %s", client.Track.Codec)
	}
	if err != nil {
		return nil, err
	}

	if quality <= 0 || quality > 100 {
		quality = 85
	}

	var buf bytes.Buffer
	buf.Grow(256 * 1024)
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

// open connection and read stream description
func connect(ctx context.Context, rtspURL string, opts ...Option) (context.Context, *rtsp.Client, Options, context.CancelFunc, error) {
	o := Options{
		Timeout: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(&o)
	}

	cancel := context.CancelFunc(func() {})
	if o.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, o.Timeout)
	}

	client, err := rtsp.Dial(ctx, rtspURL, o.Username, o.Password, !o.TLSVerify)
	if err != nil {
		cancel()
		return nil, nil, o, nil, err
	}

	client.Transport = o.Transport
	client.UserAgent = o.UserAgent
	client.DebugFunc = o.DebugFunc

	if err := client.Describe(ctx); err != nil {
		_ = client.Close()
		cancel()
		return nil, nil, o, nil, err
	}

	return ctx, client, o, cancel, nil
}

// select track and start playback
func playTrack(ctx context.Context, client *rtsp.Client, codec Codec) error {
	track, err := client.SelectTrack(string(codec))
	if err != nil {
		return err
	}
	client.Track = track

	if err := client.Setup(ctx); err != nil {
		return err
	}

	return client.Play(ctx)
}

// list video tracks without playback
func Query(ctx context.Context, rtspURL string, opts ...Option) (*StreamInfo, error) {
	_, client, _, cancel, err := connect(ctx, rtspURL, opts...)
	if err != nil {
		return nil, err
	}
	defer cancel()
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

// read mjpeg packets until full frame
func captureMJPEG(ctx context.Context, client *rtsp.Client) (image.Image, error) {
	var depack *rtp.MJPEGDepacketizer
	dec := mjpeg.NewDecoder()
	reset := func() {
		depack = rtp.NewMJPEGDepacketizer()
	}
	reset()
	var lastSeq uint16
	var seqSet bool

	for {
		frame, err := client.ReadFrame(ctx)
		if err != nil {
			return nil, err
		}
		if frame.Channel != client.RTPChannel() {
			continue
		}

		var pkt rtp.Packet
		if err := pkt.Unmarshal(frame.Payload); err != nil {
			continue
		}

		if seqSet && pkt.SequenceNumber == lastSeq {
			continue
		}
		if seqSet && pkt.SequenceNumber != uint16(lastSeq+1) {
			reset()
		}
		lastSeq = pkt.SequenceNumber
		seqSet = true

		jpegBytes, err := depack.Decode(&pkt)
		if err != nil {
			continue
		}
		if len(jpegBytes) > 0 {
			img, err := dec.Decode(jpegBytes)
			if err != nil {
				reset()
				continue
			}
			return img, nil
		}
	}
}

// read mjpeg packets and return raw bytes
func captureMJPEGRaw(ctx context.Context, client *rtsp.Client) ([]byte, error) {
	var depack *rtp.MJPEGDepacketizer
	reset := func() {
		depack = rtp.NewMJPEGDepacketizer()
	}
	reset()
	var lastSeq uint16
	var seqSet bool

	for {
		frame, err := client.ReadFrame(ctx)
		if err != nil {
			return nil, err
		}
		if frame.Channel != client.RTPChannel() {
			continue
		}

		var pkt rtp.Packet
		if err := pkt.Unmarshal(frame.Payload); err != nil {
			continue
		}

		if seqSet && pkt.SequenceNumber == lastSeq {
			continue
		}
		if seqSet && pkt.SequenceNumber != uint16(lastSeq+1) {
			reset()
		}
		lastSeq = pkt.SequenceNumber
		seqSet = true

		raw, err := depack.Decode(&pkt)
		if err != nil {
			continue
		}
		if len(raw) > 0 {
			return raw, nil
		}
	}
}

// read h264 packets until first decodable frame
func captureH264(ctx context.Context, client *rtsp.Client, fast bool) (image.Image, error) {
	depack := rtp.NewH264Depacketizer()
	dec := h264.New()
	dec.SkipDeblock = fast
	dec.FrameSizeLimit(maxFramePixels)
	feedParams := func() {
		if len(client.Track.SPS) > 0 {
			_, _ = safely(func() (*h264.Frame, error) { return dec.DecodeNALUs(client.Track.SPS) })
		}
		if len(client.Track.PPS) > 0 {
			_, _ = safely(func() (*h264.Frame, error) { return dec.DecodeNALUs(client.Track.PPS) })
		}
	}
	feedParams()

	var lastSeq uint16
	var seqSet bool

	for {
		frame, err := client.ReadFrame(ctx)
		if err != nil {
			return nil, err
		}
		if frame.Channel != client.RTPChannel() {
			continue
		}

		var pkt rtp.Packet
		if err := pkt.Unmarshal(frame.Payload); err != nil {
			continue
		}

		if seqSet && pkt.SequenceNumber == lastSeq {
			continue
		}
		if seqSet && pkt.SequenceNumber != uint16(lastSeq+1) {
			depack = rtp.NewH264Depacketizer()
			feedParams()
		}
		lastSeq = pkt.SequenceNumber
		seqSet = true

		nalus, err := depack.Decode(&pkt)
		if err != nil {
			continue
		}
		if len(nalus) == 0 {
			continue
		}

		f, err := safely(func() (*h264.Frame, error) { return dec.DecodeNALUs(nalus) })
		if err == nil && f != nil {
			if img := f.Image(); img != nil {
				return img, nil
			}
		}
	}
}

// read h265 packets until first decodable frame
func captureH265(ctx context.Context, client *rtsp.Client, fast bool) (image.Image, error) {
	var depack *rtp.H265Depacketizer
	var dec h265.Decoder
	var stash []h265.NALUnit
	var lastSeq uint16
	var seqSet bool
	var seenIRAP bool
	reset := func() {
		seenIRAP = false
		depack = rtp.NewH265Depacketizer()
		dec.Reset()
		dec.SkipLoop = fast
		dec.FrameSizeLimit(maxFramePixels)
		for _, vps := range client.Track.VPS {
			if u, ok := h265.ParseNAL(vps); ok {
				_, _ = safely(func() ([]*h265.Picture, error) { return dec.DecodeNAL(u) })
			}
		}
		for _, sps := range client.Track.SPS {
			if u, ok := h265.ParseNAL(sps); ok {
				_, _ = safely(func() ([]*h265.Picture, error) { return dec.DecodeNAL(u) })
			}
		}
		for _, pps := range client.Track.PPS {
			if u, ok := h265.ParseNAL(pps); ok {
				_, _ = safely(func() ([]*h265.Picture, error) { return dec.DecodeNAL(u) })
			}
		}
		for _, u := range stash {
			_, _ = safely(func() ([]*h265.Picture, error) { return dec.DecodeNAL(u) })
		}
	}
	reset()

	for {
		frame, err := client.ReadFrame(ctx)
		if err != nil {
			return nil, err
		}
		if frame.Channel != client.RTPChannel() {
			continue
		}

		var pkt rtp.Packet
		if err := pkt.Unmarshal(frame.Payload); err != nil {
			continue
		}

		if seqSet && pkt.SequenceNumber == lastSeq {
			continue
		}
		if seqSet && pkt.SequenceNumber != uint16(lastSeq+1) {
			reset()
		}
		lastSeq = pkt.SequenceNumber
		seqSet = true

		nalus, err := depack.Decode(&pkt)
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
				stash = append(stash, u)
			}

			firstRAP := u.Type.IsIRAP() && !seenIRAP
			if u.Type.IsIRAP() {
				seenIRAP = true
			}

			pics, err := safely(func() ([]*h265.Picture, error) { return dec.DecodeNAL(u) })
			if err != nil {
				reset()
				continue
			}
			if seenIRAP && !firstRAP {
				for _, p := range pics {
					if img := p.Image(); img != nil {
						return img, nil
					}
				}
			}

			// flush only complete pictures
			if u.Type.IsIRAP() && pkt.Marker {
				flush, _ := safely(func() ([]*h265.Picture, error) { return dec.Flush(), nil })
				for _, p := range flush {
					if img := p.Image(); img != nil {
						return img, nil
					}
				}
			}
		}
	}
}
