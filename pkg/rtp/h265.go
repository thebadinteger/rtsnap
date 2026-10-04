package rtp

import (
	"encoding/binary"
	"fmt"
)

type H265Depacketizer struct {
	fragments [][]byte
	naluType  uint8
	hdr0      uint8
	hdr1      uint8
	lastSeq   uint16
	hasSeq    bool
	fragBytes int
	MaxBytes  int
}

func (d *H265Depacketizer) maxBytes() int {
	if d.MaxBytes > 0 {
		return d.MaxBytes
	}
	return maxAssembledBytes
}

// fresh h265 depacketizer state
func NewH265Depacketizer() *H265Depacketizer {
	return &H265Depacketizer{}
}

// reassemble nal units from rtp packet
func (d *H265Depacketizer) Decode(pkt *Packet) ([][]byte, error) {
	if len(pkt.Payload) < 2 {
		return nil, nil
	}

	nalType := (pkt.Payload[0] >> 1) & 0x3f

	if d.hasSeq && pkt.SequenceNumber == d.lastSeq {
		return nil, nil
	}
	if d.hasSeq && pkt.SequenceNumber-d.lastSeq != 1 {
		d.fragments = d.fragments[:0]
	}
	d.lastSeq = pkt.SequenceNumber
	d.hasSeq = true

	switch {
	case nalType <= 47:
		d.fragments = d.fragments[:0]
		nalu := make([]byte, len(pkt.Payload))
		copy(nalu, pkt.Payload)
		return [][]byte{nalu}, nil

	case nalType == 48:
		d.fragments = d.fragments[:0]
		var nalus [][]byte
		buf := pkt.Payload[2:]
		for len(buf) >= 2 {
			size := int(binary.BigEndian.Uint16(buf[:2]))
			buf = buf[2:]
			if len(buf) < size {
				return nil, fmt.Errorf("ap packet truncated")
			}
			nalu := make([]byte, size)
			copy(nalu, buf[:size])
			nalus = append(nalus, nalu)
			buf = buf[size:]
		}
		return nalus, nil

	case nalType == 49:
		if len(pkt.Payload) < 3 {
			return nil, fmt.Errorf("fu packet too short")
		}

		header := pkt.Payload[2]
		start := (header & 0x80) != 0
		end := (header & 0x40) != 0
		subType := header & 0x3f

		if start {
			d.fragments = d.fragments[:0]
			d.naluType = subType
			d.hdr0 = (pkt.Payload[0] & 0x81) | (subType << 1)
			d.hdr1 = pkt.Payload[1]
			d.fragBytes = 0
		}
		if len(d.fragments) == 0 && !start {
			return nil, nil
		}
		if d.fragBytes+len(pkt.Payload)-3 > d.maxBytes() {
			d.fragments = d.fragments[:0]
			d.fragBytes = 0
			return nil, fmt.Errorf("fu frame too large")
		}
		d.fragments = append(d.fragments, pkt.Payload[3:])
		d.fragBytes += len(pkt.Payload) - 3

		if end {
			totalSize := 2
			for _, f := range d.fragments {
				totalSize += len(f)
			}
			nalu := make([]byte, totalSize)
			nalu[0] = d.hdr0
			nalu[1] = d.hdr1
			offset := 2
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
