package h265

type dspContext struct {
	inverseTransform func(coef []int32, n int, dst bool, bitDepth int, extended bool, s *transformScratch)
	transformSkip    func(coef []int32, n int, rotate bool)
	dequant          func(coef []int32, m []uint8, n, qp, bitDepth int, extended bool)

	addResidual8  func(dst []uint8, stride int, coef []int32, n, shift int)
	addResidual16 func(dst []uint16, stride int, coef []int32, n, shift int, maxV int32)
}

var oddAsm func(out, in []int32, stride int)

var (
	deblockStrongAsm func(p []uint8, pitch int, tc0, tc1, flags int32)
	deblockNormalAsm func(p []uint8, pitch int, tc0, tc1, nd, flags int32)

	deblockTurnIn  func(dst, src []uint8, stride int)
	deblockTurnOut func(dst []uint8, stride int, src []uint8)
)

var planarAsm func(dst []uint8, stride int, r *refSamples, shift int)

var (
	predUniAsm func(dst []uint8, dstStride int, src []int16, srcStride, w, h, shift int)
	predBiAsm  func(dst []uint8, dstStride int, a, b []int16, srcStride, w, h, shift int)
)

var (
	mcCopyAsm func(dst []int16, dstStride int, src []uint8, srcStride, w, h, shift int)

	mcTapAsm func(dst []int16, dstStride int, src []uint8, srcStride, tapStride, w, h int,
		f []int16)

	mcTapV16Asm func(dst []int16, dstStride int, src []int16, srcStride, w, h, shift int,
		f []int16)
)

var dequant32Asm func(coef []int32, m []uint8, ls, rnd int32, sh int, lo, hi int32)

var idctColsAsm func(dst, src []int32, n int, rnd int32, shift int, lo, hi int32)

var transposeAsm func(dst, src []int32, n int)

func newDSPGo() *dspContext {
	return &dspContext{
		inverseTransform: inverseTransform,
		transformSkip:    transformSkip,
		dequant:          dequant,
	}
}

var dsp = func() *dspContext {
	d := newDSPGo()
	dspInit(d)

	return d
}()
