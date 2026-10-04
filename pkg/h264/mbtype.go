package h264

func DecodeMBTypeIntra(sc *SliceContext, mbIdx int) int {
	d := sc.Cabac
	ctx := sc.Ctx

	ctxIdxInc := 0
	if mbA := sc.MBAvailA(mbIdx); mbA != nil && mbA.MBType != MBTypeINxN {
		ctxIdxInc++
	}
	if mbB := sc.MBAvailB(mbIdx); mbB != nil && mbB.MBType != MBTypeINxN {
		ctxIdxInc++
	}

	bin0 := d.DecodeDecision(&ctx[3+ctxIdxInc])
	if bin0 == 0 {
		return MBTypeINxN
	}

	binT := d.DecodeTerminate()
	if binT == 1 {
		return MBTypeIPCM
	}

	bin1 := d.DecodeDecision(&ctx[6])

	bin2 := d.DecodeDecision(&ctx[7])

	var cbpChroma int
	if bin2 == 1 {
		bin3 := d.DecodeDecision(&ctx[8])
		if bin3 == 1 {
			cbpChroma = 2
		} else {
			cbpChroma = 1
		}
	}

	bin4 := d.DecodeDecision(&ctx[9])
	bin5 := d.DecodeDecision(&ctx[10])
	predMode := int(bin4)*2 + int(bin5)

	mbType := 1 + predMode + 4*cbpChroma
	if bin1 == 1 {
		mbType += 12
	}

	return mbType
}

func DecodeTransformSize8x8Flag(sc *SliceContext, mbIdx int) bool {
	ctx := sc.Ctx
	ctxIdxInc := 0
	mbA := sc.MBAvailA(mbIdx)
	mbB := sc.MBAvailB(mbIdx)

	if mbA != nil && mbA.TransformSize8x8 {
		ctxIdxInc++
	}
	if mbB != nil && mbB.TransformSize8x8 {
		ctxIdxInc++
	}
	decision := sc.Cabac.DecodeDecision(&ctx[399+ctxIdxInc])
	return decision == 1
}

func DecodeIntraChromaPredMode(sc *SliceContext, mbIdx int) int {
	ctx := sc.Ctx

	ctxIdxInc := 0
	if mbA := sc.MBAvailA(mbIdx); mbA != nil && mbA.IntraChromaPredMode != 0 {
		ctxIdxInc++
	}
	if mbB := sc.MBAvailB(mbIdx); mbB != nil && mbB.IntraChromaPredMode != 0 {
		ctxIdxInc++
	}

	bin0 := sc.Cabac.DecodeDecision(&ctx[64+ctxIdxInc])
	if bin0 == 0 {
		return 0
	}
	bin1 := sc.Cabac.DecodeDecision(&ctx[67])
	if bin1 == 0 {
		return 1
	}
	bin2 := sc.Cabac.DecodeDecision(&ctx[67])
	if bin2 == 0 {
		return 2
	}
	return 3
}

func DecodeIntra4x4PredMode(sc *SliceContext) (prevFlag bool, rem int) {
	ctx := sc.Ctx
	flag := sc.Cabac.DecodeDecision(&ctx[68])
	if flag == 1 {
		return true, -1
	}
	rem = int(sc.Cabac.DecodeDecision(&ctx[69]))
	rem |= int(sc.Cabac.DecodeDecision(&ctx[69])) << 1
	rem |= int(sc.Cabac.DecodeDecision(&ctx[69])) << 2
	return false, rem
}

func DecodeIntra8x8PredMode(sc *SliceContext) (prevFlag bool, rem int) {
	return DecodeIntra4x4PredMode(sc)
}

func DecodeCBP(sc *SliceContext, mbIdx int) (cbpLuma, cbpChroma int) {
	ctx := sc.Ctx

	for i := range 4 {
		ctxIdxInc := deriveCBPLumaCtx(sc, mbIdx, i, cbpLuma)
		bin := sc.Cabac.DecodeDecision(&ctx[73+ctxIdxInc])
		if bin == 1 {
			cbpLuma |= 1 << uint(i)
		}
	}

	if sc.ChromaArrayType == 1 || sc.ChromaArrayType == 2 {
		ctxIdxInc := deriveCBPChromaCtx(sc, mbIdx, false)
		bin0 := sc.Cabac.DecodeDecision(&ctx[77+ctxIdxInc])
		if bin0 == 1 {
			ctxIdxInc2 := deriveCBPChromaCtx(sc, mbIdx, true)
			bin1 := sc.Cabac.DecodeDecision(&ctx[81+ctxIdxInc2])
			if bin1 == 1 {
				cbpChroma = 2
			} else {
				cbpChroma = 1
			}
		}
	}

	return cbpLuma, cbpChroma
}

func deriveCBPLumaCtx(sc *SliceContext, mbIdx, blkIdx int, currentCBP int) int {
	var condA, condB int

	neighborCBPBit := func(mb *MBData, bit int) int {
		if mb == nil {
			return 0
		}
		if mb.CBPLuma&bit != 0 {
			return 0
		}
		return 1
	}

	switch blkIdx {
	case 0:
		condA = neighborCBPBit(sc.MBAvailA(mbIdx), 2)
		condB = neighborCBPBit(sc.MBAvailB(mbIdx), 4)
	case 1:
		if currentCBP&1 != 0 {
			condA = 0
		} else {
			condA = 1
		}
		condB = neighborCBPBit(sc.MBAvailB(mbIdx), 8)
	case 2:
		condA = neighborCBPBit(sc.MBAvailA(mbIdx), 8)
		if currentCBP&1 != 0 {
			condB = 0
		} else {
			condB = 1
		}
	case 3:
		if currentCBP&4 != 0 {
			condA = 0
		} else {
			condA = 1
		}
		if currentCBP&2 != 0 {
			condB = 0
		} else {
			condB = 1
		}
	}

	return condA + 2*condB
}

