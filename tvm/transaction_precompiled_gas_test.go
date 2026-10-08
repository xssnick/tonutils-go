package tvm

import (
	"errors"
	"math"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
)

func TestTransactionPrecompiledConfigGasBoundaries(t *testing.T) {
	code := cell.BeginCell().MustStoreUInt(0xA4, 8).EndCell()
	prices, err := tlb.ToCell(&tlb.ConfigGasLimitsPrices{
		HasSeparateSpecialLimit: true,
		GasPrice:                1,
		GasLimit:                1_000,
		SpecialGasLimit:         2_000,
		BlockGasLimit:           2_000,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name     string
		usage    uint64
		limit    int64
		wantSkip bool
	}{
		{name: "zero", usage: 0, limit: 10},
		{name: "zero limit", usage: 0, limit: 0},
		{name: "equal", usage: 10, limit: 10},
		{name: "one above", usage: 11, limit: 10, wantSkip: true},
		{name: "maximum signed equal", usage: math.MaxInt64, limit: math.MaxInt64},
		{name: "high bit", usage: 1 << 63, limit: math.MaxInt64, wantSkip: true},
		{name: "maximum unsigned", usage: math.MaxUint64, limit: math.MaxInt64, wantSkip: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
				tlb.ConfigParamPrecompiledContracts: buildTransactionV13PrecompiledConfig(t, code, tc.usage),
				tlb.ConfigParamGasPricesBasechain:   prices,
				tlb.ConfigParamGasPricesMasterchain: prices,
			})
			for _, account := range []struct {
				name      string
				address   *address.Address
				special   bool
				wantLimit int64
			}{
				{name: "ordinary", address: tonopsTestAddr, wantLimit: 1_000},
				{name: "special", address: internalEmulationSrcAddr, special: true, wantLimit: 2_000},
			} {
				t.Run(account.name, func(t *testing.T) {
					gas := vmcore.Gas{Max: tc.limit, Limit: tc.limit, Base: tc.limit, Remaining: tc.limit}
					env := &transactionExecEnv{msg: &tlb.Message{MsgType: tlb.MsgTypeInternal}}
					got, skip := transactionApplyPrecompiledGasConfig(config, code, account.address, account.special, gas, env)
					if env.precompiledGasUsage == nil || env.precompiledGasUsage.Uint64() != tc.usage {
						t.Fatalf("precompiled gas usage = %v, want %d", env.precompiledGasUsage, tc.usage)
					}
					if tc.wantSkip {
						if skip == nil || skip.Type != tlb.ComputeSkipReasonNoGas || got != gas {
							t.Fatalf("gas = %+v, skip = %v, want unchanged gas and no_gas", got, skip)
						}
						return
					}
					if skip != nil || got.Limit != account.wantLimit || got.Max != account.wantLimit {
						t.Fatalf("gas = %+v, skip = %v, want fallback limit %d", got, skip, account.wantLimit)
					}
				})
			}
		})
	}
}

func TestEmulateTransactionPrecompiledUint64Gas(t *testing.T) {
	now := uint32(tonopsTestTime.Unix())
	data := cell.BeginCell().EndCell()
	code := makeTransactionInternalSuccessCode(t, data)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, data, walletSendTestBalance, now)
	msg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        data,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name     string
		usage    uint64
		wantSkip bool
	}{
		{name: "zero", usage: 0},
		{name: "within limit", usage: 7},
		{name: "maximum signed", usage: math.MaxInt64, wantSkip: true},
		{name: "high bit", usage: 1 << 63, wantSkip: true},
		{name: "maximum unsigned", usage: math.MaxUint64, wantSkip: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
				tlb.ConfigParamPrecompiledContracts: buildTransactionV13PrecompiledConfig(t, code, tc.usage),
				tlb.ConfigParamGasPricesBasechain:   buildTransactionGasLimitsCell(t, 100, 10_000),
			})
			result, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     tonopsTestAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    tonopsTestSeed,
				Config:      config,
			})
			if err != nil {
				t.Fatalf("emulate transaction: %v", err)
			}
			desc := testResultTransaction(t, result).Description.(tlb.TransactionDescriptionOrdinary)
			if tc.wantSkip {
				skipped, ok := desc.ComputePhase.Phase.(tlb.ComputePhaseSkipped)
				if !ok || skipped.Reason.Type != tlb.ComputeSkipReasonNoGas {
					t.Fatalf("compute phase = %#v, want no_gas", desc.ComputePhase.Phase)
				}
				return
			}
			if !result.Accepted || uint64(result.GasUsed) != tc.usage {
				t.Fatalf("accepted = %v, gas = %d, want accepted and gas %d", result.Accepted, result.GasUsed, tc.usage)
			}
		})
	}
}

