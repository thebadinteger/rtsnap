package h265

import (
	"image"
	"sync"
	"sync/atomic"
	"unsafe"
)

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

	// set when a slice segment or a reference was missing
	Corrupt bool

	Y, Cb, Cr       []uint8
	Y16, Cb16, Cr16 []uint16

	StrideY, StrideC int
	WidthC, HeightC  int

	Col  []colMotion
	ColW int

	pool *picPool
	refs int32
}

// return planes back to pool
func (p *Picture) Release() {
	p.release()
}

// planes outlive decoders so repeated snapshots reuse them
var sharedPool = &picPool{}

func (p *Picture) acquire() {
	if p != nil {
		atomic.AddInt32(&p.refs, 1)
	}
}

func (p *Picture) release() {
	if p == nil {
		return
	}
	if atomic.AddInt32(&p.refs, -1) != 0 {
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

const (
	picPoolDepth  = 4
	picPoolBudget = 128 << 20
)

type picPool struct {
	mu    sync.Mutex
	free  map[picGeom][]picBufs
	sizes map[picGeom]int
	bytes int
}

func (b picBufs) size() int {
	n := cap(b.y) + cap(b.cb) + cap(b.cr)
	n += 2 * (cap(b.y16) + cap(b.cb16) + cap(b.cr16))
	n += cap(b.col) * int(unsafe.Sizeof(colMotion{}))

	return n
}

func (pl *picPool) get(g picGeom) (picBufs, bool) {
	if pl == nil {
		return picBufs{}, false
	}

	pl.mu.Lock()
	defer pl.mu.Unlock()

	free := pl.free[g]
	if len(free) == 0 {
		return picBufs{}, false
	}

	b := free[len(free)-1]
	free[len(free)-1] = picBufs{}
	pl.free[g] = free[:len(free)-1]
	pl.bytes -= pl.sizes[g]

	if len(pl.free[g]) == 0 {
		delete(pl.free, g)
		delete(pl.sizes, g)
	}

	return b, true
}

func (pl *picPool) put(g picGeom, b picBufs) {
	if pl == nil {
		return
	}

	pl.mu.Lock()
	defer pl.mu.Unlock()

	if pl.free == nil {
		pl.free = make(map[picGeom][]picBufs)
		pl.sizes = make(map[picGeom]int)
	}

	n := b.size()
	if len(pl.free[g]) >= picPoolDepth {
		return
	}

	for pl.bytes+n > picPoolBudget {
		victim := picGeom{}
		worst := 0

		for og := range pl.free {
			if og == g {
				continue
			}

			if s := pl.sizes[og]; s > worst {
				victim, worst = og, s
			}
		}

		if worst == 0 {
			return
		}

		bufs := pl.free[victim]
		bufs[len(bufs)-1] = picBufs{}
		pl.free[victim] = bufs[:len(bufs)-1]
		pl.bytes -= pl.sizes[victim]

		if len(pl.free[victim]) == 0 {
			delete(pl.free, victim)
			delete(pl.sizes, victim)
		}
	}

	pl.free[g] = append(pl.free[g], b)
	pl.sizes[g] += n
	pl.bytes += n
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

// expose picture as standard image
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

	if p.deep() {
		return p.downshift(w, h)
	}

	return nil
}

// convert deep picture to 8 bit for display
func (p *Picture) downshift(w, h int) image.Image {
	shift := func(depth int) int {
		if depth > 8 {
			return depth - 8
		}

		return 0
	}

	to8 := func(plane []uint16, stride, width, height, offX, offY, sh int) []uint8 {
		out := make([]uint8, width*height)

		for y := range height {
			row := plane[(offY+y)*stride+offX:]
			dst := out[y*width:]

			for x := range width {
				dst[x] = uint8(row[x] >> sh)
			}
		}

		return out
	}

	y := to8(p.Y16, p.StrideY, w, h, p.CropX, p.CropY, shift(p.BitDepth))

	if p.ChromaFormat == 0 {
		return &image.Gray{
			Pix:    y,
			Stride: w,
			Rect:   image.Rect(0, 0, w, h),
		}
	}

	sw, sh := 1, 1
	if p.ChromaFormat == 1 {
		sw, sh = 2, 2
	} else if p.ChromaFormat == 2 {
		sw, sh = 2, 1
	}

	cw, ch := w/sw, h/sh

	cb := to8(p.Cb16, p.StrideC, cw, ch, p.CropX/sw, p.CropY/sh, shift(p.BitDepthC))
	cr := to8(p.Cr16, p.StrideC, cw, ch, p.CropX/sw, p.CropY/sh, shift(p.BitDepthC))

	ratio := image.YCbCrSubsampleRatio444
	if sw == 2 && sh == 2 {
		ratio = image.YCbCrSubsampleRatio420
	} else if sw == 2 {
		ratio = image.YCbCrSubsampleRatio422
	}

	return &image.YCbCr{
		Y:              y,
		Cb:             cb,
		Cr:             cr,
		YStride:        w,
		CStride:        cw,
		SubsampleRatio: ratio,
		Rect:           image.Rect(0, 0, w, h),
	}
}
