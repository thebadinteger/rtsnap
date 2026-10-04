package rtp

import (
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
	Payload        []byte
}

// parse raw bytes into rtp packet
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

	if p.Extension {
		if len(data) < offset+4 {
			return fmt.Errorf("rtp packet truncated reading extension")
		}
		extLen := int(binary.BigEndian.Uint16(data[offset+2:offset+4])) * 4
		offset += 4 + extLen
		if len(data) < offset {
			return fmt.Errorf("rtp packet truncated reading extension data")
		}
	}

	payload := data[offset:]
	if p.Padding && len(payload) > 0 {
		padLen := int(payload[len(payload)-1])
		if padLen <= len(payload) {
			payload = payload[:len(payload)-padLen]
		}
	}

	p.Payload = payload
	return nil
}
