package tvm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
)

func TestHistoricalReplayVersionFiveSixWitnesses(t *testing.T) {
	cases := []struct {
		name, file, checksum, postChecksum string
		account, blockRoot, blockFile      string
		oldAccount, newAccount, txHash     string
		inclusionMC, master, block, now    uint32
		version                            uint32
		lt, gas, steps                     uint64
	}{
		{
			name: "child_c4_trace", file: "child-c4-trace-v5-36714473",
			checksum:     "df5045be2d28d2b6265d0532315505b5a0b141f7e272d21db152735e4db50476",
			postChecksum: "6f04e90fdb7f258b7c7db7adc8a88e87cd5308123f320e422074d7a22b5726ee",
			account:      "2b8e251f89ad8dbff413b01eb67de3a7a1b3045fc6762f3747901ff750aaad94",
			blockRoot:    "3d37c467e5b05ca95d159c9c6ef7a02ca770cd10e59e14f5f55507381c28ae3a",
			blockFile:    "acd728c8527fe740e7383bbf55d1e4d6d7394251e9c4f3cc7027ae339d0e19f4",
			oldAccount:   "cdc7e4b3a323397469233eb4a3d1cea5c56854a0d4d2f6ab611f2c27308e5b50",
			newAccount:   "04a340f7c87600c0a8a42e9fcfd353a5d2874381d918476cbeed61e8c183d6e0",
			txHash:       "d54d60962aaf40f2a30077364aa0c22cd2ed6cbd6e3b6931af8248d37b9e4a60",
			inclusionMC:  36714473, master: 36714470, block: 42485529, now: 1710463554,
			version: 5, lt: 45266579000005, gas: 16313, steps: 320,
		},
		{
			name: "send_retry_fine", file: "send-retry-fine-v6-37149601",
			checksum:     "7d15e96c88fd44b36d62ce65be379e5d2eeac8f0e0709301efeec1c9af23ffbb",
			postChecksum: "d45be40800909b2139d78a545aa4ded6073d45fdff7e2e6babb17cf2a221e7ca",
			account:      "72442e2f093aab933d84889418ea4d1f1f91f3da48414096b8361d2845f866fd",
			blockRoot:    "b40d5a009bc0e109c87ecd75881fff1938c65fde41f10b8cfd62901caa909e0c",
			blockFile:    "42ed00bb2b7ef7f09017f43dc694baaf1315584c671a1a4d6bf23c83eedd7f4f",
			oldAccount:   "1ac457735295cb9bcc8cca73ed77c3d91a7546b8516035410e950a35c48e5174",
			newAccount:   "f39cddba15667bce6f888ff5fd453a32f83b793f154629325a669773252f43d6",
			txHash:       "b09ebc8151de0e85cf82093595d1287eb63e024c250f57fc55582cc0cbf1e6ff",
			inclusionMC:  37149601, master: 37149598, block: 42848171, now: 1712435483,
			version: 6, lt: 45713784000001, gas: 3308, steps: 68,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := historicalReadFile(t, tc.file+".json.gz")
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
				fixture.Config.GlobalVersion != tc.version || fixture.Account != tc.account ||
				hex.EncodeToString(fixture.Source.Pointer.ID.RootHash) != tc.blockRoot ||
				hex.EncodeToString(fixture.Source.Pointer.ID.FileHash) != tc.blockFile {
				t.Fatal("wrong block or execution context identity")
			}

			post := historicalReadFile(t, tc.file+"-after.boc")
			postChecksum := sha256.Sum256(post)
			if hex.EncodeToString(postChecksum[:]) != tc.postChecksum {
				t.Fatal("post-state checksum mismatch")
			}
			postCell := historicalParseCell(t, post)
			var after tlb.ShardAccount
			historicalParseTLB(t, &after, postCell)
			ctx := historicalFixtureBlockContext(t, fixture, block)
			for _, account := range historicalBlockAccounts(t, block) {
				if hex.EncodeToString(account.Account) != tc.account {
					continue
				}
				if len(account.Txs) != 1 {
					t.Fatal("wrong transaction inventory")
				}
				witness := account.Txs[0]
				if witness.Tx.LT != tc.lt || hex.EncodeToString(witness.Cell.Hash(0)) != tc.txHash ||
					hex.EncodeToString(witness.Tx.StateUpdate.OldHash) != tc.oldAccount ||
					hex.EncodeToString(witness.Tx.StateUpdate.NewHash) != tc.newAccount {
					t.Fatal("wrong transaction commitment")
				}
				phase, ok := historicalComputePhase(t, &witness.Tx).Phase.(tlb.ComputePhaseVM)
				if !ok || phase.Details.GasUsed.Uint64() != tc.gas || uint64(phase.Details.VMSteps) != tc.steps || phase.Details.ExitCode != 0 {
					t.Fatal("wrong compute witness")
				}
				if !bytes.Equal(after.Account.Hash(0), witness.Tx.StateUpdate.NewHash) ||
					after.LastTransLT != witness.Tx.LT || !bytes.Equal(after.LastTransHash, witness.Cell.Hash(0)) {
					t.Fatal("independent post-state does not match the transaction witness")
				}
				for _, proof := range []bool{false, true} {
					t.Run(fmt.Sprintf("proof_%t", proof), func(t *testing.T) {
						result := historicalReplayAccount(t, ctx, account, fixture.Before, TransactionOptions{BuildProof: proof})
						if !bytes.Equal(result.ShardAccountCell().Hash(0), postCell.Hash(0)) {
							t.Fatal("full final ShardAccount mismatch")
						}
					})
				}
				return
			}
			t.Fatal("account witness missing")
		})
	}
}
