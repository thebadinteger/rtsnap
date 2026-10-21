package h264

import "fmt"

type Decoder struct {
	spsMap         map[uint32]*SPS
	ppsMap         map[uint32]*PPS
	TraceMBCMP     bool
	SkipDeblock    bool
	frameSizeLimit int

	curSliceCtx    *SliceContext
	curFrame       *Frame
	curSPS         *SPS
	curPPS         *PPS
	decodedMBCount int
}

// cap decoded frame size in pixels
func (d *Decoder) FrameSizeLimit(n int) { d.frameSizeLimit = n }

func (d *Decoder) checkFrameSize(width, height int) error {
	if n := d.frameSizeLimit; n > 0 && int64(width)*int64(height) > int64(n) {
		return fmt.Errorf("frame size %dx%d exceeds limit", width, height)
	}
	return nil
}

type ScalingMatrices struct {
	IntraY4x4  [16]int32
	IntraCb4x4 [16]int32
	IntraCr4x4 [16]int32
	IntraY8x8  [64]int32
}

var defaultScalingList4x4Intra = [16]int{
	6, 13, 13, 20, 20, 20, 28, 28, 28, 28, 32, 32, 32, 37, 37, 42,
}

var defaultScalingList8x8Intra = [64]int{
	6, 10, 13, 16, 18, 23, 25, 27,
	10, 11, 16, 18, 23, 25, 27, 29,
	13, 16, 18, 23, 25, 27, 29, 31,
	16, 18, 23, 25, 27, 29, 31, 33,
	18, 23, 25, 27, 29, 31, 33, 36,
	23, 25, 27, 29, 31, 33, 36, 38,
	25, 27, 29, 31, 33, 36, 38, 40,
	27, 29, 31, 33, 36, 38, 40, 42,
}

func buildScalingMatrices(sps *SPS, pps *PPS) ScalingMatrices {
	sm := ScalingMatrices{}

	for i := range sm.IntraY4x4 {
		sm.IntraY4x4[i] = 16
	}
	sm.IntraCb4x4 = sm.IntraY4x4
	sm.IntraCr4x4 = sm.IntraY4x4
	for i := range sm.IntraY8x8 {
		sm.IntraY8x8[i] = 16
	}

	if sps.SeqScalingMatrixPresentFlag {
		if !applyScalingList4x4(&sm.IntraY4x4, sps.SeqScalingLists, 0) {
			applyDefault4x4Intra(&sm.IntraY4x4)
		}
		if !applyScalingList4x4(&sm.IntraCb4x4, sps.SeqScalingLists, 1) {
			sm.IntraCb4x4 = sm.IntraY4x4
		}
		if !applyScalingList4x4(&sm.IntraCr4x4, sps.SeqScalingLists, 2) {
			sm.IntraCr4x4 = sm.IntraCb4x4
		}
		if !applyScalingList8x8(&sm.IntraY8x8, sps.SeqScalingLists, 6) {
			applyDefault8x8Intra(&sm.IntraY8x8)
		}
	}

	if pps.PicScalingMatrixPresentFlag {
		if sps.SeqScalingMatrixPresentFlag {
			applyScalingList4x4(&sm.IntraY4x4, pps.PicScalingLists, 0)
			applyScalingList4x4(&sm.IntraCb4x4, pps.PicScalingLists, 1)
			applyScalingList4x4(&sm.IntraCr4x4, pps.PicScalingLists, 2)
			applyScalingList8x8(&sm.IntraY8x8, pps.PicScalingLists, 6)
		} else {
			if !applyScalingList4x4(&sm.IntraY4x4, pps.PicScalingLists, 0) {
				applyDefault4x4Intra(&sm.IntraY4x4)
			}
			if !applyScalingList4x4(&sm.IntraCb4x4, pps.PicScalingLists, 1) {
				sm.IntraCb4x4 = sm.IntraY4x4
			}
			if !applyScalingList4x4(&sm.IntraCr4x4, pps.PicScalingLists, 2) {
				sm.IntraCr4x4 = sm.IntraCb4x4
			}
			if !applyScalingList8x8(&sm.IntraY8x8, pps.PicScalingLists, 6) {
				applyDefault8x8Intra(&sm.IntraY8x8)
			}
		}
	}

	return sm
}

