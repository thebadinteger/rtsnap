package h264

import (
	"fmt"
	"math/bits"
)

type BitReader struct {
	data    []byte
	bytePos int
	bitPos  uint
}

func NewBitReader(data []byte) *BitReader {
	return &BitReader{data: data}
}

func (r *BitReader) BitsRead() int {
	return r.bytePos*8 + int(r.bitPos)
}

func (r *BitReader) ReadBit() (uint8, error) {
	if r.bytePos >= len(r.data) {
		return 0, fmt.Errorf("bitreader: end of data at byte %d", r.bytePos)
	}
	bit := (r.data[r.bytePos] >> (7 - r.bitPos)) & 1
	r.bitPos++
	if r.bitPos == 8 {
		r.bitPos = 0
		r.bytePos++
	}
	return bit, nil
}

// next n bits without consuming them, zero padded past the end, n is at most 32
func (r *BitReader) peek(n int) uint32 {
	var v uint64
	p := r.bytePos
	if p+5 <= len(r.data) {
		d := r.data[p : p+5 : p+5]
		v = uint64(d[0])<<32 | uint64(d[1])<<24 | uint64(d[2])<<16 | uint64(d[3])<<8 | uint64(d[4])
	} else {
		for i := p; i < p+5; i++ {
			v <<= 8
			if i < len(r.data) {
				v |= uint64(r.data[i])
			}
		}
	}
	return uint32(v>>(40-r.bitPos-uint(n))) & uint32(uint64(1)<<uint(n)-1)
}

func (r *BitReader) bitsLeft() int {
	return len(r.data)*8 - r.bytePos*8 - int(r.bitPos)
}

func (r *BitReader) ReadBits(n int) (uint32, error) {
	if n == 0 {
		return 0, nil
	}
	if n > 32 || n < 0 {
		return 0, fmt.Errorf("bitreader: invalid n=%d", n)
	}
	if n > r.bitsLeft() {
		return 0, fmt.Errorf("bitreader: end of data at byte %d", len(r.data))
	}
	val := r.peek(n)
	r.SkipBits(n)
	return val, nil
}

func (r *BitReader) ReadFlag() (bool, error) {
	bit, err := r.ReadBit()
	return bit == 1, err
}

func (r *BitReader) PeekBits(n int) (uint32, error) {
	return r.peek(n), nil
}

func (r *BitReader) SkipBits(n int) {
	totalBit := int(r.bitPos) + n
	r.bytePos += totalBit / 8
	r.bitPos = uint(totalBit % 8)
}

func (r *BitReader) ReadUE() (uint32, error) {
	leadingZeros := bits.LeadingZeros32(r.peek(32))
	if leadingZeros > 15 {
		return 0, fmt.Errorf("ue(v): code too long")
	}
	n := 2*leadingZeros + 1
	if n > r.bitsLeft() {
		return 0, fmt.Errorf("ue(v): bitreader: end of data at byte %d", len(r.data))
	}
	val := r.peek(n) - 1
	r.SkipBits(n)
	return val, nil
}

func (r *BitReader) ReadSE() (int32, error) {
	ue, err := r.ReadUE()
	if err != nil {
		return 0, err
	}
	if ue%2 == 0 {
		return -int32(ue / 2), nil
	}
	return int32((ue + 1) / 2), nil
}

func (r *BitReader) AlignToByte() {
	if r.bitPos > 0 {
		r.bitPos = 0
		r.bytePos++
	}
}

func (r *BitReader) HasMoreRBSPData() bool {
	if r.bytePos >= len(r.data) {
		return false
	}
	lastByte := len(r.data) - 1
	for lastByte >= r.bytePos && r.data[lastByte] == 0 {
		lastByte--
	}
	if lastByte < r.bytePos {
		return false
	}
	b := r.data[lastByte]
	stopBitPos := 0
	for (b & (1 << stopBitPos)) == 0 {
		stopBitPos++
	}
	stopBitIndex := lastByte*8 + (7 - stopBitPos)
	currentBitIndex := r.bytePos*8 + int(r.bitPos)
	return currentBitIndex < stopBitIndex
}
