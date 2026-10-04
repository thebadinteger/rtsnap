# Documentation
## rtsnap

---

- [1. Overview](#1-overview)
- [2. API (`rtsnap`)](#2-api-rtsnap)
  - [Snapshot](#snapshot)
  - [SnapshotJPEG](#snapshotjpeg)
  - [Query](#query)
  - [Options & Codec Constants](#options--codec-constants)
- [3. RTSP (`pkg/rtsp`)](#3-rtsp-pkgrtsp)
  - [Types & Structures](#types--structures)
  - [Dial](#dial)
  - [Describe](#describe)
  - [SelectTrack](#selecttrack)
  - [Setup](#setup)
  - [Play](#play)
  - [ReadFrame](#readframe)
  - [Teardown & Close](#teardown--close)
  - [Interleaved Transport](#interleaved-transport)
  - [Authentication](#authentication)
  - [Low-Level Client Example](#low-level-client-example)
- [4. RTP Depacketization (`pkg/rtp`)](#4-rtp-depacketization-pkgrtp)
  - [RTP Packet Model](#rtp-packet-model)
  - [H.264 Depacketizer](#h264-depacketizer)
  - [H.265 Depacketizer](#h265-depacketizer)
  - [MJPEG Depacketizer](#mjpeg-depacketizer)
- [5. Video Decoding](#5-video-decoding)
  - [Pure Go H.264 Decoder (`pkg/h264`)](#pure-go-h264-decoder-pkgh264)
  - [Pure Go H.265 Decoder (`pkg/h265`)](#pure-go-h265-decoder-pkgh265)
  - [MJPEG Decoder (`pkg/mjpeg`)](#mjpeg-decoder-pkgmjpeg)
- [6. Concurrency, Timeouts & Socket Deadlines](#6-concurrency-timeouts--socket-deadlines)
- [7. Error Handling & Common Pitfalls](#7-error-handling--common-pitfalls)
- [8. Testing with the Mock Server](#8-testing-with-the-mock-server)

---

## 1. Overview
`rtsnap` connects to an RTSP server over TCP, negotiates an interleaved stream, receives RTP packets containing encoded video frames, and decodes the first complete keyframe (intra frame) into a standard Go `image.Image`  
### Pipeline:  
```text
[RTSP Camera / Server]
 | TCP Interleaved ($channel + length + payload)
 V
[pkg/rtsp.Client]
 | RTP Packets (RFC 3550)
 V
[pkg/rtp Depacketizer] > Extracts NAL Units or JPEG bitstream
 | H.264 (RFC 6184) > Single / STAP-A / FU-A
 | H.265 (RFC 7798) > Single / AP / FU
 | MJPEG (RFC 2435) > Reconstructs SOI, DQT, DHT, SOF0, SOS
 V
[Video Decoder]
 | pkg/h264 > Intra / IDR frame (SPS, PPS, CAVLC, CABAC, Deblock)
 | pkg/h265 > Intra / IRAP frame (VPS, SPS, PPS, CTU, SAO)
 | pkg/mjpeg > image/jpeg decoder
 V
[image.Image] / [JPEG Bytes]
```

## 2. API (`rtsnap`)
The top-level package `github.com/thebadinteger/rtsnap` provides the primary entry points

### Snapshot
```go
func Snapshot(ctx context.Context, rtspURL string, opts ...Option) (image.Image, error)
```
Captures a single decoded video frame from an RTSP stream
- **Parameters**:
  - `ctx` - A context for cancellation and deadlines
  - `rtspURL` - The RTSP URL (e.g. `rtsp://192.168.1.100:554/stream1`). Credentials may be included in the URL (`rtsp://admin:pass@host:554/live`)
  - `opts` - Optional configuration functions (e.g. `WithAuth`, `WithTimeout`)
- **Returns**:
  - `image.Image` - The decoded frame. For H.264 and H.265, this is typically `*image.YCbCr` or `*image.NRGBA`. For MJPEG, this is `*image.YCbCr` or `*image.Gray`
  - `error` - Non-nil if the connection, authentication, transport, or decoding fails

#### Example
```go
package main

import (
	"context"
	"image/png"
	"os"
	"time"

	"github.com/thebadinteger/rtsnap"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, "rtsp://localhost:8554/live")
	if err != nil {
		panic(err)
	}

	out, _ := os.Create("frame.png")
	defer out.Close()
	_ = png.Encode(out, img)
}
```

### SnapshotJPEG
```go
func SnapshotJPEG(ctx context.Context, rtspURL string, quality int, opts ...Option) ([]byte, error)
```
Captures a single frame and encodes it directly as JPEG bytes
- **Parameters**:
  - `ctx` - A context for cancellation and deadlines
  - `rtspURL` - The RTSP URL
  - `quality` - JPEG compression quality from `1` (lowest) to `100` (highest). If `<= 0` or `> 100`, defaults to `85`
  - `opts` - Functional options
- **Returns**:
  - `[]byte` - Valid JPEG file buffer beginning with `0xFF, 0xD8` (SOI) and ending with `0xFF, 0xD9` (EOI)
  - `error` - Any operational or compression error

### Query
```go
func Query(ctx context.Context, rtspURL string, opts ...Option) (*StreamInfo, error)
```
Inspects an RTSP stream by connecting and retrieving its SDP description without initiating playback or frame decoding
- **Parameters**:
  - `ctx` - A context for cancellation and deadlines
  - `rtspURL` - The RTSP URL
  - `opts` - Functional options (e.g. `WithAuth`, `WithTimeout`)
- **Returns**:
  - `*StreamInfo` - Contains the stream URL and a slice of all detected video tracks (`Tracks []TrackInfo`), detailing their `Codec`, `PayloadType`, `ClockRate`, and resolved `Control` URL
  - `error` - Non-nil if connection or SDP retrieval fails

### Options & Codec Constants
```go
type Codec string

const (
	CodecAuto  Codec = ""      // automatic selection (priority: h264 > h265 > mjpeg)
	CodecH264  Codec = "h264"  // explicitly require h264
	CodecH265  Codec = "h265"  // explicitly require h265
	CodecMJPEG Codec = "mjpeg" // explicitly require mjpeg
)

type Option func(*Options)
```
#### `WithAuth`
```go
func WithAuth(username, password string) Option
```
Specifies credentials used if the server challenges requests with HTTP 401 Unauthorized (Basic or Digest)

#### `WithTimeout`
```go
func WithTimeout(d time.Duration) Option
```
Sets an upper time limit for the entire operation. If the parent context does not already have a shorter deadline, a child context with this timeout is created

#### `WithCodec`
```go
func WithCodec(c Codec) Option
```
Specifies the preferred video codec. If set to `CodecAuto` (or omitted), the library selects the highest-priority supported video track (`H.264` > `H.265` > `MJPEG`). If explicitly specified, the library verifies that the stream provides the requested codec; if unavailable, a descriptive error is returned listing all available codecs in the stream

## 3. RTSP (`pkg/rtsp`)
The `pkg/rtsp` package implements an RFC 2326 compliant RTSP 1.0 client with TCP interleaved transport and authentication support

### Types & Structures
```go
type Client struct {
	Tracks []*MediaTrack
	Track  *MediaTrack
}

type MediaTrack struct {
	Type        string
	Codec       string // h264, h265, mjpeg
	PayloadType uint8
	ClockRate   int
	Control     string
	Fmtp        map[string]string
	VPS         [][]byte
	SPS         [][]byte
	PPS         [][]byte
}

type Frame struct {
	Channel int
	Payload []byte
}
```

### Dial
```go
func Dial(ctx context.Context, rtspURL string, user, pass string) (*Client, error)
```
Connects to an RTSP server over TCP (or TLS for `rtsps://`)
- **Parameters**:
  - `ctx` - Context controlling connection timeout
  - `rtspURL` - Full RTSP address (e.g. `rtsp://192.168.1.100:554/live`)
  - `user` - Optional username for authentication
  - `pass` - Optional password for authentication
- **Returns**:
  - `*Client` - Initialized RTSP client ready for requests
  - `error` - Non-nil if DNS resolution or socket connection fails

### Describe
```go
func (c *Client) Describe(ctx context.Context) error
```
Queries the server with an RTSP `DESCRIBE` request, parses the SDP session description, and automatically negotiates 401 authentication
- **Parameters**:
  - `ctx` - Context controlling network deadline
- **Returns**:
  - `error` - Non-nil if the request fails, authentication is rejected, or no video tracks are found

### SelectTrack
```go
func (c *Client) SelectTrack(codec string) (*MediaTrack, error)
```
Chooses an active track from `c.Tracks` by codec name or priority (`h264` > `h265` > `mjpeg`)
- **Parameters**:
  - `codec` - Target codec string (`"h264"`, `"h265"`, `"mjpeg"`, or `""` for auto)
- **Returns**:
  - `*MediaTrack` - Selected track
  - `error` - Non-nil if requested codec is not available in the stream

### Setup
```go
func (c *Client) Setup(ctx context.Context) error
```
Sends a `SETUP` request for `c.Track.Control` requesting TCP interleaved transport (`RTP/AVP/TCP;unicast;interleaved=0-1`)
- **Parameters**:
  - `ctx` - Context controlling network deadline
- **Returns**:
  - `error` - Non-nil if transport negotiation fails

### Play
```go
func (c *Client) Play(ctx context.Context) error
```
Sends a `PLAY` request to start streaming media packets
- **Parameters**:
  - `ctx` - Context controlling network deadline
- **Returns**:
  - `error` - Non-nil if server rejects play request

### ReadFrame
```go
func (c *Client) ReadFrame(ctx context.Context) (*Frame, error)
```
Reads the next interleaved binary frame from the TCP stream with socket deadline support
- **Parameters**:
  - `ctx` - Context controlling read deadline and cancellation
- **Returns**:
  - `*Frame` - Frame containing interleaved channel number and raw payload bytes
  - `error` - Non-nil if connection dropped or context cancelled

### Teardown & Close
```go
func (c *Client) Teardown(ctx context.Context) error
func (c *Client) Close() error
```
Cleanly notifies the RTSP server of session termination and closes the underlying network connection
- **Parameters**:
  - `ctx` - Context controlling teardown deadline
- **Returns**:
  - `error` - Non-nil if network error occurs

### Interleaved Transport
Encapsulates RTP and RTCP packets directly into the TCP connection using RFC 2326 section 10.12 framing:
```text
+------+---------+----------------+---------------------+
| '$'  | Channel | Length (16-bit)|       Payload       |
| 0x24 |  1 byte |     2 bytes    |    Length bytes     |
+------+---------+----------------+---------------------+
```
- **Channels**:
  - `Channel 0` - RTP data packets for video
  - `Channel 1` - RTCP control packets for video

### Authentication
Handles 401 Unauthorized challenges:
- **Basic Auth (RFC 7617)** - Encoded with standard base64 credentials
- **Digest Auth (RFC 7616)** - MD5 and SHA-256 algorithms with `qop="auth"` support, nonce tracking, and cnonce generation

### Low-Level Client Example
```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/thebadinteger/rtsnap/pkg/rtsp"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// connect to rtsp server
	client, err := rtsp.Dial(ctx, "rtsp://localhost:8554/live", "admin", "secret")
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// query stream description
	if err := client.Describe(ctx); err != nil {
		panic(err)
	}
	fmt.Printf("found %d tracks, codecs: %v\n", len(client.Tracks), client.AvailableCodecs())

	// setup transport
	if err := client.Setup(ctx); err != nil {
		panic(err)
	}

	// start stream
	if err := client.Play(ctx); err != nil {
		panic(err)
	}
	defer client.Teardown(ctx)

	// read first interleaved packet
	frame, err := client.ReadFrame(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Printf("received frame on channel %d with %d bytes\n", frame.Channel, len(frame.Payload))
}
```

## 4. RTP Depacketization (`pkg/rtp`)
The `pkg/rtp` package handles RFC 3550 RTP parsing and codec-specific depacketization for H.264, H.265, and MJPEG

### RTP Packet Model
```go
type Packet struct {
	Version        uint8
	Padding        bool
	Extension      bool
	Marker         bool
	PayloadType    uint8
	SequenceNumber uint16
	Timestamp      uint32
	SSRC           uint32
	Payload        []byte
}

func (p *Packet) Unmarshal(data []byte) error
```
Decodes a raw byte slice into an RTP packet according to RFC 3550
- **Parameters**:
  - `data` - Raw bytes received from interleaved frame
- **Returns**:
  - `error` - Non-nil if packet is truncated or contains invalid version

### H.264 Depacketizer
```go
func NewH264Depacketizer() *H264Depacketizer
func (d *H264Depacketizer) Decode(pkt *Packet) ([][]byte, error)
```
Reassembles RFC 6184 RTP packets into discrete H.264 NAL units (Single NAL, STAP-A aggregation, and FU-A fragmentation)
- **Parameters**:
  - `pkt` - Parsed RTP packet
- **Returns**:
  - `[][]byte` - Slice of complete NAL units ready for decoding
  - `error` - Non-nil if payload structure is malformed

#### Example
```go
depack := rtp.NewH264Depacketizer()
nalus, err := depack.Decode(&pkt)
if err == nil && len(nalus) > 0 {
	// nalus ready for h264 decoder
}
```

### H.265 Depacketizer
```go
func NewH265Depacketizer() *H265Depacketizer
func (d *H265Depacketizer) Decode(pkt *Packet) ([][]byte, error)
```
Reassembles RFC 7798 RTP packets into discrete H.265 / HEVC NAL units (Single NAL, AP aggregation, and FU fragmentation)
- **Parameters**:
  - `pkt` - Parsed RTP packet
- **Returns**:
  - `[][]byte` - Slice of complete HEVC NAL units
  - `error` - Non-nil if payload structure is malformed

### MJPEG Depacketizer
```go
func NewMJPEGDepacketizer() *MJPEGDepacketizer
func (d *MJPEGDepacketizer) Decode(pkt *Packet) ([]byte, error)
```
Reassembles RFC 2435 RTP packets into a fully compliant JPEG byte stream with synthesized headers (SOI, DQT, DHT, SOF0, SOS, EOI) and dynamic Q scaling
- **Parameters**:
  - `pkt` - Parsed RTP packet
- **Returns**:
  - `[]byte` - Complete JPEG byte slice when frame assembly finishes (or nil if more fragments needed)
  - `error` - Non-nil if payload header is invalid

## 5. Video Decoding
Pure Go video decoders with zero CGO and zero external dependencies

### Pure Go H.264 Decoder (`pkg/h264`)
```go
func New() *Decoder
func (d *Decoder) DecodeNALUs(nalus [][]byte) (*Frame, error)
func ExtractNalusFromByteStream(data []byte) [][]byte
```
Decodes H.264 IDR/Intra frames across Baseline, Main, and High profiles (SPS/PPS, CAVLC/CABAC, intra prediction, deblocking)
- **Parameters**:
  - `nalus` - Slice of raw NAL unit byte slices
- **Returns**:
  - `*Frame` - Decoded frame exposing `Image() image.Image` (`*image.YCbCr` or `*image.NRGBA`)
  - `error` - Non-nil if bitstream is corrupt

#### Example
```go
dec := h264.New()

// pass initial parameter sets from sdp
_, _ = dec.DecodeNALUs(track.SPS)
_, _ = dec.DecodeNALUs(track.PPS)

// decode incoming idr nal units
frame, err := dec.DecodeNALUs(nalus)
if err == nil && frame != nil {
	img := frame.Image()
	_ = img.Bounds()
}
```

### Pure Go H.265 Decoder (`pkg/h265`)
```go
func ParseNAL(data []byte) (NALUnit, bool)
func (d *Decoder) DecodeNAL(u NALUnit) ([]Picture, error)
func (d *Decoder) Flush() []Picture
```
Decodes H.265 / HEVC intra frames (VPS/SPS/PPS, CTU quadtree, 35 intra modes, SAO filtering, DPB flushing)
- **Parameters**:
  - `u` - Parsed HEVC NAL unit
- **Returns**:
  - `[]Picture` - Decoded pictures exposing `p.Image() image.Image`
  - `error` - Non-nil if decoding fails

#### Example
```go
var dec h265.Decoder

for _, nal := range nalus {
	u, ok := h265.ParseNAL(nal)
	if !ok {
		continue
	}
	pics, err := dec.DecodeNAL(u)
	if err == nil {
		for _, p := range pics {
			img := p.Image()
			_ = img.Bounds()
		}
	}
	if u.Type.IsIRAP() {
		for _, p := range dec.Flush() {
			img := p.Image()
			_ = img.Bounds()
		}
	}
}
```

### MJPEG Decoder (`pkg/mjpeg`)
```go
func NewDecoder() *Decoder
func (d *Decoder) Decode(jpegBytes []byte) (image.Image, error)
```
Wraps Go standard library `image/jpeg` decoder for reassembled MJPEG bitstreams
- **Parameters**:
  - `jpegBytes` - Complete reconstructed JPEG byte buffer from `MJPEGDepacketizer`
- **Returns**:
  - `image.Image` - Decoded standard Go image (`*image.YCbCr` or `*image.Gray`)
  - `error` - Non-nil if JPEG data is corrupt

## 6. Concurrency, Timeouts & Socket Deadlines
RTSP streams can stall or block indefinitely if network connections hang or cameras stop transmitting without closing the TCP connection. `rtsnap` applies a dual-layer deadline architecture

### Socket-Level Read Deadlines
When `ctx.Deadline()` is set, the client applies `conn.SetReadDeadline(deadline)` directly onto the TCP connection before reading:
```go
if d, ok := ctx.Deadline(); ok {
	_ = c.conn.SetReadDeadline(d)
}
```

### Active Background Cancellation Watcher
In Go, calling `ctx.Cancel()` does not unblock pending `net.Conn.Read` calls automatically. `rtsnap` spawns a lightweight watcher goroutine for every `ReadFrame` call:
```go
stop := make(chan struct{})
defer close(stop)

go func() {
	select {
	case <-ctx.Done():
		// unblock read immediately by setting deadline in the past
		_ = c.conn.SetReadDeadline(time.Now())
	case <-stop:
	}
}()
```
- **Guarantees**:
  - Immediate unblocking within milliseconds when a context timeout or cancellation triggers
  - Zero goroutine leaks: the `stop` channel ensures the watcher terminates as soon as `ReadFrame` finishes

## 7. Error Handling & Common Pitfalls

| Symptom | Cause | Solution |
|---|---|---|
| `describe rejected: 401 Unauthorized` | Protected stream missing valid credentials | Pass credentials via `WithAuth("user", "pass")` or embedded in the URL (`rtsp://user:pass@host:554/live`) |
| `no video tracks found in sdp` | Stream is audio-only or uses non-standard SDP attributes | Verify stream has video enabled in camera settings |
| `requested codec "h265" not found (available: [h264])` | Stream does not support requested codec | Use `WithCodec(rtsnap.CodecAuto)` or request an available codec |
| `context deadline exceeded` | Server takes longer than timeout to produce an I-frame | Increase timeout via `WithTimeout(10*time.Second)` or reduce GOP size in camera settings |
| Parameter sets missing in SDP | Server sends SPS/PPS in-band instead of SDP | Handled automatically by extracting parameter sets directly from the incoming RTP NAL stream |
| RTCP packets received | Interleaved channel multiplexing | Handled automatically by verifying channel matches `client.RTPChannel()` |

## 8. Testing with the Mock Server
The integration test suite in `tests/` includes a fully functional, pure Go mock RTSP server (`tests/mock_test.go`) that runs without network access

### Mock Server Features
- Simulates RTSP 1.0 servers with dynamic local ports (`127.0.0.1:0`)
- Supports authentication modes: `none`, `basic`, `digest`, `digest-sha256`
- Simulates interleaved TCP transport and generates synthetic H.264, H.265, and MJPEG RTP streams
- Simulates multi-fragment FU-A packets, STAP-A aggregation, and missing video tracks

### Example: Writing a Test with mockServer
```go
package tests

import (
	"context"
	"testing"
	"time"

	"github.com/thebadinteger/rtsnap"
)

func TestCustomSnapshot(t *testing.T) {
	// launch local mock server
	srv, err := newMockServer(mockServer{
		codec: "h264",
		packets: [][]byte{
			// rtp packet bytes
		},
	})
	if err != nil {
		t.Fatalf("start mock: %v", err)
	}
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, srv.URL())
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected decoded image, got nil")
	}
}
```

### Running Test Commands
```bash
# run all tests
go test -v ./tests

# run all tests across the entire repository
go test ./...

# run with race detector
go test -race ./tests

# run tests with coverage report
go test -cover ./tests
```
