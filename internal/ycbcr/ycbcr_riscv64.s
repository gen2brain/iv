//go:build riscv64 && !noasm

#include "textflag.h"

#define SETUP \
	MOV dst+0(FP), X10;  \
	MOV y+8(FP), X11;    \
	MOV cb+16(FP), X12;  \
	MOV cr+24(FP), X13;  \
	MOV n+32(FP), X14;   \
	MOV X14, X29;        \
	MOV $-128, X16;      \
	MOV $91881, X17;     \
	MOV $-22554, X18;    \
	MOV $-46802, X19;    \
	MOV $116130, X20;    \
	MOV $65793, X21;     \
	MOV $255, X22;       \
	MOV $257, X23

#define CONVERT(RD, BD) \
	VMULVX    X21, V8, V8;       \
	VADDVX    X16, V12, V12;     \
	VADDVX    X16, V16, V16;     \
	VMVVV     V8, V20;           \
	VMACCVX   V16, X17, V20;     \
	VSRAVI    $16, V20, V20;     \
	VMAXVX    X0, V20, V20;      \
	VMINVX    X22, V20, V20;     \
	VMVVV     V8, V24;           \
	VMACCVX   V12, X18, V24;     \
	VMACCVX   V16, X19, V24;     \
	VSRAVI    $16, V24, V24;     \
	VMAXVX    X0, V24, V24;      \
	VMINVX    X22, V24, V24;     \
	VMVVV     V8, V28;           \
	VMACCVX   V12, X20, V28;     \
	VSRAVI    $16, V28, V28;     \
	VMAXVX    X0, V28, V28;      \
	VMINVX    X22, V28, V28;     \
	VSETVLI   X15, E16, M2, TA, MA, X6; \
	VNSRLWI   $0, V20, V4;       \
	VNSRLWI   $0, V24, V6;       \
	VNSRLWI   $0, V28, V16;      \
	VSETVLI   X15, E8, M1, TA, MA, X6; \
	VNSRLWI   $0, V4, RD;        \
	VNSRLWI   $0, V6, V1;        \
	VNSRLWI   $0, V16, BD;       \
	VMVVX     X22, V3;           \
	VSSEG4E8V V0, (X10)

#define NEXT \
	ADD  X15, X11, X11;  \
	SLLI $2, X15, X28;   \
	ADD  X28, X10, X10;  \
	SUB  X15, X14, X14

#define KERNEL1(RD, BD, loop, done) \
	SETUP;                                    \
	BEQZ X14, done;                           \
loop:                                         \
	VSETVLI  X14, E8, M1, TA, MA, X15;        \
	VLE8V    (X11), V0;                       \
	VLE8V    (X12), V1;                       \
	VLE8V    (X13), V2;                       \
	VSETVLI  X15, E32, M4, TA, MA, X6;        \
	VZEXTVF4 V0, V8;                          \
	VZEXTVF4 V1, V12;                         \
	VZEXTVF4 V2, V16;                         \
	CONVERT(RD, BD);                          \
	ADD      X15, X12, X12;                   \
	ADD      X15, X13, X13;                   \
	NEXT;                                     \
	BNEZ     X14, loop;                       \
done:                                         \
	MOV X29, ret+40(FP);                      \
	RET

#define KERNEL2(RD, BD, loop, done) \
	SETUP;                                    \
	BEQZ X14, done;                           \
loop:                                         \
	VSETVLI  X14, E8, M1, TA, MA, X15;        \
	VLE8V    (X11), V0;                       \
	ADDI     $1, X15, X5;                     \
	SRLI     $1, X5, X5;                      \
	VSETVLI  X5, E8, M1, TA, MA, X6;          \
	VLE8V    (X12), V1;                       \
	VLE8V    (X13), V2;                       \
	VSETVLI  X5, E16, M2, TA, MA, X6;         \
	VZEXTVF2 V1, V4;                          \
	VMULVX   X23, V4, V4;                     \
	VZEXTVF2 V2, V6;                          \
	VMULVX   X23, V6, V6;                     \
	VSETVLI  X15, E32, M4, TA, MA, X6;        \
	VZEXTVF4 V0, V8;                          \
	VZEXTVF4 V4, V12;                         \
	VZEXTVF4 V6, V16;                         \
	CONVERT(RD, BD);                          \
	ADD      X5, X12, X12;                    \
	ADD      X5, X13, X13;                    \
	NEXT;                                     \
	BNEZ     X14, loop;                       \
done:                                         \
	MOV X29, ret+40(FP);                      \
	RET

// func rgbaRVV(dst, y, cb, cr *byte, n int) int
TEXT ·rgbaRVV(SB), NOSPLIT, $0-48
	KERNEL1(V0, V2, rgbaRVVloop, rgbaRVVdone)

// func bgraRVV(dst, y, cb, cr *byte, n int) int
TEXT ·bgraRVV(SB), NOSPLIT, $0-48
	KERNEL1(V2, V0, bgraRVVloop, bgraRVVdone)

// func rgba2RVV(dst, y, cb, cr *byte, n int) int
TEXT ·rgba2RVV(SB), NOSPLIT, $0-48
	KERNEL2(V0, V2, rgba2RVVloop, rgba2RVVdone)

// func bgra2RVV(dst, y, cb, cr *byte, n int) int
TEXT ·bgra2RVV(SB), NOSPLIT, $0-48
	KERNEL2(V2, V0, bgra2RVVloop, bgra2RVVdone)
