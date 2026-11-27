//go:build arm64 && !noasm

#include "textflag.h"

#define MUL4S(m, n, d) WORD $(0x4EA09C00 | ((m) << 16) | ((n) << 5) | (d))
#define SQXTUN8B(n, d) WORD $(0x2E212800 | ((n) << 5) | (d))
#define SSHR4S(k, n, d) WORD $(0x4F000400 | ((64 - (k)) << 16) | ((n) << 5) | (d))
#define UQXTN2_8H(n, d) WORD $(0x6E614800 | ((n) << 5) | (d))
#define UQXTN4H(n, d) WORD $(0x2E614800 | ((n) << 5) | (d))

#define SETUP \
	MOVD dst+0(FP), R0;   \
	MOVD y+8(FP), R1;     \
	MOVD cb+16(FP), R2;   \
	MOVD cr+24(FP), R3;   \
	MOVD n+32(FP), R4;    \
	MOVD R4, R6;          \
	MOVD $91881, R5;      \
	VDUP R5, V16.S4;      \
	MOVD $22554, R5;      \
	VDUP R5, V17.S4;      \
	MOVD $46802, R5;      \
	VDUP R5, V18.S4;      \
	MOVD $116130, R5;     \
	VDUP R5, V19.S4;      \
	MOVD $128, R5;        \
	VDUP R5, V20.S4;      \
	MOVD $65793, R5;      \
	VDUP R5, V22.S4;      \
	VMOVI $255, V21.B16

#define LOAD_C1 \
	VLD1.P 8(R2), [V1.B8]; \
	VLD1.P 8(R3), [V2.B8]

#define LOAD_C2 \
	FMOVS (R2), F1;                \
	ADD   $4, R2;                  \
	VZIP1 V1.B16, V1.B16, V1.B16;  \
	FMOVS (R3), F2;                \
	ADD   $4, R3;                  \
	VZIP1 V2.B16, V2.B16, V2.B16

#define CONVERT(RD, BD) \
	VUXTL  V0.B8, V6.H8;          \
	VUXTL  V6.H4, V4.S4;          \
	VUXTL2 V6.H8, V5.S4;          \
	MUL4S(22, 4, 4);              \
	MUL4S(22, 5, 5);              \
	VUXTL  V1.B8, V6.H8;          \
	VUXTL  V6.H4, V7.S4;          \
	VUXTL2 V6.H8, V8.S4;          \
	VSUB   V20.S4, V7.S4, V7.S4;  \
	VSUB   V20.S4, V8.S4, V8.S4;  \
	VUXTL  V2.B8, V6.H8;          \
	VUXTL  V6.H4, V9.S4;          \
	VUXTL2 V6.H8, V10.S4;         \
	VSUB   V20.S4, V9.S4, V9.S4;  \
	VSUB   V20.S4, V10.S4, V10.S4; \
	MUL4S(16, 9, 11);             \
	MUL4S(16, 10, 12);            \
	VADD   V4.S4, V11.S4, V11.S4; \
	VADD   V5.S4, V12.S4, V12.S4; \
	SSHR4S(16, 11, 11);           \
	SSHR4S(16, 12, 12);           \
	UQXTN4H(11, 13);              \
	UQXTN2_8H(12, 13);            \
	SQXTUN8B(13, RD);             \
	MUL4S(19, 7, 11);             \
	MUL4S(19, 8, 12);             \
	VADD   V4.S4, V11.S4, V11.S4; \
	VADD   V5.S4, V12.S4, V12.S4; \
	SSHR4S(16, 11, 11);           \
	SSHR4S(16, 12, 12);           \
	UQXTN4H(11, 13);              \
	UQXTN2_8H(12, 13);            \
	SQXTUN8B(13, BD);             \
	MUL4S(17, 7, 11);             \
	MUL4S(17, 8, 12);             \
	MUL4S(18, 9, 13);             \
	MUL4S(18, 10, 14);            \
	VSUB   V11.S4, V4.S4, V15.S4; \
	VSUB   V13.S4, V15.S4, V15.S4; \
	VSUB   V12.S4, V5.S4, V11.S4; \
	VSUB   V14.S4, V11.S4, V11.S4; \
	SSHR4S(16, 15, 15);           \
	SSHR4S(16, 11, 11);           \
	UQXTN4H(15, 13);              \
	UQXTN2_8H(11, 13);            \
	SQXTUN8B(13, 1)

#define KERNEL(LOADC, RD, BD, loop, done) \
	SETUP;                   \
loop:                        \
	CMP  $8, R4;             \
	BLT  done;               \
	VLD1.P 8(R1), [V0.B8];   \
	LOADC;                   \
	CONVERT(RD, BD);         \
	VMOV   V21.B16, V3.B16;  \
	VST4.P [V0.B8, V1.B8, V2.B8, V3.B8], 32(R0); \
	SUB  $8, R4;             \
	B    loop;               \
done:                        \
	SUB  R4, R6, R6;         \
	MOVD R6, ret+40(FP);     \
	RET

// func rgbaNEON(dst, y, cb, cr *byte, n int) int
TEXT ·rgbaNEON(SB), NOSPLIT, $0-48
	KERNEL(LOAD_C1, 0, 2, rgbaNEONloop, rgbaNEONdone)

// func bgraNEON(dst, y, cb, cr *byte, n int) int
TEXT ·bgraNEON(SB), NOSPLIT, $0-48
	KERNEL(LOAD_C1, 2, 0, bgraNEONloop, bgraNEONdone)

// func rgba2NEON(dst, y, cb, cr *byte, n int) int
TEXT ·rgba2NEON(SB), NOSPLIT, $0-48
	KERNEL(LOAD_C2, 0, 2, rgba2NEONloop, rgba2NEONdone)

// func bgra2NEON(dst, y, cb, cr *byte, n int) int
TEXT ·bgra2NEON(SB), NOSPLIT, $0-48
	KERNEL(LOAD_C2, 2, 0, bgra2NEONloop, bgra2NEONdone)