func applyScalingList4x4(dst *[16]int32, lists []ScalingList, idx int) bool {
	if idx >= len(lists) || lists[idx] == nil || len(lists[idx]) != 16 {
		return false
	}
	sl := lists[idx]
	for k := range 16 {
		dst[zigzag4x4[k]] = int32(sl[k])
	}
	return true
}

func applyScalingList8x8(dst *[64]int32, lists []ScalingList, idx int) bool {
	if idx >= len(lists) || lists[idx] == nil || len(lists[idx]) != 64 {
		return false
	}
	sl := lists[idx]
	for k := range 64 {
		dst[zigzag8x8[k]] = int32(sl[k])
	}
	return true
}

func applyDefault4x4Intra(dst *[16]int32) {
	for k := range 16 {
		dst[zigzag4x4[k]] = int32(defaultScalingList4x4Intra[k])
	}
}

func applyDefault8x8Intra(dst *[64]int32) {
	for k := range 64 {
		dst[k] = int32(defaultScalingList8x8Intra[k])
	}
}

// fresh h264 decoder state
func New() *Decoder {
	return &Decoder{
		spsMap: make(map[uint32]*SPS),
		ppsMap: make(map[uint32]*PPS),
	}
}

// reset intermediate frame state
func (d *Decoder) Reset() {
	if d.curSliceCtx != nil {
		putMBs(d.curSliceCtx.MBs)
		d.curSliceCtx = nil
	}
	d.curFrame = nil
	d.curSPS = nil
	d.curPPS = nil
	d.decodedMBCount = 0
}

// decode nal units until first frame
func (d *Decoder) DecodeNALUs(nalus [][]byte) (*Frame, error) {
	for _, nalu := range nalus {
		if len(nalu) == 0 {
			continue
		}
		naluType := NaluType(nalu[0] & 0x1f)

		switch naluType {
		case NALU_SPS:
			sps, err := ParseSPSNALUnit(nalu, true)
			if err != nil {
				return nil, fmt.Errorf("parse SPS: %w", err)
			}
			d.spsMap[sps.ParameterID] = sps
		case NALU_PPS:
			pps, err := ParsePPSNALUnit(nalu, d.spsMap)
			if err != nil {
				return nil, fmt.Errorf("parse PPS: %w", err)
			}
			d.ppsMap[pps.PicParameterSetID] = pps
		case NALU_IDR:
			f, err := d.decodeIDR(nalu)
			if err != nil {
				return nil, err
			}
			if f != nil {
				return f, nil
			}
		}
	}
	return nil, nil
}

// decode annexb byte stream
func (d *Decoder) DecodeAnnexB(data []byte) (*Frame, error) {
	nalus := ExtractNalusFromByteStream(data)
	return d.DecodeNALUs(nalus)
}

