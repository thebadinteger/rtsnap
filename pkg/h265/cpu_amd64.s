//go:build amd64 && !noasm

#include "textflag.h"

TEXT ·cpuidAVX2(SB), NOSPLIT, $0-1
	MOVL $0, AX
	CPUID
	CMPL AX, $7
	JL   no

	MOVL $1, AX
	MOVL $0, CX
	CPUID
	BTL  $27, CX
	JNC  no

	MOVL   $0, CX
	XGETBV
	ANDL   $6, AX
	CMPL   AX, $6
	JNE    no

	MOVL $7, AX
	MOVL $0, CX
	CPUID
	BTL  $5, BX
	JNC  no

	MOVB $1, ret+0(FP)
	RET

no:
	MOVB $0, ret+0(FP)
	RET

TEXT ·cpuidAVX512ICL(SB), NOSPLIT, $0-1
	MOVL $0, AX
	CPUID
	CMPL AX, $7
	JL   noicl

	MOVL $1, AX
	MOVL $0, CX
	CPUID
	ANDL $0x18000000, CX
	CMPL CX, $0x18000000
	JNE  noicl

	MOVL   $0, CX
	XGETBV
	MOVL   AX, DX
	ANDL   $6, AX
	CMPL   AX, $6
	JNE    noicl
	ANDL   $0xe0, DX
	CMPL   DX, $0xe0
	JNE    noicl

	MOVL $7, AX
	MOVL $0, CX
	CPUID
	ANDL $0xd0230000, BX
	CMPL BX, $0xd0230000
	JNE  noicl
	ANDL $0x00005f42, CX
	CMPL CX, $0x00005f42
	JNE  noicl

	MOVB $1, ret+0(FP)
	RET

noicl:
	MOVB $0, ret+0(FP)
	RET
