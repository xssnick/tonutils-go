package tvm

import (
	"fmt"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestGasCreditAcceptChecksClampedBudget(t *testing.T) {
	// C++ GasLimits(0, max, credit) permits execution on credit before
	// ACCEPT. Clearing credit can leave an overdraft after the new limit
	// is clamped to max: it must fail at that instruction, including when
	// the transaction's accept precheck stops there.
	for _, version := range []int{3, 4, vm.MaxSupportedGlobalVersion} {
		for _, stop := range []bool{false, true} {
			for _, max := range []int64{20, 31} {
				for _, op := range []struct {
					name string
					code *cell.Cell
					set  bool
				}{
					{name: "ACCEPT", code: codeFromBuilders(t, funcsop.ACCEPT().Serialize())},
					{name: "SETGASLIMIT", code: codeFromBuilders(t, funcsop.SETGASLIMIT().Serialize()), set: true},
				} {
					t.Run(fmt.Sprintf("%s/max%d/stop%t/v%d", op.name, max, stop, version), func(t *testing.T) {
						stack := vm.NewStack()
						if op.set {
							if err := stack.PushSmallInt(100); err != nil {
								t.Fatal(err)
							}
						}
						gas := vm.Gas{}
						gas.SetLimits(max, 0, 100)
						cfg := testExecutionConfigWithVersion(t, uint32(version))
						options := executeOptionsFromConfig(cfg)
						options.stopOnAccept = stop
						res, err := NewTVM().executeWithOptions(op.code, cell.BeginCell().EndCell(), tuple.Tuple{}, gas, stack, cfg.Config, options)
						res, err = finishExecutionResult(res, err)
						if err != nil {
							t.Fatal(err)
						}

						wantExit, wantGas := int64(0), int64(26)
						if max == 20 {
							wantExit = -14
						} else if !stop {
							wantGas += 5
						}
						if res.ExitCode != wantExit || res.GasUsed != wantGas {
							t.Fatalf("exit/gas = %d/%d, want %d/%d", res.ExitCode, res.GasUsed, wantExit, wantGas)
						}
						if res.Gas.Credit != 0 || res.Gas.Limit != max || res.Gas.Remaining != max-wantGas {
							t.Fatalf("gas after clearing credit = %+v, want limit %d, credit 0, remaining %d", res.Gas, max, max-wantGas)
						}
						if wantExit == -14 {
							if res.Stack.Len() != 1 || popInt64(t, res.Stack) != wantGas || res.Committed {
								t.Fatal("out-of-gas must leave only consumed gas and prevent automatic commit")
							}
						} else if res.Stack.Len() != 0 || !res.Committed {
							t.Fatal("successful accept must preserve the empty stack and commit")
						}
					})
				}
			}
		}
	}
}
