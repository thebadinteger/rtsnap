package h265

import (
	"testing"
	"unsafe"
)

func TestPictureBufSize(t *testing.T) {
	for _, c := range []struct {
		name string
		w, h int
		deep bool
	}{
		{"1080p8", 1920, 1080, false},
		{"4k8", 3840, 2160, false},
		{"4k16", 3840, 2160, true},
		{"8k16", 7680, 4320, true},
	} {
		var b picBufs
		if c.deep {
			b.y16 = make([]uint16, c.w*c.h)
			b.cb16 = make([]uint16, c.w/2*c.h/2)
			b.cr16 = make([]uint16, c.w/2*c.h/2)
		} else {
			b.y = make([]uint8, c.w*c.h)
			b.cb = make([]uint8, c.w/2*c.h/2)
			b.cr = make([]uint8, c.w/2*c.h/2)
		}
		b.col = make([]colMotion, (c.w+15)/16*((c.h+15)/16))

		want := 0
		for _, s := range [][]uint8{b.y, b.cb, b.cr} {
			want += cap(s)
		}
		for _, s := range [][]uint16{b.y16, b.cb16, b.cr16} {
			want += cap(s) * int(unsafe.Sizeof(uint16(0)))
		}
		want += cap(b.col) * int(unsafe.Sizeof(colMotion{}))

		if got := b.size(); got != want {
			t.Fatalf("%s: size %d want %d", c.name, got, want)
		}
		t.Logf("%s: %.1f MiB deep=%v", c.name, float64(want)/(1<<20), c.deep)
	}
}
