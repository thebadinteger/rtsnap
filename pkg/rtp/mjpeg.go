package rtp

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

var lumaQuantizers = []int{
	0x10, 0x0b, 0x0c, 0x0e, 0x0c, 0x0a, 0x10, 0x0e,
	0x0d, 0x0e, 0x12, 0x11, 0x10, 0x13, 0x18, 0x28,
	0x1a, 0x18, 0x16, 0x16, 0x18, 0x31, 0x23, 0x25,
	0x1d, 0x28, 0x3a, 0x33, 0x3d, 0x3c, 0x39, 0x33,
	0x38, 0x37, 0x40, 0x48, 0x5c, 0x4e, 0x40, 0x44,
	0x57, 0x45, 0x37, 0x38, 0x50, 0x6d, 0x51, 0x57,
	0x5f, 0x62, 0x67, 0x68, 0x67, 0x3e, 0x4d, 0x71,
	0x79, 0x70, 0x64, 0x78, 0x5c, 0x65, 0x67, 0x63,
}

var chromaQuantizers = []int{
	0x11, 0x12, 0x12, 0x18, 0x15, 0x18, 0x2f, 0x1a,
	0x1a, 0x2f, 0x63, 0x42, 0x38, 0x42, 0x63, 0x63,
	0x63, 0x63, 0x63, 0x63, 0x63, 0x63, 0x63, 0x63,
	0x63, 0x63, 0x63, 0x63, 0x63, 0x63, 0x63, 0x63,
	0x63, 0x63, 0x63, 0x63, 0x63, 0x63, 0x63, 0x63,
	0x63, 0x63, 0x63, 0x63, 0x63, 0x63, 0x63, 0x63,
	0x63, 0x63, 0x63, 0x63, 0x63, 0x63, 0x63, 0x63,
	0x63, 0x63, 0x63, 0x63, 0x63, 0x63, 0x63, 0x63,
}

var lumDcCodeLens = []byte{0, 1, 5, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0}
var lumDcSymbols = []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
var lumAcCodeLens = []byte{0, 2, 1, 3, 3, 2, 4, 3, 5, 5, 4, 4, 0, 0, 1, 0x7d}
var lumAcSymbols = []byte{
	0x01, 0x02, 0x03, 0x00, 0x04, 0x11, 0x05, 0x12,
	0x21, 0x31, 0x41, 0x06, 0x13, 0x51, 0x61, 0x07,
	0x22, 0x71, 0x14, 0x32, 0x81, 0x91, 0xa1, 0x08,
	0x23, 0x42, 0xb1, 0xc1, 0x15, 0x52, 0xd1, 0xf0,
	0x24, 0x33, 0x62, 0x72, 0x82, 0x09, 0x0a, 0x16,
	0x17, 0x18, 0x19, 0x1a, 0x25, 0x26, 0x27, 0x28,
	0x29, 0x2a, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39,
	0x3a, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49,
	0x4a, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59,
	0x5a, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69,
	0x6a, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79,
	0x7a, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89,
	0x8a, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98,
	0x99, 0x9a, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7,
	0xa8, 0xa9, 0xaa, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6,
	0xb7, 0xb8, 0xb9, 0xba, 0xc2, 0xc3, 0xc4, 0xc5,
	0xc6, 0xc7, 0xc8, 0xc9, 0xca, 0xd2, 0xd3, 0xd4,
	0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda, 0xe1, 0xe2,
	0xe3, 0xe4, 0xe5, 0xe6, 0xe7, 0xe8, 0xe9, 0xea,
	0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8,
	0xf9, 0xfa,
}
var chmDcCodeLens = []byte{0, 3, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0}
var chmDcSymbols = []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
var chmAcCodeLens = []byte{0, 2, 1, 2, 4, 4, 3, 4, 7, 5, 4, 4, 0, 1, 2, 0x77}
var chmAcSymbols = []byte{
	0x00, 0x01, 0x02, 0x03, 0x11, 0x04, 0x05, 0x21,
	0x31, 0x06, 0x12, 0x41, 0x51, 0x07, 0x61, 0x71,
	0x13, 0x22, 0x32, 0x81, 0x08, 0x14, 0x42, 0x91,
	0xa1, 0xb1, 0xc1, 0x09, 0x23, 0x33, 0x52, 0xf0,
	0x15, 0x62, 0x72, 0xd1, 0x0a, 0x16, 0x24, 0x34,
	0xe1, 0x25, 0xf1, 0x17, 0x18, 0x19, 0x1a, 0x26,
	0x27, 0x28, 0x29, 0x2a, 0x35, 0x36, 0x37, 0x38,
	0x39, 0x3a, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48,
	0x49, 0x4a, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58,
	0x59, 0x5a, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68,
	0x69, 0x6a, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
	0x79, 0x7a, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87,
	0x88, 0x89, 0x8a, 0x92, 0x93, 0x94, 0x95, 0x96,
	0x97, 0x98, 0x99, 0x9a, 0xa2, 0xa3, 0xa4, 0xa5,
	0xa6, 0xa7, 0xa8, 0xa9, 0xaa, 0xb2, 0xb3, 0xb4,
	0xb5, 0xb6, 0xb7, 0xb8, 0xb9, 0xba, 0xc2, 0xc3,
	0xc4, 0xc5, 0xc6, 0xc7, 0xc8, 0xc9, 0xca, 0xd2,
	0xd3, 0xd4, 0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda,
	0xe2, 0xe3, 0xe4, 0xe5, 0xe6, 0xe7, 0xe8, 0xe9,
	0xea, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8,
	0xf9, 0xfa,
}

