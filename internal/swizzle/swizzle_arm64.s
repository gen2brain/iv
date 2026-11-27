//go:build arm64 && !noasm

#include "textflag.h"

// func swizzleBGRA(dst, src []byte) int
TEXT ·swizzleBGRA(SB), NOSPLIT, $0-56
	MOVD dst_base+0(FP), R5
	MOVD src_base+24(FP), R0
	MOVD src_len+32(FP), R1
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
	VST4 [V0.B16, V1.B16, V2.B16, V3.B16], (R5)
	ADD  $64, R0
	ADD  $64, R5
	B    loop

done:
	SUB  R2, R0, R0
	MOVD R0, ret+48(FP)
	RET
