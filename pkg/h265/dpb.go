package h265

type pocState struct {
	prevLsb int32
	prevMsb int32
}

func derivePOC(st *pocState, nalType NALType, lsb int32, log2MaxLsb uint8, noRaslOutput bool) int32 {
	maxLsb := int32(1) << log2MaxLsb

	var msb int32

	switch {
	case nalType.IsIRAP() && noRaslOutput:
		msb = 0
	case lsb < st.prevLsb && st.prevLsb-lsb >= maxLsb/2:
		msb = st.prevMsb + maxLsb
	case lsb > st.prevLsb && lsb-st.prevLsb > maxLsb/2:
		msb = st.prevMsb - maxLsb
	default:
		msb = st.prevMsb
	}

	return msb + lsb
}

func isSubLayerNonRef(t NALType) bool {
	return t < 16 && t%2 == 0
}

func (st *pocState) update(nalType NALType, temporalID uint8, poc, lsb int32, log2MaxLsb uint8) {
	if temporalID != 0 ||
		nalType == NALRaslN || nalType == NALRaslR ||
		nalType == NALRadlN || nalType == NALRadlR ||
		isSubLayerNonRef(nalType) {
		return
	}

	st.prevLsb = lsb
	st.prevMsb = poc - lsb
}

type refPicSet struct {
	stCurrBefore []int32
	stCurrAfter  []int32
	ltCurr       []int32
	stFoll       []int32
	ltFoll       []int32

	ltCurrLsbOnly []bool
	ltFollLsbOnly []bool
}

func deriveRefPicSet(poc int32, log2MaxLsb uint8, st *shortTermRPS, lt *longTermRPS,
	numLtSps int,
) refPicSet {
	var rps refPicSet

	for i, d := range st.deltaPocS0 {
		if st.usedS0[i] {
			rps.stCurrBefore = append(rps.stCurrBefore, poc+d)
		} else {
			rps.stFoll = append(rps.stFoll, poc+d)
		}
	}

	for i, d := range st.deltaPocS1 {
		if st.usedS1[i] {
			rps.stCurrAfter = append(rps.stCurrAfter, poc+d)
		} else {
			rps.stFoll = append(rps.stFoll, poc+d)
		}
	}

	maxLsb := int32(1) << log2MaxLsb

	var cycle int32

	for i := range lt.pocLsbLt {
		if i == 0 || i == numLtSps {
			cycle = int32(lt.deltaPocMsbCycle[i])
		} else {
			cycle += int32(lt.deltaPocMsbCycle[i])
		}

		v := int32(lt.pocLsbLt[i])
		if lt.deltaPocMsbPresn[i] {
			v = poc - cycle*maxLsb - poc&(maxLsb-1) + v
		}

		if lt.usedByCurrPicLt[i] {
			rps.ltCurr = append(rps.ltCurr, v)
			rps.ltCurrLsbOnly = append(rps.ltCurrLsbOnly, !lt.deltaPocMsbPresn[i])
		} else {
			rps.ltFoll = append(rps.ltFoll, v)
			rps.ltFollLsbOnly = append(rps.ltFollLsbOnly, !lt.deltaPocMsbPresn[i])
		}
	}

	return rps
}

func (r *refPicSet) numPocTotalCurr() int {
	return len(r.stCurrBefore) + len(r.stCurrAfter) + len(r.ltCurr)
}

func buildRefPicList(rps *refPicSet, numActive int, modify bool, entries []uint32, l1 bool) []int32 {
	total := rps.numPocTotalCurr()
	if total == 0 {
		return nil
	}

	n := max(numActive, total)

	first, second := rps.stCurrBefore, rps.stCurrAfter
	if l1 {
		first, second = rps.stCurrAfter, rps.stCurrBefore
	}

	temp := make([]int32, 0, n)
	for len(temp) < n {
		for _, p := range first {
			if len(temp) == n {
				break
			}

			temp = append(temp, p)
		}

		for _, p := range second {
			if len(temp) == n {
				break
			}

			temp = append(temp, p)
		}

		for _, p := range rps.ltCurr {
			if len(temp) == n {
				break
			}

			temp = append(temp, p)
		}
	}

	list := make([]int32, numActive)

	for i := range list {
		if modify && i < len(entries) {
			if int(entries[i]) >= len(temp) {
				return nil
			}

			list[i] = temp[entries[i]]

			continue
		}

		list[i] = temp[i]
	}

	return list
}

type dpbPicture struct {
	pic      *Picture
	ref      bool
	longTerm bool
	output   bool
	latency  int
}

func (d *Decoder) dpbFind(poc int32) *Picture {
	for i := range d.dpb {
		if int32(d.dpb[i].pic.POC) == poc {
			return d.dpb[i].pic
		}
	}

	return nil
}

