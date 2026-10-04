package h264

const (
	CtxBlockCatIntra16x16DC = 0
	CtxBlockCatIntra16x16AC = 1
	CtxBlockCatLuma4x4      = 2
	CtxBlockCatChromaDC     = 3
	CtxBlockCatChromaAC     = 4
	CtxBlockCatLuma8x8      = 5
)

var codedBlockFlagOffset = [6]int{85, 89, 93, 97, 101, 1012}

var significantCoeffFlagOffset = [6]int{105, 120, 134, 149, 152, 402}

var lastSignificantCoeffFlagOffset = [6]int{166, 181, 195, 210, 213, 417}

var coeffAbsLevelMinus1Offset = [6]int{227, 237, 247, 257, 266, 426}

var lastCoeffFlagOffset8x8 = [63]int{
	0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
	3, 3, 3, 3, 3, 3, 3, 3, 4, 4, 4, 4, 4, 4, 4, 4,
	5, 5, 5, 5, 6, 6, 6, 6, 7, 7, 7, 7, 8, 8, 8,
}

var coeffAbsLevel1Ctx = [8]int{1, 2, 3, 4, 0, 0, 0, 0}

var coeffAbsLevelGt1Ctx = [8]int{5, 5, 5, 5, 6, 7, 8, 9}

var coeffAbsLevelTransition = [2][8]int{
	{1, 2, 3, 3, 4, 5, 6, 7},
	{4, 4, 4, 4, 5, 6, 7, 7},
}

func DecodeResidual(sc *SliceContext, mbIdx int, ctxBlockCat int, blkIdx int, maxCoeff int) []int32 {
	coeffs := sc.coeffBuf[:maxCoeff]
	for i := range coeffs {
		coeffs[i] = 0
	}

	if ctxBlockCat != CtxBlockCatLuma8x8 {
		cbfCtx := deriveCodedBlockFlagCtx(sc, mbIdx, ctxBlockCat, blkIdx)
		ctxIdx := codedBlockFlagOffset[ctxBlockCat] + cbfCtx
		cbf := sc.Cabac.DecodeDecision(&sc.Ctx[ctxIdx])
		sc.MBs[mbIdx].CodedBlockFlag[ctxBlockCat][blkIdx] = cbf
		if cbf == 0 {
			return coeffs
		}
	}

	numCoeff := maxCoeff
	if ctxBlockCat == CtxBlockCatLuma8x8 {
		numCoeff = 64
	}

	sigFlags := sc.sigFlags[:numCoeff]
	for i := range sigFlags {
		sigFlags[i] = false
	}
	lastIdx := numCoeff - 1

	for i := 0; i < numCoeff-1; i++ {
		sigCtx := deriveSignificantCoeffFlagCtx(ctxBlockCat, i)
		sigCtxIdx := significantCoeffFlagOffset[ctxBlockCat] + sigCtx
		sig := sc.Cabac.DecodeDecision(&sc.Ctx[sigCtxIdx])

		if sig == 1 {
			sigFlags[i] = true

			lastCtx := deriveLastSignificantCoeffFlagCtx(ctxBlockCat, i)
			lastCtxIdx := lastSignificantCoeffFlagOffset[ctxBlockCat] + lastCtx
			last := sc.Cabac.DecodeDecision(&sc.Ctx[lastCtxIdx])
			if last == 1 {
				lastIdx = i
				break
			}
		}
	}

	if lastIdx == numCoeff-1 {
		sigFlags[lastIdx] = true
	}

	nSig := 0
	for i := lastIdx; i >= 0; i-- {
		if sigFlags[i] {
			sc.sigIndices[nSig] = i
			nSig++
		}
	}

	nodeCtx := 0
	baseCtx := coeffAbsLevelMinus1Offset[ctxBlockCat]

	for _, idx := range sc.sigIndices[:nSig] {
		ctxIdxInc := coeffAbsLevel1Ctx[nodeCtx]
		bin := sc.Cabac.DecodeDecision(&sc.Ctx[baseCtx+ctxIdxInc])

		if bin == 0 {
			nodeCtx = coeffAbsLevelTransition[0][nodeCtx]
			sign := sc.Cabac.DecodeBypass()
			if sign == 1 {
				coeffs[idx] = -1
			} else {
				coeffs[idx] = 1
			}
		} else {
			ctxIdxInc = coeffAbsLevelGt1Ctx[nodeCtx]
			nodeCtx = coeffAbsLevelTransition[1][nodeCtx]

			coeffAbs := int32(2)
			for coeffAbs < 15 {
				bin := sc.Cabac.DecodeDecision(&sc.Ctx[baseCtx+ctxIdxInc])
				if bin == 0 {
					break
				}
				coeffAbs++
			}

			if coeffAbs >= 15 {
				suffix := decodeExpGolombBypass(sc.Cabac)
				coeffAbs += int32(suffix)
			}

			sign := sc.Cabac.DecodeBypass()
			if sign == 1 {
				coeffs[idx] = -coeffAbs
			} else {
				coeffs[idx] = coeffAbs
			}
		}
	}

	return coeffs
}

func decodeExpGolombBypass(d *CabacDecoder) uint32 {
	k := uint(0)
	for {
		bin := d.DecodeBypass()
		if bin == 0 {
			break
		}
		k++
	}
	var val uint32
	if k > 0 {
		val = d.ReadBypassU(int(k))
	}
	return (1 << k) - 1 + val
}

