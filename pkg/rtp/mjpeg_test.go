package rtp

import (
	"testing"
)

func mjpegFrame(t *testing.T, d *MJPEGDepacketizer, seq uint16, typ, q byte, w, h int,
	payload []byte, marker bool,
) []byte {
	t.Helper()

	hdr := []byte{0, 0, 0, 0, typ, q, byte(w / 8), byte(h / 8)}

	if typ >= 64 {
		// restart marker header lives in every packet payload
		hdr = append(hdr, 0, 1, 0, 0)
	}

	pkt := &Packet{Payload: append(hdr, payload...), Marker: marker, SequenceNumber: seq}

	out, err := d.Decode(pkt)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	return out
}

func findMarker(out []byte, m0, m1 byte) int {
	for i := 0; i+1 < len(out); i++ {
		if out[i] == m0 && out[i+1] == m1 {
			return i
		}
	}

	return -1
}

func TestMJPEGZeroQ(t *testing.T) {
	d := NewMJPEGDepacketizer()

	// q 0 is invalid and used to divide by zero
	out := mjpegFrame(t, d, 1, 1, 0, 16, 16, []byte{0xFF, 0xD9}, true)
	if findMarker(out, 0xFF, 0xD8) != 0 {
		t.Fatal("missing soi")
	}
}

func TestMJPEGRestartTypeSampling(t *testing.T) {
	cases := []struct {
		typ      byte
		sampling byte
	}{
		{0, 0x21},
		{1, 0x22},
		{64, 0x21},
		{65, 0x22},
	}

	for _, c := range cases {
		d := NewMJPEGDepacketizer()

		out := mjpegFrame(t, d, 1, c.typ, 50, 16, 16, []byte{0xFF, 0xD9}, true)
		sof := findMarker(out, 0xFF, 0xC0)
		if sof < 0 || out[sof+11] != c.sampling {
			t.Fatalf("type %d: sampling 0x%02x, want 0x%02x", c.typ, out[sof+11], c.sampling)
		}

		if c.typ >= 64 && findMarker(out, 0xFF, 0xDD) < 0 {
			t.Fatalf("type %d: missing dri", c.typ)
		}
		if c.typ < 64 && findMarker(out, 0xFF, 0xDD) >= 0 {
			t.Fatalf("type %d: unexpected dri", c.typ)
		}
	}
}

func TestMJPEGDynamicTypeRejected(t *testing.T) {
	d := NewMJPEGDepacketizer()

	if _, err := d.Decode(&Packet{Payload: []byte{0, 0, 0, 0, 0x80, 50, 2, 2}, Marker: true}); err == nil {
		t.Fatal("expected error for dynamic type, got nil")
	}
}

func TestMJPEGCustomQReusesTables(t *testing.T) {
	d := NewMJPEGDepacketizer()

	tables := make([]byte, 128)
	for i := range tables {
		tables[i] = byte(i + 1)
	}

	quantHdr := []byte{0, 0, 0, 128}
	out := mjpegFrame(t, d, 1, 1, 128, 16, 16, append(quantHdr, tables...), true)
	dqt := findMarker(out, 0xFF, 0xDB)
	if dqt < 0 || out[dqt+4] != 0 {
		t.Fatal("first frame: missing luma dqt")
	}

	// a later frame may omit the tables and reuse the previous ones
	out = mjpegFrame(t, d, 2, 1, 128, 16, 16, []byte{0, 0, 0, 0, 0xFF, 0xD9}, true)
	dqt = findMarker(out, 0xFF, 0xDB)
	if dqt < 0 {
		t.Fatal("second frame: dqt dropped")
	}

	// a frame with no tables at all must fail instead of emitting nothing
	if _, err := NewMJPEGDepacketizer().Decode(&Packet{
		Payload: []byte{0, 0, 0, 0, 1, 128, 2, 2, 0, 0, 0, 0},
		Marker:  true,
	}); err == nil {
		t.Fatal("expected error for frame without quant tables, got nil")
	}
}

func TestMJPEGStapSizeLimitFirstFragment(t *testing.T) {
	d := NewMJPEGDepacketizer()
	d.MaxBytes = 16

	big := make([]byte, 64)

	_, err := d.Decode(&Packet{
		Payload:        []byte{0, 0, 0, 0, 1, 50, 2, 2},
		Marker:         true,
		SequenceNumber: 1,
	})
	_ = err

	// rebuild payload with a body beyond max bytes
	pkt := &Packet{Payload: append([]byte{0, 0, 0, 0, 1, 50, 2, 2}, big...), Marker: true, SequenceNumber: 3}
	if _, err := d.Decode(pkt); err == nil {
		t.Fatal("expected size limit error on first fragment, got nil")
	}
}
