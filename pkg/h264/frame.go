package h264

import "image"

type Frame struct {
	Width    int
	Height   int
	MBWidth  int
	MBHeight int
	Y        []uint8
	Cb       []uint8
	Cr       []uint8
	StrideY  int
	StrideC  int

	MatrixCoefficients    uint
	VideoFullRangeFlag    bool
	ColorDescriptionValid bool
}

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

func (f *Frame) SetLumaPixel(x, y int, val uint8) {
	f.Y[y*f.StrideY+x] = val
}

func (f *Frame) GetLumaPixel(x, y int) uint8 {
	return f.Y[y*f.StrideY+x]
}

func (f *Frame) GetChromaPixel(comp int, x, y int) uint8 {
	if comp == 0 {
		return f.Cb[y*f.StrideC+x]
	}
	return f.Cr[y*f.StrideC+x]
}

func (f *Frame) SetLuma16x16(mbX, mbY int, block [16][16]uint8) {
	x0 := mbX * 16
	y0 := mbY * 16
	for y := range 16 {
		for x := range 16 {
			f.Y[(y0+y)*f.StrideY+x0+x] = block[y][x]
		}
	}
}

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

// expose frame as standard image
func (f *Frame) Image() image.Image {
	return f.YCbCr()
}

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


