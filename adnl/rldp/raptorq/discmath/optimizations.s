//go:build amd64
#include "textflag.h"

// func asmSSE2XORBlocks(x, y unsafe.Pointer, blocks int)
TEXT ·asmSSE2XORBlocks(SB), NOSPLIT, $0-24
    // x  -> AX (0(FP))
    // y  -> CX (8(FP))
    // blocks-> DX (16(FP))
    MOVQ x+0(FP), AX
    MOVQ y+8(FP), CX
    MOVQ blocks+16(FP), DX

    // i := 0
    XORQ SI, SI

loop:
    // if i >= DX => done
    CMPQ SI, DX
    JGE done

    // copy i
    MOVQ SI, R8
    // mul r8 on 16 (blocks size) (<<4)
    SHLQ $4, R8

    // load x
    LEAQ (AX)(R8*1), R9
    MOVOU (R9), X0

    // load y
    LEAQ (CX)(R8*1), R10
    MOVOU (R10), X1

    // X0 ^= X1
    PXOR X1, X0

    // save to x
    MOVOU X0, (R9)

    // i++
    INCQ SI
    JMP loop

done:
    RET