// a quantization table and its precision bit from the rtp header
type quantTable struct {
	data []byte
	prec bool
}

func makeQuantTables(q uint8) []quantTable {
	var scale int
	if q < 50 {
		scale = 5000 / int(q)
	} else {
		scale = 200 - 2*int(q)
	}

	tables := make([]quantTable, 2)

	for id := range tables {
		base := lumaQuantizers
		if id == 1 {
			base = chromaQuantizers
		}

		t := make([]byte, 64)
		for i := 0; i < 64; i++ {
			v := (base[i]*scale + 50) / 100
			if v > 255 {
				v = 255
			} else if v == 0 {
				v = 1
			}
			t[i] = byte(v)
		}

		tables[id] = quantTable{data: t}
	}

	return tables
}

// onvif header extension ids that carry jpeg marker segments
const (
	onvifJPEGFirst  = 0xFFD8
	onvifReplay     = 0xABAC
	onvifReplaySize = 12
)

type MJPEGDepacketizer struct {
	// jpeg under construction, headers first and scan data after hdrLen
	out         []byte
	hdrLen      int
	quantTables []quantTable
	quantQ      uint8
	seq         seqState
	MaxBytes    int
}

func (d *MJPEGDepacketizer) maxBytes() int {
	if d.MaxBytes > 0 {
		return d.MaxBytes
	}
	return maxAssembledBytes
}

// fresh mjpeg depacketizer state
func NewMJPEGDepacketizer() *MJPEGDepacketizer {
	return &MJPEGDepacketizer{}
}

// forget the frame under construction
func (d *MJPEGDepacketizer) drop() {
	d.out = d.out[:0]
	d.hdrLen = 0
}

// jpeg marker segments of an onvif header extension, nil when the packet has none
func onvifSegments(pkt *Packet) []byte {
	ext := pkt.ExtensionPayload
	if pkt.ExtensionProfile == onvifReplay && len(ext) >= onvifReplaySize+4 {
		// the jpeg extension is appended to the replay extension
		n := onvifReplaySize + 4 + (int(ext[onvifReplaySize+2])<<8|int(ext[onvifReplaySize+3]))*4
		if ext[onvifReplaySize] == 0xFF && ext[onvifReplaySize+1] == 0xD8 && n <= len(ext) {
			return ext[onvifReplaySize+4 : n]
		}
		return nil
	}
	if pkt.ExtensionProfile == onvifJPEGFirst {
		return ext
	}
	return nil
}

// reassemble jpeg frame from rtp packet
func (d *MJPEGDepacketizer) Decode(pkt *Packet) ([]byte, error) {
	buf := pkt.Payload
	if len(buf) < 8 {
		return nil, fmt.Errorf("mjpeg packet too short")
	}

	offset := int(uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3]))
	jpegType := buf[4]
	q := buf[5]
	w := int(buf[6]) * 8
	h := int(buf[7]) * 8
	buf = buf[8:]

	if jpegType >= 128 {
		return nil, fmt.Errorf("unsupported mjpeg type %d", jpegType)
	}

	var restartInterval uint16
	if jpegType >= 64 {
		if len(buf) < 4 {
			return nil, fmt.Errorf("missing restart marker header")
		}
		restartInterval = binary.BigEndian.Uint16(buf[:2])
		buf = buf[4:]
	}

	dup, gap := d.seq.next(pkt.SequenceNumber)
	if dup {
		return nil, nil
	}

	if offset == 0 {
		d.drop()

		if q >= 128 {
			if len(buf) < 4 {
				return nil, fmt.Errorf("missing quant table header")
			}
			precision := buf[1]
			tableLen := int(binary.BigEndian.Uint16(buf[2:4]))
			buf = buf[4:]
			if tableLen > 0 {
				if len(buf) < tableLen {
					return nil, fmt.Errorf("truncated quant tables")
				}
				tablesData := buf[:tableLen]
				buf = buf[tableLen:]

				tables := make([]quantTable, 0, 2)
				for id := 0; id < 2 && len(tablesData) > 0; id++ {
					n := 64
					if precision&(1<<uint(id)) != 0 {
						n = 128
					}
					if len(tablesData) < n {
						return nil, fmt.Errorf("truncated quant tables")
					}

					t := make([]byte, n)
					copy(t, tablesData[:n])
					tables = append(tables, quantTable{data: t, prec: precision&(1<<uint(id)) != 0})
					tablesData = tablesData[n:]
				}

				d.quantTables = tables
			}
			d.quantQ = 0
		} else if q = min(max(q, 1), 99); q != d.quantQ {
			d.quantTables = makeQuantTables(q)
			d.quantQ = q
		}

		if len(buf) > d.maxBytes() {
			return nil, fmt.Errorf("mjpeg frame too large")
		}
		if err := d.writeHeaders(onvifSegments(pkt), jpegType, w, h, restartInterval); err != nil {
			d.drop()
			return nil, err
		}
	} else {
		if d.hdrLen == 0 || gap || offset != len(d.out)-d.hdrLen {
			d.drop()
			return nil, nil
		}
		if offset+len(buf) > d.maxBytes() {
			d.drop()
			return nil, fmt.Errorf("mjpeg frame too large")
		}
	}
	d.out = appendFragment(d.out, buf)

	if !pkt.Marker {
		return nil, nil
	}

	// the caller owns the frame from here on
	out, scan := d.out, d.hdrLen
	d.out, d.hdrLen = nil, 0

	if i := bytes.Index(out[scan:], eoiMarker); i >= 0 {
		return out[:scan+i+2], nil
	}
	return append(out, eoiMarker...), nil
}

