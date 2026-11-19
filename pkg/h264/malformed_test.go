package h264

import (
	"fmt"
	"runtime"
	"testing"
	"time"
)

type hardenStream struct {
	deblockCtl  bool
	sliceQPDlt  int
	alpha, beta int
	mbType      uint
	sliceType   uint
}

func hardenSPS() []byte {
	w := &spsBitWriter{}
	w.u(66, 8)
	w.u(0, 8)
	w.u(30, 8)
	w.ue(0) // sps id
	w.ue(0) // log2_max_frame_num_minus4
	w.ue(2) // poc type
	w.ue(0) // num_ref_frames
	w.bit(0)
	w.ue(1) // width 2 mbs
	w.ue(0) // height 1 mb
	w.bit(1)
	w.bit(1)
	w.bit(0)
	w.bit(0)
	return append([]byte{0x67}, w.bytes()...)
}

func hardenPPS(deblockCtl bool) []byte {
	w := &spsBitWriter{}
	w.ue(0)
	w.ue(0)
	w.bit(0) // cavlc
	w.bit(0)
	w.ue(0)
	w.ue(0)
	w.ue(0)
	w.bit(0)
	w.u(0, 2)
	w.ue(0) // pic_init_qp 26
	w.ue(0)
	w.ue(0)
	if deblockCtl {
		w.bit(1)
	} else {
		w.bit(0)
	}
	w.bit(0)
	w.bit(0)
	return append([]byte{0x68}, w.bytes()...)
}

func hardenIDR(h hardenStream) []byte {
	w := &spsBitWriter{}
	se := func(v int) {
		if v > 0 {
			w.ue(uint(2*v - 1))
		} else {
			w.ue(uint(-2 * v))
		}
	}
	w.ue(0)           // first mb
	w.ue(h.sliceType) // slice type
	w.ue(0)           // pps id
	w.u(0, 4)         // frame_num
	w.ue(0)           // idr pic id
	w.bit(0)
	w.bit(0)
	se(h.sliceQPDlt)
	if h.deblockCtl {
		w.ue(0)
		se(h.alpha)
		se(h.beta)
	}
	for range 2 {
		w.ue(h.mbType) // i16x16
		w.ue(0)        // chroma dc
		w.ue(0)        // mb_qp_delta
		w.bit(1)       // dc coeff_token, no coefficients
	}
	return append([]byte{0x65}, escapeRBSP(w.bytes())...)
}

func runHarden(t *testing.T, nalus ...[]byte) (f *Frame, panicked any, err error) {
	t.Helper()
	defer func() {
		panicked = recover()
	}()
	d := New()
	for _, n := range nalus {
		f, err = d.DecodeNALUs([][]byte{n})
		if err != nil || f != nil {
			return
		}
	}
	return
}

func TestHardenValidBaseline(t *testing.T) {
	f, p, err := runHarden(t, hardenSPS(), hardenPPS(true), hardenIDR(hardenStream{deblockCtl: true, mbType: 3, sliceType: 7}))
	if p != nil || err != nil || f == nil {
		t.Fatalf("valid stream: frame=%v err=%v panic=%v", f != nil, err, p)
	}
}

func TestHardenCases(t *testing.T) {
	cases := []struct {
		name string
		h    hardenStream
	}{
		{"alpha_offset_+40", hardenStream{deblockCtl: true, alpha: 40, mbType: 3, sliceType: 7}},
		{"alpha_offset_-40", hardenStream{deblockCtl: true, alpha: -40, mbType: 3, sliceType: 7}},
		{"beta_offset_+40", hardenStream{deblockCtl: true, beta: 40, mbType: 3, sliceType: 7}},
		{"slice_qp_delta_+60", hardenStream{deblockCtl: true, sliceQPDlt: 60, mbType: 3, sliceType: 7}},
		{"slice_qp_delta_-100", hardenStream{deblockCtl: true, sliceQPDlt: -100, mbType: 3, sliceType: 7}},
		{"i16x16_vertical_no_top", hardenStream{deblockCtl: true, mbType: 1, sliceType: 7}},
		{"i16x16_horizontal_no_left", hardenStream{deblockCtl: true, mbType: 2, sliceType: 7}},
	}
	for _, c := range cases {
		f, p, err := runHarden(t, hardenSPS(), hardenPPS(c.h.deblockCtl), hardenIDR(c.h))
		switch {
		case p != nil:
			t.Errorf("%-28s PANIC: %v", c.name, p)
		case f != nil:
			t.Errorf("%-28s returned a frame without error", c.name)
		default:
			t.Logf("%-28s ok, err=%v", c.name, err)
		}
	}
}

