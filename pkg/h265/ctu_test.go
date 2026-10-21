package h265

import (
	"os"
	"testing"
)

func TestCompleteMultiThread(t *testing.T) {
	data, err := os.ReadFile("../../tests/testdata/1080p.h265")
	if err != nil {
		t.Skip(err)
	}

	for _, threads := range []int{1, 2, 4} {
		var d Decoder
		d.Threads(threads)

		var pics []*Picture
		for _, nal := range SplitAnnexB(data) {
			p, err := d.DecodeNAL(nal)
			if err != nil {
				t.Fatalf("threads=%d: %v", threads, err)
			}
			pics = append(pics, p...)
		}
		pics = append(pics, d.Flush()...)

		if len(pics) == 0 {
			t.Fatalf("threads=%d: completeness gate dropped a valid picture", threads)
		}
		d.Close()
	}
}

func TestCompleteRejectsPartial(t *testing.T) {
	s := &sps{picWidthInCtbs: 2, picHeightInCtbs: 2}

	d := &ctuDecoder{s: s, nextCTU: 2}
	if d.complete() {
		t.Fatal("partial picture reported as complete")
	}

	d.nextCTU = 4
	if !d.complete() {
		t.Fatal("full picture reported as partial")
	}

	empty := &ctuDecoder{s: s}
	if empty.complete() {
		t.Fatal("empty picture reported as complete")
	}
}
