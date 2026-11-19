package h264

import (
	"errors"
	"fmt"
	"io"
)

// longest exp-golomb prefix accepted, larger values cannot occur in valid headers
const maxExpGolombLeadingZeroBits = 31

var errExpGolombTooLong = errors.New("exp-golomb code too long")

func CeilLog2(n uint) int {
	for i := 0; i < 32; i++ {
		maxNr := uint(1 << i)
		if maxNr >= n {
			return i
		}
	}
	return 32
}

const (
	startCodeEmulationPreventionByte = 0x03
)

// bit reader over a nal unit that skips emulation prevention bytes
type EBSPReader struct {
	data      []byte
	err       error
	n         int
	v         uint64
	pos       int
	zeroCount int
	nrEPB     int
}

func NewEBSPReader(data []byte) *EBSPReader {
	return &EBSPReader{
		data: data,
		pos:  -1,
	}
}

func (r *EBSPReader) AccError() error {
	return r.err
}

func (r *EBSPReader) NrBytesRead() int {
	return r.pos + 1
}

// bits consumed including emulation prevention bytes
func (r *EBSPReader) NrBitsRead() int {
	return (r.pos+1)*8 - r.n
}

// bits consumed of the payload with emulation prevention bytes removed
func (r *EBSPReader) NrRBSPBitsRead() int {
	return (r.pos+1-r.nrEPB)*8 - r.n
}

func (r *EBSPReader) Read(n int) uint {
	if r.err != nil {
		return 0
	}
	if n < 0 || n > 32 {
		r.err = fmt.Errorf("invalid bit count %d", n)
		return 0
	}
	for r.n < n {
		if r.pos+1 >= len(r.data) {
			r.err = io.EOF
			return 0
		}
		r.pos++
		b := r.data[r.pos]
		if r.zeroCount == 2 && b == startCodeEmulationPreventionByte {
			if r.pos+1 >= len(r.data) {
				r.err = io.EOF
				return 0
			}
			r.pos++
			r.nrEPB++
			b = r.data[r.pos]
			r.zeroCount = 0
		}
		if b != 0 {
			r.zeroCount = 0
		} else {
			r.zeroCount++
		}
		r.v = r.v<<8 | uint64(b)
		r.n += 8
	}
	v := r.v >> uint(r.n-n)
	r.n -= n
	r.v &= 1<<uint(r.n) - 1

	return uint(v)
}

func (r *EBSPReader) ReadFlag() bool {
	return r.Read(1) == 1
}

func (r *EBSPReader) ReadExpGolomb() uint {
	leadingZeroBits := 0
	for {
		b := r.Read(1)
		if r.err != nil {
			return 0
		}
		if b == 1 {
			break
		}
		leadingZeroBits++
		if leadingZeroBits > maxExpGolombLeadingZeroBits {
			r.err = errExpGolombTooLong
			return 0
		}
	}

	endBits := r.Read(leadingZeroBits)
	if r.err != nil {
		return 0
	}

	return 1<<uint(leadingZeroBits) - 1 + endBits
}

func (r *EBSPReader) ReadSignedGolomb() int {
	unsignedGolomb := r.ReadExpGolomb()
	if unsignedGolomb%2 == 1 {
		return int((unsignedGolomb + 1) / 2)
	}
	return -int(unsignedGolomb / 2)
}

// more_rbsp_data, false when only the stop bit and zero bits remain
func (r *EBSPReader) MoreRbspData() bool {
	if r.err != nil {
		return false
	}
	saved := *r
	defer func() { *r = saved }()

	if r.Read(1) != 1 {
		return r.err == nil
	}
	for {
		b := r.Read(1)
		if r.err != nil {
			return false
		}
		if b == 1 {
			return true
		}
	}
}

func (r *EBSPReader) SetError(err error) {
	if r.err == nil {
		r.err = err
	}
}
