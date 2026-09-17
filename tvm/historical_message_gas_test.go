package tvm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestHistoricalMessageGasAfterStorage(t *testing.T) {
	prices := tlb.ConfigGasLimitsPrices{
		GasPrice:      10_000 << 16,
		GasLimit:      1_000_000,
		BlockGasLimit: 1_000_000,
	}
	cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion:      transactionTestGlobalVersionCell(t, 0),
		tlb.ConfigParamGasPricesBasechain: transactionFeesGasPricesCell(t, prices),
	})
	for _, test := range []struct {
		name       string
		historical bool
		bounce     bool
		balance    int64
		wantMsg    int64
		wantLimit  int64
		wantMax    int64
	}{
		{name: "modern credit first", wantMsg: 4_999_992_053, wantLimit: 499_999, wantMax: 499_999},
		{name: "historical credit first", historical: true, wantMsg: 5_000_000_000, wantLimit: 500_000, wantMax: 499_999},
		{name: "modern storage first", bounce: true, balance: 10_000, wantMsg: 5_000_000_000, wantLimit: 500_000, wantMax: 500_000},
		{name: "historical storage first", historical: true, bounce: true, balance: 10_000, wantMsg: 5_000_000_000, wantLimit: 500_000, wantMax: 500_000},
	} {
		t.Run(test.name, func(t *testing.T) {
			acc := &transactionRuntimeAccount{
				addr:    tonopsTestAddr,
				status:  tlb.AccountStatusActive,
				balance: big.NewInt(test.balance),
			}
			msg := &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: &tlb.InternalMessage{
				Bounce: test.bounce, DstAddr: tonopsTestAddr, Amount: tlb.FromNanoTONU(5_000_000_000),
			}}
			prepared, err := transactionPrepareInitialPhases(acc, msg, big.NewInt(7947), nil, 1, cfg, transactionStorageDueLimits{}, test.historical)
			if err != nil {
				t.Fatal(err)
			}
			if got := prepared.msgBalance.grams.Int64(); got != test.wantMsg {
				t.Fatalf("message balance = %d, want %d", got, test.wantMsg)
			}
			if got, want := prepared.balance.Int64(), test.balance+5_000_000_000-7947; got != want {
				t.Fatalf("account balance = %d, want %d", got, want)
			}
			if got := prepared.storagePhase.StorageFeesCollected.Nano().Int64(); got != 7947 {
				t.Fatalf("storage fees = %d, want 7947", got)
			}
			if got := prepared.creditPhase.Credit.Coins.Nano().Int64(); got != 5_000_000_000 {
				t.Fatalf("credited amount = %d, want 5000000000", got)
			}
			gas := transactionMessageGas(vm.Gas{}, 1, cfg, acc.addr, prepared.balance, prepared.msgBalance.grams, msg.MsgType, false, test.historical)
			if gas.Max != test.wantMax || gas.Limit != test.wantLimit || gas.Base != test.wantLimit || gas.Remaining != test.wantLimit || gas.Credit != 0 {
				t.Fatalf("gas = %+v, want maximum %d, initial limit %d", gas, test.wantMax, test.wantLimit)
			}
			if acc.balance.Int64() != test.balance || msg.AsInternal().Amount.Nano().Int64() != 5_000_000_000 {
				t.Fatal("phase preparation mutated its account or message input")
			}
		})
	}
}

