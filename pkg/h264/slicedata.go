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

func DecodeSliceData(sliceData []byte, sliceQPY int, mbWidth, mbHeight int,
	transform8x8ModeFlag bool, chromaArrayType int,
	bitDepthY, bitDepthC int, chromaQpIndexOffset int, traceMBCMP bool) (*SliceContext, error) {

	totalMBs := mbWidth * mbHeight

	models := InitModels(sliceQPY, 2, 0)

	dec, err := NewCabacDecoder(sliceData)
	if err != nil {
		return nil, fmt.Errorf("cabac init: %w", err)
	}

	sc := &SliceContext{
		Cabac:                dec,
		Ctx:                  (*[1024]CtxState)(&models),
		MBWidth:              mbWidth,
		MBHeight:             mbHeight,
		TotalMBs:             totalMBs,
		QPY:                  sliceQPY,
		MBs:                  getMBs(totalMBs),
		Transform8x8ModeFlag: transform8x8ModeFlag,
		ChromaArrayType:      chromaArrayType,
		BitDepthY:            bitDepthY,
		BitDepthC:            bitDepthC,
		ChromaQpIndexOffset:  chromaQpIndexOffset,
		TraceMBCMP:           traceMBCMP,
	}

	for i := range sc.MBs {
		sc.MBs[i].QPY = sliceQPY
	}

	for mbIdx := range totalMBs {
		err := decodeMacroblock(sc, mbIdx)
		if err != nil {
			return sc, fmt.Errorf("mb %d: %w", mbIdx, err)
		}

		if mbIdx < totalMBs-1 {
			endOfSlice := dec.DecodeTerminate()
			if endOfSlice == 1 {
				break
			}
		}
	}

	return sc, nil
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
		mb.QPDelta = DecodeQPDelta(sc)
		qpBdOffsetY := 6 * (sc.BitDepthY - 8)
		qpRange := 52 + qpBdOffsetY
		mb.QPY = ((sc.QPY + mb.QPDelta + qpRange + 2*qpBdOffsetY) % qpRange) - qpBdOffsetY
		sc.PrevMBQPDeltaNonZero = mb.QPDelta != 0
		sc.QPY = mb.QPY
	} else {
		sc.PrevMBQPDeltaNonZero = false
		mb.QPY = sc.QPY
	}

	decodeResidualMB(sc, mbIdx)

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

func decodeResidualMB(sc *SliceContext, mbIdx int) {
	mb := &sc.MBs[mbIdx]

	if mb.MBType >= 1 && mb.MBType <= 24 {

		dcCoeffs := DecodeResidual(sc, mbIdx, CtxBlockCatIntra16x16DC, 0, 16)
		for i := range 16 {
			mb.Intra16x16DCLevel[i] = dcCoeffs[i]
		}

		if mb.CBPLuma > 0 {
			for i := range 16 {
				i8x8 := i / 4
				if mb.CBPLuma&(1<<uint(i8x8)) != 0 {
					acCoeffs := DecodeResidual(sc, mbIdx, CtxBlockCatIntra16x16AC, i, 15)
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
					coeffs := DecodeResidual(sc, mbIdx, CtxBlockCatLuma8x8, i, 64)
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
					coeffs := DecodeResidual(sc, mbIdx, CtxBlockCatLuma4x4, i, 16)
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
				dcCoeffs := DecodeResidual(sc, mbIdx, CtxBlockCatChromaDC, iCbCr, numDC)
				for j := range numDC {
					mb.ChromaDCLevel[iCbCr][j] = dcCoeffs[j]
				}
			}
		}

		if mb.CBPChroma > 1 {
			for iCbCr := range 2 {
				for i := range 4 {
					blkIdx := iCbCr*4 + i
					acCoeffs := DecodeResidual(sc, mbIdx, CtxBlockCatChromaAC, blkIdx, 15)
					for j := range 15 {
						mb.ChromaACLevel[iCbCr][i][j] = acCoeffs[j]
					}
				}
			}
		}
	}
}

func decodeIPCM(sc *SliceContext, mbIdx int) error {
	return nil
}

func DecodeSliceDataCAVLC(br *BitReader, sliceQPY int, mbWidth, mbHeight int,
	transform8x8ModeFlag bool, chromaArrayType int,
	bitDepthY, bitDepthC int, chromaQpIndexOffset int, traceMBCMP bool) (*SliceContext, error) {

	totalMBs := mbWidth * mbHeight

	sc := &SliceContext{
		IsCAVLC:              true,
		Br:                   br,
		MBWidth:              mbWidth,
		MBHeight:             mbHeight,
		TotalMBs:             totalMBs,
		QPY:                  sliceQPY,
		MBs:                  getMBs(totalMBs),
		Transform8x8ModeFlag: transform8x8ModeFlag,
		ChromaArrayType:      chromaArrayType,
		BitDepthY:            bitDepthY,
		BitDepthC:            bitDepthC,
		ChromaQpIndexOffset:  chromaQpIndexOffset,
		TraceMBCMP:           traceMBCMP,
	}

	for i := range sc.MBs {
		sc.MBs[i].QPY = sliceQPY
	}

	for mbIdx := range totalMBs {
		err := decodeMacroblockCAVLC(sc, mbIdx)
		if err != nil {
			return sc, fmt.Errorf("mb %d: %w", mbIdx, err)
		}
	}

	return sc, nil
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
		mb.QPDelta = qpDelta
		qpBdOffsetY := 6 * (sc.BitDepthY - 8)
		qpRange := 52 + qpBdOffsetY
		mb.QPY = ((sc.QPY + mb.QPDelta + qpRange + 2*qpBdOffsetY) % qpRange) - qpBdOffsetY
		sc.QPY = mb.QPY
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