// pps with slice group map type 3 and slice_group_change_rate_minus1 = 2^64-1
func TestHardenFMOChangeRate(t *testing.T) {
	w := &spsBitWriter{}
	w.ue(0)
	w.ue(0)
	w.bit(0)
	w.bit(0)
	w.ue(1) // num_slice_groups_minus1
	w.ue(3) // slice_group_map_type
	w.bit(0)
	for range 64 { // exp-golomb with 64 leading zeros
		w.bit(0)
	}
	w.bit(1)
	for range 64 {
		w.bit(0)
	}
	w.ue(0)
	w.ue(0)
	w.bit(0)
	w.u(0, 2)
	w.ue(0)
	w.ue(0)
	w.ue(0)
	w.bit(0)
	w.bit(0)
	w.bit(0)
	pps := append([]byte{0x68}, escapeRBSP(w.bytes())...)
	_, p, err := runHarden(t, hardenSPS(), pps, hardenIDR(hardenStream{mbType: 3, sliceType: 7}))
	if p != nil {
		t.Fatalf("PANIC: %v", p)
	}
	t.Logf("err=%v", err)
}

func measure(fn func()) (dur time.Duration, allocMB float64) {
	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	t0 := time.Now()
	fn()
	dur = time.Since(t0)
	runtime.ReadMemStats(&m1)
	return dur, float64(m1.TotalAlloc-m0.TotalAlloc) / (1 << 20)
}

// vui hrd with cpb_cnt_minus1 = 2^24-1 in a 20 byte sps
func TestHardenSPSCpbCount(t *testing.T) {
	w := &spsBitWriter{}
	w.u(66, 8)
	w.u(0, 8)
	w.u(30, 8)
	w.ue(0)
	w.ue(0)
	w.ue(2)
	w.ue(0)
	w.bit(0)
	w.ue(1)
	w.ue(0)
	w.bit(1)
	w.bit(1)
	w.bit(0)
	w.bit(1) // vui present
	w.bit(0) // aspect ratio
	w.bit(0) // overscan
	w.bit(0) // video signal
	w.bit(0) // chroma loc
	w.bit(0) // timing
	w.bit(1) // nal hrd present
	w.ue(1<<24 - 1)
	sps := append([]byte{0x67}, escapeRBSP(w.bytes())...)
	var err error
	dur, mb := measure(func() { _, err = ParseSPSNALUnit(sps, true) })
	msg := fmt.Sprintf("%d byte sps: %v, %.0f MB allocated, err=%v", len(sps), dur, mb, err)
	if mb > 8 {
		t.Errorf("unbounded: %s", msg)
	} else {
		t.Log(msg)
	}
}

// pps with num_slice_groups_minus1 = 2^24-1 and map type 0
func TestHardenPPSSliceGroups(t *testing.T) {
	w := &spsBitWriter{}
	w.ue(0)
	w.ue(0)
	w.bit(0)
	w.bit(0)
	w.ue(1<<24 - 1)
	w.ue(0)
	pps := append([]byte{0x68}, escapeRBSP(w.bytes())...)
	var err error
	dur, mb := measure(func() { _, err = ParsePPSNALUnit(pps, map[uint32]*SPS{}) })
	msg := fmt.Sprintf("%d byte pps: %v, %.0f MB allocated, err=%v", len(pps), dur, mb, err)
	if mb > 8 {
		t.Errorf("unbounded: %s", msg)
	} else {
		t.Log(msg)
	}
}

// idr nal carrying a p slice with weighted prediction and num_ref_idx_l0_active_minus1 = 2^28-1
func TestHardenPSliceRefIdxLoop(t *testing.T) {
	pw := &spsBitWriter{}
	pw.ue(0)
	pw.ue(0)
	pw.bit(0)
	pw.bit(0)
	pw.ue(0)
	pw.ue(0)
	pw.ue(0)
	pw.bit(1) // weighted_pred_flag
	pw.u(0, 2)
	pw.ue(0)
	pw.ue(0)
	pw.ue(0)
	pw.bit(0)
	pw.bit(0)
	pw.bit(0)
	pps := append([]byte{0x68}, pw.bytes()...)

	w := &spsBitWriter{}
	w.ue(0)
	w.ue(5) // p slice
	w.ue(0)
	w.u(0, 4)
	w.ue(0)
	w.bit(1) // num_ref_idx_active_override_flag
	w.ue(1<<28 - 1)
	idr := append([]byte{0x65}, escapeRBSP(w.bytes())...)
	var err error
	var p any
	dur, _ := measure(func() { _, p, err = runHarden(t, hardenSPS(), pps, idr) })
	msg := fmt.Sprintf("%d byte slice: %v, err=%v panic=%v", len(idr), dur, err, p)
	if dur > 200*time.Millisecond {
		t.Errorf("cpu burn: %s", msg)
	} else {
		t.Log(msg)
	}
}

func escapeRBSP(b []byte) []byte {
	out := make([]byte, 0, len(b)+8)
	zeros := 0
	for _, c := range b {
		if zeros >= 2 && c <= 3 {
			out = append(out, 3)
			zeros = 0
		}
		out = append(out, c)
		if c == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return out
}
