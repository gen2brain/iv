//go:build riscv64 && !noasm

#include "textflag.h"

// func swizzleBGRARVV(p []byte) int
TEXT ·swizzleBGRARVV(SB), NOSPLIT, $0-32
	MOV	p_base+0(FP), A0
	MOV	p_len+8(FP), A1
	MOV	A0, A2             // save base
	SRLI	$2, A1, A3         // A3 = pixel count = len/4

loop:
	BEQZ	A3, done
	VSETVLI	A3, E8, M1, TA, MA, A4   // A4 = pixels this iteration
	VLSEG4E8V	(A0), V0           // V0,V1,V2,V3 = R,G,B,A planes
	VMVVV	V0, V4             // swap R and B planes
	VMVVV	V2, V0
	VMVVV	V4, V2
	VSSEG4E8V	V0, (A0)
	SUB	A4, A3, A3         // pixels -= vl
	SLLI	$2, A4, A5         // bytes = vl*4
	ADD	A5, A0, A0         // ptr += bytes
	JMP	loop

done:
	SUB	A2, A0, A0         // bytes processed = ptr - base
	MOV	A0, ret+24(FP)
	RET
