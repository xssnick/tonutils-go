package tvm

import (
	"encoding/hex"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestHistoricalReplayAdditionalWitnesses(t *testing.T) {
	cases := []struct {
		name        string
		file        string
		inclusionMC uint32
		master      uint32
		block       uint32
		now         uint32
		lt          uint64
		txHash      string
		options     TransactionOptions
	}{
		{
			name: "message_gas_630388", file: "message-gas-630388.json",
			inclusionMC: 630388, master: 630387, block: 630388, now: 1575930345, lt: 882128000003,
			txHash: "f50dd8580d1b87c4cb46c0fa3dec3357ab175dfdb0dc727ce79a40f9b9810746",
			options: TransactionOptions{
				Historical:           vm.HistoricalConfig{GasSchedule: vm.GasSchedule2019},
				HistoricalMessageGas: true,
			},
		},
		{
			name: "no_prng_756356", file: "no-prng-756356.json",
			inclusionMC: 756356, master: 756354, block: 991383, now: 1576380411, lt: 1071032000001,
			txHash: "7d12c0f6f276959d6d22423edee1d20a09473515cb5c0d167d506cda36815622",
			options: TransactionOptions{
				Historical: vm.HistoricalConfig{GasSchedule: vm.GasSchedule2019, NoPRNG: true},
			},
		},
		{
			name: "external_state_init_1688013", file: "external-state-init-1688013.json",
			inclusionMC: 1688013, master: 1688011, block: 2221626, now: 1579782938, lt: 2467204000001,
			txHash: "60b2321fd1dbf1108dbaa0d20c64dab02fa56da8908956d9955e3d799bc0e502",
			options: TransactionOptions{
				Historical:                  vm.HistoricalConfig{GasSchedule: vm.GasSchedule2019},
				HistoricalExternalStateInit: true,
			},
		},
		{
			name: "no_blkdrop2_2221421", file: "no-blkdrop2-2221421.json",
			inclusionMC: 2221421, master: 2221418, block: 2928657, now: 1581755062, lt: 3274218000001,
			txHash: "de355529d9aca75c5c43407dbe4a4362af8df69dc94af9f2383ec789e34861f8",
			options: TransactionOptions{
				Historical: vm.HistoricalConfig{GasSchedule: vm.GasScheduleEarly2020, NoBLKDROP2: true},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var fixture historicalAccountFixture
			historicalParseJSON(t, historicalReadFile(t, tc.file), &fixture)
			block := historicalLoadBlock(t, fixture.Source)
			historicalVerifyParent(t, block, fixture.Master)
			if block.BlockInfo.SeqNo != tc.block || fixture.Master.SeqNo != tc.master ||
				block.BlockInfo.GenUtime != tc.now || fixture.Source.Pointer.InclusionMasterSeqno != tc.inclusionMC {
				t.Fatal("wrong block or execution context identity")
			}
			ctx := historicalFixtureBlockContext(t, fixture, block)
			for _, account := range historicalBlockAccounts(t, block) {
				if hex.EncodeToString(account.Account) != fixture.Account {
					continue
				}
				if len(account.Txs) != 1 || account.Txs[0].Tx.LT != tc.lt ||
					hex.EncodeToString(account.Txs[0].Cell.Hash(0)) != tc.txHash {
					t.Fatal("wrong original transaction witness")
				}
				if tc.options.HistoricalExternalStateInit {
					var before tlb.ShardAccount
					historicalParseTLB(t, &before, historicalParseCell(t, fixture.Before))
					var state tlb.AccountState
					historicalParseTLB(t, &state, before.Account)
					message := account.Txs[0].Tx.IO.In.Msg.(*tlb.ExternalMessage)
					if !state.IsValid || state.Status != tlb.AccountStatusActive || message.StateInit == nil {
						t.Fatal("expected an active account and an external StateInit")
					}
					init, err := tlb.ToCell(message.StateInit)
					if err != nil {
						t.Fatal(err)
					}
					if hex.EncodeToString(init.Hash(0)) != "3f078d3b7e22c8944e5561909a236ae48b48a7ea42f28dd861c22b6f64d7e97b" {
						t.Fatal("expected the original empty StateInit")
					}
				}
				historicalReplayAccount(t, ctx, account, fixture.Before, tc.options)
				return
			}
			t.Fatal("account witness missing")
		})
	}
}
