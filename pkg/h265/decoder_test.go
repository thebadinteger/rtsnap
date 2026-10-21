package h265

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"os"
	"testing"
)

func TestDecodeTinyIntra(t *testing.T) {
	data, err := os.ReadFile("../../tests/testdata/tiny_intra.h265")
	if err != nil {
		t.Skip("testdata not found")
	}

	var d Decoder
	var decodedPic *Picture

	for _, nal := range SplitAnnexB(data) {
		pics, err := d.DecodeNAL(nal)
		if err != nil {
			t.Fatalf("failed to decode nal: %v", err)
		}
		for _, p := range pics {
			decodedPic = p
		}
	}

	for _, p := range d.Flush() {
		decodedPic = p
	}

	if decodedPic == nil {
		t.Fatal("no picture decoded")
	}

	img := decodedPic.Image()
	if img == nil {
		t.Fatal("image is nil")
	}

	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		t.Fatalf("invalid bounds: %v", b)
	}
	t.Logf("decoded h265 picture size: %dx%d", b.Dx(), b.Dy())
}

func TestGenerateUnavailableMarksCorrupt(t *testing.T) {
	data, err := os.ReadFile("../../tests/testdata/tiny_intra.h265")
	if err != nil {
		t.Skip("testdata not found")
	}

	var d Decoder
	defer d.Close()

	for _, nal := range SplitAnnexB(data) {
		if _, err := d.DecodeNAL(nal); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}

	var s *sps
	for _, v := range d.sps {
		s = v
		break
	}
	if s == nil {
		t.Skip("no sps decoded")
	}

	rps := &refPicSet{stCurrBefore: []int32{12345}}
	d.generateUnavailable(rps, s)

	found := false
	for _, e := range d.dpb {
		if e.pic.POC == 12345 {
			found = true
			if !e.pic.Corrupt {
				t.Fatal("unavailable reference is not marked corrupt")
			}
		}
	}
	if !found {
		t.Fatal("unavailable reference was not stored")
	}
}

func TestDecode1080p(t *testing.T) {
	data, err := os.ReadFile("../../tests/testdata/1080p.h265")
	if err != nil {
		t.Skip("testdata not found")
	}

	var d Decoder
	var decodedPic *Picture

	for _, nal := range SplitAnnexB(data) {
		pics, err := d.DecodeNAL(nal)
		if err != nil {
			t.Fatalf("failed to decode nal: %v", err)
		}
		for _, p := range pics {
			decodedPic = p
		}
	}

	for _, p := range d.Flush() {
		decodedPic = p
	}

	if decodedPic == nil {
		t.Fatal("no picture decoded")
	}

	img := decodedPic.Image()
	if img == nil {
		t.Fatal("image is nil")
	}

	b := img.Bounds()
	t.Logf("decoded 1080p picture size: %dx%d", b.Dx(), b.Dy())
}

func TestDecodeGray(t *testing.T) {
	data, err := os.ReadFile("../../tests/testdata/gray256.h265")
	if err != nil {
		t.Skip("testdata not found")
	}
	ref, err := os.ReadFile("../../tests/testdata/gray256.yuv")
	if err != nil {
		t.Skip("reference not found")
	}

	var d Decoder
	defer d.Close()

	var p *Picture
	for _, nal := range SplitAnnexB(data) {
		pics, err := d.DecodeNAL(nal)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, x := range pics {
			p = x
		}
	}
	if p == nil {
		for _, x := range d.Flush() {
			p = x
		}
	}
	if p == nil {
		t.Fatal("no picture decoded")
	}
	defer p.Release()

	if p.CropW*p.CropH != len(ref) {
		t.Fatalf("picture %dx%d does not match reference %d", p.CropW, p.CropH, len(ref))
	}

	for y := 0; y < p.CropH; y++ {
		row := p.Y[y*p.StrideY : y*p.StrideY+p.CropW]
		want := ref[y*p.CropW : (y+1)*p.CropW]
		for x := range row {
			if row[x] != want[x] {
				t.Fatalf("pixel %d,%d: got %d want %d", x, y, row[x], want[x])
			}
		}
	}
}

func TestDecodeFrameSizeLimit(t *testing.T) {
	data, err := os.ReadFile("../../tests/testdata/tiny_intra.h265")
	if err != nil {
		t.Skip("testdata not found")
	}

	var d Decoder
	d.FrameSizeLimit(100)
	failed := false
	for _, nal := range SplitAnnexB(data) {
		if _, err := d.DecodeNAL(nal); err != nil {
			failed = true
			break
		}
	}
	if !failed {
		t.Fatal("expected error for frame over limit, got nil")
	}
}

func FuzzH265DecodeNAL(f *testing.F) {
	data, err := os.ReadFile("../../tests/testdata/tiny_intra.h265")
	if err != nil {
		f.Skip("testdata not found")
	}
	f.Add([]byte{})
	for _, nal := range SplitAnnexB(data) {
		f.Add(nal.RBSP)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if u, ok := ParseNAL(b); ok {
			var d Decoder
			d.FrameSizeLimit(7680 * 4320)
			_, _ = d.DecodeNAL(u)
		}
	})
}

