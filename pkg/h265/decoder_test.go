package h265

import (
	"os"
	"testing"
)

func TestDecodeTinyIntra(t *testing.T) {
	data, err := os.ReadFile("../../tests/testdata/tiny_intra.h265")
	if err != nil {
		t.Skip("testdata not found")
	}

	var d Decoder
	var decodedPic *Picture

	for _, nal := range SplitAnnexB(data) {
		pics, err := d.DecodeNAL(nal)
		if err != nil {
			t.Fatalf("failed to decode nal: %v", err)
		}
		for _, p := range pics {
			decodedPic = p
		}
	}

	for _, p := range d.Flush() {
		decodedPic = p
	}

	if decodedPic == nil {
		t.Fatal("no picture decoded")
	}

	img := decodedPic.Image()
	if img == nil {
		t.Fatal("image is nil")
	}

	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		t.Fatalf("invalid bounds: %v", b)
	}
	t.Logf("decoded h265 picture size: %dx%d", b.Dx(), b.Dy())
}

func TestDecode1080p(t *testing.T) {
	data, err := os.ReadFile("../../tests/testdata/1080p.h265")
	if err != nil {
		t.Skip("testdata not found")
	}

	var d Decoder
	var decodedPic *Picture

	for _, nal := range SplitAnnexB(data) {
		pics, err := d.DecodeNAL(nal)
		if err != nil {
			t.Fatalf("failed to decode nal: %v", err)
		}
		for _, p := range pics {
			decodedPic = p
		}
	}

	for _, p := range d.Flush() {
		decodedPic = p
	}

	if decodedPic == nil {
		t.Fatal("no picture decoded")
	}

	img := decodedPic.Image()
	if img == nil {
		t.Fatal("image is nil")
	}

	b := img.Bounds()
	t.Logf("decoded 1080p picture size: %dx%d", b.Dx(), b.Dy())
}
