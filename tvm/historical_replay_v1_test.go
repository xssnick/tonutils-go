package tvm

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
)

func TestHistoricalReplayMessageGasVersionOne(t *testing.T) {
	var fixture historicalAccountFixture
	historicalParseJSON(t, historicalReadFile(t, "message-gas-v1-2937658.json"), &fixture)
	block := historicalLoadBlock(t, fixture.Source)
	historicalVerifyParent(t, block, fixture.Master)
	if block.BlockInfo.SeqNo != 3961312 || fixture.Master.SeqNo != 2937655 ||
		block.BlockInfo.GenUtime != 1585214300 || fixture.Source.Pointer.InclusionMasterSeqno != 2937658 ||
		fixture.Source.Pointer.ID.Workchain != 0 || uint64(fixture.Source.Pointer.ID.Shard) != 0xa000000000000000 ||
		fixture.Config.GlobalVersion != 1 {
		t.Fatal("wrong block or execution context identity")
	}
	ctx := historicalFixtureBlockContext(t, fixture, block)
	for _, account := range historicalBlockAccounts(t, block) {
		if hex.EncodeToString(account.Account) != fixture.Account {
			continue
		}
		if len(account.Txs) != 2 || account.Txs[0].Tx.LT != 4535974000001 || account.Txs[1].Tx.LT != 4535974000002 ||
			hex.EncodeToString(account.Txs[0].Cell.Hash(0)) != "4fb038063fe4df95a855257c24748fb248fc595b91333a9316098c355d2ac0eb" ||
			hex.EncodeToString(account.Txs[1].Cell.Hash(0)) != "9696b34630ed42f5534b8067ec0fdfa9d5cd9e5195a82eb04d38c24eaef960a1" {
			t.Fatal("wrong original transaction witnesses")
		}

		t.Run("modern_default", func(t *testing.T) {
			current, msg := historicalPrepareFirstTransaction(t, account, fixture.Before)
			result, err := NewTVM().EmulateTransaction(ctx, current, msg, TransactionOptions{LogicalTime: int64(account.Txs[0].Tx.LT)})
			if err != nil {
				t.Fatal(err)
			}
			actual, err := result.ParseTransaction()
			if err != nil {
				t.Fatal(err)
			}
			phase := historicalComputePhase(t, actual).Phase.(tlb.ComputePhaseVM)
			if phase.Details.GasLimit.Uint64() != 99999 || result.GasUsed != 1259 || result.Steps != 18 || result.ExitCode != 0 ||
				hex.EncodeToString(result.TransactionCell.Hash(0)) != "15deeb440508c1d40fe7cfd4ab45c870f0390ddddb0b2f3a69e8265c446c5d34" ||
				!bytes.Equal(result.NextAccount.ShardAccount().Account.Hash(0), account.Txs[0].Tx.StateUpdate.NewHash) {
				t.Fatalf("modern replay changed: gas_limit=%s gas_used=%d steps=%d exit=%d transaction=%x",
					phase.Details.GasLimit, result.GasUsed, result.Steps, result.ExitCode, result.TransactionCell.Hash(0))
			}
		})
		t.Run("historical_message_gas", func(t *testing.T) {
			historicalReplayAccount(t, ctx, account, fixture.Before, TransactionOptions{HistoricalMessageGas: true})
		})
		return
	}
	t.Fatal("account witness missing")
}
