package h264

import "fmt"

type BitReader struct {
	data    []byte
	bytePos int
	bitPos  uint
}

func NewBitReader(data []byte) *BitReader {
	return &BitReader{data: data}
}

func (r *BitReader) BitsRead() int {
	return r.bytePos*8 + int(r.bitPos)
}

func (r *BitReader) ReadBit() (uint8, error) {
	if r.bytePos >= len(r.data) {
		return 0, fmt.Errorf("bitreader: end of data at byte %d", r.bytePos)
	}
	bit := (r.data[r.bytePos] >> (7 - r.bitPos)) & 1
	r.bitPos++
	if r.bitPos == 8 {
		r.bitPos = 0
		r.bytePos++
	}
	return bit, nil
}

func (r *BitReader) ReadBits(n int) (uint32, error) {
	if n == 0 {
		return 0, nil
	}
	if n > 32 || n < 0 {
		return 0, fmt.Errorf("bitreader: invalid n=%d", n)
	}
	var val uint32
	for range n {
		bit, err := r.ReadBit()
		if err != nil {
			return 0, err
		}
		val = (val << 1) | uint32(bit)
	}
	return val, nil
}

func (r *BitReader) ReadFlag() (bool, error) {
	bit, err := r.ReadBit()
	return bit == 1, err
}

func (r *BitReader) PeekBits(n int) (uint32, error) {
	savedBytePos := r.bytePos
	savedBitPos := r.bitPos
	var val uint32
	bitsRead := 0
	for range n {
		bit, err := r.ReadBit()
		if err != nil {
			val <<= uint(n - bitsRead)
			break
		}
		val = (val << 1) | uint32(bit)
		bitsRead++
	}
	r.bytePos = savedBytePos
	r.bitPos = savedBitPos
	return val, nil
}

func (r *BitReader) SkipBits(n int) {
	totalBit := int(r.bitPos) + n
	r.bytePos += totalBit / 8
	r.bitPos = uint(totalBit % 8)
}

func (r *BitReader) ReadUE() (uint32, error) {
	leadingZeros := 0
	for {
		bit, err := r.ReadBit()
		if err != nil {
			return 0, fmt.Errorf("ue(v): %w", err)
		}
		if bit == 1 {
			break
		}
		leadingZeros++
	}
	if leadingZeros == 0 {
		return 0, nil
	}
	suffix, err := r.ReadBits(leadingZeros)
	if err != nil {
		return 0, fmt.Errorf("ue(v) suffix: %w", err)
	}
	return (1 << uint(leadingZeros)) - 1 + suffix, nil
}

func (r *BitReader) ReadSE() (int32, error) {
	ue, err := r.ReadUE()
	if err != nil {
		return 0, err
	}
	if ue%2 == 0 {
		return -int32(ue / 2), nil
	}
	return int32((ue + 1) / 2), nil
}

func (r *BitReader) AlignToByte() {
	if r.bitPos > 0 {
		r.bitPos = 0
		r.bytePos++
	}
}

type SliceHeaderParams struct {
	FrameMbsOnly                          bool
	Log2MaxFrameNumMinus4                 uint
	PicOrderCntType                       uint
	Log2MaxPicOrderCntLsbMinus4           uint
	BottomFieldPicOrderInFramePresentFlag bool
	DeblockingFilterControlPresent        bool
	RedundantPicCntPresentFlag            bool
	NumSliceGroupsMinus1                  uint
}

func (r *BitReader) SkipSliceHeaderIDR(p SliceHeaderParams) error {
	_, err := r.ReadBits(8)
	if err != nil {
		return fmt.Errorf("nal header: %w", err)
	}

	_, err = r.ReadUE()
	if err != nil {
		return fmt.Errorf("first_mb_in_slice: %w", err)
	}

	_, err = r.ReadUE()
	if err != nil {
		return fmt.Errorf("slice_type: %w", err)
	}

	_, err = r.ReadUE()
	if err != nil {
		return fmt.Errorf("pps_id: %w", err)
	}

	_, err = r.ReadBits(int(p.Log2MaxFrameNumMinus4 + 4))
	if err != nil {
		return fmt.Errorf("frame_num: %w", err)
	}

	fieldPicFlag := false
	if !p.FrameMbsOnly {
		fieldPicFlag, err = r.ReadFlag()
		if err != nil {
			return fmt.Errorf("field_pic_flag: %w", err)
		}
		if fieldPicFlag {
			_, err = r.ReadBit()
			if err != nil {
				return fmt.Errorf("bottom_field_flag: %w", err)
			}
		}
	}

	_, err = r.ReadUE()
	if err != nil {
		return fmt.Errorf("idr_pic_id: %w", err)
	}

	if p.PicOrderCntType == 0 {
		_, err = r.ReadBits(int(p.Log2MaxPicOrderCntLsbMinus4 + 4))
		if err != nil {
			return fmt.Errorf("pic_order_cnt_lsb: %w", err)
		}
		if p.BottomFieldPicOrderInFramePresentFlag && !fieldPicFlag {
			_, err = r.ReadSE()
			if err != nil {
				return fmt.Errorf("delta_pic_order_cnt_bottom: %w", err)
			}
		}
	}

	if p.RedundantPicCntPresentFlag {
		_, err = r.ReadUE()
		if err != nil {
			return fmt.Errorf("redundant_pic_cnt: %w", err)
		}
	}

	_, err = r.ReadBit()
	if err != nil {
		return fmt.Errorf("no_output_of_prior_pics_flag: %w", err)
	}
	_, err = r.ReadBit()
	if err != nil {
		return fmt.Errorf("long_term_reference_flag: %w", err)
	}

	_, err = r.ReadSE()
	if err != nil {
		return fmt.Errorf("slice_qp_delta: %w", err)
	}

	if p.DeblockingFilterControlPresent {
		disableDeblockingFilterIdc, err := r.ReadUE()
		if err != nil {
			return fmt.Errorf("disable_deblocking_filter_idc: %w", err)
		}
		if disableDeblockingFilterIdc != 1 {
			_, err = r.ReadSE()
			if err != nil {
				return fmt.Errorf("slice_alpha: %w", err)
			}
			_, err = r.ReadSE()
			if err != nil {
				return fmt.Errorf("slice_beta: %w", err)
			}
		}
	}

	return nil
}

