package h264

import (
	"errors"
	"fmt"
)

var (
	ErrNoSliceHeader    = errors.New("no slice header")
	ErrInvalidSliceType = errors.New("invalid slice type")
	// slice is valid but not an i slice, the decoder only handles intra pictures
	ErrNotIntraSlice = errors.New("not an intra slice")
	// stream uses a coding tool the decoder does not implement
	ErrUnsupported = errors.New("unsupported h264 stream")
)

type SliceType uint

const (
	SLICE_P  = SliceType(0)
	SLICE_B  = SliceType(1)
	SLICE_I  = SliceType(2)
	SLICE_SP = SliceType(3)
	SLICE_SI = SliceType(4)
)

// header of an i slice
type SliceHeader struct {
	SliceType                  SliceType
	FirstMBInSlice             uint32
	PicParamID                 uint32
	ColorPlaneID               uint32
	FrameNum                   uint32
	IDRPicID                   uint32
	PicOrderCntLsb             uint32
	DeltaPicOrderCntBottom     int32
	DeltaPicOrderCnt           [2]int32
	RedundantPicCnt            uint32
	SliceQPDelta               int32
	DisableDeblockingFilterIDC uint32
	SliceAlphaC0OffsetDiv2     int32
	SliceBetaOffsetDiv2        int32
	// bytes of the nal unit taken by the header, emulation prevention bytes included
	Size uint32
	// bits of the header in the nal unit with emulation prevention bytes removed
	BitSize         uint32
	IDR             bool
	FieldPicFlag    bool
	BottomFieldFlag bool
}

func ParseSliceHeader(nalu []byte, spsMap map[uint32]*SPS, ppsMap map[uint32]*PPS) (*SliceHeader, error) {
	sh := SliceHeader{}
	r := NewEBSPReader(nalu)
	nalHdr := r.Read(8)
	naluType := GetNaluType(byte(nalHdr))
	if naluType != NALU_NON_IDR && naluType != NALU_IDR {
		return nil, ErrNoSliceHeader
	}
	sh.IDR = naluType == NALU_IDR
	nalRefIDC := (nalHdr >> 5) & 0x3
	sh.FirstMBInSlice = uint32(r.ReadExpGolomb())
	sliceType := r.ReadExpGolomb()
	sh.PicParamID = uint32(r.ReadExpGolomb())
	if err := r.AccError(); err != nil {
		return nil, err
	}
	if sliceType > 9 {
		return nil, ErrInvalidSliceType
	}
	sh.SliceType = SliceType(sliceType)
	if SliceType(sliceType%5) != SLICE_I {
		return nil, ErrNotIntraSlice
	}
	pps, ok := ppsMap[sh.PicParamID]
	if !ok {
		return nil, fmt.Errorf("pps ID %d unknown", sh.PicParamID)
	}
	sps, ok := spsMap[pps.SeqParameterSetID]
	if !ok {
		return nil, fmt.Errorf("sps ID %d unknown", pps.SeqParameterSetID)
	}
	if pps.NumSliceGroupsMinus1 > 0 {
		return nil, fmt.Errorf("%w: slice groups", ErrUnsupported)
	}
	if sps.SeparateColourPlaneFlag {
		sh.ColorPlaneID = uint32(r.Read(2))
	}
	sh.FrameNum = uint32(r.Read(int(sps.Log2MaxFrameNumMinus4 + 4)))
	if !sps.FrameMbsOnlyFlag {
		sh.FieldPicFlag = r.ReadFlag()
		if sh.FieldPicFlag {
			sh.BottomFieldFlag = r.ReadFlag()
		}
	}
	if sh.IDR {
		sh.IDRPicID = uint32(r.ReadExpGolomb())
	}
	if sps.PicOrderCntType == 0 {
		sh.PicOrderCntLsb = uint32(r.Read(int(sps.Log2MaxPicOrderCntLsbMinus4 + 4)))
		if pps.BottomFieldPicOrderInFramePresentFlag && !sh.FieldPicFlag {
			sh.DeltaPicOrderCntBottom = int32(r.ReadSignedGolomb())
		}
	} else if sps.PicOrderCntType == 1 && !sps.DeltaPicOrderAlwaysZeroFlag {
		sh.DeltaPicOrderCnt[0] = int32(r.ReadSignedGolomb())
		if pps.BottomFieldPicOrderInFramePresentFlag && !sh.FieldPicFlag {
			sh.DeltaPicOrderCnt[1] = int32(r.ReadSignedGolomb())
		}
	}
	if pps.RedundantPicCntPresentFlag {
		sh.RedundantPicCnt = uint32(r.ReadExpGolomb())
	}

	// dec_ref_pic_marking
	if nalRefIDC != 0 {
		if sh.IDR {
			r.Read(2)
		} else if r.ReadFlag() {
			for r.AccError() == nil {
				op := r.ReadExpGolomb()
				if op == 0 {
					break
				}
				if op == 1 || op == 3 {
					r.ReadExpGolomb()
				}
				if op == 2 || op == 3 || op == 4 || op == 6 {
					r.ReadExpGolomb()
				}
			}
		}
	}

	sliceQPDelta := r.ReadSignedGolomb()
	var idc uint
	var alpha, beta int
	if pps.DeblockingFilterControlPresentFlag {
		idc = r.ReadExpGolomb()
		if idc != 1 {
			alpha = r.ReadSignedGolomb()
			beta = r.ReadSignedGolomb()
		}
	}
	if err := r.AccError(); err != nil {
		return nil, fmt.Errorf("slice header: %w", err)
	}
	if qp := 26 + pps.PicInitQpMinus26 + sliceQPDelta; qp < 0 || qp > 51 {
		return nil, fmt.Errorf("slice qp %d out of range", qp)
	}
	if idc > 2 || alpha < -6 || alpha > 6 || beta < -6 || beta > 6 {
		return nil, fmt.Errorf("deblocking parameters out of range")
	}
	sh.SliceQPDelta = int32(sliceQPDelta)
	sh.DisableDeblockingFilterIDC = uint32(idc)
	sh.SliceAlphaC0OffsetDiv2 = int32(alpha)
	sh.SliceBetaOffsetDiv2 = int32(beta)

	sh.Size = uint32(r.NrBytesRead())
	sh.BitSize = uint32(r.NrRBSPBitsRead())
	return &sh, nil
}
