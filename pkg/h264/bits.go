package h264

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

var (
	ErrNotReadSeeker = errors.New("reader does not support Seek")
)

func Mask(n int) uint {
	if n <= 0 {
		return 0
	}
	if n >= 64 {
		return ^uint(0)
	}
	return (1 << n) - 1
}

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

type EBSPReader struct {
	rd        io.Reader
	err       error
	n         int
	v         uint
	pos       int
	zeroCount int
}

func NewEBSPReader(rd io.Reader) *EBSPReader {
	return &EBSPReader{
		rd:  rd,
		pos: -1,
	}
}

func (r *EBSPReader) AccError() error {
	return r.err
}

func (r *EBSPReader) NrBytesRead() int {
	return r.pos + 1
}

func (r *EBSPReader) NrBitsRead() int {
	nrBits := r.NrBytesRead() * 8
	if r.NrBitsReadInCurrentByte() != 8 {
		nrBits += r.NrBitsReadInCurrentByte() - 8
	}
	return nrBits
}

func (r *EBSPReader) NrBitsReadInCurrentByte() int {
	return 8 - r.n
}

func (r *EBSPReader) Read(n int) uint {
	if r.err != nil {
		return 0
	}
	var err error
	for r.n < n {
		r.v <<= 8
		var b uint8
		err = binary.Read(r.rd, binary.BigEndian, &b)
		if err != nil {
			r.err = err
			return 0
		}
		r.pos++
		if r.zeroCount == 2 && b == startCodeEmulationPreventionByte {
			err = binary.Read(r.rd, binary.BigEndian, &b)
			if err != nil {
				r.err = err
				return 0
			}
			r.pos++
			r.zeroCount = 0
		}
		if b != 0 {
			r.zeroCount = 0
		} else {
			r.zeroCount++
		}
		r.v |= uint(b)

		r.n += 8
	}
	v := r.v >> uint(r.n-n)

	r.n -= n
	r.v &= Mask(r.n)

	return v
}

func (r *EBSPReader) ReadBytes(n int) []byte {
	if r.err != nil {
		return nil
	}
	payload := make([]byte, n)
	for i := 0; i < n; i++ {
		b := byte(r.Read(8))
		payload[i] = b
	}
	if r.err != nil {
		return nil
	}
	return payload
}

func (r *EBSPReader) ReadFlag() bool {
	return r.Read(1) == 1
}

func (r *EBSPReader) ReadExpGolomb() uint {
	if r.err != nil {
		return 0
	}
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
	}

	var res uint = (1 << leadingZeroBits) - 1

	endBits := r.Read(leadingZeroBits)
	if r.err != nil {
		return 0
	}

	return res + endBits
}

func (r *EBSPReader) ReadSignedGolomb() int {
	if r.err != nil {
		return 0
	}
	unsignedGolomb := r.ReadExpGolomb()
	if r.err != nil {
		return 0
	}
	if unsignedGolomb%2 == 1 {
		return int((unsignedGolomb + 1) / 2)
	}
	return -int(unsignedGolomb / 2)
}

func (r *EBSPReader) IsSeeker() bool {
	_, ok := r.rd.(io.ReadSeeker)
	return ok
}

func (r *EBSPReader) MoreRbspData() (bool, error) {
	if !r.IsSeeker() {
		return false, ErrNotReadSeeker
	}
	stateCopy := *r

	firstBit := r.Read(1)
	if r.err != nil {
		return false, nil
	}
	if firstBit != 1 {
		err := r.reset(stateCopy)
		if err != nil {
			return false, err
		}
		return true, nil
	}
	more := false
	for {
		b := r.Read(1)
		if r.err == io.EOF {
			r.err = nil
			break
		}
		if r.err != nil {
			return false, nil
		}
		if b == 1 {
			more = true
			break
		}
	}
	err := r.reset(stateCopy)
	if err != nil {
		return false, err
	}
	return more, nil
}

func (r *EBSPReader) reset(prevState EBSPReader) error {
	rdSeek, _ := r.rd.(io.ReadSeeker)
	_, err := rdSeek.Seek(int64(prevState.pos+1), 0)
	if err != nil {
		return err
	}
	r.n = prevState.n
	r.v = prevState.v
	r.pos = prevState.pos
	r.zeroCount = prevState.zeroCount
	return nil
}

func (r *EBSPReader) ReadRbspTrailingBits() error {
	if r.err != nil {
		return nil
	}
	firstBit := r.Read(1)
	if r.err != nil {
		return nil
	}
	if firstBit != 1 {
		return fmt.Errorf("rbspTrailingBits don't start with 1")
	}
	for {
		b := r.Read(1)
		if r.err == io.EOF {
			r.err = nil
			return nil
		}
		if r.err != nil {
			return nil
		}
		if b == 1 {
			return fmt.Errorf("another 1 in RbspTrailingBits")
		}
	}
}

func (r *EBSPReader) SetError(err error) {
	if r.err == nil {
		r.err = err
	}
}
