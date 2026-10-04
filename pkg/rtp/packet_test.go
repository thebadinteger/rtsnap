package rtp

import (
	"bytes"
	"image/jpeg"
	"testing"
)

func TestRTPPacketUnmarshal(t *testing.T) {
	raw := []byte{
		0x80, 0x60, 0x12, 0x34,
		0x00, 0x01, 0x02, 0x03,
		0x11, 0x22, 0x33, 0x44,
		0xAA, 0xBB, 0xCC,
	}

	var p Packet
	if err := p.Unmarshal(raw); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if p.Version != 2 {
		t.Errorf("expected version 2, got %d", p.Version)
	}
	if p.PayloadType != 96 {
		t.Errorf("expected payload type 96, got %d", p.PayloadType)
	}
	if p.SequenceNumber != 0x1234 {
		t.Errorf("expected seq 0x1234, got 0x%x", p.SequenceNumber)
	}
	if p.Timestamp != 0x00010203 {
		t.Errorf("expected timestamp 0x00010203, got 0x%x", p.Timestamp)
	}
	if p.SSRC != 0x11223344 {
		t.Errorf("expected ssrc 0x11223344, got 0x%x", p.SSRC)
	}
	if !bytes.Equal(p.Payload, []byte{0xAA, 0xBB, 0xCC}) {
		t.Errorf("payload mismatch: %v", p.Payload)
	}
}

func TestH264DepacketizerFUA(t *testing.T) {
	d := NewH264Depacketizer()

	pkt1 := &Packet{
		SequenceNumber: 1,
		Payload: []byte{
			0x7c,
			0x85,
			0x01, 0x02,
		},
	}
	nalus, err := d.Decode(pkt1)
	if err != nil {
		t.Fatalf("decode pkt1: %v", err)
	}
	if len(nalus) != 0 {
		t.Fatalf("expected 0 nalus on start fragment, got %d", len(nalus))
	}

	pkt2 := &Packet{
		Marker:         true,
		SequenceNumber: 2,
		Payload: []byte{
			0x7c,
			0x45,
			0x03, 0x04,
		},
	}
	nalus, err = d.Decode(pkt2)
	if err != nil {
		t.Fatalf("decode pkt2: %v", err)
	}
	if len(nalus) != 1 {
		t.Fatalf("expected 1 assembled nalu, got %d", len(nalus))
	}

	expected := []byte{0x65, 0x01, 0x02, 0x03, 0x04}
	if !bytes.Equal(nalus[0], expected) {
		t.Fatalf("nalu mismatch: got %v, want %v", nalus[0], expected)
	}
}

func TestH264FUGapRecovery(t *testing.T) {
	d := NewH264Depacketizer()

	frag := func(seq uint16, hdr byte, data ...byte) *Packet {
		return &Packet{
			SequenceNumber: seq,
			Payload:        append([]byte{0x7c, hdr}, data...),
		}
	}

	if nalus, _ := d.Decode(frag(1, 0x85, 0x01)); len(nalus) != 0 {
		t.Fatalf("expected 0 nalus on start, got %d", len(nalus))
	}
	if nalus, _ := d.Decode(frag(3, 0x05, 0x02)); len(nalus) != 0 {
		t.Fatalf("expected 0 nalus on gap fragment, got %d", len(nalus))
	}
	if nalus, _ := d.Decode(frag(3, 0x05, 0x02)); len(nalus) != 0 {
		t.Fatalf("expected duplicate to be dropped, got %d nalus", len(nalus))
	}
	if nalus, _ := d.Decode(frag(4, 0x85, 0x0a)); len(nalus) != 0 {
		t.Fatalf("expected 0 nalus on fresh start, got %d", len(nalus))
	}
	nalus, _ := d.Decode(frag(5, 0x45, 0x0b))
	if len(nalus) != 1 {
		t.Fatalf("expected recovered nalu, got %d", len(nalus))
	}
	expected := []byte{0x65, 0x0a, 0x0b}
	if !bytes.Equal(nalus[0], expected) {
		t.Fatalf("recovered nalu mismatch: got %v, want %v", nalus[0], expected)
	}
}

