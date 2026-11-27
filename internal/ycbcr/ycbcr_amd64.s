//go:build amd64 && !noasm

#include "textflag.h"

DATA cY<>+0(SB)/4, $65793
DATA cRCr<>+0(SB)/4, $91881
DATA cGCb<>+0(SB)/4, $22554
DATA cGCr<>+0(SB)/4, $46802
DATA cBCb<>+0(SB)/4, $116130
DATA c128<>+0(SB)/4, $128
GLOBL cY<>(SB), RODATA|NOPTR, $4
GLOBL cRCr<>(SB), RODATA|NOPTR, $4
GLOBL cGCb<>(SB), RODATA|NOPTR, $4
GLOBL cGCr<>(SB), RODATA|NOPTR, $4
GLOBL cBCb<>(SB), RODATA|NOPTR, $4
GLOBL c128<>(SB), RODATA|NOPTR, $4

#define ARGS \
	MOVQ dst+0(FP), DI; \
	MOVQ y+8(FP), SI;   \
	MOVQ cb+16(FP), R9; \
	MOVQ cr+24(FP), R10; \
	MOVQ n+32(FP), CX;  \
	MOVQ CX, DX

#define AVX2_SETUP \
	ARGS;                           \
	VPBROADCASTD cY<>(SB), Y9;      \
	VPBROADCASTD cRCr<>(SB), Y10;   \
	VPBROADCASTD cGCb<>(SB), Y11;   \
	VPBROADCASTD cGCr<>(SB), Y12;   \
	VPBROADCASTD cBCb<>(SB), Y13;   \
	VPBROADCASTD c128<>(SB), Y14;   \
	VPCMPEQB     Y15, Y15, Y15

#define AVX2_LOAD_Y \
	VPMOVZXBD (SI), Y0; \
	VPMOVZXBD 8(SI), Y1

#define AVX2_LOAD_C1 \
	VPMOVZXBD (R9), Y2;   \
	VPMOVZXBD 8(R9), Y3;  \
	VPMOVZXBD (R10), Y4;  \
	VPMOVZXBD 8(R10), Y5

#define AVX2_LOAD_C2 \
	VMOVQ      (R9), X2;      \
	VPUNPCKLBW X2, X2, X2;    \
	VPSRLDQ    $8, X2, X3;    \
	VPMOVZXBD  X3, Y3;        \
	VPMOVZXBD  X2, Y2;        \
	VMOVQ      (R10), X4;     \
	VPUNPCKLBW X4, X4, X4;    \
	VPSRLDQ    $8, X4, X5;    \
	VPMOVZXBD  X5, Y5;        \
	VPMOVZXBD  X4, Y4

#define AVX2_CONVERT \
	VPMULLD      Y9, Y0, Y0;   \
	VPMULLD      Y9, Y1, Y1;   \
	VPSUBD       Y14, Y2, Y2;  \
	VPSUBD       Y14, Y3, Y3;  \
	VPSUBD       Y14, Y4, Y4;  \
	VPSUBD       Y14, Y5, Y5;  \
	VPMULLD      Y10, Y4, Y6;  \
	VPADDD       Y0, Y6, Y6;   \
	VPMULLD      Y10, Y5, Y7;  \
	VPADDD       Y1, Y7, Y7;   \
	VPSRAD       $16, Y6, Y6;  \
	VPSRAD       $16, Y7, Y7;  \
	VPACKSSDW    Y7, Y6, Y6;   \
	VPMULLD      Y13, Y2, Y7;  \
	VPADDD       Y0, Y7, Y7;   \
	VPMULLD      Y13, Y3, Y8;  \
	VPADDD       Y1, Y8, Y8;   \
	VPSRAD       $16, Y7, Y7;  \
	VPSRAD       $16, Y8, Y8;  \
	VPACKSSDW    Y8, Y7, Y7;   \
	VPMULLD      Y11, Y2, Y8;  \
	VPSUBD       Y8, Y0, Y0;   \
	VPMULLD      Y12, Y4, Y8;  \
	VPSUBD       Y8, Y0, Y0;   \
	VPMULLD      Y11, Y3, Y8;  \
	VPSUBD       Y8, Y1, Y1;   \
	VPMULLD      Y12, Y5, Y8;  \
	VPSUBD       Y8, Y1, Y1;   \
	VPSRAD       $16, Y0, Y0;  \
	VPSRAD       $16, Y1, Y1;  \
	VPACKSSDW    Y1, Y0, Y0;   \
	VPERMQ       $0xD8, Y6, Y6; \
	VPERMQ       $0xD8, Y7, Y7; \
	VPERMQ       $0xD8, Y0, Y0; \
	VEXTRACTI128 $1, Y6, X2;   \
	VPACKUSWB    X2, X6, X6;   \
	VEXTRACTI128 $1, Y7, X3;   \
	VPACKUSWB    X3, X7, X7;   \
	VEXTRACTI128 $1, Y0, X4;   \
	VPACKUSWB    X4, X0, X0