func TestDecodeTinyIntraConform(t *testing.T) {
	data, err := os.ReadFile("../../tests/testdata/tiny_intra.h265")
	if err != nil {
		t.Skip("testdata not found")
	}
	ref, err := os.ReadFile("../../tests/testdata/tiny_intra_ref.yuv")
	if err != nil {
		t.Skip("reference not found")
	}

	var d Decoder
	var decodedPic *Picture

	for _, nal := range SplitAnnexB(data) {
		pics, err := d.DecodeNAL(nal)
		if err != nil {
			t.Fatalf("failed to decode nal: %v", err)
		}
		for _, p := range pics {
			decodedPic = p
		}
	}

	for _, p := range d.Flush() {
		decodedPic = p
	}

	if decodedPic == nil {
		t.Fatal("no picture decoded")
	}

	p := decodedPic
	if p.BitDepth != 8 || len(p.Y) == 0 {
		t.Fatalf("unexpected picture format: depth=%d chroma=%d", p.BitDepth, p.ChromaFormat)
	}

	w := p.CropW
	if w <= 0 {
		w = p.Width
	}
	h := p.CropH
	if h <= 0 {
		h = p.Height
	}

	cw, ch := w/2, h/2
	if len(ref) != w*h+2*cw*ch {
		t.Fatalf("reference size %d does not match picture %dx%d", len(ref), w, h)
	}

	got := make([]byte, 0, len(ref))
	for y := 0; y < h; y++ {
		got = append(got, p.Y[(p.CropY+y)*p.StrideY+p.CropX:(p.CropY+y)*p.StrideY+p.CropX+w]...)
	}
	for y := 0; y < ch; y++ {
		got = append(got, p.Cb[(p.CropY/2+y)*p.StrideC+p.CropX/2:(p.CropY/2+y)*p.StrideC+p.CropX/2+cw]...)
	}
	for y := 0; y < ch; y++ {
		got = append(got, p.Cr[(p.CropY/2+y)*p.StrideC+p.CropX/2:(p.CropY/2+y)*p.StrideC+p.CropX/2+cw]...)
	}

	if !bytes.Equal(got, ref) {
		t.Fatal("decoded pixels do not match reference yuv")
	}
}

func writePictureYUV(h hash.Hash, p *Picture) {
	for y := 0; y < p.CropH; y++ {
		h.Write(p.Y[y*p.StrideY : y*p.StrideY+p.CropW])
	}
	cw, ch := p.CropW/2, p.CropH/2
	for y := 0; y < ch; y++ {
		h.Write(p.Cb[y*p.StrideC : y*p.StrideC+cw])
	}
	for y := 0; y < ch; y++ {
		h.Write(p.Cr[y*p.StrideC : y*p.StrideC+cw])
	}
}

func pictureHash(p *Picture) string {
	h := sha256.New()
	writePictureYUV(h, p)
	return hex.EncodeToString(h.Sum(nil))
}

func decodeMultiSlice(t *testing.T, threads int) ([]string, string) {
	data, err := os.ReadFile("../../tests/testdata/multislice_intra.h265")
	if err != nil {
		t.Skip("testdata not found")
	}

	d := Decoder{}
	d.Threads(threads)
	defer d.Close()

	var pics []*Picture
	for _, nal := range SplitAnnexB(data) {
		out, err := d.DecodeNAL(nal)
		if err != nil {
			t.Fatalf("threads=%d: %v", threads, err)
		}
		pics = append(pics, out...)
	}
	pics = append(pics, d.Flush()...)

	hashes := make([]string, len(pics))
	all := sha256.New()
	for i, p := range pics {
		hashes[i] = pictureHash(p)
		writePictureYUV(all, p)
		p.Release()
	}
	return hashes, hex.EncodeToString(all.Sum(nil))
}

func TestDecodeMultiSlice(t *testing.T) {
	a, ha := decodeMultiSlice(t, 1)
	b, hb := decodeMultiSlice(t, 4)

	const want = "e736ee726ca6ebc6787ecd29968dfecd50e6eba7a7e3aecb0c153c14fab82d92"
	if ha != want || hb != want {
		t.Fatalf("decoded yuv hashes %s and %s, want %s", ha, hb, want)
	}
	if len(a) != 3 || len(b) != 3 {
		t.Fatalf("decoded %d and %d pictures, want 3", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("picture %d differs between thread counts", i)
		}
	}
}

func TestDecodeMultiSliceMissingSegment(t *testing.T) {
	want, _ := decodeMultiSlice(t, 1)
	data, err := os.ReadFile("../../tests/testdata/multislice_intra.h265")
	if err != nil {
		t.Skip("testdata not found")
	}

	d := Decoder{}
	defer d.Close()

	var clean, corrupt []string
	collect := func(p *Picture) {
		if p.Corrupt {
			corrupt = append(corrupt, pictureHash(p))
		} else {
			clean = append(clean, pictureHash(p))
		}
		p.Release()
	}

	vcl := 0
	for _, nal := range SplitAnnexB(data) {
		if nal.Type.IsVCL() {
			vcl++
			if vcl == 6 {
				continue
			}
		}
		out, err := d.DecodeNAL(nal)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		for _, p := range out {
			collect(p)
		}
	}
	for _, p := range d.Flush() {
		collect(p)
	}

	known := make(map[string]bool, len(want))
	for _, h := range want {
		known[h] = true
	}
	for _, h := range clean {
		if !known[h] {
			t.Fatal("emitted a clean picture that is not in the pristine stream")
		}
	}
	if len(corrupt) == 0 {
		t.Fatal("missing segment did not produce a corrupt picture")
	}
	if len(clean) >= len(want) {
		t.Fatalf("missing segment did not drop a clean picture: %d of %d", len(clean), len(want))
	}
}