func (d *Decoder) decodeIDR(nalu []byte) (*Frame, error) {
	sh, err := ParseSliceHeader(nalu, d.spsMap, d.ppsMap)
	if err != nil {
		return nil, fmt.Errorf("parse slice header: %w", err)
	}

	if sh.RedundantPicCnt > 0 {
		return nil, nil
	}

	pps := d.ppsMap[sh.PicParamID]
	if pps.NumSliceGroupsMinus1 > 0 {
		return nil, fmt.Errorf("slice groups not supported")
	}
	sps := d.spsMap[pps.SeqParameterSetID]

	width := int(sps.Width)
	height := int(sps.Height)
	if err := d.checkFrameSize(width, height); err != nil {
		return nil, err
	}
	mbWidth := (width + 15) / 16
	mbHeight := (height + 15) / 16
	totalMBs := mbWidth * mbHeight

	firstMB := int(sh.FirstMBInSlice)
	if firstMB >= totalMBs {
		d.Reset()
		return nil, fmt.Errorf("first_mb_in_slice %d out of bounds (%d)", firstMB, totalMBs)
	}

	if firstMB == 0 {
		d.Reset()

		f := NewFrame(width, height)
		if sps.VUI != nil {
			if sps.VUI.ColourDescriptionFlag {
				f.ColorDescriptionValid = true
				f.MatrixCoefficients = sps.VUI.MatrixCoefficients
			}
			f.VideoFullRangeFlag = sps.VUI.VideoFullRangeFlag
		}

		chromaArrayType := 1
		if sps.ChromaFormatIDC == 0 {
			chromaArrayType = 0
		}
		bitDepthY := 8 + int(sps.BitDepthLumaMinus8)
		bitDepthC := 8 + int(sps.BitDepthChromaMinus8)

		sc := &SliceContext{
			MBWidth:              mbWidth,
			MBHeight:             mbHeight,
			TotalMBs:             totalMBs,
			MBs:                  getMBs(totalMBs),
			Transform8x8ModeFlag: pps.Transform8x8ModeFlag,
			ChromaArrayType:      chromaArrayType,
			BitDepthY:            bitDepthY,
			BitDepthC:            bitDepthC,
			ChromaQpIndexOffset:  int(pps.ChromaQpIndexOffset),
			TraceMBCMP:           d.TraceMBCMP,
		}

		d.curSliceCtx = sc
		d.curFrame = f
		d.curSPS = sps
		d.curPPS = pps
		d.decodedMBCount = 0
	} else {
		// Subsequent slice
		if d.curSliceCtx == nil || firstMB != d.decodedMBCount {
			d.Reset()
			return nil, nil
		}
	}

	sliceQPY := 26 + int(pps.PicInitQpMinus26) + int(sh.SliceQPDelta)
	sc := d.curSliceCtx
	sc.DeblockIDC = sh.DisableDeblockingFilterIDC
	sc.DeblockAlpha = int(sh.SliceAlphaC0OffsetDiv2) * 2
	sc.DeblockBeta = int(sh.SliceBetaOffsetDiv2) * 2

	if pps.EntropyCodingModeFlag {
		sliceData := removeEBSPPrevention(nalu[sh.Size:])
		lastMB, err2 := decodeSliceCABAC(sc, sliceData, sliceQPY, firstMB)
		if err2 != nil {
			d.Reset()
			return nil, fmt.Errorf("decode slice data (CABAC): %w", err2)
		}
		d.decodedMBCount = lastMB
	} else {
		fullData := removeEBSPPrevention(nalu)
		br := NewBitReader(fullData)

		err = br.SkipSliceHeaderIDR(SliceHeaderParams{
			FrameMbsOnly:                          sps.FrameMbsOnlyFlag,
			Log2MaxFrameNumMinus4:                 uint(sps.Log2MaxFrameNumMinus4),
			PicOrderCntType:                       uint(sps.PicOrderCntType),
			Log2MaxPicOrderCntLsbMinus4:           uint(sps.Log2MaxPicOrderCntLsbMinus4),
			BottomFieldPicOrderInFramePresentFlag: pps.BottomFieldPicOrderInFramePresentFlag,
			DeblockingFilterControlPresent:        pps.DeblockingFilterControlPresentFlag,
			RedundantPicCntPresentFlag:            pps.RedundantPicCntPresentFlag,
		})
		if err != nil {
			d.Reset()
			return nil, fmt.Errorf("skip slice header (CAVLC): %w", err)
		}

		lastMB, err2 := decodeSliceCAVLC(sc, br, sliceQPY, firstMB)
		if err2 != nil {
			d.Reset()
			return nil, fmt.Errorf("decode slice data (CAVLC): %w", err2)
		}
		d.decodedMBCount = lastMB
	}

	if d.decodedMBCount < sc.TotalMBs {
		return nil, nil
	}

	f := d.curFrame
	err = reconstructFrame(sc, f, sps, pps)
	if err != nil {
		d.Reset()
		return nil, fmt.Errorf("reconstruct frame: %w", err)
	}

	if !d.SkipDeblock {
		Deblock(f, sc)
	}

	outFrame := f
	d.Reset()
	return outFrame, nil
}

func removeEBSPPrevention(data []byte) []byte {
	var result []byte
	for i := 0; i < len(data); i++ {
		if i+2 < len(data) && data[i] == 0 && data[i+1] == 0 && data[i+2] == 3 {
			if result == nil {
				result = make([]byte, 0, len(data))
				result = append(result, data[:i]...)
			}
			result = append(result, 0, 0)
			i += 2
		} else if result != nil {
			result = append(result, data[i])
		}
	}
	if result == nil {
		return data
	}
	return result
}

