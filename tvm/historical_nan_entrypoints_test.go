package tvm

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	mathop "github.com/xssnick/tonutils-go/tvm/op/math"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMHistoricalNaNComparisonEntrypoints(t *testing.T) {
	empty := cell.BeginCell().EndCell()
	code := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize(), mathop.PUSHNAN().Serialize(),
		mathop.LESS().Serialize(), funcsop.ACCEPT().Serialize())
	machine := NewTVM()
	for _, version := range []uint32{0, 1, 2, 3} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			for _, historical := range []bool{false, true, false} {
				cfg := testExecutionConfigWithVersion(t, version)
				cfg.Historical.NaNComparison = historical
				msgCfg := MessageEmulationConfig{
					Config: cfg.Config, Historical: cfg.Historical,
					Address: tonopsTestAddr, Now: uint32(tonopsTestTime.Unix()), RandSeed: tonopsTestSeed,
				}
				execute, err := machine.Execute(code, empty, tuple.Tuple{}, vm.GasWithLimit(10000), vm.NewStack(), cfg)
				if err != nil {
					t.Fatal(err)
				}
				get, err := machine.ExecuteGetMethod(code, empty, tuple.Tuple{}, vm.GasWithLimit(10000), vm.NewStack(), cfg)
				if err != nil {
					t.Fatal(err)
				}
				internal, err := machine.EmulateInternalMessage(code, empty, empty, 1, msgCfg)
				if err != nil {
					t.Fatal(err)
				}
				external, err := machine.EmulateExternalMessage(code, empty, &tlb.ExternalMessage{DstAddr: tonopsTestAddr, Body: empty}, msgCfg)
				if err != nil {
					t.Fatal(err)
				}
				for name, result := range map[string]*ExecutionResult{
					"execute": execute, "get": get, "internal": &internal.ExecutionResult, "external": &external.ExecutionResult,
				} {
					wantExit := int64(vmerr.CodeIntOverflow)
					if historical {
						wantExit = 0
					}
					if result.ExitCode != wantExit {
						t.Fatalf("%s historical=%t: exit=%d, want %d", name, historical, result.ExitCode, wantExit)
					}
					if historical {
						got, err := result.Stack.PopIntFinite()
						if err != nil || got.Int64() != 7 {
							t.Fatalf("%s: result=%v, error=%v, want 7", name, got, err)
						}
					}
				}
			}
		})
	}
}