func TestTransactionPrecompiledMessageGasAllowance(t *testing.T) {
	code := cell.BeginCell().EndCell()
	prices, err := tlb.ToCell(&tlb.ConfigGasLimitsPrices{
		HasSeparateSpecialLimit: true,
		GasPrice:                1 << 16,
		GasLimit:                1_000,
		SpecialGasLimit:         2_000,
		BlockGasLimit:           2_000,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name     string
		msgType  tlb.MsgType
		usage    uint64
		credit   int64
		special  bool
		wide     bool
		wantSkip bool
	}{
		{name: "internal uses initial limit", msgType: tlb.MsgTypeInternal, usage: 11, wantSkip: true},
		{name: "internal with credit still uses initial limit", msgType: tlb.MsgTypeInternal, usage: 11, credit: 1, wantSkip: true},
		{name: "external without credit uses maximum", msgType: tlb.MsgTypeExternalIn, usage: 100},
		{name: "external at maximum", msgType: tlb.MsgTypeExternalIn, usage: 100, credit: 1},
		{name: "external over maximum", msgType: tlb.MsgTypeExternalIn, usage: 101, credit: 1, wantSkip: true},
		{name: "special external fallback", msgType: tlb.MsgTypeExternalIn, usage: 100, credit: 1, special: true},
		{name: "special unsigned allowance", msgType: tlb.MsgTypeExternalIn, usage: 1, special: true, wide: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
				tlb.ConfigParamPrecompiledContracts: buildTransactionV13PrecompiledConfig(t, code, tc.usage),
				tlb.ConfigParamGasPricesBasechain:   prices,
			})
			gas := vmcore.Gas{Max: 100, Limit: 10, Credit: tc.credit, Base: 10 + tc.credit, Remaining: 10 + tc.credit}
			if tc.wide {
				gas.Max, gas.Limit = -1, -1
				gas.Base, gas.Remaining = -1, -1
			}
			env := &transactionExecEnv{msg: &tlb.Message{MsgType: tc.msgType}}
			got, skip := transactionApplyPrecompiledGasConfig(cfg, code, tonopsTestAddr, tc.special, gas, env)
			if tc.wantSkip {
				if skip == nil || skip.Type != tlb.ComputeSkipReasonNoGas || got != gas {
					t.Fatalf("gas = %+v, skip = %v, want unchanged gas and no_gas", got, skip)
				}
				return
			}

			max := int64(1_000)
			if tc.special {
				max = 2_000
			}
			want := vmcore.Gas{Max: max, Limit: max, Base: max, Remaining: max}
			if tc.credit != 0 {
				want.Limit, want.Credit = 0, max
			}
			if skip != nil || got != want {
				t.Fatalf("gas = %+v, skip = %v, want %+v and no skip", got, skip, want)
			}
		})
	}
}

func TestCheckExternalMessageAcceptedPrecompiledUint64Gas(t *testing.T) {
	now := uint32(tonopsTestTime.Unix())
	data := cell.BeginCell().EndCell()
	code := makeTransactionExternalSuccessCode(t, data)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, data, walletSendTestBalance, now)
	msg, err := tlb.ToCell(&tlb.ExternalMessage{DstAddr: tonopsTestAddr, Body: data})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name       string
		usage      uint64
		wantAccept bool
	}{
		{name: "zero", usage: 0, wantAccept: true},
		{name: "positive usage", usage: 1, wantAccept: true},
		{name: "equal to maximum", usage: 1_000_000, wantAccept: true},
		{name: "above maximum", usage: 1_000_001},
		{name: "high bit", usage: 1 << 63},
		{name: "maximum unsigned", usage: math.MaxUint64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
				tlb.ConfigParamPrecompiledContracts: buildTransactionV13PrecompiledConfig(t, code, tc.usage),
				tlb.ConfigParamGasPricesBasechain:   buildTransactionGasLimitsCell(t, 100, 10_000),
			})
			params := testTxParams{
				Address:     tonopsTestAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    tonopsTestSeed,
				Config:      config,
			}
			accepted, err := testCheckExternalMessageAccepted(NewTVM(), shard, msg, params)
			if err != nil || accepted != tc.wantAccept {
				t.Fatalf("accept check = %v, %v, want %v", accepted, err, tc.wantAccept)
			}
			result, err := testEmulateTransaction(NewTVM(), shard, msg, params)
			if err != nil || result.Accepted != tc.wantAccept {
				t.Fatalf("transaction = %+v, %v, want accepted %v", result, err, tc.wantAccept)
			}
			if tc.wantAccept && (result.GasUsed != int64(tc.usage) || result.Steps != 0) {
				t.Fatalf("gas/steps = %d/%d, want %d/0", result.GasUsed, result.Steps, tc.usage)
			}
			if !tc.wantAccept && result.TransactionCell != nil {
				t.Fatal("rejected external message produced a transaction")
			}
		})
	}
}

