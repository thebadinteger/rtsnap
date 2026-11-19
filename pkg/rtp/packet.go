package rtp

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

const maxAssembledBytes = 64 << 20

type Packet struct {
	Version        uint8
	Padding        bool
	Extension      bool
	Marker         bool
	PayloadType    uint8
	SequenceNumber uint16
	Timestamp      uint32
	SSRC           uint32
	// header extension, both empty when the packet carries none
	ExtensionProfile uint16
	ExtensionPayload []byte
	Payload          []byte
}

// parse raw bytes into rtp packet, the packet aliases data
func (p *Packet) Unmarshal(data []byte) error {
	if len(data) < 12 {
		return fmt.Errorf("rtp packet too short: %d bytes", len(data))
	}

	p.Version = data[0] >> 6
	if p.Version != 2 {
		return fmt.Errorf("unsupported rtp version: %d", p.Version)
	}

	p.Padding = (data[0] & 0x20) != 0
	p.Extension = (data[0] & 0x10) != 0
	csrcCount := int(data[0] & 0x0f)

	p.Marker = (data[1] & 0x80) != 0
	p.PayloadType = data[1] & 0x7f

	p.SequenceNumber = binary.BigEndian.Uint16(data[2:4])
	p.Timestamp = binary.BigEndian.Uint32(data[4:8])
	p.SSRC = binary.BigEndian.Uint32(data[8:12])

	offset := 12 + csrcCount*4
	if len(data) < offset {
		return fmt.Errorf("rtp packet truncated reading csrc")
	}

	p.ExtensionProfile, p.ExtensionPayload = 0, nil
	if p.Extension {
		if len(data) < offset+4 {
			return fmt.Errorf("rtp packet truncated reading extension")
		}
		extLen := int(binary.BigEndian.Uint16(data[offset+2:offset+4])) * 4
		if len(data) < offset+4+extLen {
			return fmt.Errorf("rtp packet truncated reading extension data")
		}
		p.ExtensionProfile = binary.BigEndian.Uint16(data[offset : offset+2])
		p.ExtensionPayload = data[offset+4 : offset+4+extLen]
		offset += 4 + extLen
	}

	payload := data[offset:]
	if p.Padding && len(payload) > 0 {
		padLen := int(payload[len(payload)-1])
		if padLen == 0 || padLen > len(payload) {
			return fmt.Errorf("invalid rtp padding length: %d", padLen)
		}
		payload = payload[:len(payload)-padLen]
	}

	p.Payload = payload
	return nil
}

// sequence continuity shared by the depacketizers
type seqState struct {
	last uint16
	set  bool
}

// dup reports a repeated packet, gap a break since the previous one
func (s *seqState) next(seq uint16) (dup, gap bool) {
	if s.set && seq == s.last {
		return true, false
	}
	gap = s.set && seq != s.last+1
	s.last, s.set = seq, true
	return false, gap
}

func appendFragment(buf, data []byte) []byte {
	if need := len(buf) + len(data); need > cap(buf) {
		grown := make([]byte, len(buf), max(need, 2*cap(buf), 4096))
		copy(grown, buf)
		buf = grown
	}
	return append(buf, data...)
}

var startCode = []byte{0, 0, 1}

// append the start code separated units of b to out, b itself when it has none
func splitAnnexB(out [][]byte, b []byte) [][]byte {
	for len(b) > 0 {
		i := bytes.Index(b, startCode)
		if i < 0 {
			return append(out, b)
		}
		end := i
		for end > 0 && b[end-1] == 0 {
			end--
		}
		if end > 0 {
			out = append(out, b[:end:end])
		}
		b = b[i+3:]
	}
	return out
}

// read the size prefixed units of an aggregation packet
func splitAggregate(buf []byte) [][]byte {
	var nalus [][]byte
	for len(buf) > 2 {
		size := int(binary.BigEndian.Uint16(buf))
		buf = buf[2:]
		if size == 0 || size > len(buf) {
			break
		}
		nalus = append(nalus, append([]byte(nil), buf[:size]...))
		buf = buf[size:]
	}
	return nalus
}
