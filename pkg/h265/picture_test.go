package h265

import (
	"image"
	"testing"
)

func TestDownshiftDeepPicture(t *testing.T) {
	p := &Picture{
		Width: 8, Height: 8, ChromaFormat: 1,
		BitDepth: 10, BitDepthC: 10,
		StrideY: 8, StrideC: 4,
		WidthC: 4, HeightC: 4,
		CropW: 8, CropH: 8,
		Y16:  make([]uint16, 64),
		Cb16: make([]uint16, 16),
		Cr16: make([]uint16, 16),
	}

	for i := range p.Y16 {
		p.Y16[i] = 1023
	}
	for i := range p.Cb16 {
		p.Cb16[i] = 512
	}

	img := p.Image()
	if img == nil {
		t.Fatal("deep picture produced no image")
	}

	yc, ok := img.(*image.YCbCr)
	if !ok {
		t.Fatalf("expected ycbcr, got %T", img)
	}
	if yc.Rect.Dx() != 8 || yc.Rect.Dy() != 8 {
		t.Fatalf("bad rect %v", yc.Rect)
	}
	if yc.Y[0] != 255 {
		t.Fatalf("expected 255 after downshift, got %d", yc.Y[0])
	}
	if yc.Cb[0] != 128 {
		t.Fatalf("expected 128 for mid chroma, got %d", yc.Cb[0])
	}
	if yc.YStride != 8 || yc.CStride != 4 {
		t.Fatalf("bad strides %d/%d", yc.YStride, yc.CStride)
	}
}

func TestDownshiftMonoDeep(t *testing.T) {
	p := &Picture{
		Width: 8, Height: 8, ChromaFormat: 0,
		BitDepth: 10, BitDepthC: 10,
		StrideY: 8, CropW: 8, CropH: 8,
		Y16: make([]uint16, 64),
	}

	for i := range p.Y16 {
		p.Y16[i] = 1023
	}

	img := p.Image()
	g, ok := img.(*image.Gray)
	if !ok {
		t.Fatalf("expected gray, got %T", img)
	}
	if g.Pix[0] != 255 || g.Stride != 8 {
		t.Fatalf("bad mono downshift %d stride %d", g.Pix[0], g.Stride)
	}
}

func TestDownshiftMixedDepth(t *testing.T) {
	p := &Picture{
		Width: 8, Height: 8, ChromaFormat: 1,
		BitDepth: 8, BitDepthC: 10,
		StrideY: 8, StrideC: 4,
		WidthC: 4, HeightC: 4, CropW: 8, CropH: 8,
		Y16:  make([]uint16, 64),
		Cb16: make([]uint16, 16),
		Cr16: make([]uint16, 16),
	}

	for i := range p.Y16 {
		p.Y16[i] = 200
	}
	for i := range p.Cb16 {
		p.Cb16[i] = 512
	}

	yc := p.Image().(*image.YCbCr)
	if yc.Y[0] != 200 {
		t.Fatalf("luma must keep 8 bit values, got %d", yc.Y[0])
	}
	if yc.Cb[0] != 128 {
		t.Fatalf("chroma must shift by 2 bits, got %d", yc.Cb[0])
	}
}
