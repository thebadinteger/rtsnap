package h264

import "image"

// Frame represents a decoded video frame with separate Y, Cb, Cr planes.
type Frame struct {
	Width    int // luma width in pixels
	Height   int // luma height in pixels
	MBWidth  int
	MBHeight int
	Y        []uint8 // luma plane
	Cb       []uint8 // chroma blue plane
	Cr       []uint8 // chroma red plane
	StrideY  int     // bytes per row for luma
	StrideC  int     // bytes per row for chroma

	// Color space metadata (populated from SPS VUI if available)
	MatrixCoefficients    uint // 0=unspecified, 1=BT.709, 5=BT.601, 9=BT.2020
	VideoFullRangeFlag    bool // true = full range (0-255)
	ColorDescriptionValid bool // true if color description was present in SPS VUI
}

// NewFrame creates a new frame buffer for the given dimensions.
// For 4:2:0 subsampling.
func NewFrame(width, height int) *Frame {
	mbWidth := (width + 15) / 16
	mbHeight := (height + 15) / 16
	codedWidth := mbWidth * 16
	codedHeight := mbHeight * 16
	chromaWidth := codedWidth / 2
	chromaHeight := codedHeight / 2

	return &Frame{
		Width:    width,
		Height:   height,
		MBWidth:  mbWidth,
		MBHeight: mbHeight,
		Y:        make([]uint8, codedWidth*codedHeight),
		Cb:       make([]uint8, chromaWidth*chromaHeight),
		Cr:       make([]uint8, chromaWidth*chromaHeight),
		StrideY:  codedWidth,
		StrideC:  chromaWidth,
	}
}

// SetLumaPixel sets a single luma pixel.
func (f *Frame) SetLumaPixel(x, y int, val uint8) {
	f.Y[y*f.StrideY+x] = val
}

// GetLumaPixel gets a single luma pixel.
func (f *Frame) GetLumaPixel(x, y int) uint8 {
	return f.Y[y*f.StrideY+x]
}

// SetChromaPixel sets a single chroma pixel.
func (f *Frame) SetChromaPixel(comp int, x, y int, val uint8) {
	if comp == 0 {
		f.Cb[y*f.StrideC+x] = val
	} else {
		f.Cr[y*f.StrideC+x] = val
	}
}

// GetChromaPixel gets a single chroma pixel.
func (f *Frame) GetChromaPixel(comp int, x, y int) uint8 {
	if comp == 0 {
		return f.Cb[y*f.StrideC+x]
	}
	return f.Cr[y*f.StrideC+x]
}

// SetLuma16x16 sets a 16x16 luma block at the given MB position.
func (f *Frame) SetLuma16x16(mbX, mbY int, block [16][16]uint8) {
	x0 := mbX * 16
	y0 := mbY * 16
	for y := range 16 {
		for x := range 16 {
			f.Y[(y0+y)*f.StrideY+x0+x] = block[y][x]
		}
	}
}

// SetChroma8x8 sets an 8x8 chroma block at the given MB position.
func (f *Frame) SetChroma8x8(comp int, mbX, mbY int, block [8][8]uint8) {
	x0 := mbX * 8
	y0 := mbY * 8
	plane := f.Cb
	if comp == 1 {
		plane = f.Cr
	}
	for y := range 8 {
		for x := range 8 {
			plane[(y0+y)*f.StrideC+x0+x] = block[y][x]
		}
	}
}

// YUV420Bytes returns the frame data in I420 planar format (Y, then U, then V).
func (f *Frame) YUV420Bytes() []byte {
	lumaSize := f.Width * f.Height
	chromaSize := (f.Width / 2) * (f.Height / 2)
	result := make([]byte, lumaSize+2*chromaSize)

	// Copy luma, cropping to actual dimensions
	for y := 0; y < f.Height; y++ {
		copy(result[y*f.Width:], f.Y[y*f.StrideY:y*f.StrideY+f.Width])
	}

	// Copy Cb
	chromaW := f.Width / 2
	chromaH := f.Height / 2
	offset := lumaSize
	for y := range chromaH {
		copy(result[offset+y*chromaW:], f.Cb[y*f.StrideC:y*f.StrideC+chromaW])
	}

	// Copy Cr
	offset = lumaSize + chromaSize
	for y := range chromaH {
		copy(result[offset+y*chromaW:], f.Cr[y*f.StrideC:y*f.StrideC+chromaW])
	}

	return result
}

// Image returns the frame as a standard image.Image
func (f *Frame) Image() image.Image {
	return f.YCbCr()
}

// YCbCr returns the frame as a standard *image.YCbCr
func (f *Frame) YCbCr() *image.YCbCr {
	return &image.YCbCr{
		Y:              f.Y,
		Cb:             f.Cb,
		Cr:             f.Cr,
		YStride:        f.StrideY,
		CStride:        f.StrideC,
		SubsampleRatio: image.YCbCrSubsampleRatio420,
		Rect:           image.Rect(0, 0, f.Width, f.Height),
	}
}

// NRGBA converts the frame to *image.NRGBA using standard BT.601 matrix
func (f *Frame) NRGBA() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, f.Width, f.Height))
	for y := 0; y < f.Height; y++ {
		cy := y / 2
		for x := 0; x < f.Width; x++ {
			cx := x / 2
			yVal := int(f.Y[y*f.StrideY+x])
			cb := int(f.Cb[cy*f.StrideC+cx])
			cr := int(f.Cr[cy*f.StrideC+cx])

			c := yVal - 16
			d := cb - 128
			e := cr - 128
			r := (298*c + 409*e + 128) >> 8
			g := (298*c - 100*d - 208*e + 128) >> 8
			b := (298*c + 516*d + 128) >> 8

			clamp := func(v int) uint8 {
				if v < 0 {
					return 0
				}
				if v > 255 {
					return 255
				}
				return uint8(v)
			}

			off := y*img.Stride + x*4
			img.Pix[off+0] = clamp(r)
			img.Pix[off+1] = clamp(g)
			img.Pix[off+2] = clamp(b)
			img.Pix[off+3] = 255
		}
	}
	return img
}
