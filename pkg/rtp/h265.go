package rtp

import "fmt"

type H265Depacketizer struct {
	// nal unit being reassembled from fu fragments
	buf      []byte
	seq      seqState
	MaxBytes int
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

// reassemble nal units from rtp packet, the returned units do not alias the packet
func (d *H265Depacketizer) Decode(pkt *Packet) ([][]byte, error) {
	if len(pkt.Payload) < 2 {
		return nil, nil
	}

	dup, gap := d.seq.next(pkt.SequenceNumber)
	if dup {
		return nil, nil
	}
	if gap {
		d.buf = d.buf[:0]
	}

	nalType := (pkt.Payload[0] >> 1) & 0x3f

	switch {
	case pkt.Payload[0] == 0 && pkt.Payload[1] == 0:
		// a zero temporal id is not a nal header, this is annex b framing
		d.buf = d.buf[:0]
		return splitAnnexB(nil, append([]byte(nil), pkt.Payload...)), nil

	case nalType <= 47:
		d.buf = d.buf[:0]
		return [][]byte{append([]byte(nil), pkt.Payload...)}, nil

	case nalType == 48:
		d.buf = d.buf[:0]
		nalus := splitAggregate(pkt.Payload[2:])
		if len(nalus) == 0 {
			return nil, fmt.Errorf("ap packet truncated")
		}
		return nalus, nil

	case nalType == 49:
		if len(pkt.Payload) < 3 {
			return nil, fmt.Errorf("fu packet too short")
		}

		header := pkt.Payload[2]
		start := (header & 0x80) != 0
		end := (header & 0x40) != 0

		if start {
			d.buf = append(d.buf[:0], (pkt.Payload[0]&0x81)|((header&0x3f)<<1), pkt.Payload[1])
		}
		if len(d.buf) == 0 {
			return nil, nil
		}
		if len(d.buf)+len(pkt.Payload)-3 > d.maxBytes() {
			d.buf = d.buf[:0]
			return nil, fmt.Errorf("fu frame too large")
		}
		d.buf = appendFragment(d.buf, pkt.Payload[3:])

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
