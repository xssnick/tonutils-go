package tvm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestHistoricalReplayActionWitnesses(t *testing.T) {
	cases := []struct {
		name            string
		file            string
		checksum        string
		inclusionMC     uint32
		master          uint32
		block           uint32
		now             uint32
		lts             []uint64
		txHashes        []string
		options         TransactionOptions
		modernTarget    int
		modernRejected  bool
		modernTxHash    string
		modernShardHash string
		modernGas       int64
		modernSteps     uint64
		modernExit      int64
	}{
		{
			name: "active_external_state_init", file: "external-state-init-v2-19763575.json",
			checksum:    "676a4a641c121ed2e86385dec4edc1a9b0a4b0dd62ec9e59b819292bfd748e20",
			inclusionMC: 19763575, master: 19763573, block: 24895177, now: 1649779671,
			lts:          []uint64{27045248000001},
			txHashes:     []string{"881b0f781341c30dd0ee00e03cb225c10ff05c90e9847df3c96c3d78a374f130"},
			options:      TransactionOptions{HistoricalExternalStateInit: true},
			modernTarget: 1, modernRejected: true,
		},
		{
			name: "failed_action_deletion_rollback", file: "action-rollback-19950828.json",
			checksum:    "07ce903c790926472f846a7cee40af13d7311eb14b572bbe9e0045c14879804f",
			inclusionMC: 19950828, master: 19950825, block: 25097435, now: 1650439658,
			lts:      []uint64{27252507000003},
			txHashes: []string{"c97d878f5129f213b29b786560313f6119037e9023bac36b54a444bc395cb523"},
		},
		{
			name: "historical_action_state_limits", file: "action-state-limits-20693766.json.gz",
			checksum:    "f66704ac25dc376fc035f65b98f54c2413eeffdee4e7027b80fed24df255022d",
			inclusionMC: 20693766, master: 20693761, block: 25891474, now: 1653044387,
			lts: []uint64{28066916000001, 28066916000003},
			txHashes: []string{
				"898d1ab89c999ee1b88830f5979c5fe555e669af1921c2043552b5e428764ee0",
				"163c7bd0d59b80785898d6c6fe366fa5bef5ca13b80bae82cdadcc8943682761",
			},
			options:      TransactionOptions{HistoricalNoActionStateLimits: true},
			modernTarget: 1, modernGas: 16333, modernSteps: 136,
			modernTxHash: "e9ef909aa7edb3208ebf60a5b4c96a76ee3bf391e093905e9c48ab7423ad9d68",
		},
		{
			name: "malformed_outgoing_library", file: "malformed-library-23830662.json",
			checksum:    "da74cddb326161063e540c98130fec885ebf6b1a98a424d769e46c3061066593",
			inclusionMC: 23830662, master: 23830659, block: 29259549, now: 1664209601,
			lts:          []uint64{31528581000001},
			txHashes:     []string{"477d888664b01ac1056c4ed9a36c3ec96098580f1373b00d4b415343438f73a4"},
			options:      TransactionOptions{HistoricalActionLibraryValidation: true},
			modernTarget: 1, modernGas: 2917, modernSteps: 62,
			modernTxHash:    "aa7b9da6a2ba105eb66d660be5f9afff251b3e907559cd20cf953dfc14bd1ea1",
			modernShardHash: "ff6d434df49f3a0a60b2cb3f55ca9fae43c4ba1d3a9e87fd40a33d1aa51d5b12",
		},
		{
			name: "historical_nan_comparison", file: "nan-comparison-26399968.json",
			checksum:    "4ba1d2c8b655e3e477a0d0eb21ec376ddd77e652d54847ca568c1de82b514477",
			inclusionMC: 26399968, master: 26399963, block: 31958896, now: 1673158517,
			lts: []uint64{34301004000007, 34301004000011},
			txHashes: []string{
				"af440469da58bfa7bc65eeebdda6e17185895331b42133c366ee1ac97f6a41e8",
				"cc4e05364e761fbf3c4511255cee07e35f2da6c1ffbcbc8451925d65746c62a7",
			},
			options:      TransactionOptions{Historical: vm.HistoricalConfig{NaNComparison: true}},
			modernTarget: 2, modernGas: 6921, modernSteps: 169, modernExit: 4,
			modernTxHash: "2d17eb78dd47f31ef0a7dee8b34013f0d653a40025cef235002f0645d9031105",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := historicalReadFile(t, tc.file)
			checksum := sha256.Sum256(raw)
			if hex.EncodeToString(checksum[:]) != tc.checksum {
				t.Fatal("fixture checksum mismatch")
			}
			var fixture historicalAccountFixture
			historicalParseJSON(t, raw, &fixture)
			block := historicalLoadBlock(t, fixture.Source)
			historicalVerifyParent(t, block, fixture.Master)
			if block.BlockInfo.SeqNo != tc.block || fixture.Master.SeqNo != tc.master ||
				block.BlockInfo.GenUtime != tc.now || fixture.Source.Pointer.InclusionMasterSeqno != tc.inclusionMC ||
				fixture.Source.Pointer.ID.Workchain != 0 || fixture.Source.Pointer.ID.Shard != -1<<63 ||
				fixture.Config.GlobalVersion != 2 {
				t.Fatal("wrong block or execution context identity")
			}
			ctx := historicalFixtureBlockContext(t, fixture, block)
			for _, account := range historicalBlockAccounts(t, block) {
				if hex.EncodeToString(account.Account) != fixture.Account {
					continue
				}
				if len(account.Txs) != len(tc.lts) {
					t.Fatal("wrong transaction inventory")
				}
				for i, witness := range account.Txs {
					if witness.Tx.LT != tc.lts[i] || hex.EncodeToString(witness.Cell.Hash(0)) != tc.txHashes[i] {
						t.Fatal("wrong original transaction commitment")
					}
				}
				if tc.options.Historical.NaNComparison {
					historicalCheckNaNPostState(t, account)
				}
				t.Run("exact_replay", func(t *testing.T) {
					historicalReplayAccount(t, ctx, account, fixture.Before, tc.options)
				})
				if tc.modernTarget != 0 {
					t.Run("modern_default", func(t *testing.T) {
						current, message := historicalPrepareFirstTransaction(t, account, fixture.Before)
						options := TransactionOptions{}
						for i := range tc.modernTarget {
							witness := account.Txs[i]
							options.LogicalTime = int64(witness.Tx.LT)
							if i != 0 {
								var err error
								message, err = PrepareMessage(replayRegressionInputMessage(t, witness.Cell))
								if err != nil {
									t.Fatal(err)
								}
							}
							result, err := NewTVM().EmulateTransaction(ctx, current, message, options)
							if err != nil {
								t.Fatal(err)
							}
							if i+1 < tc.modernTarget {
								next := result.NextAccount.ShardAccount()
								if !bytes.Equal(result.TransactionCell.Hash(0), witness.Cell.Hash(0)) ||
									!bytes.Equal(next.Account.Hash(0), witness.Tx.StateUpdate.NewHash) ||
									next.LastTransLT != witness.Tx.LT || !bytes.Equal(next.LastTransHash, witness.Cell.Hash(0)) {
									t.Fatal("modern preceding transaction mismatch")
								}
								current, options.AccountStorageStat = result.NextAccount, result.AccountStorageStat
								continue
							}
							if tc.modernRejected {
								if result.Accepted || result.TransactionCell != nil || result.NextAccount != nil ||
									result.GasUsed != 0 || result.Steps != 0 || result.ExitCode != 0 {
									t.Fatal("modern external StateInit rejection changed")
								}
								return
							}
							if result.GasUsed != tc.modernGas || result.Steps != tc.modernSteps || result.ExitCode != tc.modernExit ||
								hex.EncodeToString(result.TransactionCell.Hash(0)) != tc.modernTxHash {
								t.Fatalf("modern result changed: gas=%d steps=%d exit=%d transaction=%x",
									result.GasUsed, result.Steps, result.ExitCode, result.TransactionCell.Hash(0))
							}
							if tc.modernShardHash != "" && hex.EncodeToString(result.NextAccount.ShardAccountCell().Hash(0)) != tc.modernShardHash {
								t.Fatalf("modern shard account changed: %x", result.NextAccount.ShardAccountCell().Hash(0))
							}
						}
					})
				}
				return
			}
			t.Fatal("account witness missing")
		})
	}
}

func historicalCheckNaNPostState(t *testing.T, account historicalAccountWitness) {
	t.Helper()

	raw := historicalReadFile(t, "nan-comparison-26399968-after.boc")
	checksum := sha256.Sum256(raw)
	if hex.EncodeToString(checksum[:]) != "7a0782ec18c38952a9d108dd4df86ed4bf1f5a118bb7b5f77ba307d0610dfe7b" {
		t.Fatal("post-state checksum mismatch")
	}
	var after tlb.ShardAccount
	historicalParseTLB(t, &after, historicalParseCell(t, raw))
	last := account.Txs[len(account.Txs)-1]
	if !bytes.Equal(after.Account.Hash(0), last.Tx.StateUpdate.NewHash) ||
		after.LastTransLT != last.Tx.LT || !bytes.Equal(after.LastTransHash, last.Cell.Hash(0)) {
		t.Fatal("independent post-state does not match the transaction witness")
	}
}
