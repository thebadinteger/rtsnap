package h264

import (
	"errors"
	"fmt"
)

type PPS struct {
	PicParameterSetID                     uint32
	SeqParameterSetID                     uint32
	EntropyCodingModeFlag                 bool
	BottomFieldPicOrderInFramePresentFlag bool
	NumSliceGroupsMinus1                  uint
	NumRefIdxI0DefaultActiveMinus1        uint
	NumRefIdxI1DefaultActiveMinus1        uint
	WeightedPredFlag                      bool
	WeightedBipredIDC                     uint
	PicInitQpMinus26                      int
	PicInitQsMinus26                      int
	ChromaQpIndexOffset                   int
	DeblockingFilterControlPresentFlag    bool
	ConstrainedIntraPredFlag              bool
	RedundantPicCntPresentFlag            bool
	Transform8x8ModeFlag                  bool
	PicScalingMatrixPresentFlag           bool
	PicScalingLists                       []ScalingList
	SecondChromaQpIndexOffset             int
}

var (
	ErrNotPPS = errors.New("not an PPS NAL unit")
)

func ParsePPSNALUnit(data []byte, spsMap map[uint32]*SPS) (*PPS, error) {
	pps := &PPS{}

	reader := NewEBSPReader(data)

	naluHdr := reader.Read(8)
	naluType := GetNaluType(byte(naluHdr))
	if naluType != NALU_PPS {
		return nil, ErrNotPPS
	}

	pps.PicParameterSetID = uint32(reader.ReadExpGolomb())
	pps.SeqParameterSetID = uint32(reader.ReadExpGolomb())
	pps.EntropyCodingModeFlag = reader.ReadFlag()
	pps.BottomFieldPicOrderInFramePresentFlag = reader.ReadFlag()
	pps.NumSliceGroupsMinus1 = reader.ReadExpGolomb()
	if err := reader.AccError(); err != nil {
		return nil, err
	}
	if pps.NumSliceGroupsMinus1 > 0 {
		// the slice group map is not parsed, slices of such a pps are rejected
		if pps.NumSliceGroupsMinus1 > 7 {
			return nil, fmt.Errorf("invalid num_slice_groups_minus1: %d", pps.NumSliceGroupsMinus1)
		}
		return pps, nil
	}

	pps.NumRefIdxI0DefaultActiveMinus1 = reader.ReadExpGolomb()
	pps.NumRefIdxI1DefaultActiveMinus1 = reader.ReadExpGolomb()
	pps.WeightedPredFlag = reader.ReadFlag()
	pps.WeightedBipredIDC = reader.Read(2)
	pps.PicInitQpMinus26 = reader.ReadSignedGolomb()
	pps.PicInitQsMinus26 = reader.ReadSignedGolomb()
	pps.ChromaQpIndexOffset = reader.ReadSignedGolomb()
	// inferred equal to chroma_qp_index_offset when not present
	pps.SecondChromaQpIndexOffset = pps.ChromaQpIndexOffset
	pps.DeblockingFilterControlPresentFlag = reader.ReadFlag()
	pps.ConstrainedIntraPredFlag = reader.ReadFlag()
	pps.RedundantPicCntPresentFlag = reader.ReadFlag()
	if err := reader.AccError(); err != nil {
		return nil, err
	}
	if pps.NumRefIdxI0DefaultActiveMinus1 > 31 || pps.NumRefIdxI1DefaultActiveMinus1 > 31 ||
		pps.PicInitQpMinus26 < -26 || pps.PicInitQpMinus26 > 25 ||
		pps.ChromaQpIndexOffset < -12 || pps.ChromaQpIndexOffset > 12 {
		return nil, fmt.Errorf("pps value out of range")
	}

	sps := spsMap[pps.SeqParameterSetID]
	// baseline, main and extended streams have no pps extension, trailing bytes there are garbage
	if sps != nil && (sps.Profile == 66 || sps.Profile == 77 || sps.Profile == 88) && sps.ProfileCompatibility&0xe0 != 0 {
		return pps, nil
	}
	if !reader.MoreRbspData() {
		return pps, nil
	}

	pps.Transform8x8ModeFlag = reader.ReadFlag()
	pps.PicScalingMatrixPresentFlag = reader.ReadFlag()
	if pps.PicScalingMatrixPresentFlag {
		// six 4x4 lists always, plus two 8x8 with transform8x8 and four more for 4:4:4, not chroma array type
		nrScalingLists := 6
		if pps.Transform8x8ModeFlag {
			nrScalingLists += 2

			if sps != nil && sps.ChromaFormatIDC == 3 {
				nrScalingLists += 4
			}
		}

		pps.PicScalingLists = make([]ScalingList, nrScalingLists)

		for i := 0; i < nrScalingLists; i++ {
			if !reader.ReadFlag() {
				continue
			}
			sizeOfScalingList := 16
			if i >= 6 {
				sizeOfScalingList = 64
			}
			pps.PicScalingLists[i] = readScalingList(reader, sizeOfScalingList)
		}
	}
	pps.SecondChromaQpIndexOffset = reader.ReadSignedGolomb()

	if err := reader.AccError(); err != nil {
		return nil, err
	}
	if pps.SecondChromaQpIndexOffset < -12 || pps.SecondChromaQpIndexOffset > 12 {
		return nil, fmt.Errorf("second_chroma_qp_index_offset %d out of range", pps.SecondChromaQpIndexOffset)
	}
	return pps, nil
}
