package h264

import (
	"encoding/binary"
	"fmt"
)

type NaluType uint16

const (
	NALU_NON_IDR = NaluType(1)
	NALU_IDR = NaluType(5)
	NALU_SEI = NaluType(6)
	NALU_SPS = NaluType(7)
	NALU_PPS = NaluType(8)
	NALU_AUD = NaluType(9)
	NALU_EO_SEQ = NaluType(10)
	NALU_EO_STREAM = NaluType(11)
	NALU_FILL = NaluType(12)
)

func (a NaluType) String() string {
	switch a {
	case NALU_NON_IDR:
		return "NonIDR_1"
	case NALU_IDR:
		return "IDR_5"
	case NALU_SEI:
		return "SEI_6"
	case NALU_SPS:
		return "SPS_7"
	case NALU_PPS:
		return "PPS_8"
	case NALU_AUD:
		return "AUD_9"
	case NALU_EO_SEQ:
		return "EndOfSequence_10"
	case NALU_EO_STREAM:
		return "EndOfStream_11"
	case NALU_FILL:
		return "FILL_12"
	default:
		return fmt.Sprintf("Other_%d", a)
	}
}

func GetNaluType(naluHeader byte) NaluType {
	return NaluType(naluHeader & 0x1f)
}

func FindNaluTypes(sample []byte) []NaluType {
	length := len(sample)
	if length < 4 {
		return nil
	}
	naluList := make([]NaluType, 0, 2)
	var pos uint32 = 0
	for pos < uint32(length-4) {
		naluLength := binary.BigEndian.Uint32(sample[pos : pos+4])
		pos += 4
		naluType := GetNaluType(sample[pos])
		naluList = append(naluList, naluType)
		pos += naluLength
	}
	return naluList
}

func FindNaluTypesUpToFirstVideoNALU(sample []byte) []NaluType {
	length := len(sample)
	if length < 4 {
		return nil
	}
	naluList := make([]NaluType, 0)
	var pos uint32 = 0
	for pos < uint32(length-4) {
		naluLength := binary.BigEndian.Uint32(sample[pos : pos+4])
		pos += 4
		naluType := GetNaluType(sample[pos])
		naluList = append(naluList, naluType)
		pos += naluLength
		if IsVideoNaluType(naluType) {
			break
		}
	}
	return naluList
}

func IsIDRSample(sample []byte) bool {
	return ContainsNaluType(sample, NALU_IDR)
}

func ContainsNaluType(sample []byte, specificNalType NaluType) bool {
	var pos uint32 = 0
	length := len(sample)
	for pos < uint32(length-4) {
		naluLength := binary.BigEndian.Uint32(sample[pos : pos+4])
		pos += 4
		naluType := GetNaluType(sample[pos])
		if naluType == specificNalType {
			return true
		}
		pos += naluLength
	}
	return false
}

func HasParameterSets(b []byte) bool {
	naluTypeList := FindNaluTypesUpToFirstVideoNALU(b)
	hasSPS := false
	hasPPS := false
	for _, naluType := range naluTypeList {
		if naluType == NALU_SPS {
			hasSPS = true
		}
		if naluType == NALU_PPS {
			hasPPS = true
		}
		if hasSPS && hasPPS {
			return true
		}
	}
	return false
}

func GetParameterSets(sample []byte) (sps [][]byte, pps [][]byte) {
	sampleLength := uint32(len(sample))
	var pos uint32 = 0
naluLoop:
	for pos < sampleLength {
		naluLength := binary.BigEndian.Uint32(sample[pos : pos+4])
		pos += 4
		naluHdr := sample[pos]
		switch naluType := GetNaluType(naluHdr); {
		case naluType == NALU_SPS:
			sps = append(sps, sample[pos:pos+naluLength])
		case naluType == NALU_PPS:
			pps = append(pps, sample[pos:pos+naluLength])
		case IsVideoNaluType(naluType):
			break naluLoop
		}
		pos += naluLength
	}
	return sps, pps
}

func IsVideoNaluType(naluType NaluType) bool {
	const highestVideoNaluType = 5
	return naluType <= highestVideoNaluType
}
