//go:build cgo && tvm_cross_emulator

package tvm

import (
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestTVMCrossEmulatorChildChangedGasLimit(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skip(err)
	}

	// The child reports its actual gas consumption, while the parent's charge
	// is capped by the child's final limit plus one. GASCONSUMED in the parent
	// distinguishes that charge from the gas returned by RUNVM +8.
	for _, version := range []int{4, 9, 10, vm.MaxSupportedGlobalVersion} {
		for _, tc := range []struct {
			name  string
			child *cell.Cell
			args  []any
			limit int64
			max   int64
			gas   int64
			want  []any
		}{
			{
				name: "accept_raises_soft_limit",
				child: codeFromBuilders(t, funcsop.ACCEPT().Serialize(),
					stackop.PUSHINT(big.NewInt(7)).Serialize()),
				limit: 26, max: 200,
				gas: 172, want: []any{int64(7), int64(0), int64(49), int64(167)},
			},
			{
				name: "accept_clamped_to_hard_limit",
				child: codeFromBuilders(t, funcsop.ACCEPT().Serialize(),
					stackop.PUSHINT(big.NewInt(7)).Serialize()),
				limit: 26, max: 30,
				gas: 154, want: []any{int64(44), int64(-14), int64(44), int64(149)},
			},
			{
				name:  "lower_limit_exactly_consumed",
				child: codeFromBuilders(t, funcsop.SETGASLIMIT().Serialize()),
				args:  []any{int64(26)}, limit: 100, max: 200,
				gas: 150, want: []any{int64(31), int64(-14), int64(31), int64(145)},
			},
			{
				name:  "lower_limit_one_above_consumed",
				child: codeFromBuilders(t, funcsop.SETGASLIMIT().Serialize()),
				args:  []any{int64(27)}, limit: 100, max: 200,
				gas: 151, want: []any{int64(31), int64(-14), int64(31), int64(146)},
			},
		} {
			t.Run(fmt.Sprintf("%s/v%d", tc.name, version), func(t *testing.T) {
				code := prependRawMethodDrop(codeFromBuilders(t,
					execop.RUNVM(8|64).Serialize(), funcsop.GASCONSUMED().Serialize()))
				values := append([]any{}, tc.args...)
				values = append(values, int64(len(tc.args)), tc.child.MustBeginParse(), tc.limit, tc.max)
				res := assertFlowParityCase(t, tc.name, code, values, version, 10_000)
				if res.exitCode != 0 || res.gasUsed != tc.gas {
					t.Fatalf("exit/gas = %d/%d, want 0/%d", res.exitCode, res.gasUsed, tc.gas)
				}
				assertCrossSkippedGoStack(t, res.stack, tc.want)
			})
		}
	}
}

func TestTVMCrossEmulatorChildReturnGasBypassesTry(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skip(err)
	}

	body := codeFromBuilders(t, execop.RUNVM(8).Serialize())
	handler := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(777)).Serialize())
	code := prependRawMethodDrop(codeFromBuilders(t,
		stackop.PUSHCONT(body).Serialize(), stackop.PUSHCONT(handler).Serialize(), execop.TRY().Serialize(),
		stackop.PUSHINT(big.NewInt(55)).Serialize()))
	values := make([]any, 0, 36)
	for i := int64(1); i <= 33; i++ {
		values = append(values, i)
	}
	values = append(values, int64(33), testEmptyCell().MustBeginParse(), int64(100))

	// Child gas is charged before its 33 values are copied. At limit 160
	// the child finishes, but the extra one-gas return charge aborts the
	// parent despite its TRY handler. At 161 the copy succeeds and the
	// following implicit RET is the first instruction to exhaust gas.
	for _, version := range []int{4, 9, 10, vm.MaxSupportedGlobalVersion} {
		for _, tc := range []struct {
			name  string
			limit int64
			exit  int32
			gas   int64
		}{
			{name: "return_copy_overruns", limit: 160, exit: -14, gas: 161},
			{name: "copy_succeeds_next_return_overruns", limit: 161, exit: -14, gas: 166},
			{name: "exact_complete_budget", limit: 197, gas: 197},
		} {
			t.Run(fmt.Sprintf("%s/v%d", tc.name, version), func(t *testing.T) {
				res := assertFlowParityCase(t, tc.name, code, values, version, tc.limit)
				if res.exitCode != tc.exit || res.gasUsed != tc.gas {
					t.Fatalf("exit/gas = %d/%d, want %d/%d", res.exitCode, res.gasUsed, tc.exit, tc.gas)
				}
				want := []any{tc.gas}
				if tc.exit == 0 {
					want = append([]any{}, values[:33]...)
					want = append(want, int64(0), int64(5), int64(55))
				}
				assertCrossSkippedGoStack(t, res.stack, want)
			})
		}
	}
}

func TestTVMCrossEmulatorChildAutoCommitFailure(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skip(err)
	}

	deep := testEmptyCell()
	for i := 0; i < 513; i++ {
		deep = cell.BeginCell().MustStoreRef(deep).EndCell()
	}
	data := cell.BeginCell().MustStoreUInt(0xA1, 8).EndCell()
	for _, version := range []int{11, vm.MaxSupportedGlobalVersion} {
		for _, commit := range []bool{false, true} {
			for _, alt := range []bool{false, true} {
				t.Run(fmt.Sprintf("commit%t/alt%t/v%d", commit, alt, version), func(t *testing.T) {
					var builders []*cell.Builder
					if commit {
						builders = append(builders, funcsop.COMMIT().Serialize())
					}
					builders = append(builders, execop.POPCTR(4).Serialize())
					if alt {
						builders = append(builders, execop.RETALT().Serialize())
					} else {
						builders = append(builders, execop.RET().Serialize())
					}
					child := codeFromBuilders(t, builders...)
					code := prependRawMethodDrop(codeFromBuilders(t,
						execop.RUNVM(4|32|256).Serialize(), stackop.PUSHINT(big.NewInt(55)).Serialize()))
					// Automatic commit fails after an otherwise successful RET/RETALT.
					// That failure takes precedence over the requested two return values,
					// and the parent continues with the child's last explicit snapshot.
					res := assertFlowParityCase(t, t.Name(), code, []any{deep, int64(1), child.MustBeginParse(), int64(2), data}, version, 10_000)
					want := []any{int64(0), int64(8), nil, nil, int64(55)}
					gas := int64(175)
					if commit {
						want[2], want[3] = data, testEmptyCell()
						gas += 26
					}
					if res.exitCode != 0 || res.gasUsed != gas {
						t.Fatalf("exit/gas = %d/%d, want 0/%d", res.exitCode, res.gasUsed, gas)
					}
					assertCrossSkippedGoStack(t, res.stack, want)
				})
			}
		}
	}
}
