package h264

import (
	"os"
	"testing"
)

func TestDecodeBlackIDR(t *testing.T) {
	data, err := os.ReadFile("../../tests/testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	d := New()
	f, err := d.DecodeAnnexB(data)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if f == nil {
		t.Fatal("frame is nil")
	}

	img := f.Image()
	if img == nil {
		t.Fatal("image is nil")
	}

	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		t.Fatalf("invalid bounds: %v", bounds)
	}
}

func TestDecodeTestpicIDR(t *testing.T) {
	data, err := os.ReadFile("../../tests/testdata/testpic_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	d := New()
	f, err := d.DecodeAnnexB(data)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if f == nil {
		t.Fatal("frame is nil")
	}

	img := f.Image()
	if img == nil {
		t.Fatal("image is nil")
	}
	t.Logf("decoded frame size: %dx%d", f.Width, f.Height)
}

type spsBitWriter struct {
	buf  []byte
	cur  byte
	nbit uint
}

func (w *spsBitWriter) bit(v uint) {
	w.cur = w.cur<<1 | byte(v&1)
	w.nbit++
	if w.nbit == 8 {
		w.buf = append(w.buf, w.cur)
		w.cur = 0
		w.nbit = 0
	}
}

func (w *spsBitWriter) u(v uint, n uint) {
	for i := n; i > 0; i-- {
		w.bit((v >> (i - 1)) & 1)
	}
}

func (w *spsBitWriter) ue(v uint) {
	c := v + 1
	l := uint(0)
	for (uint(1) << (l + 1)) <= c {
		l++
	}
	for i := uint(0); i < l; i++ {
		w.bit(0)
	}
	w.u(c, l+1)
}

func (w *spsBitWriter) bytes() []byte {
	w.bit(1)
	for w.nbit != 0 {
		w.bit(0)
	}
	return w.buf
}

func TestParseSPSHugeDimensions(t *testing.T) {
	w := &spsBitWriter{}
	w.u(66, 8)
	w.u(0, 8)
	w.u(30, 8)
	w.ue(0)
	w.ue(0)
	w.ue(0)
	w.ue(0)
	w.ue(1)
	w.bit(0)
	w.ue(100000)
	w.ue(100000)
	w.bit(1)
	w.bit(1)
	w.bit(0)
	w.bit(0)
	nalu := append([]byte{0x67}, w.bytes()...)
	if _, err := ParseSPSNALUnit(nalu, false); err == nil {
		t.Fatal("expected error for huge dimensions, got nil")
	}
}

func TestParseSPSHugeRefCycle(t *testing.T) {
	w := &spsBitWriter{}
	w.u(66, 8)
	w.u(0, 8)
	w.u(30, 8)
	w.ue(0)
	w.ue(0)
	w.ue(1)
	w.bit(0)
	w.ue(0)
	w.ue(0)
	w.ue(1000)
	nalu := append([]byte{0x67}, w.bytes()...)
	if _, err := ParseSPSNALUnit(nalu, false); err == nil {
		t.Fatal("expected error for huge ref cycle, got nil")
	}
}

func TestDecodeFrameSizeLimit(t *testing.T) {
	data, err := os.ReadFile("../../tests/testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	d := New()
	d.FrameSizeLimit(100)
	if _, err := d.DecodeAnnexB(data); err == nil {
		t.Fatal("expected error for frame over limit, got nil")
	}
}

func FuzzH264DecodeNALUs(f *testing.F) {
	data, err := os.ReadFile("../../tests/testdata/black_idr.264")
	if err != nil {
		f.Skip("testdata not found")
	}
	nalus := ExtractNalusFromByteStream(data)
	f.Add([]byte{})
	for _, n := range nalus {
		f.Add(n)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		d := New()
		d.FrameSizeLimit(7680 * 4320)
		_, _ = d.DecodeNALUs([][]byte{b})
	})
}