func TestExternalPrecompiledFallbackGasCredit(t *testing.T) {
	now := uint32(tonopsTestTime.Unix())
	data := cell.BeginCell().EndCell()
	msg, err := tlb.ToCell(&tlb.ExternalMessage{DstAddr: tonopsTestAddr, Body: data})
	if err != nil {
		t.Fatal(err)
	}
	prices := buildTransactionPrecompiledExternalGasPrices(t, 100)

	for _, tc := range []struct {
		name       string
		nops       int
		accept     bool
		wantAccept bool
		wantOOG    bool
	}{
		{name: "accept within fallback credit", nops: 26, accept: true, wantAccept: true},
		{name: "return without accept", nops: 26},
		{name: "exhaust credit before accept", nops: 28, wantOOG: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code := makeTransactionPrecompiledExternalCode(t, tc.nops, tc.accept)
			cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
				tlb.ConfigParamPrecompiledContracts:      buildTransactionV13PrecompiledConfig(t, code, 1),
				tlb.ConfigParamGasPricesBasechain:        prices,
				tlb.ConfigParamMsgForwardPricesBasechain: buildTransactionMsgForwardPricesCell(t, 0, 0),
			})
			shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, data, 5_000, now)
			params := testTxParams{
				Address:     tonopsTestAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    tonopsTestSeed,
				Config:      cfg,
			}
			accepted, acceptErr := testCheckExternalMessageAccepted(NewTVM(), shard, msg, params)
			res, txErr := testEmulateTransaction(NewTVM(), shard, msg, params)
			if tc.wantOOG {
				if !errors.Is(acceptErr, errPrecompiledOutOfGas) || !errors.Is(txErr, errPrecompiledOutOfGas) || accepted || res != nil {
					t.Fatalf("accept check = %t/%v, transaction = %+v/%v, want precompiled out-of-gas in both paths", accepted, acceptErr, res, txErr)
				}
				return
			}
			if acceptErr != nil || txErr != nil || accepted != tc.wantAccept || res.Accepted != tc.wantAccept {
				t.Fatalf("accept check = %t/%v, transaction = %+v/%v, want accepted %t", accepted, acceptErr, res, txErr, tc.wantAccept)
			}
			if tc.wantAccept {
				if res.TransactionCell == nil || res.GasUsed != 1 || res.Steps != 0 {
					t.Fatalf("transaction = %+v, want committed transaction with configured gas 1 and steps 0", res)
				}
			} else if res.TransactionCell != nil {
				t.Fatal("return on credit without ACCEPT produced a transaction")
			}
		})
	}
}

func buildTransactionPrecompiledExternalGasPrices(t *testing.T, credit uint64) *cell.Cell {
	t.Helper()
	prices, err := tlb.ToCell(&tlb.ConfigGasLimitsPrices{
		HasSeparateSpecialLimit: true,
		GasPrice:                1 << 16,
		GasLimit:                500,
		SpecialGasLimit:         800,
		GasCredit:               credit,
		BlockGasLimit:           1_000,
		FreezeDueLimit:          100,
		DeleteDueLimit:          10_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	return prices
}

func makeTransactionPrecompiledExternalCode(t *testing.T, nops int, accept bool) *cell.Cell {
	t.Helper()
	b := cell.BeginCell()
	for i := 0; i < nops; i++ {
		b.MustStoreBuilder(stackop.NOP().Serialize())
	}
	if accept {
		b.MustStoreBuilder(funcsop.ACCEPT().Serialize())
	}
	return b.EndCell()
}
