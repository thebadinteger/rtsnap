package h264

import (
	"bytes"
	"errors"
	"fmt"
)

const ExtendedSAR = 255

var (
	ErrNotSPS = errors.New("not an SPS NAL unit")
)

type SPS struct {
	Profile                         uint32
	ProfileCompatibility            uint32
	Level                           uint32
	ParameterID                     uint32
	ChromaFormatIDC                 byte
	SeparateColourPlaneFlag         bool
	BitDepthLumaMinus8              uint
	BitDepthChromaMinus8            uint
	QPPrimeYZeroTransformBypassFlag bool
	SeqScalingMatrixPresentFlag     bool
	SeqScalingLists                 []ScalingList
	Log2MaxFrameNumMinus4           uint
	PicOrderCntType                 uint
	Log2MaxPicOrderCntLsbMinus4     uint
	DeltaPicOrderAlwaysZeroFlag     bool
	OffsetForNonRefPic              uint
	OffsetForTopToBottomField       uint
	RefFramesInPicOrderCntCycle     []uint
	NumRefFrames                    uint
	GapsInFrameNumValueAllowedFlag  bool
	FrameMbsOnlyFlag                bool
	MbAdaptiveFrameFieldFlag        bool
	Direct8x8InferenceFlag          bool
	FrameCroppingFlag               bool
	FrameCropLeftOffset             uint
	FrameCropRightOffset            uint
	FrameCropTopOffset              uint
	FrameCropBottomOffset           uint
	Width                           uint
	Height                          uint
	NrBytesBeforeVUI                int
	NrBytesRead                     int
	VUI                             *VUIParameters
}

type ScalingList []int

type VUIParameters struct {
	SampleAspectRatioWidth             uint
	SampleAspectRatioHeight            uint
	OverscanInfoPresentFlag            bool
	OverscanAppropriateFlag            bool
	VideoSignalTypePresentFlag         bool
	VideoFormat                        uint
	VideoFullRangeFlag                 bool
	ColourDescriptionFlag              bool
	ColourPrimaries                    uint
	TransferCharacteristics            uint
	MatrixCoefficients                 uint
	ChromaLocInfoPresentFlag           bool
	ChromaSampleLocTypeTopField        uint
	ChromaSampleLocTypeBottomField     uint
	TimingInfoPresentFlag              bool
	NumUnitsInTick                     uint
	TimeScale                          uint
	FixedFrameRateFlag                 bool
	NalHrdParametersPresentFlag        bool
	NalHrdParameters                   *HrdParameters
	VclHrdParametersPresentFlag        bool
	VclHrdParameters                   *HrdParameters
	LowDelayHrdFlag                    bool
	PicStructPresentFlag               bool
	BitstreamRestrictionFlag           bool
	MotionVectorsOverPicBoundariesFlag bool
	MaxBytesPerPicDenom                uint
	MaxBitsPerMbDenom                  uint
	Log2MaxMvLengthHorizontal          uint
	Log2MaxMvLengthVertical            uint
	MaxNumReorderFrames                uint
	MaxDecFrameBuffering               uint
}

type HrdParameters struct {
	CpbCountMinus1                     uint
	BitRateScale                       uint
	CpbSizeScale                       uint
	CpbEntries                         []CpbEntry
	InitialCpbRemovalDelayLengthMinus1 uint
	CpbRemovalDelayLengthMinus1        uint
	DpbOutputDelayLengthMinus1         uint
	TimeOffsetLength                   uint
}

type CpbEntry struct {
	BitRateValueMinus1 uint
	CpbSizeValueMinus1 uint
	CbrFlag            bool
}