#define AVX2_STORE(F, T) \
	VPUNPCKLBW X0, F, X2;   \
	VPUNPCKHBW X0, F, X3;   \
	VPUNPCKLBW X15, T, X4;  \
	VPUNPCKHBW X15, T, X5;  \
	VPUNPCKLWD X4, X2, X8;  \
	VPUNPCKHWD X4, X2, X2;  \
	VPUNPCKLWD X5, X3, X4;  \
	VPUNPCKHWD X5, X3, X3;  \
	VMOVDQU    X8, (DI);    \
	VMOVDQU    X2, 16(DI);  \
	VMOVDQU    X4, 32(DI);  \
	VMOVDQU    X3, 48(DI)

#define AVX2_KERNEL(LOADC, CSTEP, F, T, loop, done) \
	AVX2_SETUP;      \
loop:                \
	CMPQ CX, $16;    \
	JLT  done;       \
	AVX2_LOAD_Y;     \
	LOADC;           \
	AVX2_CONVERT;    \
	AVX2_STORE(F, T); \
	ADDQ $16, SI;    \
	ADDQ $CSTEP, R9; \
	ADDQ $CSTEP, R10; \
	ADDQ $64, DI;    \
	SUBQ $16, CX;    \
	JMP  loop;       \
done:                \
	VZEROUPPER;      \
	SUBQ CX, DX;     \
	MOVQ DX, ret+40(FP); \
	RET

// func rgbaAVX2(dst, y, cb, cr *byte, n int) int
TEXT ·rgbaAVX2(SB), NOSPLIT, $0-48
	AVX2_KERNEL(AVX2_LOAD_C1, 16, X6, X7, rgbaAVX2loop, rgbaAVX2done)

// func bgraAVX2(dst, y, cb, cr *byte, n int) int
TEXT ·bgraAVX2(SB), NOSPLIT, $0-48
	AVX2_KERNEL(AVX2_LOAD_C1, 16, X7, X6, bgraAVX2loop, bgraAVX2done)

// func rgba2AVX2(dst, y, cb, cr *byte, n int) int
TEXT ·rgba2AVX2(SB), NOSPLIT, $0-48
	AVX2_KERNEL(AVX2_LOAD_C2, 8, X6, X7, rgba2AVX2loop, rgba2AVX2done)

// func bgra2AVX2(dst, y, cb, cr *byte, n int) int
TEXT ·bgra2AVX2(SB), NOSPLIT, $0-48
	AVX2_KERNEL(AVX2_LOAD_C2, 8, X7, X6, bgra2AVX2loop, bgra2AVX2done)

#define SSE_SETUP \
	ARGS;                     \
	MOVL   cY<>(SB), AX;      \
	MOVQ   AX, X9;            \
	PSHUFD $0, X9, X9;        \
	MOVL   cRCr<>(SB), AX;    \
	MOVQ   AX, X10;           \
	PSHUFD $0, X10, X10;      \
	MOVL   cGCb<>(SB), AX;    \
	MOVQ   AX, X11;           \
	PSHUFD $0, X11, X11;      \
	MOVL   cGCr<>(SB), AX;    \
	MOVQ   AX, X12;           \
	PSHUFD $0, X12, X12;      \
	MOVL   cBCb<>(SB), AX;    \
	MOVQ   AX, X13;           \
	PSHUFD $0, X13, X13;      \
	MOVL   c128<>(SB), AX;    \
	MOVQ   AX, X14;           \
	PSHUFD $0, X14, X14;      \
	PCMPEQB X15, X15

#define SSE_LOAD_Y \
	PMOVZXBD (SI), X0; \
	PMOVZXBD 4(SI), X1

#define SSE_LOAD_C1 \
	PMOVZXBD (R9), X2;  \
	PMOVZXBD 4(R9), X3; \
	PMOVZXBD (R10), X4; \
	PMOVZXBD 4(R10), X5