func deriveCodedBlockFlagCtx(sc *SliceContext, mbIdx int, ctxBlockCat int, blkIdx int) int {

	condA := 1
	condB := 1

	switch ctxBlockCat {
	case CtxBlockCatIntra16x16DC:
		if mbA := sc.MBAvailA(mbIdx); mbA != nil {
			condA = int(mbA.CodedBlockFlag[ctxBlockCat][0])
		}
		if mbB := sc.MBAvailB(mbIdx); mbB != nil {
			condB = int(mbB.CodedBlockFlag[ctxBlockCat][0])
		}
	case CtxBlockCatIntra16x16AC, CtxBlockCatLuma4x4:
		condA, condB = deriveLumaBlockNeighborCBF(sc, mbIdx, ctxBlockCat, blkIdx)
	case CtxBlockCatChromaDC:
		if mbA := sc.MBAvailA(mbIdx); mbA != nil {
			condA = int(mbA.CodedBlockFlag[ctxBlockCat][blkIdx])
		}
		if mbB := sc.MBAvailB(mbIdx); mbB != nil {
			condB = int(mbB.CodedBlockFlag[ctxBlockCat][blkIdx])
		}
	case CtxBlockCatChromaAC:
		condA, condB = deriveChromaACBlockNeighborCBF(sc, mbIdx, blkIdx)
	}

	return condA + 2*condB
}

var lumaLeftNeighbor = [16]int{-1, 0, -1, 2, 1, 4, 3, 6, -1, 8, -1, 10, 9, 12, 11, 14}
var lumaTopNeighbor = [16]int{-1, -1, 0, 1, -1, -1, 4, 5, 2, 3, 8, 9, 6, 7, 12, 13}

var lumaLeftFromMBA = [16]int{5, -1, 7, -1, -1, -1, -1, -1, 13, -1, 15, -1, -1, -1, -1, -1}

var lumaTopFromMBB = [16]int{10, 11, -1, -1, 14, 15, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1}

func getLumaBlockCBF(mb *MBData, blkIdx int) int {
	if mb.MBType == MBTypeIPCM {
		return 1
	}
	if mb.MBType >= 1 && mb.MBType <= 24 {
		return int(mb.CodedBlockFlag[CtxBlockCatIntra16x16AC][blkIdx])
	}
	if mb.MBType == MBTypeINxN {
		if mb.TransformSize8x8 {
			return int(mb.CodedBlockFlag[CtxBlockCatLuma8x8][blkIdx/4])
		}
		return int(mb.CodedBlockFlag[CtxBlockCatLuma4x4][blkIdx])
	}
	return 0
}

func deriveLumaBlockNeighborCBF(sc *SliceContext, mbIdx int, ctxBlockCat int, blkIdx int) (condA, condB int) {
	condA = 1
	condB = 1

	leftIdx := lumaLeftNeighbor[blkIdx]
	if leftIdx >= 0 {
		condA = int(sc.MBs[mbIdx].CodedBlockFlag[ctxBlockCat][leftIdx])
	} else if mbA := sc.MBAvailA(mbIdx); mbA != nil {
		condA = getLumaBlockCBF(mbA, lumaLeftFromMBA[blkIdx])
	}

	topIdx := lumaTopNeighbor[blkIdx]
	if topIdx >= 0 {
		condB = int(sc.MBs[mbIdx].CodedBlockFlag[ctxBlockCat][topIdx])
	} else if mbB := sc.MBAvailB(mbIdx); mbB != nil {
		condB = getLumaBlockCBF(mbB, lumaTopFromMBB[blkIdx])
	}

	return condA, condB
}

func deriveChromaACBlockNeighborCBF(sc *SliceContext, mbIdx int, blkIdx int) (condA, condB int) {
	condA = 1
	condB = 1

	compBase := (blkIdx / 4) * 4
	localIdx := blkIdx % 4

	x := localIdx % 2
	y := localIdx / 2

	if x > 0 {
		condA = int(sc.MBs[mbIdx].CodedBlockFlag[CtxBlockCatChromaAC][compBase+localIdx-1])
	} else if mbA := sc.MBAvailA(mbIdx); mbA != nil {
		condA = int(mbA.CodedBlockFlag[CtxBlockCatChromaAC][compBase+y*2+1])
	}

	if y > 0 {
		condB = int(sc.MBs[mbIdx].CodedBlockFlag[CtxBlockCatChromaAC][compBase+localIdx-2])
	} else if mbB := sc.MBAvailB(mbIdx); mbB != nil {
		condB = int(mbB.CodedBlockFlag[CtxBlockCatChromaAC][compBase+2+x])
	}

	return condA, condB
}

func deriveSignificantCoeffFlagCtx(ctxBlockCat int, scanIdx int) int {
	if ctxBlockCat < 5 {
		return scanIdx
	}
	if scanIdx < 63 {
		return significantCoeffCtx8x8[scanIdx]
	}
	return 0
}

var significantCoeffCtx8x8 = [63]int{
	0, 1, 2, 3, 4, 5, 5, 4, 4, 3, 3, 4, 4, 4, 5, 5,
	4, 4, 4, 4, 3, 3, 6, 7, 7, 7, 8, 9, 10, 9, 8, 7,
	7, 6, 11, 12, 13, 11, 6, 7, 8, 9, 14, 10, 9, 8, 6, 11,
	12, 13, 11, 6, 9, 14, 10, 9, 11, 12, 13, 11, 14, 10, 12,
}

func deriveLastSignificantCoeffFlagCtx(ctxBlockCat int, scanIdx int) int {
	if ctxBlockCat < 5 {
		return scanIdx
	}
	if scanIdx < 63 {
		return lastCoeffFlagOffset8x8[scanIdx]
	}
	return 0
}