var eoiMarker = []byte{0xFF, 0xD9}

// start a frame with the jpeg headers, segments of an onvif extension win over derived ones
func (d *MJPEGDepacketizer) writeHeaders(ext []byte, jpegType uint8, w, h int, restartInterval uint16) error {
	out := append(d.out[:0], 0xFF, 0xD8)

	var sos []byte
	var haveDQT, haveSOF, haveDHT, haveDRI bool
	for len(ext) >= 4 && ext[0] == 0xFF {
		if ext[1] == 0xFF {
			// fill byte padding
			ext = ext[1:]
			continue
		}
		n := 2 + (int(ext[2])<<8 | int(ext[3]))
		if n < 4 || n > len(ext) {
			break
		}
		switch ext[1] {
		case 0xDA:
			sos = ext[:n]
		case 0xDB:
			haveDQT = true
		case 0xC0, 0xC1, 0xC2:
			haveSOF = true
		case 0xC4:
			haveDHT = true
		case 0xDD:
			haveDRI = true
		}
		if ext[1] != 0xDA {
			out = append(out, ext[:n]...)
		}
		ext = ext[n:]
	}

	if !haveDQT {
		tables := d.quantTables
		if len(tables) == 0 {
			return fmt.Errorf("mjpeg frame without quant tables")
		}
		if len(tables) == 1 {
			tables = []quantTable{tables[0], tables[0]}
		}

		dqtLen := 2
		for _, t := range tables {
			dqtLen += 1 + len(t.data)
		}

		out = append(out, 0xFF, 0xDB, byte(dqtLen>>8), byte(dqtLen))
		for id, t := range tables {
			flags := byte(id)
			if t.prec {
				flags |= 0x10
			}
			out = append(out, flags)
			out = append(out, t.data...)
		}
	}

	if !haveSOF {
		// sizes beyond 2040 pixels do not fit the rtp header and need the extension
		if w == 0 || h == 0 {
			return fmt.Errorf("mjpeg frame without dimensions")
		}

		// restart marker types 64-127 mirror the sampling of types 0-63
		sampling := byte(0x11)
		switch jpegType & 0x3f {
		case 0:
			sampling = 0x21
		case 1:
			sampling = 0x22
		}
		out = append(out, 0xFF, 0xC0, 0x00, 0x11, 0x08,
			byte(h>>8), byte(h), byte(w>>8), byte(w), 0x03,
			0x01, sampling, 0x00,
			0x02, 0x11, 0x01,
			0x03, 0x11, 0x01)
	}

	if !haveDHT {
		out = appendDHT(out, 0x00, lumDcCodeLens, lumDcSymbols)
		out = appendDHT(out, 0x10, lumAcCodeLens, lumAcSymbols)
		out = appendDHT(out, 0x01, chmDcCodeLens, chmDcSymbols)
		out = appendDHT(out, 0x11, chmAcCodeLens, chmAcSymbols)
	}

	if !haveDRI && restartInterval > 0 {
		out = append(out, 0xFF, 0xDD, 0x00, 0x04, byte(restartInterval>>8), byte(restartInterval))
	}

	if sos != nil {
		out = append(out, sos...)
	} else {
		out = append(out, 0xFF, 0xDA, 0x00, 0x0C, 0x03,
			0x01, 0x00, 0x02, 0x11, 0x03, 0x11,
			0x00, 0x3F, 0x00)
	}

	d.out = out
	d.hdrLen = len(out)
	return nil
}

func appendDHT(out []byte, classID byte, lens, symbols []byte) []byte {
	l := 2 + 1 + len(lens) + len(symbols)
	out = append(out, 0xFF, 0xC4, byte(l>>8), byte(l), classID)
	out = append(out, lens...)
	return append(out, symbols...)
}
