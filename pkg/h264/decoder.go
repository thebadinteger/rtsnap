package h264

import (
	"encoding/binary"
	"fmt"
)

type Decoder struct {
	spsMap      map[uint32]*SPS
	ppsMap      map[uint32]*PPS
	refFrame    *Frame
	TraceMBCMP  bool
	SkipDeblock bool
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

func New() *Decoder {
	return &Decoder{
		spsMap: make(map[uint32]*SPS),
		ppsMap: make(map[uint32]*PPS),
	}
}

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
			d.refFrame = f
			return f, nil
		}
	}
	return nil, fmt.Errorf("no IDR NALU found")
}

func (d *Decoder) DecodeAllFrames(nalus [][]byte) ([]*Frame, error) {
	return d.decodeFrames(nalus, true)
}

func (d *Decoder) DecodeIDRFrames(nalus [][]byte) ([]*Frame, error) {
	return d.decodeFrames(nalus, false)
}

func (d *Decoder) decodeFrames(nalus [][]byte, includeNonIDR bool) ([]*Frame, error) {
	var frames []*Frame

	for _, nalu := range nalus {
		if len(nalu) == 0 {
			continue
		}
		naluType := NaluType(nalu[0] & 0x1f)

		switch naluType {
		case NALU_SPS:
			sps, err := ParseSPSNALUnit(nalu, true)
			if err != nil {
				return frames, fmt.Errorf("parse SPS: %w", err)
			}
			d.spsMap[sps.ParameterID] = sps
		case NALU_PPS:
			pps, err := ParsePPSNALUnit(nalu, d.spsMap)
			if err != nil {
				return frames, fmt.Errorf("parse PPS: %w", err)
			}
			d.ppsMap[pps.PicParameterSetID] = pps
		case NALU_IDR:
			f, err := d.decodeIDR(nalu)
			if err != nil {
				return frames, fmt.Errorf("IDR frame %d: %w", len(frames), err)
			}
			d.refFrame = f
			frames = append(frames, f)
		case 1:
			if !includeNonIDR {
				continue
			}
			f, err := d.decodePSkip(nalu)
			if err != nil {
				return frames, fmt.Errorf("p frame %d: %w", len(frames), err)
			}
			d.refFrame = f
			frames = append(frames, f)
		}
	}
	return frames, nil
}

func (d *Decoder) DecodeAnnexB(data []byte) (*Frame, error) {
	nalus := ExtractNalusFromByteStream(data)
	return d.DecodeNALUs(nalus)
}

func (d *Decoder) DecodeAllAnnexB(data []byte) ([]*Frame, error) {
	nalus := ExtractNalusFromByteStream(data)
	return d.DecodeAllFrames(nalus)
}

func (d *Decoder) DecodeIDRAnnexB(data []byte) ([]*Frame, error) {
	nalus := ExtractNalusFromByteStream(data)
	return d.DecodeIDRFrames(nalus)
}

func (d *Decoder) DecodeAVC(data []byte) (*Frame, error) {
	nalus, err := extractAVCNalus(data)
	if err != nil {
		return nil, err
	}
	return d.DecodeNALUs(nalus)
}

func (d *Decoder) DecodeAllAVC(data []byte) ([]*Frame, error) {
	nalus, err := extractAVCNalus(data)
	if err != nil {
		return nil, err
	}
	return d.DecodeAllFrames(nalus)
}

func (d *Decoder) DecodeIDRAVC(data []byte) ([]*Frame, error) {
	nalus, err := extractAVCNalus(data)
	if err != nil {
		return nil, err
	}
	return d.DecodeIDRFrames(nalus)
}

func extractAVCNalus(data []byte) ([][]byte, error) {
	var nalus [][]byte
	for len(data) >= 4 {
		length := int(binary.BigEndian.Uint32(data[:4]))
		data = data[4:]
		if length < 0 || length > len(data) {
			return nil, fmt.Errorf("AVC NALU length %d exceeds remaining data %d", length, len(data))
		}
		nalus = append(nalus, data[:length])
		data = data[length:]
	}
	return nalus, nil
}

func cloneFrame(src *Frame) *Frame {
	dst := NewFrame(src.Width, src.Height)
	copy(dst.Y, src.Y)
	copy(dst.Cb, src.Cb)
	copy(dst.Cr, src.Cr)
	return dst
}

