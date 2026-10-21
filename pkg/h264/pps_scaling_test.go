package h264

import "testing"

// separate colour plane sps, chroma_format_idc 3 but chroma array type 0
func separatePlaneSPS() []byte {
	w := &spsBitWriter{}
	w.u(100, 8)
	w.u(0, 8)
	w.u(30, 8)
	w.ue(0)
	w.ue(3)
	w.bit(1) // separate colour plane
	w.ue(0)
	w.ue(0)
	w.bit(0) // qp prime y zero transform bypass
	w.bit(0) // scaling matrix
	w.ue(0)  // log2 max frame num minus 4
	w.ue(2)
	w.ue(0)
	w.bit(0)
	w.ue(1)
	w.ue(0)
	w.bit(1)
	w.bit(1)
	w.bit(0)
	w.bit(0)

	return append([]byte{0x67}, w.bytes()...)
}

func TestPPSScalingListCounts(t *testing.T) {
	sps := separatePlaneSPS()

	// 4:4:4 by chroma_format_idc and transform8x8: twelve list present flags
	w := &spsBitWriter{}
	w.ue(0)
	w.ue(0)
	w.bit(0)
	w.bit(0)
	w.ue(0)
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
	w.bit(1) // transform8x8
	w.bit(1) // scaling matrix
	for i := 0; i < 12; i++ {
		w.bit(0)
	}
	w.ue(0)
	pps := append([]byte{0x68}, w.bytes()...)

	d := New()
	if _, err := d.DecodeNALUs([][]byte{sps}); err != nil {
		t.Fatalf("sps: %v", err)
	}
	if _, err := d.DecodeNALUs([][]byte{pps}); err != nil {
		t.Fatalf("pps with 12 lists: %v", err)
	}

	// six list present flags when transform8x8 is off, even for 4:4:4
	w2 := &spsBitWriter{}
	w2.ue(0)
	w2.ue(0)
	w2.bit(0)
	w2.bit(0)
	w2.ue(0)
	w2.ue(0)
	w2.ue(0)
	w2.bit(0)
	w2.u(0, 2)
	w2.ue(0)
	w2.ue(0)
	w2.ue(0)
	w2.bit(0)
	w2.bit(0)
	w2.bit(0)
	w2.bit(0) // no transform8x8
	w2.bit(1) // scaling matrix
	for i := 0; i < 6; i++ {
		w2.bit(0)
	}
	w2.ue(0)
	pps2 := append([]byte{0x68}, w2.bytes()...)

	if _, err := d.DecodeNALUs([][]byte{pps2}); err != nil {
		t.Fatalf("pps with 6 lists: %v", err)
	}
}
