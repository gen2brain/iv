//go:build amd64 && !noasm

#include "textflag.h"

// func haveSSSE3() bool
TEXT ·haveSSSE3(SB), NOSPLIT, $0-1
	MOVQ $1, AX
	CPUID
	SHRQ $9, CX
	ANDQ $1, CX
	MOVB CX, ret+0(FP)
	RET

// func swizzleBGRASSSE3(p []byte) int
TEXT ·swizzleBGRASSSE3(SB), NOSPLIT, $0-32
	MOVQ p_base+0(FP), SI
	MOVQ p_len+8(FP), DI
	MOVQ SI, R8          // save base

	ANDQ $-16, DI        // whole 16-byte chunks

	// Shuffle control mask (low byte first):
	// 02 01 00 03  06 05 04 07  0a 09 08 0b  0e 0d 0c 0f
	MOVQ      $0x0704050603000102, AX
	MOVQ      AX, X0
	MOVQ      $0x0f0c0d0e0b08090a, AX
	MOVQ      AX, X1
	PUNPCKLQDQ X1, X0

	ADDQ SI, DI          // end pointer

loop:
	CMPQ SI, DI
	JEQ  done
	MOVOU  (SI), X1
	PSHUFB X0, X1
	MOVOU  X1, (SI)
	ADDQ $16, SI
	JMP  loop

done:
	SUBQ R8, SI
	MOVQ SI, ret+24(FP)
	RET
