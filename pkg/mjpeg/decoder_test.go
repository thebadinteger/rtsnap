package mjpeg

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestMJPEGDecode(t *testing.T) {
	// generate small test jpeg image
	src := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			src.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}

	dec := NewDecoder()
	img, err := dec.Decode(buf.Bytes())
	if err != nil {
		t.Fatalf("decode jpeg: %v", err)
	}

	if img.Bounds().Dx() != 32 || img.Bounds().Dy() != 32 {
		t.Fatalf("unexpected dimensions: %v", img.Bounds())
	}
}
