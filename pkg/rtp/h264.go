package rtp

import (
	"encoding/binary"
	"fmt"
)

type H264Depacketizer struct {
	fragments [][]byte
	naluType  uint8
	nri       uint8
	fbit      uint8
	lastSeq   uint16
	hasSeq    bool
	fragBytes int
	MaxBytes  int
}

func (d *H264Depacketizer) maxBytes() int {
	if d.MaxBytes > 0 {
		return d.MaxBytes
	}
	return maxAssembledBytes
}

// drop collected fragments and release their backing buffers
func (d *H264Depacketizer) dropFragments() {
	for i := range d.fragments {
		d.fragments[i] = nil
	}
	d.fragments = d.fragments[:0]
	d.fragBytes = 0
}

// fresh h264 depacketizer state
func NewH264Depacketizer() *H264Depacketizer {
	return &H264Depacketizer{}
}

// reassemble nal units from rtp packet
func (d *H264Depacketizer) Decode(pkt *Packet) ([][]byte, error) {
	if len(pkt.Payload) == 0 {
		return nil, nil
	}

	nalType := pkt.Payload[0] & 0x1f

	if d.hasSeq && pkt.SequenceNumber == d.lastSeq {
		return nil, nil
	}
	if d.hasSeq && pkt.SequenceNumber-d.lastSeq != 1 {
		d.dropFragments()
	}
	d.lastSeq = pkt.SequenceNumber
	d.hasSeq = true

	switch {
	case nalType >= 1 && nalType <= 23:
		d.dropFragments()
		nalu := make([]byte, len(pkt.Payload))
		copy(nalu, pkt.Payload)
		return [][]byte{nalu}, nil

	case nalType == 24:
		d.dropFragments()
		var nalus [][]byte
		buf := pkt.Payload[1:]
		for len(buf) >= 2 {
			size := int(binary.BigEndian.Uint16(buf[:2]))
			buf = buf[2:]
			if size == 0 || len(buf) < size {
				return nil, fmt.Errorf("stap-a truncated")
			}
			nalu := make([]byte, size)
			copy(nalu, buf[:size])
			nalus = append(nalus, nalu)
			buf = buf[size:]
		}
		if len(buf) > 0 {
			return nil, fmt.Errorf("stap-a truncated")
		}
		return nalus, nil

	case nalType == 28:
		if len(pkt.Payload) < 2 {
			return nil, fmt.Errorf("fu-a packet too short")
		}

		indicator := pkt.Payload[0]
		header := pkt.Payload[1]
		start := (header & 0x80) != 0
		end := (header & 0x40) != 0
		subType := header & 0x1f

		if start {
			d.dropFragments()
			d.naluType = subType
			d.nri = indicator & 0x60
			d.fbit = indicator & 0x80
		}
		if len(d.fragments) == 0 && !start {
			return nil, nil
		}
		if d.fragBytes+len(pkt.Payload)-2 > d.maxBytes() {
			d.dropFragments()
			return nil, fmt.Errorf("fu-a frame too large")
		}

		frag := make([]byte, len(pkt.Payload)-2)
		copy(frag, pkt.Payload[2:])
		d.fragments = append(d.fragments, frag)
		d.fragBytes += len(frag)

		if end {
			totalSize := 1
			for _, f := range d.fragments {
				totalSize += len(f)
			}
			nalu := make([]byte, totalSize)
			nalu[0] = d.fbit | d.nri | d.naluType
			offset := 1
			for _, f := range d.fragments {
				copy(nalu[offset:], f)
				offset += len(f)
			}
			d.dropFragments()
			return [][]byte{nalu}, nil
		}
		return nil, nil

	default:
		return nil, nil
	}
}
