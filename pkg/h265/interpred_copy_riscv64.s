//go:build riscv64 && riscv64.rva23u64 && !noasm

#include "textflag.h"

TEXT ·mcCopy8RVV(SB), NOSPLIT, $0-56
	MOV dst+0(FP), X10
	MOV dstStride+8(FP), X11
	MOV src+16(FP), X12
	MOV srcStride+24(FP), X13
	MOV w+32(FP), X14
	MOV h+40(FP), X15
	MOV shift+48(FP), X16

	SLLI $1, X11

rows:
	MOV X10, X17
	MOV X12, X18
	MOV X14, X19

cols:
	VSETVLI X19, E16, M1, TA, MA, X20

	VLE8V    (X18), V1
	VZEXTVF2 V1, V2
	VSLLVX   X16, V2, V2
	VSE16V   V2, (X17)

	SLLI $1, X20, X21
	ADD  X21, X17
	ADD  X20, X18
	SUB  X20, X19
	BNEZ X19, cols

	ADD  X11, X10
	ADD  X13, X12
	ADD  $-1, X15
	BNEZ X15, rows

	RET
