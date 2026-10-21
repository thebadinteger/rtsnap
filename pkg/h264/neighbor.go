package h264

import "fmt"

const (
	MBTypePSkip  = -1
	MBTypeINxN   = 0
	MBTypeI16x16 = 1
	MBTypeIPCM   = 25
)

func I16x16PredMode(mbType int) int {
	return (mbType - 1) % 4
}

func I16x16CBPLuma(mbType int) int {
	if (mbType-1)/12 > 0 {
		return 15
	}
	return 0
}

func I16x16CBPChroma(mbType int) int {
	return ((mbType - 1) / 4) % 3
}

type MBData struct {
	MBType              int
	TransformSize8x8    bool
	IntraPredMode16x16  int
	Intra4x4PredMode    [16]int
	Intra8x8PredMode    [4]int
	IntraChromaPredMode int
	CBPLuma             int
	CBPChroma           int
	QPY                 int
	QPDelta             int
	Intra16x16DCLevel   [16]int32
	Intra16x16ACLevel   [16][15]int32
	LumaLevel4x4        [16][16]int32
	LumaLevel8x8        [4][64]int32
	ChromaDCLevel       [2][4]int32
	ChromaACLevel       [2][4][15]int32

	CodedBlockFlag [6][16]uint8

	NzCoeffLuma   [16]int
	NzCoeffChroma [8]int
	TotalCoeff    [24]int

	SliceStart   int
	DeblockIDC   uint32
	DeblockAlpha int
	DeblockBeta  int

	IsPCM   bool
	PCMLuma [256]uint8
	PCMCb   [256]uint8
	PCMCr   [256]uint8
}

type SliceContext struct {
	Cabac    *CabacDecoder
	Ctx      *[1024]CtxState
	MBWidth  int
	MBHeight int
	TotalMBs int
	QPY      int
	MBs      []MBData

	IsCAVLC bool
	Br      *BitReader

	Transform8x8ModeFlag bool
	ChromaArrayType      int
	BitDepthY            int
	BitDepthC            int
	ChromaQpIndexOffset  int

	PrevMBQPDeltaNonZero bool
	SliceStartMB         int
	DeblockIDC           uint32
	DeblockAlpha         int
	DeblockBeta          int

	TraceMBCMP bool

	coeffBuf   [64]int32
	sigFlags   [64]bool
	sigIndices [64]int
}

func (sc *SliceContext) MBAvailA(mbIdx int) *MBData {
	mbX := mbIdx % sc.MBWidth
	if mbX == 0 || mbIdx-1 < sc.SliceStartMB {
		return nil
	}
	return &sc.MBs[mbIdx-1]
}

func (sc *SliceContext) MBAvailB(mbIdx int) *MBData {
	if mbIdx < sc.MBWidth || mbIdx-sc.MBWidth < sc.SliceStartMB {
		return nil
	}
	return &sc.MBs[mbIdx-sc.MBWidth]
}

func (sc *SliceContext) applyQPDelta(mb *MBData, delta int) error {
	qpBdOffsetY := 6 * (sc.BitDepthY - 8)
	half := qpBdOffsetY / 2
	if delta < -26-half || delta > 25+half {
		return fmt.Errorf("mb_qp_delta %d out of range", delta)
	}

	mb.QPDelta = delta
	qpRange := 52 + qpBdOffsetY
	mb.QPY = ((sc.QPY + delta + qpRange + 2*qpBdOffsetY) % qpRange) - qpBdOffsetY
	sc.QPY = mb.QPY

	return nil
}
