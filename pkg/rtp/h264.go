package rtp

import (
	"encoding/binary"
	"fmt"
)

type H264Depacketizer struct {
	fragments [][]byte
	naluType  uint8
	nri       uint8
}

func NewH264Depacketizer() *H264Depacketizer {
	return &H264Depacketizer{}
}

// decode extracts nal units from an rtp packet
func (d *H264Depacketizer) Decode(pkt *Packet) ([][]byte, error) {
	if len(pkt.Payload) == 0 {
		return nil, nil
	}

	nalType := pkt.Payload[0] & 0x1f

	switch {
	case nalType >= 1 && nalType <= 23:
		// single nal unit
		nalu := make([]byte, len(pkt.Payload))
		copy(nalu, pkt.Payload)
		return [][]byte{nalu}, nil

	case nalType == 24:
		// stap-a aggregation packet
		var nalus [][]byte
		buf := pkt.Payload[1:]
		for len(buf) >= 2 {
			size := int(binary.BigEndian.Uint16(buf[:2]))
			buf = buf[2:]
			if len(buf) < size {
				return nil, fmt.Errorf("stap-a truncated")
			}
			nalu := make([]byte, size)
			copy(nalu, buf[:size])
			nalus = append(nalus, nalu)
			buf = buf[size:]
		}
		return nalus, nil

	case nalType == 28:
		// fu-a fragmentation unit
		if len(pkt.Payload) < 2 {
			return nil, fmt.Errorf("fu-a packet too short")
		}

		indicator := pkt.Payload[0]
		header := pkt.Payload[1]
		start := (header & 0x80) != 0
		end := (header & 0x40) != 0
		subType := header & 0x1f

		if start {
			d.fragments = d.fragments[:0]
			d.naluType = subType
			d.nri = indicator & 0x60
			d.fragments = append(d.fragments, pkt.Payload[2:])
		} else {
			if len(d.fragments) == 0 {
				// middle fragment without start
				return nil, nil
			}
			d.fragments = append(d.fragments, pkt.Payload[2:])
		}

		if end {
			totalSize := 1
			for _, f := range d.fragments {
				totalSize += len(f)
			}
			nalu := make([]byte, totalSize)
			nalu[0] = d.nri | d.naluType
			offset := 1
			for _, f := range d.fragments {
				copy(nalu[offset:], f)
				offset += len(f)
			}
			d.fragments = d.fragments[:0]
			return [][]byte{nalu}, nil
		}
		return nil, nil

	default:
		return nil, nil
	}
}
