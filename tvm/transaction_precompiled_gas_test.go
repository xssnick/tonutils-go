package tvm

import (
	"math"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
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
					env := &transactionExecEnv{}
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
		{name: "above external gas limit", usage: 1},
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
			if !tc.wantAccept && result.TransactionCell != nil {
				t.Fatal("rejected external message produced a transaction")
			}
		})
	}
}