func ParseSPSNALUnit(data []byte, parseVUIBeyondAspectRatio bool) (*SPS, error) {

	sps := &SPS{}

	rd := bytes.NewReader(data)
	reader := NewEBSPReader(rd)

	nalHdr := reader.Read(8)
	nalType := GetNaluType(byte(nalHdr))
	if nalType != NALU_SPS {
		return nil, ErrNotSPS
	}

	sps.Profile = uint32(reader.Read(8))
	sps.ProfileCompatibility = uint32(reader.Read(8))
	sps.Level = uint32(reader.Read(8))
	sps.ParameterID = uint32(reader.ReadExpGolomb())
	sps.ChromaFormatIDC = 1

	if sps.Profile == 138 {
		sps.ChromaFormatIDC = 0
	}

	switch sps.Profile {
	case 100, 110, 122, 244, 44, 83, 86, 118, 128, 138, 139, 134, 135:
		sps.ChromaFormatIDC = byte(reader.ReadExpGolomb())
		if sps.ChromaFormatIDC == 3 {
			sps.SeparateColourPlaneFlag = reader.ReadFlag()
		}
		sps.BitDepthLumaMinus8 = reader.ReadExpGolomb()
		sps.BitDepthChromaMinus8 = reader.ReadExpGolomb()
		sps.QPPrimeYZeroTransformBypassFlag = reader.ReadFlag()
		sps.SeqScalingMatrixPresentFlag = reader.ReadFlag()
		if sps.SeqScalingMatrixPresentFlag {
			nrScalingLists := 12
			if sps.ChromaFormatIDC != 3 {
				nrScalingLists = 8
			}
			sps.SeqScalingLists = make([]ScalingList, nrScalingLists)

			for i := 0; i < nrScalingLists; i++ {
				seqScalingPresent := reader.ReadFlag()
				if !seqScalingPresent {
					sps.SeqScalingLists[i] = nil
					continue
				}
				sizeOfScalingList := 16
				if i >= 6 {
					sizeOfScalingList = 64
				}
				sps.SeqScalingLists[i] = readScalingList(reader, sizeOfScalingList)
			}
		}
	default:
	}

	sps.Log2MaxFrameNumMinus4 = reader.ReadExpGolomb()
	sps.PicOrderCntType = reader.ReadExpGolomb()
	switch sps.PicOrderCntType {
	case 0:
		sps.Log2MaxPicOrderCntLsbMinus4 = reader.ReadExpGolomb()
	case 1:
		sps.DeltaPicOrderAlwaysZeroFlag = reader.ReadFlag()
		sps.OffsetForNonRefPic = reader.ReadExpGolomb()
		sps.OffsetForTopToBottomField = reader.ReadExpGolomb()
		numRefFramesInPicOrderCntCycle := reader.ReadExpGolomb()
		if numRefFramesInPicOrderCntCycle > 255 {
			return nil, fmt.Errorf("invalid num_ref_frames_in_pic_order_cnt_cycle: %d", numRefFramesInPicOrderCntCycle)
		}
		sps.RefFramesInPicOrderCntCycle = make([]uint, numRefFramesInPicOrderCntCycle)
		for i := 0; i < int(numRefFramesInPicOrderCntCycle); i++ {
			sps.RefFramesInPicOrderCntCycle[i] = reader.ReadExpGolomb()
		}
	}

	sps.NumRefFrames = reader.ReadExpGolomb()
	sps.GapsInFrameNumValueAllowedFlag = reader.ReadFlag()

	picWidthInMbsUnitsMinus1 := reader.ReadExpGolomb()
	picHeightInMbsUnitsMinus1 := reader.ReadExpGolomb()

	sps.Width = (picWidthInMbsUnitsMinus1 + 1) * 16
	sps.Height = (picHeightInMbsUnitsMinus1 + 1) * 16

	sps.FrameMbsOnlyFlag = reader.ReadFlag()
	if !sps.FrameMbsOnlyFlag {
		sps.MbAdaptiveFrameFieldFlag = reader.ReadFlag()
	}
	sps.Direct8x8InferenceFlag = reader.ReadFlag()
	sps.FrameCroppingFlag = reader.ReadFlag()
	var cropUnitX, cropUnitY uint
	var frameMbsOnly uint = 0
	if sps.FrameMbsOnlyFlag {
		frameMbsOnly = 1
	} else {
		sps.Height *= 2
	}
	if sps.FrameCroppingFlag {
		switch sps.ChromaFormatIDC {
		case 0:
			cropUnitX, cropUnitY = 1, 2-frameMbsOnly
		case 1:
			cropUnitX, cropUnitY = 2, 2*(2-frameMbsOnly)
		case 2:
			cropUnitX, cropUnitY = 2, 1*(2-frameMbsOnly)
		case 3:
			cropUnitX, cropUnitY = 1, 1*(2-frameMbsOnly)
		default:
			return nil, fmt.Errorf("non-vaild chroma_format_idc value: %d", sps.ChromaFormatIDC)
		}

		sps.FrameCropLeftOffset = reader.ReadExpGolomb()
		sps.FrameCropRightOffset = reader.ReadExpGolomb()
		sps.FrameCropTopOffset = reader.ReadExpGolomb()
		sps.FrameCropBottomOffset = reader.ReadExpGolomb()

		frameCropWidth := sps.FrameCropLeftOffset + sps.FrameCropRightOffset
		frameCropHeight := sps.FrameCropTopOffset + sps.FrameCropBottomOffset

		cropW := frameCropWidth * cropUnitX
		cropH := frameCropHeight * cropUnitY
		if cropW > sps.Width || cropH > sps.Height {
			return nil, fmt.Errorf("crop %dx%d exceeds frame %dx%d", cropW, cropH, sps.Width, sps.Height)
		}
		sps.Width -= cropW
		sps.Height -= cropH
	}

	if sps.Width == 0 || sps.Height == 0 || sps.Width > 16384 || sps.Height > 16384 {
		return nil, fmt.Errorf("invalid picture size %dx%d", sps.Width, sps.Height)
	}

	vuiParametersPresentFlag := reader.ReadFlag()
	sps.NrBytesBeforeVUI = reader.NrBytesRead()
	if vuiParametersPresentFlag {
		sps.VUI = parseVUI(reader, parseVUIBeyondAspectRatio)
	}
	sps.NrBytesRead = reader.NrBytesRead()

	return sps, reader.AccError()
}

