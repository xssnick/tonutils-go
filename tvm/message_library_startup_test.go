package tvm

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestMessageDryRunChargesLibraryRootAtV9(t *testing.T) {
	data := cell.BeginCell().EndCell()
	for _, external := range []bool{false, true} {
		name := "internal"
		builders := []*cell.Builder{}
		if external {
			name = "external"
			builders = append(builders, funcsop.ACCEPT().Serialize())
		}
		builders = append(builders, stackop.PUSHINT(big.NewInt(42)).Serialize())
		targetCode := codeFromBuilders(t, builders...)
		code := mustLibraryCellForHash(t, targetCode.Hash())
		libraries := []*cell.Cell{mustLibraryCollection(t, targetCode)}

		for _, version := range []uint32{8, 9} {
			t.Run(fmt.Sprintf("%s/v%d", name, version), func(t *testing.T) {
				config := transactionTestConfigWithGlobalVersion(t, version)
				machine := NewTVM()
				raw, err := machine.Execute(code, data, tuple.Tuple{}, vm.GasWithLimit(10000), vm.NewStack(), ExecutionConfig{
					Config:    config,
					Libraries: libraries,
				})
				if err != nil {
					t.Fatal(err)
				}

				cfg := MessageEmulationConfig{
					Address:   tonopsTestAddr,
					Now:       uint32(tonopsTestTime.Unix()),
					RandSeed:  tonopsTestSeed,
					Config:    config,
					Libraries: libraries,
					Gas:       vm.GasWithLimit(10000),
				}
				var message *MessageExecutionResult
				if external {
					message, err = machine.EmulateExternalMessage(code, data, &tlb.ExternalMessage{
						DstAddr: tonopsTestAddr,
						Body:    data,
					}, cfg)
				} else {
					message, err = machine.EmulateInternalMessage(code, data, data, internalMessageTestAmount, cfg)
				}
				if err != nil {
					t.Fatal(err)
				}
				if raw.ExitCode != 0 || !raw.Committed || message.ExitCode != 0 || !message.Committed || !message.Accepted {
					t.Fatalf("library code failed: raw exit=%d committed=%t; message exit=%d committed=%t accepted=%t",
						raw.ExitCode, raw.Committed, message.ExitCode, message.Committed, message.Accepted)
				}
				for _, res := range []*ExecutionResult{raw, &message.ExecutionResult} {
					value, err := res.Stack.PopIntFinite()
					if err != nil || value.Int64() != 42 {
						t.Fatalf("library code result = %v, err=%v, want 42", value, err)
					}
				}

				var extraGas int64
				var extraSteps uint64
				if version >= 9 {
					// Implicit JMPREF costs 10 gas; the first code cell load costs 100.
					extraGas = 110
					extraSteps = 1
				}
				if message.GasUsed-raw.GasUsed != extraGas || message.Steps-raw.Steps != extraSteps {
					t.Fatalf("dry-run startup overhead = gas %d, steps %d; want gas %d, steps %d",
						message.GasUsed-raw.GasUsed, message.Steps-raw.Steps, extraGas, extraSteps)
				}
			})
		}
	}
}
