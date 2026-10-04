package h264

import (
	"encoding/binary"
	"math/bits"
	"unsafe"
)

func ExtractNalusFromByteStream(data []byte) [][]byte {
	currNaluStart := -1
	n := len(data)
	var nalus [][]byte
	for i := 0; i < n-3; i++ {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			if currNaluStart > 0 {
				currNaluEnd := i
				for j := i - 1; j > currNaluStart; j-- {
					if data[j] == 0 {
						currNaluEnd = j
					} else {
						break
					}
				}
				nalus = append(nalus, extractSlice(data, currNaluStart, currNaluEnd))
			}
			currNaluStart = i + 3
		}
	}
	if currNaluStart < 0 {
		return nil
	}
	nalus = append(nalus, extractSlice(data, currNaluStart, n))
	return nalus
}

func extractSlice(data []byte, start, stop int) []byte {
	sl := make([]byte, stop-start)
	_ = copy(sl, data[start:stop])
	return sl
}

type scNalu struct {
	startCodeLength int
	startPos        int
}

func ConvertByteStreamToNaluSample(stream []byte) []byte {
	streamLen := len(stream)
	scNalus, minStartCodeLength := getStartCodePositions(stream)

	lengthField := make([]byte, 4)
	var naluLength int
	if minStartCodeLength == 4 {
		for i, s := range scNalus {

			if i+1 < len(scNalus) {
				naluLength = scNalus[i+1].startPos - s.startPos - 4
			} else {
				naluLength = len(stream) - scNalus[i].startPos
			}
			binary.BigEndian.PutUint32(lengthField, uint32(naluLength))
			copy(stream[s.startPos-4:s.startPos], lengthField)
		}
		return stream
	}
	out := make([]byte, 0, streamLen+len(scNalus))
	for i, s := range scNalus {
		if i+1 < len(scNalus) {
			naluLength = scNalus[i+1].startPos - s.startPos - scNalus[i+1].startCodeLength
		} else {
			naluLength = len(stream) - scNalus[i].startPos
		}
		binary.BigEndian.PutUint32(lengthField, uint32(naluLength))
		out = append(out, lengthField...)
		out = append(out, stream[s.startPos:s.startPos+naluLength]...)
	}
	return out
}

const (
	magicLeft  uint = 0x0101010101010101 >> (64 - bits.UintSize)
	magicRight uint = 0x8080808080808080 >> (64 - bits.UintSize)
)

func hasZeroByte(x uint) bool {
	return ((x - magicLeft) & (^x) & magicRight) != 0
}

func getStartCodePositions(stream []byte) (scNalus []scNalu, minStartCodeLength int) {
	streamLen := len(stream)
	uintSize := int(unsafe.Sizeof(uint(0)))
	streamLenLim := streamLen - (streamLen % uintSize) - uintSize
	minStartCodeLength = 4

	i := 0
	for ; i < streamLenLim; i += uintSize {

		if hasZeroByte(*(*uint)(unsafe.Pointer(&stream[i]))) {
			for j := i + 1; j < i+uintSize; j += 2 {
				if stream[j] == 0 {
					startPos, startCodeLength := 0, 3
					if stream[j-1] == 0 && stream[j+1] == 1 {
						if j-2 >= 0 && stream[j-2] == 0 {
							startCodeLength++
						}
						startPos = j + 2
					} else if stream[j+1] == 0 && stream[j+2] == 1 {
						if j-1 >= 0 && stream[j-1] == 0 {
							startCodeLength++
						}
						startPos = j + 3
					}
					if startPos != 0 {
						if startCodeLength < minStartCodeLength {
							minStartCodeLength = startCodeLength
						}
						scNalus = append(scNalus, scNalu{startCodeLength, startPos})
					}
				}
			}
		}
	}
	for ; i < streamLen-3; i++ {
		if stream[i] == 0 && stream[i+1] == 0 && stream[i+2] == 1 {
			startCodeLength := 3
			startPos := i + 3
			if i-1 >= 0 && stream[i-1] == 0 {
				startCodeLength++
			}
			if startCodeLength < minStartCodeLength {
				minStartCodeLength = startCodeLength
			}
			scNalus = append(scNalus, scNalu{startCodeLength, startPos})
		}
	}
	return
}