func (s *SPS) CpbDpbDelaysPresent() bool {
	if s.VUI == nil {
		return false
	}
	return (s.VUI.NalHrdParametersPresentFlag ||
		s.VUI.VclHrdParametersPresentFlag)
}

func (s *SPS) PicStructPresent() bool {
	if s.VUI == nil {
		return false
	}
	return s.VUI.PicStructPresentFlag
}

func (s *SPS) ChromaArrayType() byte {
	if !s.SeparateColourPlaneFlag {
		return byte(s.ChromaFormatIDC)
	}
	return 0
}

func parseVUI(reader *EBSPReader, parseVUIBeyondAspectRatio bool) *VUIParameters {
	vui := &VUIParameters{}
	var err error
	aspectRatioInfoPresentFlag := reader.ReadFlag()
	if aspectRatioInfoPresentFlag {
		aspectRatioIDC := reader.Read(8)
		if aspectRatioIDC == ExtendedSAR {
			vui.SampleAspectRatioWidth = reader.Read(16)
			vui.SampleAspectRatioHeight = reader.Read(16)
		} else {
			vui.SampleAspectRatioWidth, vui.SampleAspectRatioHeight, err = GetSARfromIDC(aspectRatioIDC)
			if err != nil {
				reader.SetError(fmt.Errorf("GetSARFromIDC: %w", err))
			}
		}
	}
	if !parseVUIBeyondAspectRatio {
		return vui
	}
	vui.OverscanInfoPresentFlag = reader.ReadFlag()
	if vui.OverscanInfoPresentFlag {
		vui.OverscanAppropriateFlag = reader.ReadFlag()
	}
	vui.VideoSignalTypePresentFlag = reader.ReadFlag()
	if vui.VideoSignalTypePresentFlag {
		vui.VideoFormat = reader.Read(3)
		vui.VideoFullRangeFlag = reader.ReadFlag()
		vui.ColourDescriptionFlag = reader.ReadFlag()
		if vui.ColourDescriptionFlag {
			vui.ColourPrimaries = reader.Read(8)
			vui.TransferCharacteristics = reader.Read(8)
			vui.MatrixCoefficients = reader.Read(8)
		}
	}
	vui.ChromaLocInfoPresentFlag = reader.ReadFlag()
	if vui.ChromaLocInfoPresentFlag {
		vui.ChromaSampleLocTypeTopField = reader.ReadExpGolomb()
		vui.ChromaSampleLocTypeBottomField = reader.ReadExpGolomb()
	}
	vui.TimingInfoPresentFlag = reader.ReadFlag()
	if vui.TimingInfoPresentFlag {
		vui.NumUnitsInTick = reader.Read(32)
		vui.TimeScale = reader.Read(32)
		vui.FixedFrameRateFlag = reader.ReadFlag()
	}
	vui.NalHrdParametersPresentFlag = reader.ReadFlag()
	if vui.NalHrdParametersPresentFlag {
		vui.NalHrdParameters = parseHrdParameters(reader)
	}
	vui.VclHrdParametersPresentFlag = reader.ReadFlag()
	if vui.VclHrdParametersPresentFlag {
		vui.VclHrdParameters = parseHrdParameters(reader)
	}
	if vui.NalHrdParametersPresentFlag || vui.VclHrdParametersPresentFlag {
		vui.LowDelayHrdFlag = reader.ReadFlag()
	}
	vui.PicStructPresentFlag = reader.ReadFlag()
	vui.BitstreamRestrictionFlag = reader.ReadFlag()
	if vui.BitstreamRestrictionFlag {
		vui.MotionVectorsOverPicBoundariesFlag = reader.ReadFlag()
		vui.MaxBytesPerPicDenom = reader.ReadExpGolomb()
		vui.MaxBitsPerMbDenom = reader.ReadExpGolomb()
		vui.Log2MaxMvLengthHorizontal = reader.ReadExpGolomb()
		vui.Log2MaxMvLengthVertical = reader.ReadExpGolomb()
		vui.MaxNumReorderFrames = reader.ReadExpGolomb()
		vui.MaxDecFrameBuffering = reader.ReadExpGolomb()
	}

	return vui
}

