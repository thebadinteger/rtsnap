package rtp

import "fmt"

type H264Depacketizer struct {
	// nal unit being reassembled from fu-a fragments
	buf      []byte
	seq      seqState
	MaxBytes int
}

func (d *H264Depacketizer) maxBytes() int {
	if d.MaxBytes > 0 {
		return d.MaxBytes
	}
	return maxAssembledBytes
}

// fresh h264 depacketizer state
func NewH264Depacketizer() *H264Depacketizer {
	return &H264Depacketizer{}
}

// reassemble nal units from rtp packet, the returned units do not alias the packet
func (d *H264Depacketizer) Decode(pkt *Packet) ([][]byte, error) {
	if len(pkt.Payload) == 0 {
		return nil, nil
	}

	dup, gap := d.seq.next(pkt.SequenceNumber)
	if dup {
		return nil, nil
	}
	if gap {
		d.buf = d.buf[:0]
	}

	nalType := pkt.Payload[0] & 0x1f

	switch {
	case nalType >= 1 && nalType <= 23:
		d.buf = d.buf[:0]
		return [][]byte{append([]byte(nil), pkt.Payload...)}, nil

	case nalType == 0:
		// annex b framing inside the payload starts with a zero byte
		d.buf = d.buf[:0]
		return splitAnnexB(nil, append([]byte(nil), pkt.Payload...)), nil

	case nalType == 24:
		d.buf = d.buf[:0]
		nalus := splitAggregate(pkt.Payload[1:])
		if len(nalus) == 0 {
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

		if start {
			d.buf = append(d.buf[:0], (indicator&0xe0)|(header&0x1f))
		}
		if len(d.buf) == 0 {
			return nil, nil
		}
		if len(d.buf)+len(pkt.Payload)-2 > d.maxBytes() {
			d.buf = d.buf[:0]
			return nil, fmt.Errorf("fu-a frame too large")
		}
		d.buf = appendFragment(d.buf, pkt.Payload[2:])

		if !end {
			return nil, nil
		}
		// the caller owns the unit from here on
		nalu := d.buf
		d.buf = nil
		return splitAnnexB(nil, nalu), nil

	default:
		return nil, nil
	}
}
