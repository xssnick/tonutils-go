package tvm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTransactionStrictConfigRequiredAndExact(t *testing.T) {
	params := transactionReportStrictConfigParams(t)
	if _, err := PrepareBlockchainConfig(buildTransactionConfigRoot(t, params)); err != nil {
		t.Fatalf("valid strict config was rejected: %v", err)
	}

	for _, id := range []uint32{
		tlb.ConfigParamStoragePrices,
		tlb.ConfigParamGasPricesBasechain,
		tlb.ConfigParamGasPricesMasterchain,
		tlb.ConfigParamMsgForwardPricesBasechain,
		tlb.ConfigParamMsgForwardPricesMasterchain,
	} {
		t.Run(new(big.Int).SetUint64(uint64(id)).String()+"_required", func(t *testing.T) {
			missing := transactionReportCloneConfigParams(params)
			delete(missing, id)
			if _, err := PrepareBlockchainConfig(buildTransactionConfigRoot(t, missing)); err == nil {
				t.Fatalf("strict config accepted without param %d", id)
			}
		})
	}

	for _, id := range []uint32{
		tlb.ConfigParamGasPricesBasechain,
		tlb.ConfigParamMsgForwardPricesBasechain,
	} {
		t.Run(new(big.Int).SetUint64(uint64(id)).String()+"_exact", func(t *testing.T) {
			suffixed := transactionReportCloneConfigParams(params)
			suffixed[id] = transactionReportAppendBit(params[id])
			if _, err := PrepareBlockchainConfig(buildTransactionConfigRoot(t, suffixed)); err == nil {
				t.Fatalf("strict config accepted suffixed param %d", id)
			}
		})
	}

	sizeLimits := buildTransactionSizeLimitsCell(t, 1<<21, 1<<13, 1000, 1<<16, 1<<11)
	suffixedSize := transactionReportCloneConfigParams(params)
	suffixedSize[tlb.ConfigParamSizeLimits] = transactionReportAppendBit(sizeLimits)
	if _, err := PrepareBlockchainConfig(buildTransactionConfigRoot(t, suffixedSize)); err == nil {
		t.Fatal("strict config accepted a suffixed size-limits param")
	}

	storagePrice, err := tlb.ToCell(&tlb.ConfigStoragePrices{ValidSince: 1})
	if err != nil {
		t.Fatal(err)
	}
	storagePrices := cell.NewDict(32)
	if err = storagePrices.SetIntKey(big.NewInt(1), transactionReportAppendBit(storagePrice)); err != nil {
		t.Fatal(err)
	}
	suffixedStorage := transactionReportCloneConfigParams(params)
	suffixedStorage[tlb.ConfigParamStoragePrices] = storagePrices.AsCell()
	if _, err = PrepareBlockchainConfig(buildTransactionConfigRoot(t, suffixedStorage)); err == nil {
		t.Fatal("strict config accepted a suffixed storage-prices entry")
	}
}

func TestTransactionStrictConfigWithoutWorkchainsRejectsBasechainDestination(t *testing.T) {
	cfg, err := PrepareBlockchainConfig(buildTransactionConfigRoot(t, transactionReportStrictConfigParams(t)))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := transactionValidateAndNormalizeInternalDestAddr(tonopsTestAddr, cfg, tonopsTestAddr); ok {
		t.Fatal("strict config without param 12 accepted an undeclared basechain destination")
	}
}

