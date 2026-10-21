package h264

import "testing"

func pcmSPS() []byte {
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
	w.bit(0)

	return append([]byte{0x67}, w.bytes()...)
}

func pcmPPS() []byte {
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

	return append([]byte{0x68}, w.bytes()...)
}

func pcmBlockSamples(w *spsBitWriter, base byte) {
	w.ue(25) // mb type i pcm

	// pcm samples are byte aligned
	w.bit(1)
	for w.nbit != 0 {
		w.bit(0)
	}

	for i := range 256 {
		w.buf = append(w.buf, base+byte(i%200))
	}
	for i := range 64 {
		w.buf = append(w.buf, base+byte(i))
	}
	for i := range 64 {
		w.buf = append(w.buf, base+byte((i+30)%200))
	}
}

func pcmIDRCAVLC() []byte {
	w := &spsBitWriter{}
	w.ue(0) // first mb
	w.ue(7) // slice type i
	w.ue(0) // pps id
	w.u(0, 4)
	w.ue(0)  // idr pic id
	w.bit(0) // no output of prior pics
	w.bit(0) // long term reference
	w.ue(0)  // slice qp delta

	// two consecutive pcm macroblocks, the second must parse cleanly
	pcmBlockSamples(w, 1)
	pcmBlockSamples(w, 2)

	return append([]byte{0x65}, w.bytes()...)
}

func TestDecodeIDRPCMCAVLC(t *testing.T) {
	d := New()

	if _, err := d.DecodeNALUs([][]byte{pcmSPS()}); err != nil {
		t.Fatalf("sps: %v", err)
	}
	if _, err := d.DecodeNALUs([][]byte{pcmPPS()}); err != nil {
		t.Fatalf("pps: %v", err)
	}

	f, err := d.DecodeNALUs([][]byte{pcmIDRCAVLC()})
	if err != nil {
		t.Fatalf("idr: %v", err)
	}
	if f == nil {
		t.Fatal("no frame")
	}
	if f.Width != 32 || f.Height != 16 {
		t.Fatalf("size %dx%d", f.Width, f.Height)
	}

	if got := f.GetLumaPixel(0, 0); got != 1 {
		t.Errorf("y(0,0) = %d, want 1", got)
	}
	// edge columns are deblocked, check the clean pixel at col 15
	if got := f.GetLumaPixel(15, 0); got != 16 {
		t.Errorf("y(15,0) = %d, want 16", got)
	}

	if got := f.GetLumaPixel(16, 0); got != 2 {
		t.Errorf("y(16,0) = %d, want 2", got)
	}
	if got := f.GetLumaPixel(31, 0); got != 17 {
		t.Errorf("y(31,0) = %d, want 17", got)
	}

	if got := f.GetChromaPixel(0, 0, 0); got != 1 {
		t.Errorf("cb(0,0) = %d, want 1", got)
	}
	if got := f.GetChromaPixel(1, 0, 0); got != 31 {
		t.Errorf("cr(0,0) = %d, want 31", got)
	}
	if got := f.GetChromaPixel(0, 8, 0); got != 2 {
		t.Errorf("cb(8,0) = %d, want 2", got)
	}
}

func TestDecodeIPCMCABAC(t *testing.T) {
	sc := &SliceContext{
		ChromaArrayType: 1,
		BitDepthY:       8,
		BitDepthC:       8,
		MBs:             make([]MBData, 1),
	}

	data := make([]byte, 0, 2+384+2)
	data = append(data, 0xAB, 0xCD)
	for i := range 256 {
		data = append(data, byte(i%255+1))
	}
	for i := range 128 {
		data = append(data, byte(200-i%100))
	}
	data = append(data, 0x67, 0x45)

	dec, err := NewCabacDecoder(data)
	if err != nil {
		t.Fatalf("cabac init: %v", err)
	}
	sc.Cabac = dec

	if err := decodeIPCM(sc, 0); err != nil {
		t.Fatalf("decode ipcm: %v", err)
	}

	mb := &sc.MBs[0]
	if !mb.IsPCM || mb.MBType != MBTypeIPCM {
		t.Fatalf("mb state wrong: pcm=%v type=%d", mb.IsPCM, mb.MBType)
	}
	if mb.QPY != 0 {
		t.Errorf("qpy %d, want 0", mb.QPY)
	}
	if mb.PCMLuma[0] != 1 || mb.PCMLuma[254] != 255 {
		t.Error("luma samples wrong")
	}
	if mb.PCMCb[63] != 137 {
		t.Errorf("cb sample %d, want 137", mb.PCMCb[63])
	}

	// engine must be reinitialized after the pcm bytes
	if dec.pos != len(data) {
		t.Errorf("cabac pos %d, want %d", dec.pos, len(data))
	}
	ifo := uint16(data[len(data)-2])<<1 | uint16(data[len(data)-1]>>7)
	if dec.codIOffset != ifo || dec.codIRange != 510 {
		t.Errorf("engine not reinitialized")
	}
}
