package h264

type Models [1024]CtxState

func clip3(min, max, val int) int {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

func InitModels(sliceQPY int, sliceType int, cabacInitIDC int) Models {
	var models Models

	var tab *[1024][2]int8
	if sliceType == 2 || sliceType == 7 {
		tab = &cabacContextInitI
	} else {
		tab = &cabacContextInitPB[cabacInitIDC]
	}

	qp := clip3(0, 51, sliceQPY)

	for i := range 1024 {
		m := int(tab[i][0])
		n := int(tab[i][1])

		preCtxState := clip3(1, 126, ((m*qp)>>4)+n)

		if preCtxState <= 63 {
			models[i].PStateIdx = uint8(63 - preCtxState)
			models[i].ValMPS = 0
		} else {
			models[i].PStateIdx = uint8(preCtxState - 64)
			models[i].ValMPS = 1
		}
	}

	return models
}