func TestTransactionActionNodesRequireExactExhaustion(t *testing.T) {
	valid, err := tlb.ToCell(tlb.OutList{
		Prev: cell.BeginCell().EndCell(),
		Out:  tlb.ActionSetCode{NewCode: cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()},
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := transactionLoadActions(valid, 14)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.resultCode != 0 || len(loaded.actions) != 1 {
		t.Fatalf("valid action node result = %+v", loaded)
	}

	for _, tc := range []struct {
		name string
		root *cell.Cell
	}{
		{name: "trailing bit", root: transactionReportAppendBit(valid)},
		{name: "trailing ref", root: cell.BeginCell().MustStoreBuilder(valid.ToBuilder()).MustStoreRef(cell.BeginCell().EndCell()).EndCell()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loaded, err := transactionLoadActions(tc.root, 14)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.resultCode != 34 || len(loaded.actions) != 0 {
				t.Fatalf("suffixed action node result = %+v, want code 34", loaded)
			}
		})
	}
}

func TestTransactionAccountLastLTInvariant(t *testing.T) {
	for _, tc := range []struct {
		name      string
		storageLT uint64
		shardLT   uint64
		wantErr   bool
	}{
		{name: "initial", storageLT: 0, shardLT: 0},
		{name: "strictly after", storageLT: 2, shardLT: 1},
		{name: "equal", storageLT: 1, shardLT: 1, wantErr: true},
		{name: "behind", storageLT: 1, shardLT: 2, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := &tlb.AccountState{
				IsValid: true,
				Address: tonopsTestAddr,
				StorageInfo: tlb.StorageInfo{
					StorageUsed:  tlb.StorageUsed{CellsUsed: big.NewInt(0), BitsUsed: big.NewInt(0)},
					StorageExtra: tlb.StorageExtraNone{},
				},
				AccountStorage: tlb.AccountStorage{
					LastTransactionLT: tc.storageLT,
					Balance:           tlb.FromNanoTONU(1),
					Status:            tlb.AccountStatusUninit,
				},
			}
			account, err := state.ToCell()
			if err != nil {
				t.Fatal(err)
			}
			_, err = PrepareAccount(&tlb.ShardAccount{
				Account:       account,
				LastTransHash: make([]byte, 32),
				LastTransLT:   tc.shardLT,
			}, tonopsTestAddr)
			if (err != nil) != tc.wantErr {
				t.Fatalf("PrepareAccount error = %v, wantErr=%t", err, tc.wantErr)
			}
		})
	}
}

func TestTransactionActiveAccountWithoutCodeRunsVMFatal(t *testing.T) {
	now := uint32(tonopsTestTime.Unix())
	shard := buildTransactionTestShardAccount(
		t,
		tonopsTestAddr,
		nil,
		cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell(),
		2_000_000_000,
		now,
	)
	msgCell, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        cell.BeginCell().EndCell(),
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := testEmulateTransaction(NewTVM(), shard, msgCell, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		Config:      transactionTestConfigWithGlobalVersion(t, 14),
	})
	if err != nil {
		t.Fatal(err)
	}
	desc, ok := testResultTransaction(t, res).Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		t.Fatalf("transaction description = %T", testResultTransaction(t, res).Description)
	}
	phase, ok := desc.ComputePhase.Phase.(tlb.ComputePhaseVM)
	if !ok || phase.Details.ExitCode != ^int32(vmerr.CodeFatal) {
		t.Fatalf("compute phase = %+v, want VM fatal exit", desc.ComputePhase.Phase)
	}
}

func TestTransactionSpecialStorageDebtDoesNotChangeStatus(t *testing.T) {
	msg := &tlb.Message{
		MsgType: tlb.MsgTypeExternalIn,
		Msg:     &tlb.ExternalMessage{DstAddr: tonopsTestAddr},
	}
	limits := transactionStorageDueLimits{freezeDue: big.NewInt(1), deleteDue: big.NewInt(1)}
	cfg := transactionTestConfigWithGlobalVersion(t, 14)

	prepare := func(special bool) *transactionPreparedPhases {
		prepared, err := transactionPrepareInitialPhases(&transactionRuntimeAccount{
			addr:      tonopsTestAddr,
			status:    tlb.AccountStatusActive,
			balance:   big.NewInt(1),
			isSpecial: special,
			storageInfo: tlb.StorageInfo{
				StorageExtra: tlb.StorageExtraNone{},
				DuePayment:   transactionCoinsPtr(big.NewInt(7)),
			},
		}, msg, big.NewInt(10), big.NewInt(0), 100, cfg, limits)
		if err != nil {
			t.Fatal(err)
		}
		return prepared
	}

	special := prepare(true)
	if special.status != tlb.AccountStatusActive || special.storagePhase.StatusChange.Type != tlb.AccStatusChangeUnchanged {
		t.Fatalf("special account status = %s/%s", special.status, special.storagePhase.StatusChange.Type)
	}
	if due := transactionCoinsNano(special.duePayment); due == nil || due.Cmp(big.NewInt(7)) != 0 {
		t.Fatalf("special account due payment = %v, want original 7", due)
	}

	ordinary := prepare(false)
	if ordinary.status != tlb.AccountStatusFrozen || ordinary.storagePhase.StatusChange.Type != tlb.AccStatusChangeFrozen {
		t.Fatalf("ordinary account status = %s/%s", ordinary.status, ordinary.storagePhase.StatusChange.Type)
	}
}

