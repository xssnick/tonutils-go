package tvm

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
)

func TestHistoricalReplayVersionTwoWitnesses(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		block   uint32
		now     uint32
		lt      uint64
		txHash  string
		options TransactionOptions
	}{
		{
			name: "storage_fee_13503401", file: "storage-fee-13503401.json",
			block: 13503401, now: 1627999052, lt: 20127550000001,
			txHash:  "58f76bc96f30a2d52b64fabb052c1603bbd9637b720ea08cc795210f3c4b7db6",
			options: TransactionOptions{HistoricalStorageFee: true},
		},
		{
			name: "public_library_deploy_17734191", file: "public-library-deploy-17734191.json",
			block: 17734191, now: 1642757054, lt: 24836995000001,
			txHash:  "ca53acbff14ecc7dce82eed6667ba25f96d5dea5ec6ba2ce9e0c36338f49d81b",
			options: TransactionOptions{HistoricalPublicLibraryDeploy: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var fixture historicalAccountFixture
			historicalParseJSON(t, historicalReadFile(t, tc.file), &fixture)
			block := historicalLoadBlock(t, fixture.Source)
			historicalVerifyParent(t, block, fixture.Master)
			if block.BlockInfo.SeqNo != tc.block || fixture.Master.SeqNo != tc.block-1 ||
				block.BlockInfo.GenUtime != tc.now || fixture.Source.Pointer.InclusionMasterSeqno != tc.block ||
				fixture.Source.Pointer.ID.Workchain != -1 || uint64(fixture.Source.Pointer.ID.Shard) != 0x8000000000000000 ||
				fixture.Config.GlobalVersion != 2 {
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
				current, message := historicalPrepareFirstTransaction(t, account, fixture.Before)
				if tc.options.HistoricalPublicLibraryDeploy {
					if current.State().Status != tlb.AccountStatusUninit {
						t.Fatal("expected an uninitialized account")
					}
					external := account.Txs[0].Tx.IO.In.Msg.(*tlb.ExternalMessage)
					if external.StateInit == nil || external.StateInit.Lib == nil {
						t.Fatal("missing deployment StateInit or libraries")
					}
					init, err := tlb.ToCell(external.StateInit)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(init.Hash(0), account.Account) {
						t.Fatal("deployment StateInit address mismatch")
					}
					libs, err := external.StateInit.Lib.LoadAll()
					if err != nil {
						t.Fatal(err)
					}
					if len(libs) != 1 || !libs[0].Value.MustLoadBoolBit() {
						t.Fatal("expected one public library")
					}
				}

				t.Run("modern_default", func(t *testing.T) {
					result, err := NewTVM().EmulateTransaction(ctx, current, message, TransactionOptions{LogicalTime: int64(tc.lt)})
					if err != nil {
						t.Fatal(err)
					}
					if tc.options.HistoricalPublicLibraryDeploy {
						if result.Accepted || result.TransactionCell != nil || result.NextAccount != nil ||
							result.GasUsed != 0 || result.Steps != 0 || result.ExitCode != 0 {
							t.Fatal("modern public-library deployment rejection changed")
						}
						return
					}
					actual, err := result.ParseTransaction()
					if err != nil {
						t.Fatal(err)
					}
					description := actual.Description.(tlb.TransactionDescriptionOrdinary)
					fee := description.StoragePhase.StorageFeesCollected.Nano()
					if fee.Uint64() != 69742905468 || actual.TotalFees.Coins.Nano().Uint64() != 73427288750 ||
						result.GasUsed != 366465 || result.Steps != 2400 || result.ExitCode != 0 ||
						hex.EncodeToString(result.TransactionCell.Hash(0)) != "7ab0e6f4cc937d2d2648dae93f05251df89bfd2e38e63cc22510b1fb20606c19" ||
						hex.EncodeToString(result.NextAccount.ShardAccount().Account.Hash(0)) != "a6010d91ac9510e92a9561aa34ec91b72ad0d66bda9b1a5ade4e7afba9fe7109" {
						t.Fatalf("modern storage-fee replay changed: fee=%s transaction=%x", fee, result.TransactionCell.Hash(0))
					}
				})
				t.Run("historical", func(t *testing.T) {
					historicalReplayAccount(t, ctx, account, fixture.Before, tc.options)
				})
				if tc.options.HistoricalPublicLibraryDeploy {
					for _, historical := range []bool{false, true} {
						name := "accept_only_modern"
						if historical {
							name = "accept_only_historical"
						}
						t.Run(name, func(t *testing.T) {
							options := TransactionOptions{LogicalTime: int64(tc.lt), HistoricalPublicLibraryDeploy: historical}
							accepted, err := NewTVM().CheckExternalMessageAccepted(ctx, current, message, options)
							if err != nil || accepted != historical {
								t.Fatalf("historical=%t accepted=%t error=%v", historical, accepted, err)
							}
						})
					}
				}
				return
			}
			t.Fatal("account witness missing")
		})
	}
}