func parseHrdParameters(r *EBSPReader) *HrdParameters {
	hp := &HrdParameters{}
	hp.CpbCountMinus1 = r.ReadExpGolomb()

	hp.BitRateScale = r.Read(4)
	hp.CpbSizeScale = r.Read(4)
	for schedSelIdx := uint(0); schedSelIdx <= hp.CpbCountMinus1; schedSelIdx++ {
		ce := CpbEntry{}
		ce.BitRateValueMinus1 = r.ReadExpGolomb()
		ce.CpbSizeValueMinus1 = r.ReadExpGolomb()
		ce.CbrFlag = r.ReadFlag()
		hp.CpbEntries = append(hp.CpbEntries, ce)
	}
	hp.InitialCpbRemovalDelayLengthMinus1 = r.Read(5)
	hp.CpbRemovalDelayLengthMinus1 = r.Read(5)
	hp.DpbOutputDelayLengthMinus1 = r.Read(5)
	hp.TimeOffsetLength = r.Read(5)
	return hp
}

func (a *SPS) ConstraintFlags() byte {
	return byte(a.ProfileCompatibility >> 4)
}

func GetSARfromIDC(index uint) (uint, uint, error) {
	if index == 0 {
		return 1, 1, nil
	}
	if index > 16 {
		return 0, 0, fmt.Errorf("SAR bad index %d", index)
	}
	aspectRatioTable := [][]uint{
		{1, 1}, {12, 11}, {10, 11}, {16, 11},
		{40, 33}, {24, 11}, {20, 11}, {32, 11},
		{80, 33}, {18, 11}, {15, 11}, {64, 33},
		{160, 99}, {4, 3}, {3, 2}, {2, 1}}
	return aspectRatioTable[index-1][0], aspectRatioTable[index-1][1], nil
}

func readScalingList(reader *EBSPReader, sizeOfScalingList int) ScalingList {
	scalingList := make([]int, sizeOfScalingList)
	lastScale := 8
	nextScale := 8
	for j := 0; j < sizeOfScalingList; j++ {
		if nextScale != 0 {
			deltaScale := reader.ReadSignedGolomb()
			nextScale = (lastScale + deltaScale + 256) % 256
		}
		if nextScale == 0 {
			scalingList[j] = lastScale
		} else {
			scalingList[j] = nextScale
		}
		lastScale = scalingList[j]
	}
	return scalingList
}