func TestHistoricalMessageGasUsesConfiguredPrices(t *testing.T) {
	prices := tlb.ConfigGasLimitsPrices{
		HasFlatPricing:          true,
		FlatGasLimit:            100,
		FlatGasPrice:            1000,
		GasPrice:                10_000 << 16,
		GasLimit:                1000,
		SpecialGasLimit:         75,
		HasSeparateSpecialLimit: true,
		GasCredit:               200,
		BlockGasLimit:           1000,
	}
	cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion:      transactionTestGlobalVersionCell(t, 0),
		tlb.ConfigParamGasPricesBasechain: transactionFeesGasPricesCell(t, prices),
	})
	for _, test := range []struct {
		name       string
		balance    int64
		msgBalance int64
		external   bool
		special    bool
		wantMax    int64
		wantLimit  int64
		wantCredit int64
	}{
		{name: "flat and variable price", balance: 10_000, msgBalance: 20_000, wantMax: 100, wantLimit: 101},
		{name: "configured limit", balance: 10_000, msgBalance: 20_000_000, wantMax: 100, wantLimit: 1000},
		{name: "below flat price", balance: 10_000, msgBalance: 999, wantMax: 100},
		{name: "external credit", balance: 10_000, external: true, wantMax: 100, wantCredit: 100},
		{name: "special initial limit", balance: 10_000, msgBalance: 20_000, special: true, wantMax: 75, wantLimit: 101},
		{name: "special external credit", balance: 10_000, external: true, special: true, wantMax: 75, wantCredit: 75},
	} {
		t.Run(test.name, func(t *testing.T) {
			msgType := tlb.MsgTypeInternal
			if test.external {
				msgType = tlb.MsgTypeExternalIn
			}
			gas := transactionMessageGas(vm.Gas{}, 1, cfg, tonopsTestAddr, big.NewInt(test.balance), big.NewInt(test.msgBalance), msgType, test.special, true)
			if gas.Max != test.wantMax || gas.Limit != test.wantLimit || gas.Credit != test.wantCredit || gas.Remaining != test.wantLimit+test.wantCredit {
				t.Fatalf("gas = %+v, want max/limit/credit %d/%d/%d", gas, test.wantMax, test.wantLimit, test.wantCredit)
			}
		})
	}

	override := transactionGasFromLimits(90, 80, 7)
	if got := transactionMessageGas(override, 1, cfg, tonopsTestAddr, big.NewInt(10_000), big.NewInt(20_000), tlb.MsgTypeInternal, false, true); got != override {
		t.Fatalf("explicit gas override = %+v, want %+v", got, override)
	}
}

func TestHistoricalMessageGasLimitInstructionsKeepMaximum(t *testing.T) {
	prices := tlb.ConfigGasLimitsPrices{
		GasPrice:      10_000 << 16,
		GasLimit:      1_000_000,
		BlockGasLimit: 1_000_000,
	}
	cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion:      transactionTestGlobalVersionCell(t, 0),
		tlb.ConfigParamGasPricesBasechain: transactionFeesGasPricesCell(t, prices),
	})
	initial := transactionMessageGas(vm.Gas{}, 1, cfg, tonopsTestAddr, big.NewInt(4_999_992_053), big.NewInt(5_000_000_000), tlb.MsgTypeInternal, false, true)
	if initial.Limit != 500_000 || initial.Max != 499_999 {
		t.Fatalf("initial gas = %+v, want limit 500000 and maximum 499999", initial)
	}
	for _, instruction := range []string{"ACCEPT", "SETGASLIMIT"} {
		t.Run(instruction, func(t *testing.T) {
			state := vm.NewExecutionState(0, initial, nil, tuple.Tuple{}, vm.NewStack())
			if err := state.ConsumeGas(404); err != nil {
				t.Fatal(err)
			}
			op := funcsop.ACCEPT()
			if instruction == "SETGASLIMIT" {
				if err := state.Stack.PushSmallInt(500_000); err != nil {
					t.Fatal(err)
				}
				op = funcsop.SETGASLIMIT()
			}
			if err := op.Interpret(state); err != nil {
				t.Fatal(err)
			}
			if state.Gas.Max != 499_999 || state.Gas.Limit != 499_999 || state.Gas.Remaining != 499_595 || state.Gas.Used() != 404 {
				t.Fatalf("gas after %s = %+v, want maximum and limit 499999 with 404 consumed", instruction, state.Gas)
			}
		})
	}
}