func TestH265DepacketizerFU(t *testing.T) {
	d := NewH265Depacketizer()

	pkt1 := &Packet{
		SequenceNumber: 1,
		Payload: []byte{
			49 << 1, 0x01,
			0x80 | 19,
			0x10, 0x20,
		},
	}
	nalus, err := d.Decode(pkt1)
	if err != nil {
		t.Fatalf("decode pkt1: %v", err)
	}
	if len(nalus) != 0 {
		t.Fatalf("expected 0 nalus on start fragment, got %d", len(nalus))
	}

	pkt2 := &Packet{
		Marker:         true,
		SequenceNumber: 2,
		Payload: []byte{
			49 << 1, 0x01,
			0x40 | 19,
			0x30, 0x40,
		},
	}
	nalus, err = d.Decode(pkt2)
	if err != nil {
		t.Fatalf("decode pkt2: %v", err)
	}
	if len(nalus) != 1 {
		t.Fatalf("expected 1 assembled nalu, got %d", len(nalus))
	}

	expectedHdr0 := byte((49 << 1 & 0x81) | (19 << 1))
	if nalus[0][0] != expectedHdr0 || nalus[0][1] != 0x01 {
		t.Fatalf("invalid reconstructed h265 header: %02x %02x", nalus[0][0], nalus[0][1])
	}
}

func TestH265FUGapRecovery(t *testing.T) {
	d := NewH265Depacketizer()

	frag := func(seq uint16, hdr byte, data ...byte) *Packet {
		return &Packet{
			SequenceNumber: seq,
			Payload:        append([]byte{49 << 1, 0x01, hdr}, data...),
		}
	}

	if nalus, _ := d.Decode(frag(1, 0x80|19, 0x10)); len(nalus) != 0 {
		t.Fatalf("expected 0 nalus on start, got %d", len(nalus))
	}
	if nalus, _ := d.Decode(frag(3, 19, 0x20)); len(nalus) != 0 {
		t.Fatalf("expected 0 nalus on gap fragment, got %d", len(nalus))
	}
	if nalus, _ := d.Decode(frag(4, 0x80|19, 0x30)); len(nalus) != 0 {
		t.Fatalf("expected 0 nalus on fresh start, got %d", len(nalus))
	}
	nalus, _ := d.Decode(frag(5, 0x40|19, 0x40))
	if len(nalus) != 1 {
		t.Fatalf("expected recovered nalu, got %d", len(nalus))
	}
	if len(nalus[0]) != 4 || !bytes.Equal(nalus[0][2:], []byte{0x30, 0x40}) {
		t.Fatalf("recovered nalu mismatch: %v", nalus[0])
	}
}

func TestMJPEGDepacketizer(t *testing.T) {
	d := NewMJPEGDepacketizer()

	pkt := &Packet{
		Marker: true,
		Payload: []byte{
			0x00,
			0x00, 0x00, 0x00,
			0x01,
			50,
			2,
			2,
			0x00, 0x00,
		},
	}

	jpegBytes, err := d.Decode(pkt)
	if err != nil {
		t.Fatalf("decode mjpeg rtp: %v", err)
	}
	if len(jpegBytes) == 0 {
		t.Fatal("expected jpeg bytes")
	}

	if jpegBytes[0] != 0xFF || jpegBytes[1] != 0xD8 {
		t.Fatalf("invalid jpeg start: %02x %02x", jpegBytes[0], jpegBytes[1])
	}
	if jpegBytes[len(jpegBytes)-2] != 0xFF || jpegBytes[len(jpegBytes)-1] != 0xD9 {
		t.Fatalf("invalid jpeg end: %02x %02x", jpegBytes[len(jpegBytes)-2], jpegBytes[len(jpegBytes)-1])
	}

	_, err = jpeg.Decode(bytes.NewReader(jpegBytes))
	t.Logf("jpeg constructed size: %d bytes, decode status: %v", len(jpegBytes), err)
}

func TestMJPEGOffsetGapRecovery(t *testing.T) {
	d := NewMJPEGDepacketizer()

	mk := func(seq uint16, marker bool, offset int, data string) *Packet {
		return &Packet{
			Marker:         marker,
			SequenceNumber: seq,
			Payload: append([]byte{
				0x00, byte(offset >> 16), byte(offset >> 8), byte(offset),
				0x01, 50, 2, 2,
			}, []byte(data)...),
		}
	}

	if out, _ := d.Decode(mk(1, false, 0, "AA")); len(out) != 0 {
		t.Fatalf("expected 0 bytes on first fragment, got %d", len(out))
	}
	if out, _ := d.Decode(mk(3, false, 10, "BB")); len(out) != 0 {
		t.Fatalf("expected gap fragment to be dropped, got %d bytes", len(out))
	}
	out, err := d.Decode(mk(4, true, 0, "CC"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected recovered jpeg frame")
	}
	if out[0] != 0xFF || out[1] != 0xD8 {
		t.Fatalf("invalid jpeg start: %02x %02x", out[0], out[1])
	}
}
