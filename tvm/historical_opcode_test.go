package tvm

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestHistoricalMissingOpcodes(t *testing.T) {
	machine := NewTVM()
	for _, tc := range []struct {
		name       string
		opcode     uint64
		minBits    uint
		historical vm.HistoricalConfig
	}{
		{"RANDU256", 0xf810, 12, vm.HistoricalConfig{NoPRNG: true}},
		{"RAND", 0xf811, 12, vm.HistoricalConfig{NoPRNG: true}},
		{"SETRAND", 0xf814, 12, vm.HistoricalConfig{NoPRNG: true}},
		{"ADDRAND", 0xf815, 12, vm.HistoricalConfig{NoPRNG: true}},
		{"BLKDROP2", 0x6c11, 8, vm.HistoricalConfig{NoBLKDROP2: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for bits := tc.minBits; bits <= 16; bits++ {
				t.Run(fmt.Sprintf("bits_%d", bits), func(t *testing.T) {
					code := cell.BeginCell().MustStoreUInt(tc.opcode>>(16-bits), bits).EndCell()
					stack := vm.NewStack()
					for _, n := range []int64{11, 22, 33} {
						if err := stack.PushSmallInt(n); err != nil {
							t.Fatal(err)
						}
					}
					seed := big.NewInt(123)
					c7 := tuple.NewTupleValue(tuple.NewTupleValue(nil, nil, nil, nil, nil, nil, seed))
					state := vm.NewExecutionState(0, vm.GasWithLimit(1000), nil, c7, stack)
					state.Historical = tc.historical
					state.PrepareExecution(code.MustBeginParse())
					defer state.Cells.FinishExecution()

					err := machine.step(state)
					if got, ok := vmerr.ErrorCode(err); !ok || got != vmerr.CodeInvalidOpcode {
						t.Fatalf("step error = %v, want invalid opcode", err)
					}
					if state.Gas.Used() != 10 || state.CurrentCode.BitsLeft() != bits {
						t.Fatalf("gas/code bits = %d/%d, want 10/%d", state.Gas.Used(), state.CurrentCode.BitsLeft(), bits)
					}
					if state.Stack.Len() != 3 {
						t.Fatalf("stack length = %d, want 3", state.Stack.Len())
					}
					for _, want := range []int64{33, 22, 11} {
						if got := popInt64(t, state.Stack); got != want {
							t.Fatalf("stack value = %d, want %d", got, want)
						}
					}
					inner, _ := state.Reg.C7.Index(0)
					params := inner.(tuple.Tuple)
					gotSeed, _ := params.Index(6)
					if gotSeed.(*big.Int).Cmp(seed) != 0 {
						t.Fatal("unknown opcode changed random seed")
					}
				})
			}

			cfg := testExecutionConfigWithVersion(t, 0)
			cfg.Historical = tc.historical
			code := cell.BeginCell().MustStoreUInt(tc.opcode, 16).EndCell()
			result, err := machine.Execute(code, nil, tuple.Tuple{}, vm.GasWithLimit(1000), vm.NewStack(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			if result.ExitCode != 6 || result.GasUsed != 60 || result.Steps != 2 {
				t.Fatalf("exit/gas/steps = %d/%d/%d, want 6/60/2", result.ExitCode, result.GasUsed, result.Steps)
			}
		})
	}
}

func TestHistoricalOpcodeFlagsPreserveOtherInstructions(t *testing.T) {
	machine := NewTVM()
	for _, tc := range []struct {
		name       string
		opcode     uint64
		version    uint32
		historical vm.HistoricalConfig
	}{
		{"modern_rand", 0xf810, 0, vm.HistoricalConfig{}},
		{"modern_blkdrop2", 0x6c11, 0, vm.HistoricalConfig{}},
		{"rand_with_no_blkdrop2", 0xf810, 0, vm.HistoricalConfig{NoBLKDROP2: true}},
		{"blkdrop2_with_no_prng", 0x6c11, 0, vm.HistoricalConfig{NoPRNG: true}},
		{"adjacent_accept", 0xf800, 0, vm.HistoricalConfig{NoPRNG: true, NoBLKDROP2: true}},
		{"adjacent_blkdrop", 0x5f01, 0, vm.HistoricalConfig{NoPRNG: true, NoBLKDROP2: true}},
		{"version_one_rand", 0xf810, 1, vm.HistoricalConfig{}},
		{"version_one_blkdrop2", 0x6c11, 1, vm.HistoricalConfig{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code := cell.BeginCell().MustStoreUInt(tc.opcode, 16).EndCell()
			stack := vm.NewStack()
			for _, n := range []int64{11, 22, 33} {
				if err := stack.PushSmallInt(n); err != nil {
					t.Fatal(err)
				}
			}
			c7 := tuple.NewTupleValue(tuple.NewTupleValue(nil, nil, nil, nil, nil, nil, big.NewInt(123)))
			cfg := testExecutionConfigWithVersion(t, tc.version)
			cfg.Historical = tc.historical
			res, err := machine.Execute(code, nil, c7, vm.GasWithLimit(1000), stack, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if res.ExitCode != 0 {
				t.Fatalf("exit code = %d, want success", res.ExitCode)
			}
		})
	}
}
