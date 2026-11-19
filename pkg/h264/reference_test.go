package h264

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// first picture of every stream must equal what ffmpeg decodes, cropped planes as i420
func TestDecodeMatchesReference(t *testing.T) {
	cases := []struct {
		name          string
		width, height int
		sum           string
	}{
		{"scaling_default_flag", 640, 480, "c82fbf37f66eab8b6bda30acbe3d374baa41dee71f1517224d6507b4a0f3ecd6"},
		{"scaling_fallback_b", 640, 480, "9f6a2b163308df88e83a5669062ae2dd6c1c150b70924fe1a16fd5a58f987103"},
		{"second_chroma_qp_cabac", 640, 480, "01d7b4b07f457e9458d68560690ba190146ea3f94d120a47aa9902397e706fab"},
		{"second_chroma_qp_cavlc", 640, 480, "f8200222472bc2b59dc45b9e3baeab013a5b6e645c32c8b774fddd3c957e816b"},
		{"monochrome", 640, 480, "55b8ca96dcc1f3b4d004cd0a9e4f6f1f8a60206702079f7a0a33ce50be1b9617"},
		{"crop_left_top", 648, 360, "82baca4238f609ee86621cd2c6865ede87e460be81101fb9a547850ebe22a1b3"},
		{"non_idr_i_cabac", 640, 480, "d280614afc022a7b4322dcda7036f5567a5db9a7acdba4a5aab6dcfa9baf3205"},
		{"non_idr_i_cavlc", 640, 480, "911d4ab0f4857d77f5f49fb00741e1dcb559c05d195f7b8869e532dd9a949a71"},
	}

	for _, tc := range cases {
		data, err := os.ReadFile("testdata/" + tc.name + ".264")
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}

		d := New()
		var f *Frame
		for _, nalu := range ExtractNalusFromByteStream(data) {
			if f, err = d.DecodeNALUs([][]byte{nalu}); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if f != nil {
				break
			}
		}
		if f == nil {
			t.Fatalf("%s: no frame", tc.name)
		}
		if f.Width != tc.width || f.Height != tc.height {
			t.Fatalf("%s: size %dx%d, want %dx%d", tc.name, f.Width, f.Height, tc.width, tc.height)
		}

		img := f.YCbCr()
		cw, ch := (f.Width+1)/2, (f.Height+1)/2
		h := sha256.New()
		for y := 0; y < f.Height; y++ {
			h.Write(img.Y[y*img.YStride : y*img.YStride+f.Width])
		}
		for y := 0; y < ch; y++ {
			h.Write(img.Cb[y*img.CStride : y*img.CStride+cw])
		}
		for y := 0; y < ch; y++ {
			h.Write(img.Cr[y*img.CStride : y*img.CStride+cw])
		}
		if got := hex.EncodeToString(h.Sum(nil)); got != tc.sum {
			t.Errorf("%s: picture differs from the reference decode", tc.name)
		}
	}
}