func TestTransactionEmptyBlockSeedGeneratesBits256(t *testing.T) {
	block, err := transactionTestConfigWithGlobalVersion(t, 14).NewBlockContext(BlockOptions{Now: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(block.randSeed) != 32 {
		t.Fatalf("generated block seed length = %d, want 32", len(block.randSeed))
	}
}

func TestTransactionC7StorageFeesPreserveWideInteger(t *testing.T) {
	wide := new(big.Int).Or(new(big.Int).Lsh(big.NewInt(1), 80), big.NewInt(17))
	cfg := transactionTestConfigWithGlobalVersion(t, 4)
	block, err := cfg.NewBlockContext(BlockOptions{
		Now:      1,
		BlockLT:  2,
		RandSeed: append([]byte(nil), tonopsTestSeed...),
	})
	if err != nil {
		t.Fatal(err)
	}
	env := newTransactionExecEnv(block, &TransactionOptions{}, &transactionRuntimeAccount{addr: tonopsTestAddr}, nil, nil, &transactionPreparedPhases{
		balance:    big.NewInt(0),
		msgBalance: transactionZeroCurrencyBalance(),
		storagePhase: &tlb.StoragePhase{
			StorageFeesCollected: tlb.FromNanoTON(wide),
		},
	}, 3)
	if env.storageFees.Cmp(wide) != 0 {
		t.Fatalf("execution environment storage fees = %s, want %s", env.storageFees, wide)
	}
	input, err := env.c7Input(cell.BeginCell().EndCell(), big.NewInt(0))
	if err != nil {
		t.Fatal(err)
	}
	c7, err := buildEmulationC7(input, nil)
	if err != nil {
		t.Fatal(err)
	}
	innerRaw, err := c7.RawIndex(0)
	if err != nil {
		t.Fatal(err)
	}
	inner := innerRaw.(tuple.Tuple)
	gotRaw, err := inner.RawIndex(12)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := gotRaw.(*big.Int)
	if !ok || got.Cmp(wide) != 0 {
		t.Fatalf("c7 storage fees = %T(%v), want %s", gotRaw, gotRaw, wide)
	}
}

func TestTransactionSendMode128ExtraCurrencyBoundary(t *testing.T) {
	for _, tc := range []struct {
		version       uint32
		wantOutbound  uint64
		wantRemaining uint64
	}{
		{version: 9, wantOutbound: 11},
		{version: 10, wantRemaining: 11},
	} {
		t.Run(new(big.Int).SetUint64(uint64(tc.version)).String(), func(t *testing.T) {
			extra := makeTransactionExtraCurrencies(t, 7, 11)
			out := applyTransactionSendActionForTestWithParams(t, tlb.ActionSendMsg{
				Mode: 128,
				Msg:  buildTransactionOutboundInternalCell(t, 0),
			}, transactionTestConfigWithGlobalVersion(t, tc.version), big.NewInt(1_000), extra, transactionZeroCurrencyBalance())
			if out.phase == nil || !out.phase.Success || len(out.outMsgs) != 1 {
				t.Fatalf("action result = phase=%+v out=%d", out.phase, len(out.outMsgs))
			}
			outExtra, err := transactionLoadExtraCurrencies(out.outMsgs[0].Msg.AsInternal().ExtraCurrencies)
			if err != nil {
				t.Fatal(err)
			}
			if got := transactionReportExtraAmount(outExtra, 7); got != tc.wantOutbound {
				t.Fatalf("v%d outbound extra = %d, want %d", tc.version, got, tc.wantOutbound)
			}
			remaining, err := transactionLoadExtraCurrencies(out.extraCurrencies)
			if err != nil {
				t.Fatal(err)
			}
			if got := transactionReportExtraAmount(remaining, 7); got != tc.wantRemaining {
				t.Fatalf("v%d remaining extra = %d, want %d", tc.version, got, tc.wantRemaining)
			}
		})
	}
}

func TestTransactionInvalidBounceDestinationReturnsError(t *testing.T) {
	workchains, err := tlb.ToCell(&tlb.WorkchainsConfig{Workchains: cell.NewDict(32)})
	if err != nil {
		t.Fatal(err)
	}
	cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion: transactionTestGlobalVersionCell(t, 14),
		tlb.ConfigParamWorkchains:    workchains,
	})
	msg := &tlb.Message{
		MsgType: tlb.MsgTypeInternal,
		Msg: &tlb.InternalMessage{
			Bounce:  true,
			SrcAddr: tonopsTestAddr,
			DstAddr: tonopsTestAddr,
		},
	}
	res, err := transactionPrepareBouncePhase(
		msg,
		tonopsTestAddr,
		big.NewInt(1),
		nil,
		&transactionCurrencyBalance{grams: big.NewInt(1), extra: map[uint32]*big.Int{}},
		big.NewInt(0),
		big.NewInt(0),
		1,
		1,
		0,
		cfg,
		nil,
		nil,
		nil,
	)
	if err == nil || res != nil {
		t.Fatalf("invalid bounce destination result = %+v, err=%v", res, err)
	}
}

