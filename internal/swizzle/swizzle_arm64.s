//go:build arm64 && !noasm

#include "textflag.h"

// func swizzleBGRA(p []byte) int
TEXT ·swizzleBGRA(SB), NOSPLIT, $0-32
	MOVD p_base+0(FP), R0
	MOVD p_len+8(FP), R1
	MOVD R0, R2          // save base

	LSR  $6, R1, R1      // whole 64-byte chunks
	LSL  $6, R1, R1
	ADD  R0, R1, R3      // end pointer

loop:
	CMP  R3, R0
	BHS  done
	VLD4 (R0), [V0.B16, V1.B16, V2.B16, V3.B16]
	VMOV V0.B16, V4.B16  // swap R and B channels
	VMOV V2.B16, V0.B16
	VMOV V4.B16, V2.B16
	VST4 [V0.B16, V1.B16, V2.B16, V3.B16], (R0)
	ADD  $64, R0
	B    loop

done:
	SUB  R2, R0, R0
	MOVD R0, ret+24(FP)
	RET
