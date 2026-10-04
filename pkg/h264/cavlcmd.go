package h264

import "fmt"

func DecodeCoeffToken(br *BitReader, nC int) (totalCoeff, trailingOnes int, err error) {
	if nC == -1 {
		return decodeChromaDCCoeffToken(br)
	}

	tableIdx := coeffTokenTableIdx(nC)
	maxLen := coeffTokenMaxLen[tableIdx]

	peeked, err := br.PeekBits(maxLen)
	if err != nil {
		return 0, 0, fmt.Errorf("coeff_token peek: %w", err)
	}

	for tc := 0; tc <= 16; tc++ {
		maxT1 := min(tc, 3)
		for t1 := 0; t1 <= maxT1; t1++ {
			idx := 4*tc + t1
			codeLen := int(coeffTokenLen[tableIdx][idx])
			if codeLen == 0 {
				continue
			}
			codeBits := uint32(coeffTokenBits[tableIdx][idx])
			shifted := peeked >> uint(maxLen-codeLen)
			if shifted == codeBits {
				br.SkipBits(codeLen)
				return tc, t1, nil
			}
		}
	}

	return 0, 0, fmt.Errorf("no matching coeff_token (nC=%d, peeked=0x%x)", nC, peeked)
}

func decodeChromaDCCoeffToken(br *BitReader) (totalCoeff, trailingOnes int, err error) {
	const maxLen = 8

	peeked, err := br.PeekBits(maxLen)
	if err != nil {
		return 0, 0, fmt.Errorf("chroma DC coeff_token peek: %w", err)
	}

	for tc := 0; tc <= 4; tc++ {
		maxT1 := min(tc, 3)
		for t1 := 0; t1 <= maxT1; t1++ {
			idx := 4*tc + t1
			codeLen := int(chromaDCCoeffTokenLen[idx])
			if codeLen == 0 {
				continue
			}
			codeBits := uint32(chromaDCCoeffTokenBits[idx])
			shifted := peeked >> uint(maxLen-codeLen)
			if shifted == codeBits {
				br.SkipBits(codeLen)
				return tc, t1, nil
			}
		}
	}

	return 0, 0, fmt.Errorf("no matching chroma DC coeff_token (peeked=0x%x)", peeked)
}

func DecodeLevelPrefix(br *BitReader) (int, error) {
	zeros := 0
	for {
		bit, err := br.ReadBit()
		if err != nil {
			return 0, fmt.Errorf("level_prefix: %w", err)
		}
		if bit == 1 {
			return zeros, nil
		}
		zeros++
		if zeros > 25 {
			return 0, fmt.Errorf("level_prefix too long (%d)", zeros)
		}
	}
}

func DecodeTotalZeros(br *BitReader, totalCoeff, maxNumCoeff int) (int, error) {
	if maxNumCoeff == 4 {
		return decodeChromaDCTotalZeros(br, totalCoeff)
	}
	return decodeTotalZeros4x4(br, totalCoeff, maxNumCoeff)
}

func decodeTotalZeros4x4(br *BitReader, totalCoeff, maxNumCoeff int) (int, error) {
	tableRow := totalCoeff - 1
	if tableRow < 0 || tableRow >= 15 {
		return 0, fmt.Errorf("total_zeros: invalid totalCoeff=%d", totalCoeff)
	}

	maxLen := 0
	maxZeros := maxNumCoeff - totalCoeff
	for tz := 0; tz <= maxZeros && tz < 16; tz++ {
		if int(totalZerosLen[tableRow][tz]) > maxLen {
			maxLen = int(totalZerosLen[tableRow][tz])
		}
	}

	peeked, err := br.PeekBits(maxLen)
	if err != nil {
		return 0, fmt.Errorf("total_zeros peek: %w", err)
	}

	for tz := 0; tz <= maxZeros && tz < 16; tz++ {
		codeLen := int(totalZerosLen[tableRow][tz])
		if codeLen == 0 {
			continue
		}
		codeBits := uint32(totalZerosBits[tableRow][tz])
		shifted := peeked >> uint(maxLen-codeLen)
		if shifted == codeBits {
			br.SkipBits(codeLen)
			return tz, nil
		}
	}

	return 0, fmt.Errorf("no matching total_zeros (totalCoeff=%d, peeked=0x%x)", totalCoeff, peeked)
}

func decodeChromaDCTotalZeros(br *BitReader, totalCoeff int) (int, error) {
	tableRow := totalCoeff - 1
	if tableRow < 0 || tableRow >= 3 {
		return 0, fmt.Errorf("chroma DC total_zeros: invalid totalCoeff=%d", totalCoeff)
	}

	maxLen := 0
	maxZeros := 4 - totalCoeff
	for tz := 0; tz <= maxZeros && tz < 4; tz++ {
		if int(chromaDCTotalZerosLen[tableRow][tz]) > maxLen {
			maxLen = int(chromaDCTotalZerosLen[tableRow][tz])
		}
	}

	peeked, err := br.PeekBits(maxLen)
	if err != nil {
		return 0, fmt.Errorf("chroma DC total_zeros peek: %w", err)
	}

	for tz := 0; tz <= maxZeros && tz < 4; tz++ {
		codeLen := int(chromaDCTotalZerosLen[tableRow][tz])
		if codeLen == 0 {
			continue
		}
		codeBits := uint32(chromaDCTotalZerosBits[tableRow][tz])
		shifted := peeked >> uint(maxLen-codeLen)
		if shifted == codeBits {
			br.SkipBits(codeLen)
			return tz, nil
		}
	}

	return 0, fmt.Errorf("no matching chroma DC total_zeros (totalCoeff=%d, peeked=0x%x)", totalCoeff, peeked)
}

