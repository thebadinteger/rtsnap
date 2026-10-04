package h264

import (
	"os"
	"testing"
)

func TestDecodeBlackIDR(t *testing.T) {
	data, err := os.ReadFile("../../src/hi264/testdata/black_idr.264")
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
	data, err := os.ReadFile("../../src/hi264/testdata/testpic_idr.264")
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
