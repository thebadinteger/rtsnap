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

func ppsWithScaling(sps []byte, transform8x8 bool, firstList bool, lists int) ([]byte, error) {
	d := New()
	if _, err := d.DecodeNALUs([][]byte{sps}); err != nil {
		return nil, err
	}

	w := &spsBitWriter{}
	w.ue(0)  // pps id
	w.ue(0)  // sps id
	w.bit(0) // entropy coding mode
	w.bit(0)
	w.ue(0) // num slice groups
	w.ue(0) // ref idx l0
	w.ue(0) // ref idx l1
	w.bit(0)
	w.u(0, 2)
	w.ue(0) // pic init qp
	w.ue(0)
	w.ue(0)
	w.bit(0) // deblocking filter control
	w.bit(0)
	w.bit(0)

	if transform8x8 {
		w.bit(1)
	} else {
		w.bit(0)
	}

	w.bit(1) // pic scaling matrix present

	for i := 0; i < lists; i++ {
		if i == 0 && firstList {
			w.bit(1)

			// four by four list with alternating deltas, values become 9 8 9 8 ...
			for j := 0; j < 16; j++ {
				if j%2 == 0 {
					w.ue(1)
				} else {
					w.ue(2)
				}
			}

			continue
		}

		w.bit(0)
	}

	w.ue(0) // second chroma qp index offset

	return append([]byte{0x68}, w.bytes()...), nil
}

func TestPPSScalingListCountSeparatePlane(t *testing.T) {
	spsNAL := separatePlaneSPS()

	sps, err := ParseSPSNALUnit(spsNAL, true)
	if err != nil {
		t.Fatalf("sps: %v", err)
	}

	ppsNAL, err := ppsWithScaling(spsNAL, true, true, 12)
	if err != nil {
		t.Fatalf("build pps: %v", err)
	}

	pps, err := ParsePPSNALUnit(ppsNAL, map[uint32]*SPS{sps.ParameterID: sps})
	if err != nil {
		t.Fatalf("parse pps: %v", err)
	}

	if len(pps.PicScalingLists) != 12 {
		t.Fatalf("pic scaling lists %d, want 12", len(pps.PicScalingLists))
	}

	if got := pps.PicScalingLists[0]; got == nil || len(got) != 16 || got[0] != 9 || got[1] != 8 || got[15] != 8 {
		t.Fatalf("first scaling list wrong: %v", got)
	}

	if pps.SecondChromaQpIndexOffset != 0 {
		t.Errorf("second chroma qp offset %d, want 0", pps.SecondChromaQpIndexOffset)
	}
}

func TestPPSScalingListCountNo8x8(t *testing.T) {
	spsNAL := separatePlaneSPS()

	sps, err := ParseSPSNALUnit(spsNAL, true)
	if err != nil {
		t.Fatalf("sps: %v", err)
	}

	// without transform8x8 the eight by eight lists are absent, six remain
	ppsNAL, err := ppsWithScaling(spsNAL, false, true, 6)
	if err != nil {
		t.Fatalf("build pps: %v", err)
	}

	pps, err := ParsePPSNALUnit(ppsNAL, map[uint32]*SPS{sps.ParameterID: sps})
	if err != nil {
		t.Fatalf("parse pps: %v", err)
	}

	if len(pps.PicScalingLists) != 6 {
		t.Fatalf("pic scaling lists %d, want 6", len(pps.PicScalingLists))
	}

	if got := pps.PicScalingLists[0]; got == nil || len(got) != 16 || got[0] != 9 || got[1] != 8 {
		t.Fatalf("first scaling list wrong: %v", got)
	}
}

func TestPPSScalingListsEightWith8x8(t *testing.T) {
	sps, err := ParseSPSNALUnit(pcmSPS(), true)
	if err != nil {
		t.Fatalf("sps: %v", err)
	}

	// 4:2:0 keeps eight lists: six 4x4 and two 8x8 blocks, values checked too
	ppsNAL, err := ppsWithScaling(pcmSPS(), true, true, 8)
	if err != nil {
		t.Fatalf("build pps: %v", err)
	}

	pps, err := ParsePPSNALUnit(ppsNAL, map[uint32]*SPS{sps.ParameterID: sps})
	if err != nil {
		t.Fatalf("parse pps: %v", err)
	}

	if len(pps.PicScalingLists) != 8 {
		t.Fatalf("pic scaling lists %d, want 8", len(pps.PicScalingLists))
	}

	if got := pps.PicScalingLists[0]; got == nil || len(got) != 16 || got[0] != 9 {
		t.Fatalf("first scaling list wrong: %v", got)
	}

	if pps.PicScalingLists[6] != nil {
		t.Errorf("eight by eight list must stay nil")
	}
}