func reconstructFrame(sc *SliceContext, f *Frame, sps *SPS, pps *PPS) error {
	sm := buildScalingMatrices(sps, pps)

	for mbIdx := 0; mbIdx < sc.TotalMBs; mbIdx++ {
		mbX := mbIdx % sc.MBWidth
		mbY := mbIdx / sc.MBWidth
		mb := &sc.MBs[mbIdx]

		if mb.IsPCM {
			reconstructPCM(sc, f, mbX, mbY, mb)

			continue
		}

		if mb.MBType >= 1 && mb.MBType <= 24 {
			err := reconstructI16x16(sc, f, mbIdx, mbX, mbY, mb, &sm)
			if err != nil {
				return fmt.Errorf("reconstruct I_16x16 mb %d: %w", mbIdx, err)
			}
		} else if mb.MBType == MBTypeINxN {
			if mb.TransformSize8x8 {
				err := reconstructI8x8(sc, f, mbIdx, mbX, mbY, mb, &sm)
				if err != nil {
					return fmt.Errorf("reconstruct I_8x8 mb %d: %w", mbIdx, err)
				}
			} else {
				err := reconstructI4x4(sc, f, mbIdx, mbX, mbY, mb, &sm)
				if err != nil {
					return fmt.Errorf("reconstruct I_4x4 mb %d: %w", mbIdx, err)
				}
			}
		}

		if sc.ChromaArrayType != 0 {
			reconstructChroma(sc, f, mbIdx, mbX, mbY, mb, &sm)
		}
	}

	return nil
}

// pcm samples are already reconstructed, copy them into the frame planes
func reconstructPCM(sc *SliceContext, f *Frame, mbX, mbY int, mb *MBData) {
	x0 := mbX * 16
	y0 := mbY * 16

	for y := range 16 {
		for x := range 16 {
			f.SetLumaPixel(x0+x, y0+y, mb.PCMLuma[y*16+x])
		}
	}

	if sc.ChromaArrayType == 0 {
		return
	}

	cx0 := mbX * 8
	cy0 := mbY * 8

	for y := range 8 {
		for x := range 8 {
			f.SetChromaPixel(0, cx0+x, cy0+y, mb.PCMCb[y*8+x])
			f.SetChromaPixel(1, cx0+x, cy0+y, mb.PCMCr[y*8+x])
		}
	}
}

func reconstructI16x16(sc *SliceContext, f *Frame,
	mbIdx, mbX, mbY int, mb *MBData, sm *ScalingMatrices) error {
	nb := mbNeighbors(sc, mbIdx)
	top, left, topLeft, hasTop, hasLeft := getLuma16x16Neighbors(f, mbX, mbY, nb)
	var topSlice, leftSlice []uint8
	if hasTop {
		topSlice = top[:]
	}
	if hasLeft {
		leftSlice = left[:]
	}
	predBlock := Predict16x16(mb.IntraPredMode16x16, topSlice, leftSlice, topLeft)

	var dcMatrix [16]int32
	for k := range 16 {
		dcMatrix[zigzag4x4[k]] = mb.Intra16x16DCLevel[k]
	}
	dcTransformed := InverseHadamard4x4(dcMatrix)
	dcScaledRaster := DequantDC4x4(dcTransformed, mb.QPY, sm.IntraY4x4[0])
	var dcScaled [16]int32
	for i := range 16 {
		dcScaled[i] = dcScaledRaster[zScanToRaster[i]]
	}

	sl := &sm.IntraY4x4
	var lumaBlock [16][16]uint8
	for i := range 16 {
		bx := inverseRasterX4x4[i]
		by := inverseRasterY4x4[i]

		var block4x4 [16]int32
		block4x4[0] = dcScaled[i]
		for j := range 15 {
			block4x4[zigzag4x4AC[j]] = mb.Intra16x16ACLevel[i][j]
		}

		dequantBlock := Dequant4x4(block4x4, mb.QPY, sl)
		dequantBlock[0] = dcScaled[i]

		residual := InverseTransform4x4(dequantBlock)

		for y := range 4 {
			for x := range 4 {
				val := int32(predBlock[by+y][bx+x]) + residual[y*4+x]
				lumaBlock[by+y][bx+x] = clip8Int32(val)
			}
		}
	}

	f.SetLuma16x16(mbX, mbY, lumaBlock)
	return nil
}