func deriveCBPChromaCtx(sc *SliceContext, mbIdx int, secondBin bool) int {
	var condA, condB int

	if mbA := sc.MBAvailA(mbIdx); mbA != nil {
		if !secondBin {
			if mbA.CBPChroma > 0 {
				condA = 1
			}
		} else {
			if mbA.CBPChroma > 1 {
				condA = 1
			}
		}
	}
	if mbB := sc.MBAvailB(mbIdx); mbB != nil {
		if !secondBin {
			if mbB.CBPChroma > 0 {
				condB = 1
			}
		} else {
			if mbB.CBPChroma > 1 {
				condB = 1
			}
		}
	}

	return condA + 2*condB
}

func DecodeQPDelta(sc *SliceContext) int {
	ctx := sc.Ctx
	d := sc.Cabac

	ctxIdx := 60
	if sc.PrevMBQPDeltaNonZero {
		ctxIdx = 61
	}

	bin0 := d.DecodeDecision(&ctx[ctxIdx])
	if bin0 == 0 {
		return 0
	}

	val := 1
	for {
		ctxIdx := 62
		if val > 1 {
			ctxIdx = 63
		}
		bin := d.DecodeDecision(&ctx[ctxIdx])
		if bin == 0 {
			break
		}
		val++
	}

	if val%2 == 1 {
		return (val + 1) / 2
	}
	return -(val / 2)
}

var zScanToBlockX = [16]int{0, 1, 0, 1, 2, 3, 2, 3, 0, 1, 0, 1, 2, 3, 2, 3}

var zScanToBlockY = [16]int{0, 0, 1, 1, 0, 0, 1, 1, 2, 2, 3, 3, 2, 2, 3, 3}

var rasterToZScan = [16]int{0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15}

func derivePredIntra4x4PredMode(sc *SliceContext, mbIdx, blkIdx int) int {
	predModeA := -1
	predModeB := -1

	bx := zScanToBlockX[blkIdx]
	by := zScanToBlockY[blkIdx]

	if bx > 0 {
		leftIdx := rasterToZScan[by*4+(bx-1)]
		predModeA = sc.MBs[mbIdx].Intra4x4PredMode[leftIdx]
	} else if mbA := sc.MBAvailA(mbIdx); mbA != nil {
		if mbA.MBType == MBTypeINxN && mbA.TransformSize8x8 {
			predModeA = mbA.Intra8x8PredMode[(by/2)*2+1]
		} else if mbA.MBType == MBTypeINxN {
			rightIdx := rasterToZScan[by*4+3]
			predModeA = mbA.Intra4x4PredMode[rightIdx]
		} else {
			predModeA = 2
		}
	}

	if by > 0 {
		topIdx := rasterToZScan[(by-1)*4+bx]
		predModeB = sc.MBs[mbIdx].Intra4x4PredMode[topIdx]
	} else if mbB := sc.MBAvailB(mbIdx); mbB != nil {
		if mbB.MBType == MBTypeINxN && mbB.TransformSize8x8 {
			predModeB = mbB.Intra8x8PredMode[2+bx/2]
		} else if mbB.MBType == MBTypeINxN {
			botIdx := rasterToZScan[3*4+bx]
			predModeB = mbB.Intra4x4PredMode[botIdx]
		} else {
			predModeB = 2
		}
	}

	if predModeA < 0 || predModeB < 0 {
		return 2
	}
	if predModeA < predModeB {
		return predModeA
	}
	return predModeB
}

func derivePredIntra8x8PredMode(sc *SliceContext, mbIdx, blk8x8Idx int) int {
	predModeA := -1
	predModeB := -1

	x := blk8x8Idx % 2
	y := blk8x8Idx / 2

	if x > 0 {
		predModeA = sc.MBs[mbIdx].Intra8x8PredMode[blk8x8Idx-1]
	} else if mbA := sc.MBAvailA(mbIdx); mbA != nil {
		if mbA.MBType == MBTypeINxN && mbA.TransformSize8x8 {
			predModeA = mbA.Intra8x8PredMode[y*2+1]
		} else if mbA.MBType == MBTypeINxN {
			rightIdx := rasterToZScan[2*y*4+3]
			predModeA = mbA.Intra4x4PredMode[rightIdx]
		} else {
			predModeA = 2
		}
	}

	if y > 0 {
		predModeB = sc.MBs[mbIdx].Intra8x8PredMode[blk8x8Idx-2]
	} else if mbB := sc.MBAvailB(mbIdx); mbB != nil {
		if mbB.MBType == MBTypeINxN && mbB.TransformSize8x8 {
			predModeB = mbB.Intra8x8PredMode[2+x]
		} else if mbB.MBType == MBTypeINxN {
			botIdx := rasterToZScan[3*4+2*x]
			predModeB = mbB.Intra4x4PredMode[botIdx]
		} else {
			predModeB = 2
		}
	}

	if predModeA < 0 || predModeB < 0 {
		return 2
	}
	if predModeA < predModeB {
		return predModeA
	}
	return predModeB
}
