package h264

import (
	"fmt"
)

func DecodeMBTypeIntraCAVLC(br *BitReader) (int, error) {
	val, err := br.ReadUE()
	if err != nil {
		return 0, fmt.Errorf("mb_type: %w", err)
	}
	if val > 25 {
		return 0, fmt.Errorf("mb_type %d out of range", val)
	}
	return int(val), nil
}

func DecodeTransformSize8x8FlagCAVLC(br *BitReader) (bool, error) {
	return br.ReadFlag()
}

func DecodeIntra4x4PredModeCAVLC(br *BitReader) (bool, int, error) {
	flag, err := br.ReadFlag()
	if err != nil {
		return false, 0, fmt.Errorf("prev_intra4x4_pred_mode_flag: %w", err)
	}
	if flag {
		return true, -1, nil
	}
	rem, err := br.ReadBits(3)
	if err != nil {
		return false, 0, fmt.Errorf("rem_intra4x4_pred_mode: %w", err)
	}
	return false, int(rem), nil
}

func DecodeIntraChromaPredModeCAVLC(br *BitReader) (int, error) {
	val, err := br.ReadUE()
	if err != nil {
		return 0, fmt.Errorf("intra_chroma_pred_mode: %w", err)
	}
	if val > 3 {
		return 0, fmt.Errorf("intra_chroma_pred_mode %d out of range", val)
	}
	return int(val), nil
}

func DecodeCBPCAVLC(br *BitReader) (int, int, error) {
	val, err := br.ReadUE()
	if err != nil {
		return 0, 0, fmt.Errorf("cbp: %w", err)
	}
	if val > 47 {
		return 0, 0, fmt.Errorf("cbp %d out of range for I-slice", val)
	}
	cbp := int(GolombToIntra4x4CBP(int(val)))
	cbpLuma := cbp & 0x0F
	cbpChroma := (cbp >> 4) & 0x03
	return cbpLuma, cbpChroma, nil
}

func DecodeQPDeltaCAVLC(br *BitReader) (int, error) {
	val, err := br.ReadSE()
	if err != nil {
		return 0, fmt.Errorf("mb_qp_delta: %w", err)
	}
	return int(val), nil
}

func DeriveNC(sc *SliceContext, mbIdx int, blkIdx int, isChromaDC bool) int {
	if isChromaDC {
		return -1
	}

	nA := -1
	nB := -1

	leftIdx := lumaLeftNeighbor[blkIdx]
	if leftIdx >= 0 {
		nA = sc.MBs[mbIdx].NzCoeffLuma[leftIdx]
	} else if mbA := sc.MBAvailA(mbIdx); mbA != nil {
		nA = mbA.NzCoeffLuma[lumaLeftFromMBA[blkIdx]]
	}

	topIdx := lumaTopNeighbor[blkIdx]
	if topIdx >= 0 {
		nB = sc.MBs[mbIdx].NzCoeffLuma[topIdx]
	} else if mbB := sc.MBAvailB(mbIdx); mbB != nil {
		nB = mbB.NzCoeffLuma[lumaTopFromMBB[blkIdx]]
	}

	if nA >= 0 && nB >= 0 {
		return (nA + nB + 1) >> 1
	}
	if nA >= 0 {
		return nA
	}
	if nB >= 0 {
		return nB
	}
	return 0
}

func DeriveChromaNC(sc *SliceContext, mbIdx int, blkIdx int) int {
	nA := -1
	nB := -1

	compBase := (blkIdx / 4) * 4
	localIdx := blkIdx % 4
	x := localIdx % 2
	y := localIdx / 2

	if x > 0 {
		nA = sc.MBs[mbIdx].NzCoeffChroma[compBase+localIdx-1]
	} else if mbA := sc.MBAvailA(mbIdx); mbA != nil {
		nA = mbA.NzCoeffChroma[compBase+y*2+1]
	}

	if y > 0 {
		nB = sc.MBs[mbIdx].NzCoeffChroma[compBase+localIdx-2]
	} else if mbB := sc.MBAvailB(mbIdx); mbB != nil {
		nB = mbB.NzCoeffChroma[compBase+2+x]
	}

	if nA >= 0 && nB >= 0 {
		return (nA + nB + 1) >> 1
	}
	if nA >= 0 {
		return nA
	}
	if nB >= 0 {
		return nB
	}
	return 0
}
