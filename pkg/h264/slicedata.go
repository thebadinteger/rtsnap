package h264

import (
	"fmt"
	"sync"
)

var mbPool sync.Pool

func getMBs(count int) []MBData {
	if v := mbPool.Get(); v != nil {
		pp := v.(*[]MBData)
		if cap(*pp) >= count {
			buf := (*pp)[:count]
			clear(buf)
			return buf
		}
	}
	return make([]MBData, count)
}

func putMBs(buf []MBData) {
	if cap(buf) > 0 {
		buf = buf[:0]
		mbPool.Put(&buf)
	}
}

func decodeSliceCABAC(sc *SliceContext, sliceData []byte, sliceQPY int, firstMB int) (int, error) {
	models := InitModels(sliceQPY, 2, 0)
	dec, err := NewCabacDecoder(sliceData)
	if err != nil {
		return firstMB, fmt.Errorf("cabac init: %w", err)
	}

	sc.Cabac = dec
	sc.Ctx = (*[1024]CtxState)(&models)
	sc.QPY = sliceQPY
	sc.SliceStartMB = firstMB
	sc.PrevMBQPDeltaNonZero = false

	lastMB := firstMB
	for mbIdx := firstMB; mbIdx < sc.TotalMBs; mbIdx++ {
		sc.MBs[mbIdx].QPY = sliceQPY
		sc.MBs[mbIdx].SliceStart = firstMB
		sc.MBs[mbIdx].DeblockIDC = sc.DeblockIDC
		sc.MBs[mbIdx].DeblockAlpha = sc.DeblockAlpha
		sc.MBs[mbIdx].DeblockBeta = sc.DeblockBeta
		err := decodeMacroblock(sc, mbIdx)
		if err != nil {
			return lastMB, fmt.Errorf("mb %d: %w", mbIdx, err)
		}
		lastMB = mbIdx + 1

		if dec.Overread() {
			return lastMB, fmt.Errorf("cabac: unexpected end of slice data at mb %d", mbIdx)
		}

		// end_of_slice_flag is not coded for pcm macroblocks
		if mbIdx < sc.TotalMBs-1 && !sc.MBs[mbIdx].IsPCM {
			endOfSlice := dec.DecodeTerminate()
			if endOfSlice == 1 {
				break
			}
			if dec.Overread() {
				return lastMB, fmt.Errorf("cabac: unexpected end of slice data at mb %d", mbIdx)
			}
		}
	}

	return lastMB, nil
}