func reconstructI4x4(sc *SliceContext, f *Frame,
	mbIdx, mbX, mbY int, mb *MBData, sm *ScalingMatrices) error {
	x0 := mbX * 16
	y0 := mbY * 16
	sl := &sm.IntraY4x4
	nb := mbNeighbors(sc, mbIdx)

	for i := range 16 {
		bx := inverseRasterX4x4[i]
		by := inverseRasterY4x4[i]

		av := lumaBlockAvail(bx, by, 4, nb)
		if topRightNotAvail4x4[i] {
			av.topRight = false
		}
		ref := getLuma4x4Neighbors(f, x0+bx, y0+by, av)

		predBlock := Predict4x4(mb.Intra4x4PredMode[i], ref, av.left, av.top)

		var coeffs [16]int32
		for j := range 16 {
			coeffs[zigzag4x4[j]] = mb.LumaLevel4x4[i][j]
		}
		dequant := Dequant4x4(coeffs, mb.QPY, sl)
		residual := InverseTransform4x4(dequant)

		for y := range 4 {
			for x := range 4 {
				val := int32(predBlock[y][x]) + residual[y*4+x]
				f.SetLumaPixel(x0+bx+x, y0+by+y, clip8Int32(val))
			}
		}
	}

	return nil
}

func reconstructI8x8(sc *SliceContext, f *Frame,
	mbIdx, mbX, mbY int, mb *MBData, sm *ScalingMatrices) error {
	x0 := mbX * 16
	y0 := mbY * 16
	sl := &sm.IntraY8x8
	nb := mbNeighbors(sc, mbIdx)

	for i := range 4 {
		bx := (i % 2) * 8
		by := (i / 2) * 8

		av := lumaBlockAvail(bx, by, 8, nb)
		ref := getLuma8x8Neighbors(f, x0+bx, y0+by, av)

		predBlock := Predict8x8(mb.Intra8x8PredMode[i], ref, av.left, av.top)

		var coeffs [64]int32
		if sc.IsCAVLC {
			coeffs = mb.LumaLevel8x8[i]
		} else {
			for j := range 64 {
				coeffs[zigzag8x8[j]] = mb.LumaLevel8x8[i][j]
			}
		}

		dequant := Dequant8x8(coeffs, mb.QPY, sl)

		residual := InverseTransform8x8(dequant)

		for y := range 8 {
			for x := range 8 {
				val := int32(predBlock[y][x]) + residual[y*8+x]
				f.SetLumaPixel(x0+bx+x, y0+by+y, clip8Int32(val))
			}
		}

	}

	return nil
}

func getLuma8x8Neighbors(f *Frame, blkX, blkY int, av mbAvail) [25]uint8 {
	var p [25]int

	if av.left {
		for i := range 8 {
			p[i] = int(f.GetLumaPixel(blkX-1, blkY+7-i))
		}
	}

	if av.topLeft {
		p[8] = int(f.GetLumaPixel(blkX-1, blkY-1))
	}

	if av.top {
		for i := range 8 {
			p[9+i] = int(f.GetLumaPixel(blkX+i, blkY-1))
		}
		for i := range 8 {
			if av.topRight {
				p[17+i] = int(f.GetLumaPixel(blkX+8+i, blkY-1))
			} else {
				p[17+i] = p[16]
			}
		}
	}

	var ref [25]uint8
	if av.top {
		if av.topLeft {
			ref[9] = uint8((p[8] + 2*p[9] + p[10] + 2) >> 2)
		} else {
			ref[9] = uint8((3*p[9] + p[10] + 2) >> 2)
		}
		for i := 10; i < 24; i++ {
			ref[i] = uint8((p[i-1] + 2*p[i] + p[i+1] + 2) >> 2)
		}
		ref[24] = uint8((p[23] + 3*p[24] + 2) >> 2)
	}

	if av.topLeft {
		switch {
		case av.top && av.left:
			ref[8] = uint8((p[9] + 2*p[8] + p[7] + 2) >> 2)
		case av.top:
			ref[8] = uint8((3*p[8] + p[9] + 2) >> 2)
		case av.left:
			ref[8] = uint8((3*p[8] + p[7] + 2) >> 2)
		default:
			ref[8] = uint8(p[8])
		}
	}

	if av.left {
		if av.topLeft {
			ref[7] = uint8((p[8] + 2*p[7] + p[6] + 2) >> 2)
		} else {
			ref[7] = uint8((3*p[7] + p[6] + 2) >> 2)
		}
		for i := 1; i < 7; i++ {
			ref[i] = uint8((p[i+1] + 2*p[i] + p[i-1] + 2) >> 2)
		}
		ref[0] = uint8((p[1] + 3*p[0] + 2) >> 2)
	}

	return ref
}

