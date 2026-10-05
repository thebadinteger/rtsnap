package h265

import "bytes"

type NALType uint8

const (
	NALTrailN NALType = iota
	NALTrailR
	NALTsaN
	NALTsaR
	NALStsaN
	NALStsaR
	NALRadlN
	NALRadlR
	NALRaslN
	NALRaslR
)

const (
	NALBlaWLP NALType = iota + 16
	NALBlaWRadl
	NALBlaNLP
	NALIdrWRadl
	NALIdrNLP
	NALCra
)

const (
	NALVPS NALType = iota + 32
	NALSPS
	NALPPS
	NALAUD
	NALEOS
	NALEOB
	NALFD
	NALPrefixSEI
	NALSuffixSEI
)

func (t NALType) IsVCL() bool {
	return t < 32
}

func (t NALType) IsIDR() bool {
	return t == NALIdrWRadl || t == NALIdrNLP
}

func (t NALType) IsIRAP() bool {
	return t >= NALBlaWLP && t <= NALCra
}

type NALUnit struct {
	Type       NALType
	LayerID    uint8
	TemporalID uint8
	RBSP       []byte

	EPB []uint32
}

// split header and payload of nal unit
func ParseNAL(data []byte) (NALUnit, bool) {
	if len(data) < 2 {
		return NALUnit{}, false
	}

	b0, b1 := data[0], data[1]
	if b0&0x80 != 0 {
		return NALUnit{}, false
	}

	tidPlus1 := b1 & 0x07
	if tidPlus1 == 0 {
		return NALUnit{}, false
	}

	rbsp, epb := unescape(data[2:])

	return NALUnit{
		Type:       NALType(b0 >> 1 & 0x3f),
		LayerID:    b0&0x01<<5 | b1>>3,
		TemporalID: tidPlus1 - 1,
		RBSP:       rbsp,
		EPB:        epb,
	}, true
}

func SplitAnnexB(data []byte) []NALUnit {
	var nals []NALUnit

	i, _, ok := startCode(data, 0)
	if !ok {
		return nals
	}

	for i < len(data) {
		next, scStart, found := startCode(data, i)

		end := len(data)
		if found {
			end = scStart
			for end > i && data[end-1] == 0 {
				end--
			}
		}

		if nal, ok := ParseNAL(data[i:end]); ok {
			nals = append(nals, nal)
		}

		if !found {
			break
		}

		i = next
	}

	return nals
}

func startCode(data []byte, off int) (next, start int, ok bool) {
	for i := off; i+2 < len(data); i++ {
		if data[i] != 0 || data[i+1] != 0 {
			continue
		}

		if data[i+2] == 1 {
			return i + 3, i, true
		}

		if i+3 < len(data) && data[i+2] == 0 && data[i+3] == 1 {
			return i + 4, i, true
		}
	}

	return 0, 0, false
}

func unescape(data []byte) ([]byte, []uint32) {
	if !bytes.Contains(data, []byte{0, 0, 3}) {
		return data, nil
	}

	rbsp := make([]byte, 0, len(data))

	var epb []uint32

	for i := 0; i < len(data); {
		if i+2 < len(data) && data[i] == 0 && data[i+1] == 0 && data[i+2] == 3 {
			rbsp = append(rbsp, 0, 0)
			epb = append(epb, uint32(i+2))
			i += 3

			continue
		}

		rbsp = append(rbsp, data[i])
		i++
	}

	return rbsp, epb
}

func (n NALUnit) RBSPOffset(off int) int {
	rbsp := off

	for _, p := range n.EPB {
		if int(p) >= off {
			break
		}

		rbsp--
	}

	return rbsp
}

func (n NALUnit) NALOffset(off int) int {
	for _, p := range n.EPB {
		if int(p) <= off {
			off++

			continue
		}

		break
	}

	return off
}