func decodeMacroblock(sc *SliceContext, mbIdx int) error {
	mb := &sc.MBs[mbIdx]

	mb.MBType = DecodeMBTypeIntra(sc, mbIdx)

	if mb.MBType == MBTypeIPCM {
		return decodeIPCM(sc, mbIdx)
	}

	if mb.MBType == MBTypeINxN {
		if sc.Transform8x8ModeFlag {
			mb.TransformSize8x8 = DecodeTransformSize8x8Flag(sc, mbIdx)
		}

		if mb.TransformSize8x8 {
			for i := range 4 {
				prevFlag, rem := DecodeIntra8x8PredMode(sc)
				predicted := derivePredIntra8x8PredMode(sc, mbIdx, i)
				if prevFlag {
					mb.Intra8x8PredMode[i] = predicted
				} else {
					if rem >= predicted {
						mb.Intra8x8PredMode[i] = rem + 1
					} else {
						mb.Intra8x8PredMode[i] = rem
					}
				}
			}
		} else {
			for i := range 16 {
				prevFlag, rem := DecodeIntra4x4PredMode(sc)
				predicted := derivePredIntra4x4PredMode(sc, mbIdx, i)
				if prevFlag {
					mb.Intra4x4PredMode[i] = predicted
				} else {
					if rem >= predicted {
						mb.Intra4x4PredMode[i] = rem + 1
					} else {
						mb.Intra4x4PredMode[i] = rem
					}
				}
			}
		}

		if sc.ChromaArrayType != 0 {
			mb.IntraChromaPredMode = DecodeIntraChromaPredMode(sc, mbIdx)
		}

		mb.CBPLuma, mb.CBPChroma = DecodeCBP(sc, mbIdx)
	} else {
		mb.IntraPredMode16x16 = I16x16PredMode(mb.MBType)
		mb.CBPLuma = I16x16CBPLuma(mb.MBType)
		mb.CBPChroma = I16x16CBPChroma(mb.MBType)

		if sc.ChromaArrayType != 0 {
			mb.IntraChromaPredMode = DecodeIntraChromaPredMode(sc, mbIdx)
		}
	}

	if mb.CBPLuma > 0 || mb.CBPChroma > 0 || (mb.MBType >= 1 && mb.MBType <= 24) {
		qpDelta, err := DecodeQPDelta(sc)
		if err != nil {
			return err
		}
		if err := sc.applyQPDelta(mb, qpDelta); err != nil {
			return err
		}
		sc.PrevMBQPDeltaNonZero = mb.QPDelta != 0
	} else {
		sc.PrevMBQPDeltaNonZero = false
		mb.QPY = sc.QPY
	}

	if err := decodeResidualMB(sc, mbIdx); err != nil {
		return err
	}

	if sc.TraceMBCMP {
		cbp := mb.CBPLuma | (mb.CBPChroma << 4)
		pred := mb.IntraPredMode16x16
		if mb.MBType == MBTypeINxN {
			pred = 255
		}
		t8x8 := ""
		if mb.TransformSize8x8 {
			t8x8 = " 8x8"
		}
		fmt.Printf("MBCMP[%d] type=%d cbp=0x%02x pred=%d cpred=%d qp=%d R=%d O=%d%s\n",
			mbIdx, mb.MBType, cbp, pred, mb.IntraChromaPredMode, mb.QPY,
			sc.Cabac.Range(), sc.Cabac.Offset(), t8x8)
	}

	return nil
}

func decodeResidualMB(sc *SliceContext, mbIdx int) error {
	mb := &sc.MBs[mbIdx]

	if mb.MBType >= 1 && mb.MBType <= 24 {

		dcCoeffs, err := DecodeResidual(sc, mbIdx, CtxBlockCatIntra16x16DC, 0, 16)
		if err != nil {
			return err
		}
		for i := range 16 {
			mb.Intra16x16DCLevel[i] = dcCoeffs[i]
		}

		if mb.CBPLuma > 0 {
			for i := range 16 {
				i8x8 := i / 4
				if mb.CBPLuma&(1<<uint(i8x8)) != 0 {
					acCoeffs, err := DecodeResidual(sc, mbIdx, CtxBlockCatIntra16x16AC, i, 15)
					if err != nil {
						return err
					}
					for j := range 15 {
						mb.Intra16x16ACLevel[i][j] = acCoeffs[j]
					}
				}
			}
		}
	} else if mb.MBType == MBTypeINxN {
		if mb.TransformSize8x8 {
			for i := range 4 {
				if mb.CBPLuma&(1<<uint(i)) != 0 {
					coeffs, err := DecodeResidual(sc, mbIdx, CtxBlockCatLuma8x8, i, 64)
					if err != nil {
						return err
					}
					for j := range 64 {
						mb.LumaLevel8x8[i][j] = coeffs[j]
					}
					mb.CodedBlockFlag[CtxBlockCatLuma8x8][i] = 1
				}
			}
		} else {
			for i := range 16 {
				i8x8 := i / 4
				if mb.CBPLuma&(1<<uint(i8x8)) != 0 {
					coeffs, err := DecodeResidual(sc, mbIdx, CtxBlockCatLuma4x4, i, 16)
					if err != nil {
						return err
					}
					for j := range 16 {
						mb.LumaLevel4x4[i][j] = coeffs[j]
					}
				}
			}
		}
	}

	if sc.ChromaArrayType != 0 && (mb.CBPChroma > 0 || (mb.MBType >= 1 && mb.MBType <= 24 && mb.CBPChroma > 0)) {
		for iCbCr := range 2 {
			if mb.CBPChroma > 0 {
				numDC := 4
				dcCoeffs, err := DecodeResidual(sc, mbIdx, CtxBlockCatChromaDC, iCbCr, numDC)
				if err != nil {
					return err
				}
				for j := range numDC {
					mb.ChromaDCLevel[iCbCr][j] = dcCoeffs[j]
				}
			}
		}

		if mb.CBPChroma > 1 {
			for iCbCr := range 2 {
				for i := range 4 {
					blkIdx := iCbCr*4 + i
					acCoeffs, err := DecodeResidual(sc, mbIdx, CtxBlockCatChromaAC, blkIdx, 15)
					if err != nil {
						return err
					}
					for j := range 15 {
						mb.ChromaACLevel[iCbCr][i][j] = acCoeffs[j]
					}
				}
			}
		}
	}

	return nil
}