func reconstructChroma(sc *SliceContext, f *Frame,
	mbIdx, mbX, mbY int, mb *MBData, sm *ScalingMatrices) {
	chromaSL := [2]*[16]int32{&sm.IntraCb4x4, &sm.IntraCr4x4}
	nb := mbNeighbors(sc, mbIdx)

	for iCbCr := range 2 {
		sl := chromaSL[iCbCr]

		top, left, topLeft, hasTop, hasLeft := getChromaNeighbors(f, iCbCr, mbX, mbY, nb)
		var topSlice, leftSlice []uint8
		if hasTop {
			topSlice = top[:]
		}
		if hasLeft {
			leftSlice = left[:]
		}
		predBlock := PredictChroma(mb.IntraChromaPredMode, topSlice, leftSlice, topLeft, 8)

		var dcCoeffs [4]int32
		copy(dcCoeffs[:], mb.ChromaDCLevel[iCbCr][:])
		dcTransformed := InverseHadamard2x2(dcCoeffs)

		qpc := chromaQP(mb.QPY + sc.ChromaQpIndexOffset)
		dcScaled := DequantChromaDC2x2(dcTransformed, qpc, sl[0])

		var chromaBlock [8][8]uint8
		for blk := range 4 {
			bx := (blk % 2) * 4
			by := (blk / 2) * 4

			var block4x4 [16]int32
			block4x4[0] = dcScaled[blk]
			if mb.CBPChroma > 1 {
				for j := range 15 {
					block4x4[zigzag4x4AC[j]] = mb.ChromaACLevel[iCbCr][blk][j]
				}
			}

			dequant := Dequant4x4(block4x4, qpc, sl)
			dequant[0] = dcScaled[blk]
			residual := InverseTransform4x4(dequant)

			for y := range 4 {
				for x := range 4 {
					val := int32(predBlock[by+y][bx+x]) + residual[y*4+x]
					chromaBlock[by+y][bx+x] = clip8Int32(val)
				}
			}
		}

		f.SetChroma8x8(iCbCr, mbX, mbY, chromaBlock)
	}
}

func getLuma16x16Neighbors(f *Frame, mbX, mbY int, nb mbAvail) (
	top [16]uint8, left [16]uint8, topLeft uint8, hasTop, hasLeft bool) {
	x0 := mbX * 16
	y0 := mbY * 16

	if nb.top {
		hasTop = true
		for x := range 16 {
			top[x] = f.GetLumaPixel(x0+x, y0-1)
		}
	}

	if nb.left {
		hasLeft = true
		for y := range 16 {
			left[y] = f.GetLumaPixel(x0-1, y0+y)
		}
	}

	if nb.topLeft {
		topLeft = f.GetLumaPixel(x0-1, y0-1)
	}

	return
}

type mbAvail struct {
	left, top, topLeft, topRight bool
}

func mbNeighbors(sc *SliceContext, mbIdx int) mbAvail {
	start := sc.MBs[mbIdx].SliceStart
	mbX := mbIdx % sc.MBWidth
	mbY := mbIdx / sc.MBWidth

	return mbAvail{
		left:     mbX > 0 && mbIdx-1 >= start,
		top:      mbY > 0 && mbIdx-sc.MBWidth >= start,
		topLeft:  mbX > 0 && mbY > 0 && mbIdx-sc.MBWidth-1 >= start,
		topRight: mbX < sc.MBWidth-1 && mbY > 0 && mbIdx-sc.MBWidth+1 >= start,
	}
}

