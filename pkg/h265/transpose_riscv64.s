//go:build riscv64 && riscv64.rva23u64 && !noasm

#include "textflag.h"

TEXT ·transposeRVV(SB), NOSPLIT, $0-24
	MOV dst+0(FP), X10
	MOV src+8(FP), X11
	MOV n+16(FP), X12

	SLLI $2, X12, X13

	MOV X12, X14
	MOV X10, X15

rows:
	MOV X12, X16
	MOV X11, X17
	MOV X15, X18

cols:
	VSETVLI X16, E32, M1, TA, MA, X19

	VLE32V  (X17), V1
	VSSE32V V1, X13, (X18)

	SLLI $2, X19, X20
	ADD  X20, X17
	MUL  X13, X19, X20
	ADD  X20, X18
	SUB  X19, X16
	BNEZ X16, cols

	ADD  X13, X11
	ADD  $4, X15
	ADD  $-1, X14
	BNEZ X14, rows

	RET
