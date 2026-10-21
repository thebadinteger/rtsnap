package h264

import "testing"

// cabac slice data with two pcm macroblocks
func pcmCabacSliceData() []byte {
	var s []byte

	// mb 0: bin0 and the pcm terminate bin decode to i_pcm from these bytes
	s = append(s, 0xfe, 0xf0)

	for i := 0; i < 384; i++ {
		s = append(s, byte(i%250+1))
	}

	s = append(s, 0xfd, 0x80)

	for i := 0; i < 384; i++ {
		s = append(s, byte(i%250+2))
	}

	// the engine is reinitialized after the last pcm bytes as well
	return append(s, 0, 0)
}

func TestDecodeSliceCABACPCMTerminate(t *testing.T) {
	sc := &SliceContext{
		MBWidth:         2,
		TotalMBs:        2,
		MBs:             make([]MBData, 2),
		ChromaArrayType: 1,
		BitDepthY:       8,
		BitDepthC:       8,
	}
	sc.MBs[0].SliceStart = 0
	sc.MBs[1].SliceStart = 0

	lastMB, err := decodeSliceCABAC(sc, pcmCabacSliceData(), 26, 0)
	if err != nil {
		t.Fatalf("decode slice: %v", err)
	}
	if lastMB != 2 {
		t.Fatalf("lastMB %d, want 2", lastMB)
	}

	if !sc.MBs[0].IsPCM || !sc.MBs[1].IsPCM {
		t.Fatalf("both macroblocks must be pcm: %v %v", sc.MBs[0].IsPCM, sc.MBs[1].IsPCM)
	}

	if sc.MBs[0].PCMLuma[0] != 1 || sc.MBs[1].PCMLuma[0] != 2 {
		t.Errorf("pcm samples wrong: %d %d", sc.MBs[0].PCMLuma[0], sc.MBs[1].PCMLuma[0])
	}

	// pcm must not disturb the running qp or the dquant context
	if sc.QPY != 26 {
		t.Errorf("sc.QPY %d, want 26", sc.QPY)
	}
	if sc.PrevMBQPDeltaNonZero {
		t.Error("PrevMBQPDeltaNonZero must be false after pcm")
	}
}