func DecodeRunBefore(br *BitReader, zerosLeft int) (int, error) {
	if zerosLeft <= 0 {
		return 0, nil
	}

	tableRow := min(zerosLeft-1, 6)

	maxLen := 0
	for rb := 0; rb <= zerosLeft && rb < 16; rb++ {
		if int(runBeforeLen[tableRow][rb]) > maxLen {
			maxLen = int(runBeforeLen[tableRow][rb])
		}
	}

	peeked, err := br.PeekBits(maxLen)
	if err != nil {
		return 0, fmt.Errorf("run_before peek: %w", err)
	}

	maxRB := zerosLeft
	if tableRow == 6 && maxRB > 14 {
		maxRB = 14
	} else if tableRow < 6 && maxRB > tableRow+1 {
		maxRB = tableRow + 1
	}

	for rb := 0; rb <= maxRB && rb < 16; rb++ {
		codeLen := int(runBeforeLen[tableRow][rb])
		if codeLen == 0 {
			continue
		}
		codeBits := uint32(runBeforeBits[tableRow][rb])
		shifted := peeked >> uint(maxLen-codeLen)
		if shifted == codeBits {
			br.SkipBits(codeLen)
			return rb, nil
		}
	}

	return 0, fmt.Errorf("no matching run_before (zerosLeft=%d, peeked=0x%x)", zerosLeft, peeked)
}

func DecodeResidualBlock(br *BitReader, nC int, maxNumCoeff int) ([]int32, int, error) {
	coeffs := make([]int32, maxNumCoeff)

	totalCoeff, trailingOnes, err := DecodeCoeffToken(br, nC)
	if err != nil {
		return coeffs, 0, fmt.Errorf("residual block: %w", err)
	}

	if totalCoeff == 0 {
		return coeffs, 0, nil
	}

	levels := make([]int32, totalCoeff)

	for i := range trailingOnes {
		sign, err := br.ReadBit()
		if err != nil {
			return coeffs, 0, fmt.Errorf("trailing_ones_sign: %w", err)
		}
		if sign == 0 {
			levels[i] = 1
		} else {
			levels[i] = -1
		}
	}

	suffixLength := 0
	if totalCoeff > 10 && trailingOnes < 3 {
		suffixLength = 1
	}

	for i := trailingOnes; i < totalCoeff; i++ {
		levelPrefix, err := DecodeLevelPrefix(br)
		if err != nil {
			return coeffs, 0, fmt.Errorf("level[%d]: %w", i, err)
		}

		var levelCode int
		levelSuffixSize := suffixLength
		if levelPrefix == 14 && suffixLength == 0 {
			levelSuffixSize = 4
		} else if levelPrefix >= 15 {
			levelSuffixSize = levelPrefix - 3
		}

		if levelSuffixSize > 0 {
			levelSuffix, err := br.ReadBits(levelSuffixSize)
			if err != nil {
				return coeffs, 0, fmt.Errorf("level_suffix[%d]: %w", i, err)
			}
			clampedPrefix := min(levelPrefix, 15)
			levelCode = (clampedPrefix << uint(suffixLength)) + int(levelSuffix)
		} else {
			levelCode = levelPrefix
		}

		if levelPrefix >= 15 && suffixLength == 0 {
			levelCode += 15
		}
		if levelPrefix >= 16 {
			levelCode += (1 << uint(levelPrefix-3)) - 4096
		}

		if i == trailingOnes && trailingOnes < 3 {
			levelCode += 2
		}

		if levelCode%2 == 0 {
			levels[i] = int32(levelCode/2 + 1)
		} else {
			levels[i] = int32(-(levelCode + 1) / 2)
		}

		if suffixLength == 0 {
			suffixLength = 1
		}
		absLevel := levels[i]
		if absLevel < 0 {
			absLevel = -absLevel
		}
		if int(absLevel) > (3<<uint(suffixLength-1)) && suffixLength < 6 {
			suffixLength++
		}
	}

	zerosLeft := 0
	if totalCoeff < maxNumCoeff {
		zerosLeft, err = DecodeTotalZeros(br, totalCoeff, maxNumCoeff)
		if err != nil {
			return coeffs, 0, fmt.Errorf("total_zeros: %w", err)
		}
	}

	coeffIdx := totalCoeff + zerosLeft - 1
	for i := 0; i < totalCoeff-1; i++ {
		runBefore := 0
		if zerosLeft > 0 {
			runBefore, err = DecodeRunBefore(br, zerosLeft)
			if err != nil {
				return coeffs, 0, fmt.Errorf("run_before[%d]: %w", i, err)
			}
		}
		if coeffIdx >= 0 && coeffIdx < maxNumCoeff {
			coeffs[coeffIdx] = levels[i]
		}
		coeffIdx -= 1 + runBefore
		zerosLeft -= runBefore
	}
	if coeffIdx >= 0 && coeffIdx < maxNumCoeff {
		coeffs[coeffIdx] = levels[totalCoeff-1]
	}

	return coeffs, totalCoeff, nil
}
