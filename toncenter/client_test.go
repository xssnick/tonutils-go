package toncenter

import (
	"context"
	"encoding/json"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/wallet"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"math/big"
	"os"
	"testing"
	"time"
)

var cli = New("https://toncenter.com",
	WithTimeout(20*time.Second),
	WithRateLimit(0.6),
)

func TestTestnetEC(t *testing.T) {
	cliTest := New("https://testnet.toncenter.com",
		// WithAPIKey("YOUR_KEY"), // X-API-Key
		WithTimeout(20*time.Second),
		WithRateLimit(0.85),
	)

	t.Run("ext addr info", func(t *testing.T) {
		resp, err := cliTest.V2().GetExtendedAddressInformation(context.Background(), address.MustParseAddr("0QDPMgkvdvGj6ZeGN4zaVrZNwFPmg3LUjvUxcNxEWIDSaBUR"))
		if err != nil {
			t.Fatal(err)
		}
		if resp.ExtraCurrencies[0].Currency != 100 || !resp.ExtraCurrencies[0].Balance.MustCoins(9).IsPositive() {
			t.Fatal("incorrect", resp.ExtraCurrencies[0].Currency)
		}
	})
}

func TestV2(t *testing.T) {
	ctx := context.Background()
	t.Run("addr info", func(t *testing.T) {
		resp, err := cli.V2().GetAddressInformation(ctx, address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N"))
		if err != nil {
			t.Fatal(err)
		}
		if !resp.Balance.MustCoins(9).IsPositive() {
			t.Fatal("balance is incorrect")
		}
	})

	t.Run("ext addr info", func(t *testing.T) {
		resp, err := cli.V2().GetExtendedAddressInformation(ctx, address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N"))
		if err != nil {
			t.Fatal(err)
		}
		if !resp.Balance.MustCoins(9).IsPositive() {
			t.Fatal("balance is incorrect")
		}
	})

	t.Run("wallet info", func(t *testing.T) {
		resp, err := cli.V2().GetWalletInformation(ctx, address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N"))
		if err != nil {
			t.Fatal(err)
		}
		if resp.Seqno <= 0 {
			t.Fatal("seqno is negative")
		}
	})

	var master BlockID
	t.Run("master info", func(t *testing.T) {
		resp, err := cli.V2().GetMasterchainInfo(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Last.Seqno < 10000 {
			t.Fatal("incorrect")
		}
		master = resp.Last
	})

	t.Run("master signatures", func(t *testing.T) {
		resp, err := cli.V2().GetMasterchainBlockSignatures(ctx, master.Seqno)
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Signatures) == 0 {
			t.Fatal("incorrect")
		}
	})

	var shard BlockID
	t.Run("get shards", func(t *testing.T) {
		resp, err := cli.V2().GetShards(ctx, master.Seqno)
		if err != nil {
			t.Fatal(err)
		}
		if len(resp) == 0 {
			t.Fatal("incorrect")
		}
		shard = resp[0]
	})

	t.Run("shard block proof", func(t *testing.T) {
		resp, err := cli.V2().GetShardBlockProof(ctx, shard.Workchain, shard.Shard, shard.Seqno, nil)
		if err != nil {
			t.Fatal(err)
		}
		json.NewEncoder(os.Stdout).Encode(resp)
		if resp == nil {
			t.Fatal("incorrect")
		}
	})

	t.Run("addr balance", func(t *testing.T) {
		resp, err := cli.V2().GetAddressBalance(ctx, address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N"))
		if err != nil {
			t.Fatal(err)
		}
		if !resp.MustCoins(TonDecimals).IsPositive() {
			t.Fatal("balance is negative")
		}
	})

	var tx *TransactionV2
	t.Run("get transactions", func(t *testing.T) {
		resp, err := cli.V2().GetTransactions(context.Background(), address.MustParseAddr("EQAYqo4u7VF0fa4DPAebk4g9lBytj2VFny7pzXR0trjtXQaO"), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(resp) == 0 {
			t.Fatal("no transactions")
		}
		tx = &resp[0]
	})

	t.Run("get consensus block", func(t *testing.T) {
		resp, err := cli.V2().GetConsensusBlock(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if resp.Seqno == 0 {
			t.Fatal("incorrect")
		}
	})

	t.Run("lookup block", func(t *testing.T) {
		seqno := master.Seqno - 5
		resp, err := cli.V2().LookupBlock(context.Background(), master.Workchain, master.Shard, &LookupBlockV2Options{
			Seqno: &seqno,
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp.Seqno == 0 {
			t.Fatal("incorrect")
		}
	})

	t.Run("get block transactions", func(t *testing.T) {
		resp, err := cli.V2().GetBlockTransactions(context.Background(), shard.Workchain, shard.Shard, shard.Seqno, &GetBlockTransactionsV2Options{
			RootHash: shard.RootHash,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Transactions) == 0 || resp.Transactions[0].LT == 0 {
			t.Fatal("incorrect")
		}
	})

	t.Run("get block transactions ext", func(t *testing.T) {
		resp, err := cli.V2().GetBlockTransactionsExt(context.Background(), shard.Workchain, shard.Shard, shard.Seqno, &GetBlockTransactionsV2Options{
			RootHash: shard.RootHash,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Transactions) == 0 || !resp.Transactions[0].Fee.MustCoins(9).IsPositive() {
			t.Fatal("incorrect")
		}
	})

	t.Run("get block header", func(t *testing.T) {
		if len(master.RootHash) == 0 {
			t.Fatal("root hash empty")
		}

		resp, err := cli.V2().GetBlockHeader(context.Background(), master.Workchain, master.Shard, master.Seqno, &GetBlockHeaderOptions{
			RootHash: master.RootHash,
			FileHash: master.FileHash,
		})
		if err != nil {
			// since this api call may don't work on the toncenter side, it we skip it in test, just log
			t.Fatal(err)
			return
		}
		if len(resp.PrevBlocks) == 0 {
			t.Fatal("incorrect")
		}
	})

	t.Run("get config param", func(t *testing.T) {
		resp, err := cli.V2().GetConfigParam(context.Background(), 11, nil)
		if err != nil {
			t.Fatal(err)
		}
		if resp == nil {
			t.Fatal("incorrect")
		}
	})

	t.Run("get config all", func(t *testing.T) {
		resp, err := cli.V2().GetConfigAll(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if resp == nil {
			t.Fatal("incorrect")
		}
	})

	t.Run("get out msg queue size", func(t *testing.T) {
		resp, err := cli.V2().GetOutMsgQueueSizes(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if resp.SizeLimit == 0 || len(resp.Shards) == 0 {
			t.Fatal("incorrect")
		}
	})

	t.Run("get token data", func(t *testing.T) {
		resp, err := cli.V2().GetTokenData(context.Background(), address.MustParseAddr("EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs"))
		if err != nil {
			t.Fatal(err)
		}
		if !resp.Mintable {
			t.Fatal("incorrect")
		}

	})

	t.Run("try locate tx", func(t *testing.T) {
		resp, err := cli.V2().TryLocateTx(context.Background(), tx.InMsg.Source.Addr, tx.InMsg.Destination.Addr, tx.InMsg.CreatedLt)
		if err != nil {
			t.Fatal(err)
		}
		if resp.InMsg.Source.Addr.String() != tx.InMsg.Source.Addr.String() {
			t.Fatal("incorrect")
		}
	})

	t.Run("try locate result tx", func(t *testing.T) {
		resp, err := cli.V2().TryLocateResultTx(context.Background(), tx.InMsg.Source.Addr, tx.InMsg.Destination.Addr, tx.InMsg.CreatedLt)
		if err != nil {
			t.Fatal(err)
		}
		if resp.InMsg.Source.Addr.String() != tx.InMsg.Source.Addr.String() {
			t.Fatal("incorrect")
		}
		tx = resp
	})

	t.Run("try locate source tx", func(t *testing.T) {
		resp, err := cli.V2().TryLocateSourceTx(context.Background(), tx.InMsg.Source.Addr, tx.InMsg.Destination.Addr, tx.InMsg.CreatedLt)
		if err != nil {
			// as Toncenter team said, it is not always works on api side, thats why it is deprecated
			t.Log(err)

			return
		}
		if resp.InMsg.Destination.Addr.String() != tx.InMsg.Source.Addr.String() {
			t.Fatal("incorrect")
		}
	})

	t.Run("estimate fee", func(t *testing.T) {
		w, err := wallet.FromSeedWithOptions(ton.NewAPIClient(liteclient.NewOfflineClient()), wallet.NewSeed(), wallet.V4R2)
		if err != nil {
			t.Fatal(err)
		}
		w.GetSpec().(*wallet.SpecV4R2).SetSeqnoFetcher(func(ctx context.Context, sub uint32) (uint32, error) {
			// Get seqno from your database here, this func will be called during BuildTransfer to get seqno for transaction
			return 1, nil
		})

		outMsg := wallet.SimpleMessage(w.WalletAddress(), tlb.MustFromTON("0.1"), cell.BeginCell().EndCell())
		msg, err := w.PrepareExternalMessageForMany(context.Background(), true, []*wallet.Message{outMsg})
		if err != nil {
			t.Fatal(err)
		}

		resp, err := cli.V2().EstimateFee(context.Background(), EstimateFeeRequest{
			Address:      w.WalletAddress(),
			Body:         msg.Body,
			InitCode:     msg.StateInit.Code,
			InitData:     msg.StateInit.Data,
			IgnoreChkSig: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp.SourceFees.InFwdFee == 0 {
			t.Fatal("incorrect")
		}
	})

	t.Run("run get method", func(t *testing.T) {
		resp, err := cli.V2().RunGetMethod(context.Background(),
			address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N"), "seqno", []any{big.NewInt(123)}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Stack[0].(*big.Int).Uint64() == 0 {
			t.Fatal("incorrect")
		}
	})

}

func TestV3(t *testing.T) {
	ctx := context.Background()
	t.Run("addr info", func(t *testing.T) {
		resp, err := cli.V3().GetAddressInformation(ctx, address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N"))
		if err != nil {
			t.Fatal(err)
		}
		if !resp.Balance.MustCoins(9).IsPositive() {
			t.Fatal("balance is incorrect")
		}
	})

	t.Run("wallet info", func(t *testing.T) {
		resp, err := cli.V3().GetWalletInformation(ctx, address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N"))
		if err != nil {
			t.Fatal(err)
		}
		if resp.Seqno <= 0 {
			t.Fatal("seqno is negative")
		}
	})

	t.Run("run get method", func(t *testing.T) {
		resp, err := cli.V3().RunGetMethod(context.Background(),
			address.MustParseAddr("EQB9cFa5EIS-v9LXhJ_MbuYxg9wBm7kOH81qAMVDlZx4AJ_K"), "get_providers", []any{big.NewInt(123), cell.BeginCell().EndCell(), cell.BeginCell().ToSlice(), address.MustParseAddr("EQB9cFa5EIS-v9LXhJ_MbuYxg9wBm7kOH81qAMVDlZx4AJ_K")}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Stack[5].(*big.Int).Uint64() == 0 {
			t.Fatal("incorrect")
		}
	})

	t.Run("estimate fee", func(t *testing.T) {
		w, err := wallet.FromSeedWithOptions(ton.NewAPIClient(liteclient.NewOfflineClient()), wallet.NewSeed(), wallet.V4R2)
		if err != nil {
			t.Fatal(err)
		}
		w.GetSpec().(*wallet.SpecV4R2).SetSeqnoFetcher(func(ctx context.Context, sub uint32) (uint32, error) {
			// Get seqno from your database here, this func will be called during BuildTransfer to get seqno for transaction
			return 1, nil
		})

		outMsg := wallet.SimpleMessage(w.WalletAddress(), tlb.MustFromTON("0.1"), cell.BeginCell().EndCell())
		msg, err := w.PrepareExternalMessageForMany(context.Background(), true, []*wallet.Message{outMsg})
		if err != nil {
			t.Fatal(err)
		}

		resp, err := cli.V3().EstimateFee(context.Background(), EstimateFeeRequest{
			Address:      w.WalletAddress(),
			Body:         msg.Body,
			InitCode:     msg.StateInit.Code,
			InitData:     msg.StateInit.Data,
			IgnoreChkSig: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp.SourceFees.InFwdFee == 0 {
			t.Fatal("incorrect")
		}
	})
}
