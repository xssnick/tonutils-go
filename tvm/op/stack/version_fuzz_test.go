package stack

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func fuzzStackVersion(raw int64) int {
	version := int(raw % int64(vm.DefaultGlobalVersion+1))
	if version < 0 {
		version = -version
	}
	return version
}

func TestFuzzStackVersionCoversDefaultRange(t *testing.T) {
	for version := 0; version <= vm.DefaultGlobalVersion; version++ {
		if got := fuzzStackVersion(int64(version)); got != version {
			t.Fatalf("seed version %d mapped to %d", version, got)
		}
	}
	if got := fuzzStackVersion(-int64(vm.DefaultGlobalVersion)); got != vm.DefaultGlobalVersion {
		t.Fatalf("negative default version mapped to %d, want %d", got, vm.DefaultGlobalVersion)
	}
	for _, raw := range []int64{
		-1,
		-int64(vm.DefaultGlobalVersion) - 1,
		-int64(vm.DefaultGlobalVersion) - 2,
		-123456789,
		123456789,
	} {
		got := fuzzStackVersion(raw)
		if got < 0 || got > vm.DefaultGlobalVersion {
			t.Fatalf("raw version %d mapped to %d, want within [0, %d]", raw, got, vm.DefaultGlobalVersion)
		}
	}
}

func fuzzStackIndex(raw int64) int64 {
	switch raw {
	case -1, 0, 1, 254, 255, 256, 300, int64(maxSmallIndex), int64(maxSmallIndex) + 1:
		return raw
	}
	if raw >= -100 && raw <= 600 {
		return raw
	}
	return fuzzStackNonNegativeModulo(raw, 700) - 100
}

func fuzzStackNonNegativeModulo(raw, mod int64) int64 {
	res := raw % mod
	if res < 0 {
		res = -res
	}
	return res
}

func TestFuzzStackIndexNormalizesArbitrarySeeds(t *testing.T) {
	for _, raw := range []int64{
		-1 << 63,
		-123456789,
		-701,
		-700,
		-699,
		-101,
		-100,
		-1,
		0,
		1,
		600,
		601,
		123456789,
		1<<63 - 1,
	} {
		got := fuzzStackIndex(raw)
		if got == int64(maxSmallIndex) || got == int64(maxSmallIndex)+1 {
			continue
		}
		if got < -100 || got > 600 {
			t.Fatalf("raw index %d mapped to %d, want within [-100, 600]", raw, got)
		}
	}
	if got := fuzzStackIndex(int64(maxSmallIndex)); got != int64(maxSmallIndex) {
		t.Fatalf("maxSmallIndex mapped to %d", got)
	}
	if got := fuzzStackIndex(int64(maxSmallIndex) + 1); got != int64(maxSmallIndex)+1 {
		t.Fatalf("maxSmallIndex+1 mapped to %d", got)
	}
}

func fuzzStackIndexState(t *testing.T, version int, indices ...int64) *vm.State {
	t.Helper()

	st := vm.NewStack()
	for i := 1; i <= 300; i++ {
		if err := st.PushInt(big.NewInt(int64(i))); err != nil {
			t.Fatalf("push stack value: %v", err)
		}
	}
	for _, idx := range indices {
		if err := st.PushInt(big.NewInt(idx)); err != nil {
			t.Fatalf("push index: %v", err)
		}
	}
	return &vm.State{
		GlobalVersion:           version,
		GlobalVersionConfigured: true,
		Stack:                   st,
		Gas:                     vm.GasWithLimit(10_000),
	}
}

func expectStackVersionVMError(t *testing.T, err error, code int64) {
	t.Helper()

	var vmErr vmerr.VMError
	if !errors.As(err, &vmErr) || vmErr.Code != code {
		t.Fatalf("error = %v, want VM error code %d", err, code)
	}
}

func FuzzTVMVersionedDynamicStackIndexLimits(f *testing.F) {
	f.Add(int64(3), int64(255), uint8(0))
	f.Add(int64(3), int64(256), uint8(0))
	f.Add(int64(4), int64(256), uint8(0))
	f.Add(int64(4), int64(300), uint8(0))
	f.Add(int64(4), int64(maxSmallIndex), uint8(0))
	f.Add(int64(4), int64(maxSmallIndex)+1, uint8(0))
	f.Add(int64(3), int64(-1), uint8(1))
	for version := int64(0); version <= int64(vm.DefaultGlobalVersion); version++ {
		for opKind := uint8(0); opKind < 12; opKind++ {
			f.Add(version, int64(255), opKind)
			f.Add(version, int64(256), opKind)
			f.Add(version, int64(300), opKind)
			f.Add(version, int64(maxSmallIndex), opKind)
			f.Add(version, int64(maxSmallIndex)+1, opKind)
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawIndex int64, rawOp uint8) {
		version := fuzzStackVersion(rawVersion)
		idx := fuzzStackIndex(rawIndex)
		opKind := rawOp % 12

		var op vm.OP
		var state *vm.State
		switch opKind {
		case 0:
			op = PICK()
		case 1:
			op = ROLL()
		case 2:
			op = ROLLREV()
		case 3:
			op = XCHGX()
		case 4:
			op = CHKDEPTH()
		case 5:
			op = DROPX()
		case 6:
			op = ONLYTOPX()
		case 7:
			op = ONLYX()
		case 8, 9:
			op = REVX()
		default:
			op = BLKSWX()
		}

		if opKind >= 8 {
			x, y := idx, int64(0)
			if opKind == 9 || opKind == 11 {
				x, y = 0, idx
			}
			state = fuzzStackIndexState(t, version, x, y)
		} else {
			state = fuzzStackIndexState(t, version, idx)
		}

		err := op.Interpret(state)

		limit := int64(maxSmallIndex)
		if version < 4 {
			limit = 255
		}
		if idx < 0 || idx > limit {
			expectStackVersionVMError(t, err, vmerr.CodeRangeCheck)
			return
		}

		depth := int64(300)
		needsExistingSlot := opKind <= 3
		if (needsExistingSlot && idx >= depth) || (!needsExistingSlot && idx > depth) {
			expectStackVersionVMError(t, err, vmerr.CodeStackUnderflow)
			return
		}

		if err != nil {
			t.Fatalf("%s version=%d index=%d failed: %v", op.SerializeText(), version, idx, err)
		}
	})
}