func TestTransactionLayoutRetryRepeatsAbsoluteSizeChecks(t *testing.T) {
	prices, err := tlb.ToCell(&tlb.ConfigMsgForwardPrices{
		LumpPrice: 400_000,
		CellPrice: 4 << 16,
		FirstFrac: 21_845,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion:               transactionTestGlobalVersionCell(t, 14),
		tlb.ConfigParamWorkchains:                  buildTransactionResult39WorkchainsConfig(t),
		tlb.ConfigParamMsgForwardPricesBasechain:   prices,
		tlb.ConfigParamMsgForwardPricesMasterchain: prices,
		tlb.ConfigParamSizeLimits:                  buildTransactionSizeLimitsCell(t, 1<<21, 2, 1000, 1<<16, 1<<11),
	})
	out := applyTransactionSendActionForTestWithParams(t, tlb.ActionSendMsg{
		Mode: 1,
		Msg:  buildTransactionResult39OutboundMessage(t),
	}, cfg, new(big.Int).SetUint64((1<<56)+1_000_000_000), nil, transactionZeroCurrencyBalance())
	if out.phase == nil || out.phase.ResultCode != 40 {
		t.Fatalf("layout retry result = %+v, want size-limit code 40", out.phase)
	}
}

func TestTransactionStorageStatHintRequiresAuthenticatedHash(t *testing.T) {
	storage := cell.BeginCell().MustStoreUInt(0xA, 4).
		MustStoreRef(cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()).
		EndCell()
	usage, dictRoot, err := transactionComputeAccountStorageStat(storage)
	if err != nil {
		t.Fatal(err)
	}
	storageUsed := tlb.StorageUsed{
		CellsUsed: new(big.Int).SetUint64(usage.cells),
		BitsUsed:  new(big.Int).SetUint64(usage.bits),
	}
	for _, hash := range [][]byte{nil, make([]byte, 32)} {
		stat, err := transactionInitAccountStorageStat(dictRoot, storage, storageUsed, hash)
		if err != nil {
			t.Fatal(err)
		}
		if stat != nil {
			t.Fatal("unauthenticated account storage stat hint was reused")
		}
	}
	wrong := make([]byte, 32)
	wrong[0] = 1
	if _, err = transactionInitAccountStorageStat(dictRoot, storage, storageUsed, wrong); err == nil {
		t.Fatal("account storage stat hint with a mismatching hash was accepted")
	}
	hash := dictRoot.HashKey()
	stat, err := transactionInitAccountStorageStat(dictRoot, storage, storageUsed, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	if stat == nil {
		t.Fatal("authenticated account storage stat hint was ignored")
	}
}

func transactionReportStrictConfigParams(t *testing.T) map[uint32]*cell.Cell {
	t.Helper()

	gas, err := (&tlb.ConfigGasLimitsPrices{
		GasLimit:        1,
		SpecialGasLimit: 1,
		BlockGasLimit:   1,
	}).ToCell()
	if err != nil {
		t.Fatal(err)
	}
	forward, err := tlb.ToCell(&tlb.ConfigMsgForwardPrices{})
	if err != nil {
		t.Fatal(err)
	}
	storagePrice, err := tlb.ToCell(&tlb.ConfigStoragePrices{ValidSince: 1})
	if err != nil {
		t.Fatal(err)
	}
	storagePrices := cell.NewDict(32)
	if err = storagePrices.SetIntKey(big.NewInt(1), storagePrice); err != nil {
		t.Fatal(err)
	}
	return map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion:               transactionTestGlobalVersionCell(t, 14),
		tlb.ConfigParamStoragePrices:               storagePrices.AsCell(),
		tlb.ConfigParamGasPricesBasechain:          gas,
		tlb.ConfigParamGasPricesMasterchain:        gas,
		tlb.ConfigParamMsgForwardPricesBasechain:   forward,
		tlb.ConfigParamMsgForwardPricesMasterchain: forward,
	}
}

func transactionReportCloneConfigParams(params map[uint32]*cell.Cell) map[uint32]*cell.Cell {
	out := make(map[uint32]*cell.Cell, len(params))
	for id, param := range params {
		out[id] = param
	}
	return out
}

func transactionReportAppendBit(root *cell.Cell) *cell.Cell {
	return cell.BeginCell().MustStoreBuilder(root.ToBuilder()).MustStoreBoolBit(true).EndCell()
}

func transactionReportExtraAmount(extra map[uint32]*big.Int, id uint32) uint64 {
	if extra[id] == nil {
		return 0
	}
	return extra[id].Uint64()
}
