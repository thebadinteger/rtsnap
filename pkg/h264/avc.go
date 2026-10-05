package h264

import "fmt"

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