func (d *Decoder) dpbMark(rps *refPicSet) {
	keep := make(map[int32]bool, rps.numPocTotalCurr())
	long := make(map[int32]bool, len(rps.ltCurr)+len(rps.ltFoll))

	for _, l := range [][]int32{rps.stCurrBefore, rps.stCurrAfter, rps.stFoll} {
		for _, p := range l {
			keep[p] = true
		}
	}

	for _, l := range [][]int32{rps.ltCurr, rps.ltFoll} {
		for _, p := range l {
			keep[p] = true
			long[p] = true
		}
	}

	for i := range d.dpb {
		poc := int32(d.dpb[i].pic.POC)
		d.dpb[i].ref = keep[poc]
		d.dpb[i].longTerm = long[poc]
	}
}

func (d *Decoder) dpbLongTerm(poc int32) bool {
	for i := range d.dpb {
		if int32(d.dpb[i].pic.POC) == poc {
			return d.dpb[i].longTerm
		}
	}

	return false
}

func (d *Decoder) dpbRemoveUnused() {
	kept := d.dpb[:0]

	for _, e := range d.dpb {
		if e.ref || e.output {
			kept = append(kept, e)

			continue
		}

		e.pic.release()
	}

	clear(d.dpb[len(kept):])
	d.dpb = kept
}

func (d *Decoder) dpbNeedOutput() int {
	n := 0

	for _, e := range d.dpb {
		if e.output {
			n++
		}
	}

	return n
}

func (d *Decoder) dpbBump() *Picture {
	best := -1

	for i := range d.dpb {
		if d.dpb[i].output && (best < 0 || d.dpb[i].pic.POC < d.dpb[best].pic.POC) {
			best = i
		}
	}

	if best < 0 {
		return nil
	}

	p := d.dpb[best].pic
	d.dpb[best].output = false

	p.acquire()

	d.dpbRemoveUnused()

	return p
}

func (d *Decoder) dpbFull(includeBuffering bool) bool {
	if d.dpbNeedOutput() > d.maxReorder {
		return true
	}

	if d.maxLatency != 0 {
		for _, e := range d.dpb {
			if e.output && e.latency >= d.maxReorder+d.maxLatency-1 {
				return true
			}
		}
	}

	return includeBuffering && len(d.dpb) >= d.maxDecPicBuf
}

func (d *Decoder) dpbDrain(includeBuffering bool) []*Picture {
	var out []*Picture

	for d.dpbFull(includeBuffering) {
		p := d.dpbBump()
		if p == nil {
			break
		}

		out = append(out, p)
	}

	return out
}

func (d *Decoder) dpbStore(p *Picture, output bool) {
	if output {
		for i := range d.dpb {
			if d.dpb[i].output {
				d.dpb[i].latency++
			}
		}
	}

	d.dpb = append(d.dpb, dpbPicture{pic: p, ref: true, output: output})
}

func (d *Decoder) resolveLongTerm(rps *refPicSet, log2MaxLsb uint8) {
	mask := int32(1)<<log2MaxLsb - 1

	taken := make(map[int32]bool)

	fix := func(pocs []int32, lsbOnly []bool) {
		for i := range pocs {
			if i >= len(lsbOnly) || !lsbOnly[i] {
				taken[pocs[i]] = true

				continue
			}

			for _, e := range d.dpb {
				poc := int32(e.pic.POC)
				if e.ref && !taken[poc] && poc&mask == pocs[i]&mask {
					pocs[i] = poc
					taken[poc] = true

					break
				}
			}
		}
	}

	fix(rps.ltCurr, rps.ltCurrLsbOnly)
	fix(rps.ltFoll, rps.ltFollLsbOnly)
}

func (d *Decoder) generateUnavailable(rps *refPicSet, s *sps) {
	grey := func(poc int32) {
		if d.dpbFind(poc) != nil {
			return
		}

		p := newPicture(&d.pool, s)
		p.POC = int(poc)

		for cIdx, plane := range [][]uint8{p.Y, p.Cb, p.Cr} {
			mid := uint8(1) << (p.depth(cIdx) - 1)

			for i := range plane {
				plane[i] = mid
			}
		}

		for cIdx, plane := range [][]uint16{p.Y16, p.Cb16, p.Cr16} {
			mid := uint16(1) << (p.depth(cIdx) - 1)

			for i := range plane {
				plane[i] = mid
			}
		}

		d.dpb = append(d.dpb, dpbPicture{pic: p, ref: true})
	}

	for _, l := range [][]int32{rps.stCurrBefore, rps.stCurrAfter, rps.ltCurr,
		rps.stFoll, rps.ltFoll} {
		for _, poc := range l {
			grey(poc)
		}
	}
}
