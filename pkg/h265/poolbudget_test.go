package h265

import "testing"

func TestPoolBudget(t *testing.T) {
	pl := &picPool{}
	pool := pl

	for h := 240; h <= 2160; h += 120 {
		g := picGeom{strideY: h * 16 / 9, height: h, strideC: h * 8 / 9, heightC: h / 2,
			colLen: (h*16/9 + 15) / 16 * ((h + 15) / 16)}
		for range 8 {
			b := picBufs{
				y:  make([]uint8, g.strideY*g.height),
				cb: make([]uint8, g.strideC*g.heightC),
				cr: make([]uint8, g.strideC*g.heightC),
			}
			pool.put(g, b)
		}
	}

	if pool.bytes > picPoolBudget {
		t.Fatalf("pool holds %d bytes, budget %d", pool.bytes, picPoolBudget)
	}
	t.Logf("retained %d MB across many geometries", pool.bytes/1024/1024)
}
