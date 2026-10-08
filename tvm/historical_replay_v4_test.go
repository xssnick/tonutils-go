package tvm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
)

func TestHistoricalReplayVersionFourWitnesses(t *testing.T) {
	cases := []struct {
		name, file, checksum, postChecksum string
		inclusionMC, master, block, now    uint32
		workchain                          int32
		withProof                          bool
		lts                                []uint64
		txHashes                           []string
	}{
		{
			name: "globalid_context", file: "globalid-v4-34946802",
			checksum:     "86b7e6608777c32b774f933530143c1db59a3f40ba2c76d1f6451c38c82af24e",
			postChecksum: "0d6c591d3b3fc9f7cfb8677a92a5fb04c7345523e202841ae64731420e98d49e",
			inclusionMC:  34946802, master: 34946799, block: 40804162, now: 1703378650,
			lts:      []uint64{43425913000003},
			txHashes: []string{"ff00bdb566430b7b7c7de340d0759cbcfd6fa238a18cb544c6beebffdf90e5ca"},
		},
		{
			name: "nonexist_storage_debt", file: "nonexist-storage-debt-v4-35184388",
			withProof:    true,
			checksum:     "199803fbce9da5631a0cdfa957f3c812fdc7352f98da1e09e027b9080c25b64f",
			postChecksum: "454a5de85f8f9314b9bfdf88911cae47381e298f6b93ca98ad88f9a1daacd439",
			inclusionMC:  35184388, master: 35184385, block: 41026304, now: 1704308811,
			lts: []uint64{43671543000009, 43671543000010},
			txHashes: []string{
				"eafa9e6741649165aaff845a5a27a9cdca7f9e6b205d51fc37bfbb98e3570ccf",
				"de81d7115b3cb3f21b07ee7901ca35217b4f6d3b8cc5a3b908428a330b55f40a",
			},
		},
		{
			name: "special_outgoing_body", file: "special-outgoing-body-v4-35461295",
			checksum:     "7f01befebfb729fde311d27f1a27d710d5b28d305cdc143feca81e0cdff53b73",
			postChecksum: "c5d6ef8bf5362f4c764716a4a4c8decc56b8e54b6874e674ead4c89982778aa4",
			inclusionMC:  35461295, master: 35461294, block: 35461295, now: 1705401106, workchain: -1,
			lts:      []uint64{43958197000001},
			txHashes: []string{"59464129acf036f9d820deb263233e46e63477f897861927df04b45bd0cc5f2f"},
		},
		{
			name: "inline_state_size", file: "inline-state-size-v4-35514859",
			withProof:    true,
			checksum:     "5b585e5843f12703fd027cbe172ee0012131a3d896e7b82cd758c700ff559725",
			postChecksum: "4f2f76b4d9363dae5d8b35eacba17de85322b41eef39cfc5c4dc4370328560f1",
			inclusionMC:  35514859, master: 35514856, block: 41347125, now: 1705603715,
			lts: []uint64{44013721000001, 44013721000005},
			txHashes: []string{
				"bc2580b71b05f4ab84dfd7aac56f87926a36d780fa4953321183d20461e7c9b3",
				"5542eadf184aa113d33fe8777c1d3082138a2e9430963c44ee455c1b2bd5ba1b",
			},
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
				fixture.Source.Pointer.ID.Workchain != tc.workchain || fixture.Source.Pointer.ID.Shard != -1<<63 ||
				fixture.Config.GlobalVersion != 4 {
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
				if hex.EncodeToString(account.Account) != fixture.Account {
					continue
				}
				if len(account.Txs) != len(tc.lts) {
					t.Fatal("wrong transaction inventory")
				}
				for i, witness := range account.Txs {
					if witness.Tx.LT != tc.lts[i] || hex.EncodeToString(witness.Cell.Hash(0)) != tc.txHashes[i] {
						t.Fatal("wrong transaction commitment")
					}
				}
				last := account.Txs[len(account.Txs)-1]
				if !bytes.Equal(after.Account.Hash(0), last.Tx.StateUpdate.NewHash) ||
					after.LastTransLT != last.Tx.LT || !bytes.Equal(after.LastTransHash, last.Cell.Hash(0)) {
					t.Fatal("independent post-state does not match the transaction witness")
				}
				for _, proof := range []bool{false, true} {
					// Account-only proofs cannot cover code supplied by inbound
					// StateInit, as in the GLOBALID and special-body witnesses.
					if proof && !tc.withProof {
						continue
					}
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
