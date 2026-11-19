package rtp

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestPacketExtension(t *testing.T) {
	raw := []byte{
		0x90, 0x60, 0x00, 0x01,
		0, 0, 0, 1,
		0, 0, 0, 2,
		0xFF, 0xD8, 0x00, 0x01,
		0xAA, 0xBB, 0xCC, 0xDD,
		0x11, 0x22,
	}

	var p Packet
	if err := p.Unmarshal(raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.ExtensionProfile != 0xFFD8 || !bytes.Equal(p.ExtensionPayload, []byte{0xAA, 0xBB, 0xCC, 0xDD}) {
		t.Fatalf("extension %04x %x", p.ExtensionProfile, p.ExtensionPayload)
	}
	if !bytes.Equal(p.Payload, []byte{0x11, 0x22}) {
		t.Fatalf("payload %x", p.Payload)
	}

	// a packet without extension must not keep the previous one
	if err := p.Unmarshal([]byte{0x80, 0x60, 0, 2, 0, 0, 0, 1, 0, 0, 0, 2, 0x33}); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.ExtensionProfile != 0 || p.ExtensionPayload != nil {
		t.Fatalf("stale extension %04x %x", p.ExtensionProfile, p.ExtensionPayload)
	}
}

func TestDepacketizersOwnOutput(t *testing.T) {
	payload := []byte{0x65, 1, 2, 3}
	nalus, err := NewH264Depacketizer().Decode(&Packet{SequenceNumber: 1, Payload: payload})
	if err != nil || len(nalus) != 1 {
		t.Fatalf("decode: %v %d", err, len(nalus))
	}
	payload[1] = 0xEE
	if nalus[0][1] != 1 {
		t.Fatal("h264 unit aliases the packet buffer")
	}

	payload = []byte{0x26, 0x01, 1, 2}
	nalus, err = NewH265Depacketizer().Decode(&Packet{SequenceNumber: 1, Payload: payload})
	if err != nil || len(nalus) != 1 {
		t.Fatalf("decode: %v %d", err, len(nalus))
	}
	payload[2] = 0xEE
	if nalus[0][2] != 1 {
		t.Fatal("h265 unit aliases the packet buffer")
	}
}

func TestH264FUWithAnnexBUnits(t *testing.T) {
	d := NewH264Depacketizer()

	// sps, pps and idr squeezed into a single fragmented unit
	first := []byte{0x7c, 0x87, 0x42, 0x00, 0x00, 0x00, 0x00, 0x01, 0x68, 0xCE}
	second := []byte{0x7c, 0x47, 0x00, 0x00, 0x01, 0x65, 0x88, 0x84}

	if nalus, err := d.Decode(&Packet{SequenceNumber: 1, Payload: first}); err != nil || nalus != nil {
		t.Fatalf("start fragment: %v %v", err, nalus)
	}
	nalus, err := d.Decode(&Packet{SequenceNumber: 2, Payload: second, Marker: true})
	if err != nil {
		t.Fatalf("end fragment: %v", err)
	}
	want := [][]byte{{0x67, 0x42}, {0x68, 0xCE}, {0x65, 0x88, 0x84}}
	if len(nalus) != len(want) {
		t.Fatalf("got %d units, want %d: %x", len(nalus), len(want), nalus)
	}
	for i := range want {
		if !bytes.Equal(nalus[i], want[i]) {
			t.Fatalf("unit %d: %x, want %x", i, nalus[i], want[i])
		}
	}
}

func TestH264SingleFragmentFU(t *testing.T) {
	// start and end in one fragment is forbidden by the rfc but sent by some cameras
	nalus, err := NewH264Depacketizer().Decode(&Packet{SequenceNumber: 1, Payload: []byte{0x7c, 0xC5, 0x11, 0x22}})
	if err != nil || len(nalus) != 1 || !bytes.Equal(nalus[0], []byte{0x65, 0x11, 0x22}) {
		t.Fatalf("single fragment fu: %v %x", err, nalus)
	}
}

func TestH264AnnexBPayload(t *testing.T) {
	payload := []byte{0, 0, 0, 1, 0x67, 0x42, 0, 0, 1, 0x68, 0xCE}
	nalus, err := NewH264Depacketizer().Decode(&Packet{SequenceNumber: 1, Payload: payload})
	if err != nil || len(nalus) != 2 {
		t.Fatalf("annex b payload: %v %x", err, nalus)
	}
	if !bytes.Equal(nalus[0], []byte{0x67, 0x42}) || !bytes.Equal(nalus[1], []byte{0x68, 0xCE}) {
		t.Fatalf("units %x", nalus)
	}
}

func TestSTAPAKeepsUnitsBeforePadding(t *testing.T) {
	cases := [][]byte{
		// zero padding behind the last unit
		{24, 0, 2, 0x67, 0x42, 0, 2, 0x68, 0xCE, 0, 0, 0},
		// last unit cut short
		{24, 0, 2, 0x67, 0x42, 0, 2, 0x68, 0xCE, 0, 9, 0x65},
	}
	for _, payload := range cases {
		nalus, err := NewH264Depacketizer().Decode(&Packet{SequenceNumber: 1, Payload: payload})
		if err != nil || len(nalus) != 2 {
			t.Fatalf("stap-a %x: %v %x", payload, err, nalus)
		}
	}

	if _, err := NewH264Depacketizer().Decode(&Packet{SequenceNumber: 1, Payload: []byte{24, 0, 9, 0x67}}); err == nil {
		t.Fatal("expected error for a stap-a without any complete unit")
	}
}

func TestH265FUWithAnnexBUnitsAndAP(t *testing.T) {
	d := NewH265Depacketizer()

	first := []byte{49 << 1, 0x01, 0x80 | 32, 0xAA, 0x00, 0x00, 0x01, 0x42, 0x01, 0xBB}
	second := []byte{49 << 1, 0x01, 0x40 | 32, 0xCC}
	if nalus, _ := d.Decode(&Packet{SequenceNumber: 1, Payload: first}); nalus != nil {
		t.Fatalf("start fragment produced units: %x", nalus)
	}
	nalus, err := d.Decode(&Packet{SequenceNumber: 2, Payload: second})
	if err != nil || len(nalus) != 2 {
		t.Fatalf("fu with annex b: %v %x", err, nalus)
	}
	if !bytes.Equal(nalus[0], []byte{0x40, 0x01, 0xAA}) || !bytes.Equal(nalus[1], []byte{0x42, 0x01, 0xBB, 0xCC}) {
		t.Fatalf("units %x", nalus)
	}

	ap := []byte{48 << 1, 0x01, 0, 3, 0x40, 0x01, 0xAA, 0, 3, 0x42, 0x01, 0xBB, 0, 0}
	nalus, err = d.Decode(&Packet{SequenceNumber: 3, Payload: ap})
	if err != nil || len(nalus) != 2 {
		t.Fatalf("ap with padding: %v %x", err, nalus)
	}

	annexB := []byte{0, 0, 0, 1, 0x40, 0x01, 0xAA}
	nalus, err = d.Decode(&Packet{SequenceNumber: 4, Payload: annexB})
	if err != nil || len(nalus) != 1 || !bytes.Equal(nalus[0], []byte{0x40, 0x01, 0xAA}) {
		t.Fatalf("annex b payload: %v %x", err, nalus)
	}
}

func TestFUGrowsAcrossManyFragments(t *testing.T) {
	d := NewH264Depacketizer()
	want := []byte{0x65}
	var got [][]byte

	const fragments = 300
	for i := 0; i < fragments; i++ {
		header := byte(5)
		if i == 0 {
			header |= 0x80
		}
		if i == fragments-1 {
			header |= 0x40
		}
		body := bytes.Repeat([]byte{byte(i%250 + 2)}, 1400)
		want = append(want, body...)

		nalus, err := d.Decode(&Packet{SequenceNumber: uint16(i), Payload: append([]byte{0x7c, header}, body...)})
		if err != nil {
			t.Fatalf("fragment %d: %v", i, err)
		}
		got = append(got, nalus...)
	}
	if len(got) != 1 || !bytes.Equal(got[0], want) {
		t.Fatalf("reassembled %d units", len(got))
	}
}

// entropy coded scan and the full file of a small test picture
func testJPEG(t *testing.T, w, h int) (scan, file []byte) {
	t.Helper()
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			src.Set(x, y, color.RGBA{R: uint8(x * 5), G: uint8(y * 9), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 50}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	file = buf.Bytes()
	sos := bytes.Index(file, []byte{0xFF, 0xDA})
	start := sos + 2 + (int(file[sos+2])<<8 | int(file[sos+3]))
	return file[start : len(file)-2], file
}

func TestMJPEGStopsAtFirstEOI(t *testing.T) {
	scan, _ := testJPEG(t, 32, 32)
	body := append(append([]byte{}, scan...), 0xFF, 0xD9, 0, 0, 0xFF, 0xD9, 7)

	out, err := NewMJPEGDepacketizer().Decode(&Packet{
		SequenceNumber: 1,
		Marker:         true,
		Payload:        append([]byte{0, 0, 0, 0, 1, 50, 4, 4}, body...),
	})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !bytes.HasSuffix(out, append(scan[len(scan)-4:], 0xFF, 0xD9)) {
		t.Fatalf("frame does not end at the first eoi: % x", out[len(out)-8:])
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil || img.Bounds().Dx() != 32 {
		t.Fatalf("decode rebuilt jpeg: %v", err)
	}
}

func TestMJPEGOnvifExtensionSize(t *testing.T) {
	// 2048 pixels do not fit the 8 bit size field of rfc 2435
	const w, h = 2048, 16
	scan, file := testJPEG(t, w, h)

	sof := bytes.Index(file, []byte{0xFF, 0xC0})
	segment := file[sof : sof+2+(int(file[sof+2])<<8|int(file[sof+3]))]
	ext := append([]byte{}, segment...)
	for len(ext)%4 != 0 {
		ext = append(ext, 0xFF)
	}

	half := len(scan) / 2
	d := NewMJPEGDepacketizer()
	first := &Packet{
		SequenceNumber:   1,
		ExtensionProfile: 0xFFD8,
		ExtensionPayload: ext,
		Payload:          append([]byte{0, 0, 0, 0, 1, 50, 0, 0}, scan[:half]...),
	}
	if out, err := d.Decode(first); err != nil || out != nil {
		t.Fatalf("first fragment: %v %d", err, len(out))
	}
	second := &Packet{
		SequenceNumber:   2,
		Marker:           true,
		ExtensionProfile: 0xFFFF,
		Payload:          append([]byte{0, byte(half >> 16), byte(half >> 8), byte(half), 1, 50, 0, 0}, scan[half:]...),
	}
	out, err := d.Decode(second)
	if err != nil || out == nil {
		t.Fatalf("second fragment: %v", err)
	}

	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode rebuilt jpeg: %v", err)
	}
	if b := img.Bounds(); b.Dx() != w || b.Dy() != h {
		t.Fatalf("size %dx%d, want %dx%d", b.Dx(), b.Dy(), w, h)
	}

	// without the extension such a frame cannot be rebuilt
	if _, err := NewMJPEGDepacketizer().Decode(&Packet{
		SequenceNumber: 1,
		Marker:         true,
		Payload:        append([]byte{0, 0, 0, 0, 1, 50, 0, 0}, scan...),
	}); err == nil {
		t.Fatal("expected error for a frame without dimensions")
	}
}

func TestMJPEGOnvifReplayExtension(t *testing.T) {
	const w, h = 2048, 16
	scan, file := testJPEG(t, w, h)

	sof := bytes.Index(file, []byte{0xFF, 0xC0})
	segment := file[sof : sof+2+(int(file[sof+2])<<8|int(file[sof+3]))]
	for len(segment)%4 != 0 {
		segment = append(segment[:len(segment):len(segment)], 0xFF)
	}
	ext := make([]byte, 12)
	ext = append(ext, 0xFF, 0xD8, 0, byte(len(segment)/4))
	ext = append(ext, segment...)

	out, err := NewMJPEGDepacketizer().Decode(&Packet{
		SequenceNumber:   1,
		Marker:           true,
		ExtensionProfile: 0xABAC,
		ExtensionPayload: ext,
		Payload:          append([]byte{0, 0, 0, 0, 1, 50, 0, 0}, scan...),
	})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil || img.Bounds().Dx() != w {
		t.Fatalf("decode rebuilt jpeg: %v", err)
	}
}
