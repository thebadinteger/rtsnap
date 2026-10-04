package h265

import (
	"bytes"
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

func TestDecodeTinyIntraConform(t *testing.T) {
	data, err := os.ReadFile("../../tests/testdata/tiny_intra.h265")
	if err != nil {
		t.Skip("testdata not found")
	}
	ref, err := os.ReadFile("../../tests/testdata/tiny_intra_ref.yuv")
	if err != nil {
		t.Skip("reference not found")
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

	p := decodedPic
	if p.BitDepth != 8 || len(p.Y) == 0 {
		t.Fatalf("unexpected picture format: depth=%d chroma=%d", p.BitDepth, p.ChromaFormat)
	}

	w := p.CropW
	if w <= 0 {
		w = p.Width
	}
	h := p.CropH
	if h <= 0 {
		h = p.Height
	}

	cw, ch := w/2, h/2
	if len(ref) != w*h+2*cw*ch {
		t.Fatalf("reference size %d does not match picture %dx%d", len(ref), w, h)
	}

	got := make([]byte, 0, len(ref))
	for y := 0; y < h; y++ {
		got = append(got, p.Y[(p.CropY+y)*p.StrideY+p.CropX:(p.CropY+y)*p.StrideY+p.CropX+w]...)
	}
	for y := 0; y < ch; y++ {
		got = append(got, p.Cb[(p.CropY/2+y)*p.StrideC+p.CropX/2:(p.CropY/2+y)*p.StrideC+p.CropX/2+cw]...)
	}
	for y := 0; y < ch; y++ {
		got = append(got, p.Cr[(p.CropY/2+y)*p.StrideC+p.CropX/2:(p.CropY/2+y)*p.StrideC+p.CropX/2+cw]...)
	}

	if !bytes.Equal(got, ref) {
		t.Fatal("decoded pixels do not match reference yuv")
	}
}
