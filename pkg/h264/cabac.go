package h264

import "fmt"

type CtxState struct {
	PStateIdx uint8
	ValMPS    uint8
}

type CabacDecoder struct {
	codIRange  uint16
	codIOffset uint16
	data       []byte
	pos        int
	bitsLeft   int
}

func NewCabacDecoder(data []byte) (*CabacDecoder, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("cabac: need at least 2 bytes, got %d", len(data))
	}
	codIOffset := (uint16(data[0]) << 1) | uint16(data[1]>>7)

	d := &CabacDecoder{
		codIRange:  510,
		codIOffset: codIOffset,
		data:       data,
		pos:        2,
		bitsLeft:   7,
	}
	return d, nil
}

func (d *CabacDecoder) readBit() uint16 {
	if d.bitsLeft == 0 {
		if d.pos < len(d.data) {
			d.bitsLeft = 8
			d.pos++
		} else {
			return 0
		}
	}
	d.bitsLeft--
	bit := uint16((d.data[d.pos-1] >> uint(d.bitsLeft)) & 1)
	return bit
}

func (d *CabacDecoder) renormalize() {
	for d.codIRange < 256 {
		d.codIRange <<= 1
		d.codIOffset <<= 1
		d.codIOffset |= d.readBit()
	}
}

func (d *CabacDecoder) DecodeDecision(ctx *CtxState) uint8 {
	qCodIRangeIdx := (d.codIRange >> 6) & 3
	codIRangeLPS := rangeTabLPS[ctx.PStateIdx][qCodIRangeIdx]
	d.codIRange -= codIRangeLPS

	var binVal uint8
	if d.codIOffset >= d.codIRange {
		binVal = 1 - ctx.ValMPS
		d.codIOffset -= d.codIRange
		d.codIRange = codIRangeLPS

		if ctx.PStateIdx == 0 {
			ctx.ValMPS = 1 - ctx.ValMPS
		}
		ctx.PStateIdx = transIdxLPS[ctx.PStateIdx]
	} else {
		binVal = ctx.ValMPS
		ctx.PStateIdx = transIdxMPS[ctx.PStateIdx]
	}

	d.renormalize()
	return binVal
}

func (d *CabacDecoder) DecodeBypass() uint8 {
	d.codIOffset <<= 1
	d.codIOffset |= d.readBit()

	var val uint8
	if d.codIOffset >= d.codIRange {
		d.codIOffset -= d.codIRange
		val = 1
	}
	return val
}

func (d *CabacDecoder) DecodeTerminate() uint8 {
	d.codIRange -= 2
	if d.codIOffset >= d.codIRange {
		return 1
	}
	d.renormalize()
	return 0
}

func (d *CabacDecoder) BitsRead() int {
	return d.pos*8 - d.bitsLeft
}

func (d *CabacDecoder) State() (codIRange, codIOffset uint16) {
	return d.codIRange, d.codIOffset
}

func (d *CabacDecoder) Range() uint16 { return d.codIRange }

func (d *CabacDecoder) Offset() uint16 { return d.codIOffset }

func (d *CabacDecoder) BytePos() int {
	return d.pos
}

func (d *CabacDecoder) AlignToByte() {
	d.bitsLeft = 0
}

func (d *CabacDecoder) ReadBypassU(n int) uint32 {
	var val uint32
	for range n {
		val = (val << 1) | uint32(d.DecodeBypass())
	}
	return val
}
