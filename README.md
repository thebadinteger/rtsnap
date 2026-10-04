# rtsnap  
### Go library for capturing snapshots from live RTSP streams  
![Go](https://img.shields.io/badge/Go-1.22%2B-00647d?style=flat&logo=go&logoColor=ffffff)
[![License](https://img.shields.io/github/license/thebadinteger/rtsnap)](LICENSE)
[![Test](https://github.com/thebadinteger/rtsnap/actions/workflows/test.yml/badge.svg)](https://github.com/thebadinteger/rtsnap/actions/workflows/test.yml)
[![Lint](https://github.com/thebadinteger/rtsnap/actions/workflows/lint.yml/badge.svg)](https://github.com/thebadinteger/rtsnap/actions/workflows/lint.yml)
[![PkgGoDev](https://pkg.go.dev/badge/github.com/thebadinteger/rtsnap)](https://pkg.go.dev/github.com/thebadinteger/rtsnap)

---

- [Features](#features)
- [Start](#start)
- [Examples](#examples)
- [API](#api)
- [Codecs](#codecs)
- [Authentication](#authentication)
- [Specifications](#specifications)
- [Architecture](#architecture)
- [Documentation](#documentation)
- [License](#license)

## Features:  
- Pure Go & Zero Dependencies
- Codecs support: `H.264 (AVC), H.265 (HEVC), MJPEG`
- Basic and Digest auth support
- Transports: `TCP interleaved, UDP unicast` 
- Auto fallback with packet loss recovery

## Start  
Install the library:  
```bash
go get github.com/thebadinteger/rtsnap
```  
Capture a snapshot to an `image.Image`:
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

	// capture single image frame
	img, err := rtsnap.Snapshot(ctx, "rtsp://192.168.1.100:554/live",
		rtsnap.WithAuth("admin", "secret123"),
		rtsnap.WithTimeout(5*time.Second),
	)
	if err != nil {
		panic(err)
	}

	f, _ := os.Create("snapshot.png")
	defer f.Close()
	_ = png.Encode(f, img)
}
```  
Capture directly to JPEG bytes:
```go
package main

import (
	"context"
	"os"
	"time"

	"github.com/thebadinteger/rtsnap"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// capture frame directly as compressed jpeg bytes
	jpegBytes, err := rtsnap.SnapshotJPEG(
		ctx,
		"rtsp://admin:secret123@192.168.1.100:554/live",
		85, // jpeg quality 1-100
		rtsnap.WithTimeout(3*time.Second),
	)
	if err != nil {
		panic(err)
	}

	_ = os.WriteFile("snapshot.jpg", jpegBytes, 0644)
}
```

## Examples
Practical and runnable code examples are available in the [`examples/`](examples) directory:  
- [examples/snapshot](examples/snapshot/main.go) - Basic snapshot capture returning `image.Image` and saving to PNG
- [examples/jpeg](examples/jpeg/main.go) - Direct capture to JPEG bytes with custom quality encoding
- [examples/auth](examples/auth/main.go) - Handling Basic and Digest authentication on protected RTSP streams
- [examples/codec](examples/codec/main.go) - Explicit codec selection and fallback handling
- [examples/query](examples/query/main.go) - Inspecting streams and discovering available tracks without capturing
- [examples/timeout](examples/timeout/main.go) - Proper handling of contexts, deadlines, and network timeouts
- [examples/lowlevel](examples/lowlevel/main.go) - Low-level RTSP client session negotiation and raw RTP packet reading
- [examples/h264](examples/h264/main.go) - Manual H.264 depacketization and NAL decoding
- [examples/mjpeg](examples/mjpeg/main.go) - Manual MJPEG depacketization and JPEG frame reconstruction

**Run any example:**
```bash
go run ./examples/snapshot
```  

## API
### Primary Functions
#### `rtsnap.Snapshot`
```go
func Snapshot(ctx context.Context, rtspURL string, opts ...Option) (image.Image, error)
```
Connects to the RTSP stream, issues `DESCRIBE`, `SETUP`, and `PLAY`, reads incoming interleaved RTP packets until the first complete intra/key frame is decoded, issues `TEARDOWN`, and returns the decoded `image.Image`  
#### `rtsnap.SnapshotJPEG`
```go
func SnapshotJPEG(ctx context.Context, rtspURL string, quality int, opts ...Option) ([]byte, error)
```
Convenience helper that captures a frame and returns JPEG bytes. For `H.264`/`H.265` it decodes via `Snapshot` and re-encodes with the specified quality (`1` to `100`). For `MJPEG` it returns the original camera bytes directly without decode and re-encode, the `quality` argument is ignored  
#### `rtsnap.Query`
```go
func Query(ctx context.Context, rtspURL string, opts ...Option) (*StreamInfo, error)
```
Connects to the RTSP stream and queries available video tracks (codec, payload type, clock rate, control URL) without initiating streaming or frame decoding  
### Functional Options  
- `WithAuth(username, password string)`: Sets credentials for HTTP Basic or Digest authentication
- `WithTimeout(d time.Duration)`: Sets a client-side timeout that bounds the total operation duration
- `WithCodec(c Codec)`: Selects a preferred video codec (`rtsnap.CodecH264`, `rtsnap.CodecH265`, `rtsnap.CodecMJPEG`, or `rtsnap.CodecAuto`). By default, `CodecAuto` selects the highest priority available codec (`H.264` > `H.265` > `MJPEG`)
- `WithFast()`: Skips loop filters (`H.264` deblocking, `H.265` deblocking and SAO) for faster decoding with negligible quality loss on single snapshots
- `WithTransport(t Transport)`: Selects RTP transport (`rtsnap.TransportTCP`, `rtsnap.TransportUDP`, `rtsnap.TransportAuto`). Defaults to TCP interleaved. `Auto` tries UDP first and falls back to TCP when the camera rejects it

## Codecs

| Codec | RFC | Profiles / Capabilities | Output Image Type | Documentation |
|---|---|---|---|---|
| **H.264 (AVC)** | [RFC 6184](https://datatracker.ietf.org/doc/html/rfc6184) | Baseline, Main, High; CABAC, CAVLC, 4x4 & 8x8 intra, deblocking | `*image.YCbCr` / `*image.NRGBA` | [pkg/h264](https://pkg.go.dev/github.com/thebadinteger/rtsnap/pkg/h264) |
| **H.265 (HEVC)** | [RFC 7798](https://datatracker.ietf.org/doc/html/rfc7798) | Main Profile; 35 intra prediction modes, SAO, transform blocks up to 32x32, SIMD kernels (AVX2/NEON/RVV) | `*image.YCbCr` / `image.Image` | [pkg/h265](https://pkg.go.dev/github.com/thebadinteger/rtsnap/pkg/h265) |
| **MJPEG** | [RFC 2435](https://datatracker.ietf.org/doc/html/rfc2435) | Standard JPEG payload header, custom and standard quantization tables | `*image.YCbCr` / `*image.Gray` | [pkg/mjpeg](https://pkg.go.dev/github.com/thebadinteger/rtsnap/pkg/mjpeg) |

## Authentication
1. **Basic Authentication (RFC 7617)**: Encoded with standard base64 credentials
2. **Digest Authentication (RFC 7616)**:
   - Supported algorithms: `MD5`, `SHA-256`, `MD5-sess`, `SHA-256-sess`
   - Supported quality of protection: `qop="auth"`
   - Automatic nonce tracking, cnonce generation, and request counter (`nc`) management

Credentials are supplied via `WithAuth("user", "pass")` or in the URL: `rtsp://user:pass@0.0.0.0:554/live`

## Specifications
| Name | Area |
|---|---|
| [RFC2326, RTSP 1.0](https://datatracker.ietf.org/doc/html/rfc2326) | protocol ([pkg/rtsp](https://pkg.go.dev/github.com/thebadinteger/rtsnap/pkg/rtsp)) |
| [RFC3550, RTP](https://datatracker.ietf.org/doc/html/rfc3550) | transport ([pkg/rtp](https://pkg.go.dev/github.com/thebadinteger/rtsnap/pkg/rtp)) |
| [RFC8866, SDP](https://datatracker.ietf.org/doc/html/rfc8866) | session description |
| [RFC6184, RTP Payload Format for H.264 Video](https://datatracker.ietf.org/doc/html/rfc6184) | payload formats / H.264 |
| [RFC7798, RTP Payload Format for HEVC](https://datatracker.ietf.org/doc/html/rfc7798) | payload formats / H.265 |
| [RFC2435, RTP Payload Format for JPEG-compressed Video](https://datatracker.ietf.org/doc/html/rfc2435) | payload formats / MJPEG |
| [RFC7616, HTTP Digest Access Authentication](https://datatracker.ietf.org/doc/html/rfc7616) | digest authentication |
| [RFC7617, HTTP Basic Authentication](https://datatracker.ietf.org/doc/html/rfc7617) | basic authentication |

## Architecture  
Library is organized in modules inside `pkg/` that can also be used independently:  
```
rtsnap/
- rtsnap.go # high-level snapshot api
- options.go # functional options
- pkg/
-- rtsp/ # rtsp 1.0 client, basic/digest auth, sdp parser
-- rtp/ # rtp packet parser, h264/h265/mjpeg depacketizers
-- h264/ # h.264 intra/idr decoder
-- h265/ # h.265 hevc intra decoder
-- mjpeg/ # mjpeg decoder
- examples/ # runnable examples
- tests/ # test suite
```

Transports: TCP interleaved (default), UDP unicast, auto UDP with TCP fallback  
`rtsps` skips TLS certificate verification because cameras use self-signed certificates

## Documentation
Docs live on **[pkg.go.dev/github.com/thebadinteger/rtsnap](https://pkg.go.dev/github.com/thebadinteger/rtsnap)**

## License  
Made by [badinteger](https://github.com/thebadinteger) `[MIT License]`  
Special thanks: **[THANKS.md](THANKS.md)**