func (r *BitReader) SkipSliceHeaderP(p SliceHeaderParams, nalRefIdc uint8) error {
	_, err := r.ReadBits(8)
	if err != nil {
		return fmt.Errorf("nal header: %w", err)
	}

	_, err = r.ReadUE()
	if err != nil {
		return fmt.Errorf("first_mb_in_slice: %w", err)
	}

	_, err = r.ReadUE()
	if err != nil {
		return fmt.Errorf("slice_type: %w", err)
	}

	_, err = r.ReadUE()
	if err != nil {
		return fmt.Errorf("pps_id: %w", err)
	}

	_, err = r.ReadBits(int(p.Log2MaxFrameNumMinus4 + 4))
	if err != nil {
		return fmt.Errorf("frame_num: %w", err)
	}

	fieldPicFlag := false
	if !p.FrameMbsOnly {
		fieldPicFlag, err = r.ReadFlag()
		if err != nil {
			return fmt.Errorf("field_pic_flag: %w", err)
		}
		if fieldPicFlag {
			_, err = r.ReadBit()
			if err != nil {
				return fmt.Errorf("bottom_field_flag: %w", err)
			}
		}
	}

	if p.PicOrderCntType == 0 {
		_, err = r.ReadBits(int(p.Log2MaxPicOrderCntLsbMinus4 + 4))
		if err != nil {
			return fmt.Errorf("pic_order_cnt_lsb: %w", err)
		}
		if p.BottomFieldPicOrderInFramePresentFlag && !fieldPicFlag {
			_, err = r.ReadSE()
			if err != nil {
				return fmt.Errorf("delta_pic_order_cnt_bottom: %w", err)
			}
		}
	}

	if p.RedundantPicCntPresentFlag {
		_, err = r.ReadUE()
		if err != nil {
			return fmt.Errorf("redundant_pic_cnt: %w", err)
		}
	}

	overrideFlag, err := r.ReadFlag()
	if err != nil {
		return fmt.Errorf("num_ref_idx_active_override_flag: %w", err)
	}
	if overrideFlag {
		_, err = r.ReadUE()
		if err != nil {
			return fmt.Errorf("num_ref_idx_l0_active_minus1: %w", err)
		}
	}

	rplmFlag, err := r.ReadFlag()
	if err != nil {
		return fmt.Errorf("ref_pic_list_modification_flag_l0: %w", err)
	}
	if rplmFlag {
		for {
			op, err := r.ReadUE()
			if err != nil {
				return fmt.Errorf("modification_of_pic_nums_idc: %w", err)
			}
			if op == 3 {
				break
			}
			_, err = r.ReadUE()
			if err != nil {
				return fmt.Errorf("rplm operand: %w", err)
			}
		}
	}

	if nalRefIdc != 0 {
		adaptiveFlag, err := r.ReadFlag()
		if err != nil {
			return fmt.Errorf("adaptive_ref_pic_marking_mode_flag: %w", err)
		}
		if adaptiveFlag {
			for {
				op, err := r.ReadUE()
				if err != nil {
					return fmt.Errorf("mmco op: %w", err)
				}
				if op == 0 {
					break
				}
				_, err = r.ReadUE()
				if err != nil {
					return fmt.Errorf("mmco operand: %w", err)
				}
				if op == 3 {
					_, err = r.ReadUE()
					if err != nil {
						return fmt.Errorf("mmco op3 operand: %w", err)
					}
				}
			}
		}
	}

	_, err = r.ReadSE()
	if err != nil {
		return fmt.Errorf("slice_qp_delta: %w", err)
	}

	if p.DeblockingFilterControlPresent {
		disableDeblockingFilterIdc, err := r.ReadUE()
		if err != nil {
			return fmt.Errorf("disable_deblocking_filter_idc: %w", err)
		}
		if disableDeblockingFilterIdc != 1 {
			_, err = r.ReadSE()
			if err != nil {
				return fmt.Errorf("slice_alpha: %w", err)
			}
			_, err = r.ReadSE()
			if err != nil {
				return fmt.Errorf("slice_beta: %w", err)
			}
		}
	}

	return nil
}
