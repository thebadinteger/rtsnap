package h265

import "testing"

func TestPoolEvictsOtherGeometries(t *testing.T) {
	pl := &picPool{}
	geoms := []picGeom{
		{strideY: 3840, height: 2160, strideC: 1920, heightC: 1080, colLen: 240 * 135},
		{strideY: 2560, height: 1440, strideC: 1280, heightC: 720, colLen: 160 * 90},
		{strideY: 1920, height: 1080, strideC: 960, heightC: 540, colLen: 120 * 68},
		{strideY: 1280, height: 720, strideC: 640, heightC: 360, colLen: 80 * 45},
		{strideY: 1024, height: 576, strideC: 512, heightC: 288, colLen: 64 * 36},
		{strideY: 800, height: 600, strideC: 400, heightC: 300, colLen: 50 * 38},
		{strideY: 4096, height: 2160, strideC: 2048, heightC: 1080, colLen: 256 * 135},
		{strideY: 3840, height: 1600, strideC: 1920, heightC: 800, colLen: 240 * 100},
	}

	for _, g := range geoms {
		for range picPoolDepth {
			pl.put(g, picBufs{
				y:  make([]uint8, g.strideY*g.height),
				cb: make([]uint8, g.strideC*g.heightC),
				cr: make([]uint8, g.strideC*g.heightC),
			})
		}
	}

	if pl.bytes > picPoolBudget {
		t.Fatalf("pool holds %d bytes, budget %d", pl.bytes, picPoolBudget)
	}

	total := 0
	for _, bufs := range pl.free {
		total += len(bufs)
	}
	if total == 0 || total >= picPoolDepth*len(geoms) {
		t.Fatalf("eviction did not happen: %d of %d buffers retained", total, picPoolDepth*len(geoms))
	}

	g := picGeom{strideY: 640, height: 480, strideC: 320, heightC: 240, colLen: 40 * 30}
	pl.put(g, picBufs{
		y:  make([]uint8, g.strideY*g.height),
		cb: make([]uint8, g.strideC*g.heightC),
		cr: make([]uint8, g.strideC*g.heightC),
	})
	if len(pl.free[g]) != 1 {
		t.Fatal("new geometry was not admitted")
	}
}