// chroma samples per plane for the mb chroma array size in use
func pcmChromaCount(chromaArrayType int) int {
	switch chromaArrayType {
	case 0:
		return 0
	case 1:
		return 64
	case 2:
		return 128
	default:
		return 256
	}
}

func readPCMSamples(sc *SliceContext, mb *MBData, data []byte, off int) {
	shiftY := uint(0)
	if sc.BitDepthY > 8 {
		shiftY = uint(sc.BitDepthY - 8)
	}
	shiftC := uint(0)
	if sc.BitDepthC > 8 {
		shiftC = uint(sc.BitDepthC - 8)
	}

	for i := range 256 {
		mb.PCMLuma[i] = data[off+i] >> shiftY
	}

	n := pcmChromaCount(sc.ChromaArrayType)
	for i := range min(n, 64) {
		mb.PCMCb[i] = data[off+256+i] >> shiftC
		mb.PCMCr[i] = data[off+256+n+i] >> shiftC
	}
}

func decodeIPCM(sc *SliceContext, mbIdx int) error {
	dec := sc.Cabac
	dec.AlignToByte()

	mb := &sc.MBs[mbIdx]
	mb.MBType = MBTypeIPCM
	mb.IsPCM = true
	mb.QPY = 26
	sc.QPY = 26

	off := dec.pos
	count := 256 + 2*pcmChromaCount(sc.ChromaArrayType)
	if off+count > len(dec.data) {
		return fmt.Errorf("pcm samples overrun slice data at mb %d", mbIdx)
	}

	readPCMSamples(sc, mb, dec.data, off)

	return dec.reinit(off + count)
}

func decodeSliceCAVLC(sc *SliceContext, br *BitReader, sliceQPY int, firstMB int) (int, error) {
	sc.IsCAVLC = true
	sc.Br = br
	sc.QPY = sliceQPY
	sc.SliceStartMB = firstMB

	lastMB := firstMB
	for mbIdx := firstMB; mbIdx < sc.TotalMBs; mbIdx++ {
		sc.MBs[mbIdx].QPY = sliceQPY
		sc.MBs[mbIdx].SliceStart = firstMB
		sc.MBs[mbIdx].DeblockIDC = sc.DeblockIDC
		sc.MBs[mbIdx].DeblockAlpha = sc.DeblockAlpha
		sc.MBs[mbIdx].DeblockBeta = sc.DeblockBeta
		err := decodeMacroblockCAVLC(sc, mbIdx)
		if err != nil {
			return lastMB, fmt.Errorf("mb %d: %w", mbIdx, err)
		}
		lastMB = mbIdx + 1

		if !br.HasMoreRBSPData() {
			break
		}
	}

	return lastMB, nil
}

