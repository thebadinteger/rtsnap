package h264

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
