package h264

var LevelScale4x4 = [6][3]int32{
	{10, 13, 16},
	{11, 14, 18},
	{13, 16, 20},
	{14, 18, 23},
	{16, 20, 25},
	{18, 23, 29},
}

var LevelScale8x8 = [6][6]int32{
	{20, 18, 32, 19, 25, 24},
	{22, 19, 35, 21, 28, 26},
	{26, 23, 42, 24, 33, 31},
	{28, 25, 45, 26, 35, 33},
	{32, 28, 51, 30, 40, 38},
	{36, 32, 58, 34, 46, 43},
}

var DefaultScalingList4x4 = [16]int32{
	16, 16, 16, 16,
	16, 16, 16, 16,
	16, 16, 16, 16,
	16, 16, 16, 16,
}

var DefaultScalingList8x8 = [64]int32{
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
	16, 16, 16, 16, 16, 16, 16, 16,
}

var SpecDefaultScalingList8x8Intra = [64]int32{
	6, 10, 13, 16, 18, 23, 25, 27,
	10, 11, 16, 18, 23, 25, 27, 29,
	13, 16, 18, 23, 25, 27, 29, 31,
	16, 18, 23, 25, 27, 29, 31, 33,
	18, 23, 25, 27, 29, 31, 33, 36,
	23, 25, 27, 29, 31, 33, 36, 38,
	25, 27, 29, 31, 33, 36, 38, 40,
	27, 29, 31, 33, 36, 38, 40, 42,
}

func Dequant4x4(coeffs [16]int32, qp int, scalingList *[16]int32) [16]int32 {
	var result [16]int32

	qpPer := qp / 6
	qpRem := qp % 6

	sl := &DefaultScalingList4x4
	if scalingList != nil {
		sl = scalingList
	}

	for i := range 16 {
		if coeffs[i] == 0 {
			continue
		}
		row := i / 4
		col := i % 4
		v := levelScaleIdx(row, col)

		if qpPer >= 4 {
			result[i] = coeffs[i] * LevelScale4x4[qpRem][v] * int32(sl[i]) << uint(qpPer-4)
		} else {
			result[i] = (coeffs[i]*LevelScale4x4[qpRem][v]*int32(sl[i]) + (1 << uint(3-qpPer))) >> uint(4-qpPer)
		}
	}

	return result
}

func levelScaleIdx(row, col int) int {
	r := row % 2
	c := col % 2
	if r == 0 && c == 0 {
		return 0
	}
	if r == 1 && c == 1 {
		return 2
	}
	return 1
}

func Dequant8x8(coeffs [64]int32, qp int, scalingList *[64]int32) [64]int32 {
	var result [64]int32

	qpPer := qp / 6
	qpRem := qp % 6

	sl := &DefaultScalingList8x8
	if scalingList != nil {
		sl = scalingList
	}

	for i := range 64 {
		if coeffs[i] == 0 {
			continue
		}
		row := i / 8
		col := i % 8
		v := levelScale8x8Idx(row, col)

		if qpPer >= 6 {
			result[i] = coeffs[i] * LevelScale8x8[qpRem][v] * int32(sl[i]) << uint(qpPer-6)
		} else {
			result[i] = (coeffs[i]*LevelScale8x8[qpRem][v]*int32(sl[i]) + (1 << uint(5-qpPer))) >> uint(6-qpPer)
		}
	}

	return result
}

var normAdjust8x8 = [4][4]int{
	{0, 3, 4, 3},
	{3, 1, 5, 1},
	{4, 5, 2, 5},
	{3, 1, 5, 1},
}

func levelScale8x8Idx(row, col int) int {
	return normAdjust8x8[row%4][col%4]
}

func DequantDC4x4(coeffs [16]int32, qp int, weightScaleDC int32) [16]int32 {
	var result [16]int32

	qpPer := qp / 6
	qpRem := qp % 6
	levelScale := LevelScale4x4[qpRem][0] * weightScaleDC

	if qpPer >= 6 {
		for i := range 16 {
			result[i] = coeffs[i] * levelScale << uint(qpPer-6)
		}
	} else {
		for i := range 16 {
			result[i] = (coeffs[i]*levelScale + (1 << uint(5-qpPer))) >> uint(6-qpPer)
		}
	}

	return result
}

func DequantChromaDC2x2(coeffs [4]int32, qpc int, weightScaleDC int32) [4]int32 {
	var result [4]int32

	qpPer := qpc / 6
	qpRem := qpc % 6
	levelScale := LevelScale4x4[qpRem][0] * weightScaleDC

	if qpPer >= 5 {
		for i := range 4 {
			result[i] = coeffs[i] * levelScale << uint(qpPer-5)
		}
	} else {
		for i := range 4 {
			result[i] = (coeffs[i] * levelScale) >> uint(5-qpPer)
		}
	}

	return result
}