func decodeMacroblockCAVLC(sc *SliceContext, mbIdx int) error {
	mb := &sc.MBs[mbIdx]
	br := sc.Br

	mbType, err := DecodeMBTypeIntraCAVLC(br)
	if err != nil {
		return fmt.Errorf("mb_type: %w", err)
	}
	mb.MBType = mbType

	if mb.MBType == MBTypeIPCM {
		return decodeIPCMCAVLC(sc, mbIdx)
	}

	if mb.MBType == MBTypeINxN {
		if sc.Transform8x8ModeFlag {
			flag, err := DecodeTransformSize8x8FlagCAVLC(br)
			if err != nil {
				return err
			}
			mb.TransformSize8x8 = flag
		}

		if mb.TransformSize8x8 {
			for i := range 4 {
				prevFlag, rem, err := DecodeIntra4x4PredModeCAVLC(br)
				if err != nil {
					return err
				}
				predicted := derivePredIntra8x8PredMode(sc, mbIdx, i)
				if prevFlag {
					mb.Intra8x8PredMode[i] = predicted
				} else {
					if rem >= predicted {
						mb.Intra8x8PredMode[i] = rem + 1
					} else {
						mb.Intra8x8PredMode[i] = rem
					}
				}
			}
		} else {
			for i := range 16 {
				prevFlag, rem, err := DecodeIntra4x4PredModeCAVLC(br)
				if err != nil {
					return err
				}
				predicted := derivePredIntra4x4PredMode(sc, mbIdx, i)
				if prevFlag {
					mb.Intra4x4PredMode[i] = predicted
				} else {
					if rem >= predicted {
						mb.Intra4x4PredMode[i] = rem + 1
					} else {
						mb.Intra4x4PredMode[i] = rem
					}
				}
			}
		}

		if sc.ChromaArrayType != 0 {
			mode, err := DecodeIntraChromaPredModeCAVLC(br)
			if err != nil {
				return err
			}
			mb.IntraChromaPredMode = mode
		}

		cbpLuma, cbpChroma, err := DecodeCBPCAVLC(br)
		if err != nil {
			return err
		}
		mb.CBPLuma = cbpLuma
		mb.CBPChroma = cbpChroma
	} else {
		mb.IntraPredMode16x16 = I16x16PredMode(mb.MBType)
		mb.CBPLuma = I16x16CBPLuma(mb.MBType)
		mb.CBPChroma = I16x16CBPChroma(mb.MBType)

		if sc.ChromaArrayType != 0 {
			mode, err := DecodeIntraChromaPredModeCAVLC(br)
			if err != nil {
				return err
			}
			mb.IntraChromaPredMode = mode
		}
	}

	if mb.CBPLuma > 0 || mb.CBPChroma > 0 || (mb.MBType >= 1 && mb.MBType <= 24) {
		qpDelta, err := DecodeQPDeltaCAVLC(br)
		if err != nil {
			return err
		}
		if err := sc.applyQPDelta(mb, qpDelta); err != nil {
			return err
		}
	} else {
		mb.QPY = sc.QPY
	}

	err = decodeResidualMBCAVLC(sc, mbIdx)
	if err != nil {
		return fmt.Errorf("residual: %w", err)
	}

	if sc.TraceMBCMP {
		cbp := mb.CBPLuma | (mb.CBPChroma << 4)
		pred := mb.IntraPredMode16x16
		if mb.MBType == MBTypeINxN {
			pred = 255
		}
		t8x8 := ""
		if mb.TransformSize8x8 {
			t8x8 = " 8x8"
		}
		fmt.Printf("MBCMP[%d] type=%d cbp=0x%02x pred=%d cpred=%d qp=%d B=%d%s\n",
			mbIdx, mb.MBType, cbp, pred, mb.IntraChromaPredMode, mb.QPY, sc.Br.BitsRead(), t8x8)
	}

	return nil
}

