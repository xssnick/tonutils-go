//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTVMCrossEmulatorTransactionTotalMessageLimits(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	inMsg := transactionPhaseInternalMessage(t, body, 1_000_000_000, false, 0)

	tests := []struct {
		name       string
		msg        *cell.Cell
		firstMode  uint8
		limitCells bool
	}{
		{
			name:       "internal_cells",
			msg:        buildTransactionOutboundInternalCell(t, 100_000_000),
			firstMode:  1,
			limitCells: true,
		},
		{
			name:      "external_bits",
			msg:       buildTransactionExternalOutCell(t, address.NewAddressExt(0, 16, []byte{0xAB, 0xCD})),
			firstMode: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oneBits, oneCells := transactionSingleOutboundMessageUsage(t, tt.msg, tt.firstMode)
			maxTotalBits := ^uint32(0)
			maxTotalCells := ^uint32(0)
			if tt.limitCells {
				maxTotalCells = oneCells
			} else {
				maxTotalBits = oneBits
			}

			for _, tc := range []struct {
				name       string
				version    uint32
				secondMode uint8
				want       transactionActionPhaseExpectation
			}{
				{
					name:       "v14_not_enforced",
					version:    14,
					secondMode: tt.firstMode,
					want: transactionActionPhaseExpectation{
						success:         true,
						valid:           true,
						messagesCreated: 2,
					},
				},
				{
					name:       "v15_result_47",
					version:    15,
					secondMode: tt.firstMode,
					want: transactionActionPhaseExpectation{
						success:         false,
						valid:           true,
						resultCode:      47,
						messagesCreated: 1,
					},
				},
				{
					name:       "v15_mode_2_skips",
					version:    15,
					secondMode: 3,
					want: transactionActionPhaseExpectation{
						success:         true,
						valid:           true,
						skippedActions:  1,
						messagesCreated: 1,
					},
				},
			} {
				t.Run(tc.name, func(t *testing.T) {
					configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, tc.version)
					configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
						int32(tlb.ConfigParamSizeLimits): transactionTestSizeLimitsV3Cell(t, maxTotalBits, maxTotalCells),
					})
					actions := buildTransactionActionList(t,
						tlb.ActionSendMsg{Mode: tt.firstMode, Msg: tt.msg},
						tlb.ActionSendMsg{Mode: tc.secondMode, Msg: tt.msg},
					)
					code := makeTransactionInternalActionsCode(t, actions, newData)
					shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

					goRes, err := testEmulateTransaction(NewTVM(), shard, inMsg, testTxParams{
						Address:     tonopsTestAddr,
						Now:         now,
						BlockLT:     transactionTestLogicalTime,
						LogicalTime: transactionTestLogicalTime,
						RandSeed:    append([]byte(nil), tonopsTestSeed...),
						ConfigRoot:  configRoot,
					})
					if err != nil {
						t.Fatalf("go transaction emulation failed: %v", err)
					}

					refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, inMsg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
					if err != nil {
						t.Fatalf("reference transaction emulation failed: %v", err)
					}

					assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, tc.want)
					assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, tc.want)
					if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
						t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
					}
					if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
						t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
					}
				})
			}
		})
	}
}

func transactionSingleOutboundMessageUsage(t *testing.T, msg *cell.Cell, mode uint8) (uint32, uint32) {
	t.Helper()

	res := applyTransactionSendActionForTestWithParams(t, tlb.ActionSendMsg{Mode: mode, Msg: msg}, transactionTestConfigWithGlobalVersion(t, 15), big.NewInt(1_000_000_000), nil, transactionZeroCurrencyBalance())
	if res.phase == nil || !res.phase.Success {
		t.Fatalf("failed to measure outbound message usage: %+v", res.phase)
	}
	return uint32(res.phase.TotalMsgSize.Bits.Uint64()), uint32(res.phase.TotalMsgSize.Cells.Uint64())
}