#define SSE_LOAD_C2 \
	MOVL      (R9), AX;   \
	MOVQ      AX, X2;     \
	PUNPCKLBW X2, X2;     \
	MOVO      X2, X3;     \
	PSRLO     $4, X3;     \
	PMOVZXBD  X3, X3;     \
	PMOVZXBD  X2, X2;     \
	MOVL      (R10), AX;  \
	MOVQ      AX, X4;     \
	PUNPCKLBW X4, X4;     \
	MOVO      X4, X5;     \
	PSRLO     $4, X5;     \
	PMOVZXBD  X5, X5;     \
	PMOVZXBD  X4, X4

#define SSE_CONVERT \
	PMULLD   X9, X0;   \
	PMULLD   X9, X1;   \
	PSUBL    X14, X2;  \
	PSUBL    X14, X3;  \
	PSUBL    X14, X4;  \
	PSUBL    X14, X5;  \
	MOVO     X4, X6;   \
	PMULLD   X10, X6;  \
	PADDL    X0, X6;   \
	MOVO     X5, X7;   \
	PMULLD   X10, X7;  \
	PADDL    X1, X7;   \
	PSRAL    $16, X6;  \
	PSRAL    $16, X7;  \
	PACKSSLW X7, X6;   \
	PACKUSWB X6, X6;   \
	MOVO     X2, X7;   \
	PMULLD   X13, X7;  \
	PADDL    X0, X7;   \
	MOVO     X3, X8;   \
	PMULLD   X13, X8;  \
	PADDL    X1, X8;   \
	PSRAL    $16, X7;  \
	PSRAL    $16, X8;  \
	PACKSSLW X8, X7;   \
	PACKUSWB X7, X7;   \
	PMULLD   X11, X2;  \
	PSUBL    X2, X0;   \
	PMULLD   X12, X4;  \
	PSUBL    X4, X0;   \
	PMULLD   X11, X3;  \
	PSUBL    X3, X1;   \
	PMULLD   X12, X5;  \
	PSUBL    X5, X1;   \
	PSRAL    $16, X0;  \
	PSRAL    $16, X1;  \
	PACKSSLW X1, X0;   \
	PACKUSWB X0, X0

#define SSE_STORE(F, T) \
	MOVO      F, X2;      \
	PUNPCKLBW X0, X2;     \
	MOVO      T, X3;      \
	PUNPCKLBW X15, X3;    \
	MOVO      X2, X4;     \
	PUNPCKLWL X3, X2;     \
	PUNPCKHWL X3, X4;     \
	MOVOU     X2, (DI);   \
	MOVOU     X4, 16(DI)

#define SSE_KERNEL(LOADC, CSTEP, F, T, loop, done) \
	SSE_SETUP;        \
loop:                 \
	CMPQ CX, $8;      \
	JLT  done;        \
	SSE_LOAD_Y;       \
	LOADC;            \
	SSE_CONVERT;      \
	SSE_STORE(F, T);  \
	ADDQ $8, SI;      \
	ADDQ $CSTEP, R9;  \
	ADDQ $CSTEP, R10; \
	ADDQ $32, DI;     \
	SUBQ $8, CX;      \
	JMP  loop;        \
done:                 \
	SUBQ CX, DX;      \
	MOVQ DX, ret+40(FP); \
	RET

// func rgbaSSE41(dst, y, cb, cr *byte, n int) int
TEXT ·rgbaSSE41(SB), NOSPLIT, $0-48
	SSE_KERNEL(SSE_LOAD_C1, 8, X6, X7, rgbaSSE41loop, rgbaSSE41done)

// func bgraSSE41(dst, y, cb, cr *byte, n int) int
TEXT ·bgraSSE41(SB), NOSPLIT, $0-48
	SSE_KERNEL(SSE_LOAD_C1, 8, X7, X6, bgraSSE41loop, bgraSSE41done)

// func rgba2SSE41(dst, y, cb, cr *byte, n int) int
TEXT ·rgba2SSE41(SB), NOSPLIT, $0-48
	SSE_KERNEL(SSE_LOAD_C2, 4, X6, X7, rgba2SSE41loop, rgba2SSE41done)

// func bgra2SSE41(dst, y, cb, cr *byte, n int) int
TEXT ·bgra2SSE41(SB), NOSPLIT, $0-48
	SSE_KERNEL(SSE_LOAD_C2, 4, X7, X6, bgra2SSE41loop, bgra2SSE41done)