func lumaBlockAvail(bx, by, size int, nb mbAvail) mbAvail {
	av := mbAvail{
		left:     bx > 0 || nb.left,
		top:      by > 0 || nb.top,
		topRight: bx+size < 16,
	}

	switch {
	case bx > 0 && by > 0:
		av.topLeft = true
	case by > 0:
		av.topLeft = nb.left
	case bx > 0:
		av.topLeft = nb.top
	default:
		av.topLeft = nb.topLeft
	}

	if by == 0 {
		if bx+size < 16 {
			av.topRight = nb.top
		} else {
			av.topRight = nb.topRight
		}
	}

	return av
}

var topRightNotAvail4x4 = [16]bool{
	false, false, false, true,
	false, false, false, true,
	false, false, false, true,
	false, false, false, true,
}

func getLuma4x4Neighbors(f *Frame, x0, y0 int, av mbAvail) [13]uint8 {
	var ref [13]uint8

	for i := range 4 {
		if av.left {
			ref[i] = f.GetLumaPixel(x0-1, y0+3-i)
		} else {
			ref[i] = 128
		}
	}

	if av.topLeft {
		ref[4] = f.GetLumaPixel(x0-1, y0-1)
	} else {
		ref[4] = 128
	}

	for i := range 4 {
		if av.top {
			ref[5+i] = f.GetLumaPixel(x0+i, y0-1)
		} else {
			ref[5+i] = 128
		}
	}

	if av.top && av.topRight {
		for i := 4; i < 8; i++ {
			ref[5+i] = f.GetLumaPixel(x0+i, y0-1)
		}
	} else {
		for i := 4; i < 8; i++ {
			ref[5+i] = ref[8]
		}
	}

	return ref
}

func getChromaNeighbors(f *Frame, comp int, mbX, mbY int, nb mbAvail) (
	top [8]uint8, left [8]uint8, topLeft uint8, hasTop, hasLeft bool) {
	x0 := mbX * 8
	y0 := mbY * 8

	if nb.top {
		hasTop = true
		for x := range 8 {
			top[x] = f.GetChromaPixel(comp, x0+x, y0-1)
		}
	}

	if nb.left {
		hasLeft = true
		for y := range 8 {
			left[y] = f.GetChromaPixel(comp, x0-1, y0+y)
		}
	}

	if nb.topLeft {
		topLeft = f.GetChromaPixel(comp, x0-1, y0-1)
	}

	return
}

var inverseRasterX4x4 = [16]int{
	0, 4, 0, 4, 8, 12, 8, 12,
	0, 4, 0, 4, 8, 12, 8, 12,
}

var inverseRasterY4x4 = [16]int{
	0, 0, 4, 4, 0, 0, 4, 4,
	8, 8, 12, 12, 8, 8, 12, 12,
}

var zScanToRaster = [16]int{0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15}

var zigzag4x4 = [16]int{
	0, 1, 4, 8, 5, 2, 3, 6,
	9, 12, 13, 10, 7, 11, 14, 15,
}

var zigzag4x4AC = [15]int{
	1, 4, 8, 5, 2, 3, 6,
	9, 12, 13, 10, 7, 11, 14, 15,
}

var zigzag8x8 = [64]int{
	0, 1, 8, 16, 9, 2, 3, 10,
	17, 24, 32, 25, 18, 11, 4, 5,
	12, 19, 26, 33, 40, 48, 41, 34,
	27, 20, 13, 6, 7, 14, 21, 28,
	35, 42, 49, 56, 57, 50, 43, 36,
	29, 22, 15, 23, 30, 37, 44, 51,
	58, 59, 52, 45, 38, 31, 39, 46,
	53, 60, 61, 54, 47, 55, 62, 63,
}

var qpcTable = [22]int{
	29, 30, 31, 32, 32, 33, 34, 34,
	35, 35, 36, 36, 37, 37, 37, 38,
	38, 38, 39, 39, 39, 39,
}

func chromaQP(qpY int) int {
	if qpY < 0 {
		qpY = 0
	}
	if qpY < 30 {
		return qpY
	}
	if qpY > 51 {
		return 51
	}
	return qpcTable[qpY-30]
}

func clip8Int32(val int32) uint8 {
	if val < 0 {
		return 0
	}
	if val > 255 {
		return 255
	}
	return uint8(val)
}