func fuzzStackBlockCount(raw int64) int64 {
	switch raw {
	case -1, 0, 1, 127, 128, 254, 255, 256, 257, 300, 511, 512, 600:
		return raw
	}
	if raw >= -16 && raw <= 700 {
		return raw
	}
	return fuzzStackNonNegativeModulo(raw, 800) - 16
}

func TestFuzzStackBlockCountNormalizesArbitrarySeeds(t *testing.T) {
	for _, raw := range []int64{
		-1 << 63,
		-123456789,
		-801,
		-800,
		-799,
		-17,
		-16,
		-1,
		0,
		1,
		700,
		701,
		123456789,
		1<<63 - 1,
	} {
		got := fuzzStackBlockCount(raw)
		if got < -16 || got > 783 {
			t.Fatalf("raw block count %d mapped to %d, want within [-16, 783]", raw, got)
		}
	}
}

func fuzzStackBlockState(t *testing.T, version int, depth int, x, y int64) *vm.State {
	t.Helper()

	st := vm.NewStack()
	for i := 1; i <= depth; i++ {
		if err := st.PushInt(big.NewInt(int64(i))); err != nil {
			t.Fatalf("push stack value: %v", err)
		}
	}
	if err := st.PushInt(big.NewInt(x)); err != nil {
		t.Fatalf("push BLKSWX x: %v", err)
	}
	if err := st.PushInt(big.NewInt(y)); err != nil {
		t.Fatalf("push BLKSWX y: %v", err)
	}

	return &vm.State{
		GlobalVersion:           version,
		GlobalVersionConfigured: true,
		Stack:                   st,
		Gas:                     vm.GasWithLimit(100_000),
	}
}

func FuzzTVMVersionedBLKSWXLargeMoveGas(f *testing.F) {
	for version := int64(0); version <= int64(vm.DefaultGlobalVersion); version++ {
		for _, counts := range [][2]int64{
			{0, 300},
			{1, 254},
			{1, 255},
			{1, 256},
			{128, 128},
			{200, 60},
			{255, 255},
			{256, 1},
			{-1, 1},
		} {
			f.Add(version, counts[0], counts[1], int64(600))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawX, rawY, rawDepth int64) {
		version := fuzzStackVersion(rawVersion)
		x := fuzzStackBlockCount(rawX)
		y := fuzzStackBlockCount(rawY)
		depth := fuzzStackBlockCount(rawDepth)
		if depth < 0 {
			depth = -depth
		}
		depth %= 700
		if depth < 2 {
			depth = 2
		}

		limit := int64(maxSmallIndex)
		if version < 4 {
			limit = 255
		}

		state := fuzzStackBlockState(t, version, int(depth), x, y)
		err := BLKSWX().Interpret(state)

		if x < 0 || y < 0 || x > limit || y > limit {
			expectStackVersionVMError(t, err, vmerr.CodeRangeCheck)
			if got := state.Gas.Used(); got != 0 {
				t.Fatalf("BLKSWX version=%d x=%d y=%d range error gas=%d, want 0", version, x, y, got)
			}
			return
		}
		if x+y > depth {
			expectStackVersionVMError(t, err, vmerr.CodeStackUnderflow)
			if got := state.Gas.Used(); got != 0 {
				t.Fatalf("BLKSWX version=%d x=%d y=%d depth=%d underflow gas=%d, want 0", version, x, y, depth, got)
			}
			return
		}
		if err != nil {
			t.Fatalf("BLKSWX version=%d x=%d y=%d depth=%d failed: %v", version, x, y, depth, err)
		}

		wantGas := int64(0)
		if version >= 4 && x > 0 && y > 0 && x+y > 255 {
			wantGas = x + y - 255
		}
		if got := state.Gas.Used(); got != wantGas {
			t.Fatalf("BLKSWX version=%d x=%d y=%d depth=%d gas=%d, want %d", version, x, y, depth, got, wantGas)
		}
		if got := state.Stack.Len(); got != int(depth) {
			t.Fatalf("BLKSWX version=%d x=%d y=%d depth=%d stack len=%d", version, x, y, depth, got)
		}
	})
}