func (d *Decoder) decodePSkip(nalu []byte) (*Frame, error) {
	if d.refFrame == nil {
		return nil, fmt.Errorf("P_Skip: no reference frame available")
	}

	sh, err := ParseSliceHeader(nalu, d.spsMap, d.ppsMap)
	if err != nil {
		return nil, fmt.Errorf("parse P-slice header: %w", err)
	}

	pps := d.ppsMap[sh.PicParamID]
	sps := d.spsMap[pps.SeqParameterSetID]

	width := int(sps.Width)
	height := int(sps.Height)

	if !pps.EntropyCodingModeFlag {
		fullData := removeEBSPPrevention(nalu)
		br := NewBitReader(fullData)
		nalRefIdc := (nalu[0] >> 5) & 0x3

		err = br.SkipSliceHeaderP(SliceHeaderParams{
			FrameMbsOnly:                          sps.FrameMbsOnlyFlag,
			Log2MaxFrameNumMinus4:                 uint(sps.Log2MaxFrameNumMinus4),
			PicOrderCntType:                       uint(sps.PicOrderCntType),
			Log2MaxPicOrderCntLsbMinus4:           uint(sps.Log2MaxPicOrderCntLsbMinus4),
			BottomFieldPicOrderInFramePresentFlag: pps.BottomFieldPicOrderInFramePresentFlag,
			DeblockingFilterControlPresent:        pps.DeblockingFilterControlPresentFlag,
			RedundantPicCntPresentFlag:            pps.RedundantPicCntPresentFlag,
		}, nalRefIdc)
		if err != nil {
			return nil, fmt.Errorf("skip P-slice header: %w", err)
		}

		mbSkipRun, err := br.ReadUE()
		if err != nil {
			return nil, fmt.Errorf("read mb_skip_run: %w", err)
		}

		totalMBs := ((width + 15) / 16) * ((height + 15) / 16)
		if int(mbSkipRun) != totalMBs {
			return nil, fmt.Errorf("P_Skip: mb_skip_run=%d, expected %d (non-skip P-frames not yet supported)",
				mbSkipRun, totalMBs)
		}
	} else {
		sliceData := removeEBSPPrevention(nalu[sh.Size:])
		sliceQPY := 26 + int(pps.PicInitQpMinus26) + int(sh.SliceQPDelta)
		dec, err2 := NewCabacDecoder(sliceData)
		if err2 != nil {
			return nil, fmt.Errorf("CABAC P_Skip: init decoder: %w", err2)
		}
		models := InitModels(sliceQPY, 0, int(sh.CabacInitIDC))
		ctx := models[:]

		totalMBs := ((width + 15) / 16) * ((height + 15) / 16)
		for mbIdx := range totalMBs {
			mbSkipFlag := dec.DecodeDecision(&ctx[11])
			if mbSkipFlag != 1 {
				return nil, fmt.Errorf("CABAC P-frame: non-skip MB %d not supported (mb_skip_flag=%d)",
					mbIdx, mbSkipFlag)
			}
			isLast := mbIdx == totalMBs-1
			term := dec.DecodeTerminate()
			if isLast && term != 1 {
				return nil, fmt.Errorf("CABAC P_Skip: expected end_of_slice at last MB %d", mbIdx)
			}
			if !isLast && term != 0 {
				return nil, fmt.Errorf("CABAC P_Skip: unexpected end_of_slice at MB %d", mbIdx)
			}
		}
	}

	return cloneFrame(d.refFrame), nil
}

func (d *Decoder) decodeIDR(nalu []byte) (*Frame, error) {
	sh, err := ParseSliceHeader(nalu, d.spsMap, d.ppsMap)
	if err != nil {
		return nil, fmt.Errorf("parse slice header: %w", err)
	}

	pps := d.ppsMap[sh.PicParamID]
	sps := d.spsMap[pps.SeqParameterSetID]

	width := int(sps.Width)
	height := int(sps.Height)
	mbWidth := (width + 15) / 16
	mbHeight := (height + 15) / 16

	sliceQPY := 26 + int(pps.PicInitQpMinus26) + int(sh.SliceQPDelta)

	chromaArrayType := 1
	if sps.ChromaFormatIDC == 0 {
		chromaArrayType = 0
	}

	bitDepthY := 8 + int(sps.BitDepthLumaMinus8)
	bitDepthC := 8 + int(sps.BitDepthChromaMinus8)

	var sc *SliceContext
	if pps.EntropyCodingModeFlag {
		sliceData := removeEBSPPrevention(nalu[sh.Size:])
		var err2 error
		sc, err2 = DecodeSliceData(sliceData, sliceQPY, mbWidth, mbHeight,
			pps.Transform8x8ModeFlag, chromaArrayType, bitDepthY, bitDepthC,
			int(pps.ChromaQpIndexOffset), d.TraceMBCMP)
		if err2 != nil {
			return nil, fmt.Errorf("decode slice data (CABAC): %w", err2)
		}
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
			return nil, fmt.Errorf("skip slice header (CAVLC): %w", err)
		}

		var err2 error
		sc, err2 = DecodeSliceDataCAVLC(br, sliceQPY, mbWidth, mbHeight,
			pps.Transform8x8ModeFlag, chromaArrayType, bitDepthY, bitDepthC,
			int(pps.ChromaQpIndexOffset), d.TraceMBCMP)
		if err2 != nil {
			return nil, fmt.Errorf("decode slice data (CAVLC): %w", err2)
		}
	}

	f := NewFrame(width, height)

	if sps.VUI != nil {
		if sps.VUI.ColourDescriptionFlag {
			f.ColorDescriptionValid = true
			f.MatrixCoefficients = sps.VUI.MatrixCoefficients
		}
		f.VideoFullRangeFlag = sps.VUI.VideoFullRangeFlag
	}

	err = reconstructFrame(sc, f, sps, pps)
	if err != nil {
		putMBs(sc.MBs)
		return nil, fmt.Errorf("reconstruct frame: %w", err)
	}

	if sh.DisableDeblockingFilterIDC != 1 && !d.SkipDeblock {
		Deblock(f, sc,
			int(sh.SliceAlphaC0OffsetDiv2)*2,
			int(sh.SliceBetaOffsetDiv2)*2)
	}
	putMBs(sc.MBs)

	return f, nil
}