func decodeResidualMBCAVLC(sc *SliceContext, mbIdx int) error {
	mb := &sc.MBs[mbIdx]
	br := sc.Br

	if mb.MBType >= 1 && mb.MBType <= 24 {

		nC := DeriveNC(sc, mbIdx, 0, false)
		dcCoeffs, totalCoeff, err := DecodeResidualBlock(br, nC, 16)
		if err != nil {
			return fmt.Errorf("i16x16 DC: %w", err)
		}
		for i := range 16 {
			mb.Intra16x16DCLevel[i] = dcCoeffs[i]
		}
		_ = totalCoeff

		if mb.CBPLuma > 0 {
			for i := range 16 {
				i8x8 := i / 4
				if mb.CBPLuma&(1<<uint(i8x8)) != 0 {
					nC := DeriveNC(sc, mbIdx, i, false)
					acCoeffs, tc, err := DecodeResidualBlock(br, nC, 15)
					if err != nil {
						return fmt.Errorf("i16x16 AC[%d]: %w", i, err)
					}
					for j := range 15 {
						mb.Intra16x16ACLevel[i][j] = acCoeffs[j]
					}
					mb.NzCoeffLuma[i] = tc
				}
			}
		}
	} else if mb.MBType == MBTypeINxN {
		if mb.TransformSize8x8 {
			for i8x8 := range 4 {
				if mb.CBPLuma&(1<<uint(i8x8)) != 0 {
					for i4x4 := range 4 {
						blkIdx := i8x8*4 + i4x4
						nC := DeriveNC(sc, mbIdx, blkIdx, false)
						subCoeffs, tc, err := DecodeResidualBlock(br, nC, 16)
						if err != nil {
							return fmt.Errorf("i8x8[%d] sub[%d]: %w", i8x8, i4x4, err)
						}
						for j := range 16 {
							mb.LumaLevel8x8[i8x8][zigzagScan8x8CAVLC[i4x4*16+j]] = subCoeffs[j]
						}
						mb.NzCoeffLuma[blkIdx] = tc
					}
				}
			}
		} else {
			for i := range 16 {
				i8x8 := i / 4
				if mb.CBPLuma&(1<<uint(i8x8)) != 0 {
					nC := DeriveNC(sc, mbIdx, i, false)
					coeffs, tc, err := DecodeResidualBlock(br, nC, 16)
					if err != nil {
						return fmt.Errorf("i4x4[%d]: %w", i, err)
					}
					for j := range 16 {
						mb.LumaLevel4x4[i][j] = coeffs[j]
					}
					mb.NzCoeffLuma[i] = tc
				}
			}
		}
	}

	if sc.ChromaArrayType != 0 && mb.CBPChroma > 0 {
		for iCbCr := range 2 {
			dcCoeffs, _, err := DecodeResidualBlock(br, -1, 4)
			if err != nil {
				return fmt.Errorf("chroma DC[%d]: %w", iCbCr, err)
			}
			for j := range 4 {
				mb.ChromaDCLevel[iCbCr][j] = dcCoeffs[j]
			}
		}

		if mb.CBPChroma > 1 {
			for iCbCr := range 2 {
				for i := range 4 {
					blkIdx := iCbCr*4 + i
					nC := DeriveChromaNC(sc, mbIdx, blkIdx)
					acCoeffs, tc, err := DecodeResidualBlock(br, nC, 15)
					if err != nil {
						return fmt.Errorf("chroma AC[%d][%d]: %w", iCbCr, i, err)
					}
					for j := range 15 {
						mb.ChromaACLevel[iCbCr][i][j] = acCoeffs[j]
					}
					mb.NzCoeffChroma[blkIdx] = tc
				}
			}
		}
	}

	return nil
}

func decodeIPCMCAVLC(sc *SliceContext, mbIdx int) error {
	br := sc.Br
	br.AlignToByte()

	mb := &sc.MBs[mbIdx]
	mb.MBType = MBTypeIPCM
	mb.IsPCM = true
	mb.QPY = 26
	sc.QPY = 26

	off := br.bytePos
	count := 256 + 2*pcmChromaCount(sc.ChromaArrayType)
	if br.bitPos != 0 || off+count > len(br.data) {
		return fmt.Errorf("pcm samples overrun slice data at mb %d", mbIdx)
	}

	readPCMSamples(sc, mb, br.data, off)

	br.bytePos += count

	return nil
}

var zigzagScan8x8CAVLC = [64]int{
	0, 9, 17, 18, 12, 40, 27, 7,
	35, 57, 29, 30, 58, 38, 53, 47,
	1, 2, 24, 11, 19, 48, 20, 14,
	42, 50, 22, 37, 59, 31, 60, 55,
	8, 3, 32, 4, 26, 41, 13, 21,
	49, 43, 15, 44, 52, 39, 61, 62,
	16, 10, 25, 5, 33, 34, 6, 28,
	56, 36, 23, 51, 45, 46, 54, 63,
}