func ConvertSampleToByteStream(sample []byte) []byte {
	sampleLength := uint32(len(sample))
	var pos uint32 = 0
	for pos < sampleLength {
		naluLength := binary.BigEndian.Uint32(sample[pos : pos+4])
		startCode := []byte{0, 0, 0, 1}
		copy(sample[pos:pos+4], startCode)
		pos += naluLength + 4
	}
	return sample
}

func GetParameterSetsFromByteStream(data []byte) (spss, ppss [][]byte) {
	n := len(data)
	currNaluStart := -1
	totSize := 0
	for i := 0; i < n-4; i++ {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			if currNaluStart > 0 {
				currNaluEnd := i
				for j := i - 1; j > currNaluStart; j-- {
					if data[j] == 0 {
						currNaluEnd = j
					} else {
						break
					}
				}
				naluType := GetNaluType(data[currNaluStart])
				switch naluType {
				case NALU_SPS:
					spss = append(spss, data[currNaluStart:currNaluEnd])
					totSize += currNaluEnd - currNaluStart
				case NALU_PPS:
					ppss = append(ppss, data[currNaluStart:currNaluEnd])
					totSize += currNaluEnd - currNaluStart
				}
			}
			currNaluStart = i + 3
			nextNaluType := GetNaluType(data[currNaluStart])
			if nextNaluType < 6 {
				break
			}
		}
	}
	psData := make([]byte, totSize)
	pos := 0
	for i := range spss {
		copy(psData[pos:], spss[i])
		spss[i] = psData[pos : pos+len(spss[i])]
		pos += len(spss[i])
	}
	for i := range ppss {
		copy(psData[pos:], ppss[i])
		ppss[i] = psData[pos : pos+len(ppss[i])]
		pos += len(ppss[i])
	}
	return spss, ppss
}

func ExtractNalusOfTypeFromByteStream(nType NaluType, data []byte, stopAtVideo bool) [][]byte {
	currNaluStart := -1
	n := len(data)
	var nalus [][]byte
	for i := 0; i < n-3; i++ {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			if currNaluStart > 0 {
				currNaluEnd := i
				for j := i - 1; j > currNaluStart; j-- {
					if data[j] == 0 {
						currNaluEnd = j
					} else {
						break
					}
				}
				naluType := GetNaluType(data[currNaluStart])
				if naluType == nType {
					nalus = append(nalus, extractSlice(data, currNaluStart, currNaluEnd))
				}
			}
			currNaluStart = i + 3
			if currNaluStart < n-1 {
				nextNaluType := GetNaluType(data[currNaluStart])
				if stopAtVideo && nextNaluType < 6 {
					return nalus
				}
			}
		}
	}
	if currNaluStart < 0 {
		return nil
	}
	if GetNaluType(data[currNaluStart]) == nType {
		nalus = append(nalus, extractSlice(data, currNaluStart, n))
	}
	return nalus
}

func GetFirstAVCVideoNALUFromByteStream(data []byte) []byte {
	currNaluStart := -1
	n := len(data)
	naluStart, naluEnd := 0, 0
	for i := 0; i < n-3; i++ {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			if currNaluStart > 0 {
				currNaluEnd := i
				for j := i - 1; j > currNaluStart; j-- {
					if data[j] == 0 {
						currNaluEnd = j
					} else {
						break
					}
				}
				naluType := GetNaluType(data[currNaluStart])
				if IsVideoNaluType(naluType) {
					naluStart = currNaluStart
					naluEnd = currNaluEnd
					break
				}
			}
			currNaluStart = i + 3
		}
	}
	if currNaluStart > 0 && naluStart == 0 {
		naluType := GetNaluType(data[currNaluStart])
		if IsVideoNaluType(naluType) {
			naluStart = currNaluStart
			naluEnd = n
		}
	}
	if naluStart == 0 {
		return nil
	}
	return data[naluStart:naluEnd]
}