func removeEBSPPrevention(data []byte) []byte {
	found := false
	for i := 0; i+2 < len(data); i++ {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 3 {
			found = true
			break
		}
	}
	if !found {
		return data
	}
	result := make([]byte, 0, len(data))
	i := 0
	for i < len(data) {
		if i+2 < len(data) && data[i] == 0 && data[i+1] == 0 && data[i+2] == 3 {
			result = append(result, 0, 0)
			i += 3
		} else {
			result = append(result, data[i])
			i++
		}
	}
	return result
}

func reconstructFrame(sc *SliceContext, f *Frame, sps *SPS, pps *PPS) error {
	sm := buildScalingMatrices(sps, pps)

	for mbIdx := 0; mbIdx < sc.TotalMBs; mbIdx++ {
		mbX := mbIdx % sc.MBWidth
		mbY := mbIdx / sc.MBWidth
		mb := &sc.MBs[mbIdx]

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

func reconstructI16x16(sc *SliceContext, f *Frame,
	mbIdx, mbX, mbY int, mb *MBData, sm *ScalingMatrices) error {
	top, left, topLeft, hasTop, hasLeft := getLuma16x16Neighbors(f, mbX, mbY)
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

	for i := range 16 {
		bx := inverseRasterX4x4[i]
		by := inverseRasterY4x4[i]

		ref := getLuma4x4Neighbors(f, x0+bx, y0+by, i, mbX, mbY, sc.MBWidth, sc.MBHeight)

		leftAvail := x0+bx > 0
		topAvail := y0+by > 0
		predBlock := Predict4x4(mb.Intra4x4PredMode[i], ref, leftAvail, topAvail)

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
	frameW := sc.MBWidth * 16
	frameH := sc.MBHeight * 16
	sl := &sm.IntraY8x8

	for i := range 4 {
		bx := (i % 2) * 8
		by := (i / 2) * 8

		ref := getLuma8x8Neighbors(f, x0+bx, y0+by, i, frameW, frameH)

		leftAvail := x0+bx > 0
		topAvail := y0+by > 0
		predBlock := Predict8x8(mb.Intra8x8PredMode[i], ref, leftAvail, topAvail)

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

func getLuma8x8Neighbors(f *Frame, blkX, blkY int, i8x8 int, frameW, frameH int) [25]uint8 {
	var ref [25]uint8
	var avail [25]bool

	for i := range 8 {
		px, py := blkX-1, blkY+7-i
		if px >= 0 && py >= 0 && py < frameH {
			ref[i] = f.GetLumaPixel(px, py)
			avail[i] = true
		}
	}

	if blkX > 0 && blkY > 0 {
		ref[8] = f.GetLumaPixel(blkX-1, blkY-1)
		avail[8] = true
	}

	for i := range 8 {
		px, py := blkX+i, blkY-1
		if py >= 0 && px >= 0 && px < frameW {
			ref[9+i] = f.GetLumaPixel(px, py)
			avail[9+i] = true
		}
	}

	topRightOK := i8x8 != 3
	for i := range 8 {
		px, py := blkX+8+i, blkY-1
		if topRightOK && py >= 0 && px >= 0 && px < frameW {
			ref[17+i] = f.GetLumaPixel(px, py)
			avail[17+i] = true
		}
	}

	firstAvailIdx := -1
	for i := range 25 {
		if avail[i] {
			firstAvailIdx = i
			break
		}
	}
	if firstAvailIdx == -1 {
		for i := range ref {
			ref[i] = 128
		}
	} else {
		for i := 0; i < firstAvailIdx; i++ {
			ref[i] = ref[firstAvailIdx]
		}
		for i := firstAvailIdx + 1; i < 25; i++ {
			if !avail[i] {
				ref[i] = ref[i-1]
			}
		}
	}

	return filterRefSamples8x8(ref)
}

func filterRefSamples8x8(ref [25]uint8) [25]uint8 {
	var f [25]uint8
	f[0] = uint8((int(ref[1]) + 3*int(ref[0]) + 2) >> 2)
	for i := 1; i < 24; i++ {
		f[i] = uint8((int(ref[i-1]) + 2*int(ref[i]) + int(ref[i+1]) + 2) >> 2)
	}
	f[24] = uint8((int(ref[23]) + 3*int(ref[24]) + 2) >> 2)
	return f
}

func reconstructChroma(sc *SliceContext, f *Frame,
	mbIdx, mbX, mbY int, mb *MBData, sm *ScalingMatrices) {
	chromaSL := [2]*[16]int32{&sm.IntraCb4x4, &sm.IntraCr4x4}

	for iCbCr := range 2 {
		sl := chromaSL[iCbCr]

		top, left, topLeft, hasTop, hasLeft := getChromaNeighbors(f, iCbCr, mbX, mbY)
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

func getLuma16x16Neighbors(f *Frame, mbX, mbY int) (
	top [16]uint8, left [16]uint8, topLeft uint8, hasTop, hasLeft bool) {
	x0 := mbX * 16
	y0 := mbY * 16

	if mbY > 0 {
		hasTop = true
		for x := range 16 {
			top[x] = f.GetLumaPixel(x0+x, y0-1)
		}
	}

	if mbX > 0 {
		hasLeft = true
		for y := range 16 {
			left[y] = f.GetLumaPixel(x0-1, y0+y)
		}
	}

	if mbX > 0 && mbY > 0 {
		topLeft = f.GetLumaPixel(x0-1, y0-1)
	}

	return
}

var topRightNotAvail4x4 = [16]bool{
	false, false, false, true,
	false, false, false, true,
	false, false, false, true,
	false, false, false, true,
}

func getLuma4x4Neighbors(f *Frame, x0, y0 int, blkIdx int, mbX, mbY int, mbW, mbH int) [13]uint8 {
	var ref [13]uint8
	frameW := mbW * 16
	frameH := mbH * 16

	for i := range 4 {
		if x0 > 0 && y0+3-i >= 0 && y0+3-i < frameH {
			ref[i] = f.GetLumaPixel(x0-1, y0+3-i)
		} else {
			ref[i] = 128
		}
	}

	if x0 > 0 && y0 > 0 {
		ref[4] = f.GetLumaPixel(x0-1, y0-1)
	} else {
		ref[4] = 128
	}

	for i := range 4 {
		if y0 > 0 && x0+i >= 0 && x0+i < frameW {
			ref[5+i] = f.GetLumaPixel(x0+i, y0-1)
		} else {
			ref[5+i] = 128
		}
	}

	trAvail := true
	if topRightNotAvail4x4[blkIdx] {
		trAvail = false
	} else if blkIdx == 5 && mbX >= mbW-1 {
		trAvail = false
	} else if blkIdx == 13 {
		trAvail = false
	}

	if trAvail {
		for i := 4; i < 8; i++ {
			if y0 > 0 && x0+i >= 0 && x0+i < frameW {
				ref[5+i] = f.GetLumaPixel(x0+i, y0-1)
			} else {
				ref[5+i] = 128
			}
		}
	} else {
		for i := 4; i < 8; i++ {
			ref[5+i] = ref[8]
		}
	}

	return ref
}

func getChromaNeighbors(f *Frame, comp int, mbX, mbY int) (
	top [8]uint8, left [8]uint8, topLeft uint8, hasTop, hasLeft bool) {
	x0 := mbX * 8
	y0 := mbY * 8

	if mbY > 0 {
		hasTop = true
		for x := range 8 {
			top[x] = f.GetChromaPixel(comp, x0+x, y0-1)
		}
	}

	if mbX > 0 {
		hasLeft = true
		for y := range 8 {
			left[y] = f.GetChromaPixel(comp, x0-1, y0+y)
		}
	}

	if mbX > 0 && mbY > 0 {
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
