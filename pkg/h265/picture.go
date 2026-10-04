package h265

import "image"

type Picture struct {
	Width, Height int

	CropX, CropY int
	CropW, CropH int

	ChromaFormat int

	BitDepth  int
	BitDepthC int

	ColorPrimaries uint16
	ColorTransfer  uint16
	ColorMatrix    uint16
	FullRange      bool

	POC int

	Y, Cb, Cr       []uint8
	Y16, Cb16, Cr16 []uint16

	StrideY, StrideC int
	WidthC, HeightC  int

	Col  []colMotion
	ColW int

	pool *picPool
	refs int32
}

func (p *Picture) Release() {
	p.release()
}

func (p *Picture) acquire() {
	if p != nil {
		p.refs++
	}
}

func (p *Picture) release() {
	if p == nil || p.refs == 0 {
		return
	}

	p.refs--
	if p.refs > 0 {
		return
	}

	g := p.geom()

	b := picBufs{
		col: p.Col,
		y:   p.Y, cb: p.Cb, cr: p.Cr,
		y16: p.Y16, cb16: p.Cb16, cr16: p.Cr16,
	}

	pool := p.pool
	p.pool = nil

	p.Col = nil
	p.Y, p.Cb, p.Cr = nil, nil, nil
	p.Y16, p.Cb16, p.Cr16 = nil, nil, nil

	pool.put(g, b)
}

type picBufs struct {
	col             []colMotion
	y, cb, cr       []uint8
	y16, cb16, cr16 []uint16
}

type picGeom struct {
	strideY, height  int
	strideC, heightC int
	colLen           int
	deep             bool
}

func (p *Picture) geom() picGeom {
	return picGeom{
		strideY: p.StrideY, height: p.Height,
		strideC: p.StrideC, heightC: p.HeightC,
		colLen: len(p.Col),
		deep:   p.Y16 != nil,
	}
}

const picPoolDepth = 8

type picPool struct {
	free map[picGeom][]picBufs
}

func (pl *picPool) get(g picGeom) (picBufs, bool) {
	if pl == nil {
		return picBufs{}, false
	}

	free := pl.free[g]
	if len(free) == 0 {
		return picBufs{}, false
	}

	b := free[len(free)-1]
	free[len(free)-1] = picBufs{}
	pl.free[g] = free[:len(free)-1]

	return b, true
}

func (pl *picPool) put(g picGeom, b picBufs) {
	if pl == nil {
		return
	}

	if pl.free == nil {
		pl.free = make(map[picGeom][]picBufs)
	}

	if len(pl.free[g]) < picPoolDepth {
		pl.free[g] = append(pl.free[g], b)
	}
}

func newPicture(pool *picPool, s *sps) *Picture {
	p := &Picture{
		Width:        int(s.picWidthInLumaSamples),
		Height:       int(s.picHeightInLumaSamples),
		ChromaFormat: int(s.chromaFormatIDC),
		BitDepth:     int(s.bitDepthLuma),
		BitDepthC:    int(s.bitDepthChroma),

		ColorPrimaries: s.colourPrimaries,
		ColorTransfer:  s.transferChar,
		ColorMatrix:    s.matrixCoeffs,
		FullRange:      s.fullRange,
	}

	p.CropX = int(s.confWinLeft) * s.subWidthC
	p.CropY = int(s.confWinTop) * s.subHeightC
	p.CropW = int(s.croppedWidth())
	p.CropH = int(s.croppedHeight())

	p.StrideY = p.Width
	p.WidthC = p.Width / s.subWidthC
	p.HeightC = p.Height / s.subHeightC
	p.StrideC = p.WidthC

	if s.chromaFormatIDC == 0 {
		p.WidthC, p.HeightC, p.StrideC = 0, 0, 0
	}

	p.ColW = (p.Width + 15) / 16

	g := picGeom{
		strideY: p.StrideY, height: p.Height,
		strideC: p.StrideC, heightC: p.HeightC,
		colLen: p.ColW * ((p.Height + 15) / 16),
		deep:   max(s.bitDepthLuma, s.bitDepthChroma) > 8,
	}

	if r, ok := pool.get(g); ok {
		p.Col, p.Y, p.Cb, p.Cr = r.col, r.y, r.cb, r.cr
		p.Y16, p.Cb16, p.Cr16 = r.y16, r.cb16, r.cr16

		clear(p.Col)
		clear(p.Y)
		clear(p.Cb)
		clear(p.Cr)
		clear(p.Y16)
		clear(p.Cb16)
		clear(p.Cr16)

		p.pool, p.refs = pool, 1

		return p
	}

	p.pool, p.refs = pool, 1

	p.Col = make([]colMotion, g.colLen)

	if g.deep {
		p.Y16 = make([]uint16, p.StrideY*p.Height)
		p.Cb16 = make([]uint16, p.StrideC*p.HeightC)
		p.Cr16 = make([]uint16, p.StrideC*p.HeightC)

		return p
	}

	p.Y = make([]uint8, p.StrideY*p.Height)
	p.Cb = make([]uint8, p.StrideC*p.HeightC)
	p.Cr = make([]uint8, p.StrideC*p.HeightC)

	return p
}

func (p *Picture) deep() bool {
	return p.Y16 != nil
}

func (p *Picture) depth(cIdx int) int {
	if cIdx == 0 {
		return p.BitDepth
	}

	return p.BitDepthC
}

func (p *Picture) plane8(cIdx int) ([]uint8, int) {
	switch cIdx {
	case 0:
		return p.Y, p.StrideY
	case 1:
		return p.Cb, p.StrideC
	default:
		return p.Cr, p.StrideC
	}
}

func (p *Picture) plane16(cIdx int) ([]uint16, int) {
	switch cIdx {
	case 0:
		return p.Y16, p.StrideY
	case 1:
		return p.Cb16, p.StrideC
	default:
		return p.Cr16, p.StrideC
	}
}

type colMotion struct {
	info    mvInfo
	refPoc  [2]int32
	refLong [2]bool
	intra   bool
}

func (p *Picture) colIndex(x, y int) int {
	return (y>>4)*p.ColW + x>>4
}

func (p *Picture) Image() image.Image {
	w := p.CropW
	if w <= 0 {
		w = p.Width
	}
	h := p.CropH
	if h <= 0 {
		h = p.Height
	}

	if p.BitDepth == 8 && len(p.Y) > 0 {
		subRatio := image.YCbCrSubsampleRatio420
		sw, sh := 2, 2
		switch p.ChromaFormat {
		case 0:
			rect := image.Rect(0, 0, w, h)
			yOff := p.CropY*p.StrideY + p.CropX
			return &image.Gray{
				Pix:    p.Y[yOff:],
				Stride: p.StrideY,
				Rect:   rect,
			}
		case 1:
			subRatio = image.YCbCrSubsampleRatio420
			sw, sh = 2, 2
		case 2:
			subRatio = image.YCbCrSubsampleRatio422
			sw, sh = 2, 1
		case 3:
			subRatio = image.YCbCrSubsampleRatio444
			sw, sh = 1, 1
		}

		yOff := p.CropY*p.StrideY + p.CropX
		cbOff := (p.CropY/sh)*p.StrideC + p.CropX/sw
		crOff := (p.CropY/sh)*p.StrideC + p.CropX/sw

		return &image.YCbCr{
			Y:              p.Y[yOff:],
			Cb:             p.Cb[cbOff:],
			Cr:             p.Cr[crOff:],
			YStride:        p.StrideY,
			CStride:        p.StrideC,
			SubsampleRatio: subRatio,
			Rect:           image.Rect(0, 0, w, h),
		}
	}

	return nil
}
