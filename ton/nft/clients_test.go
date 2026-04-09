package nft

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type nftAPIMock struct {
	waitedSeq uint32

	currentMasterchainInfo func(ctx context.Context) (*ton.BlockIDExt, error)
	runGetMethod           func(ctx context.Context, blockInfo *ton.BlockIDExt, addr *address.Address, method string, params ...interface{}) (*ton.ExecutionResult, error)
}

func (m *nftAPIMock) GetBlockDataAsCell(ctx context.Context, block *ton.BlockIDExt) (*cell.Cell, error) {
	return nil, nil
}

func (m *nftAPIMock) WaitForBlock(seqno uint32) ton.APIClientWrapped {
	m.waitedSeq = seqno
	return m
}

func (m *nftAPIMock) CurrentMasterchainInfo(ctx context.Context) (*ton.BlockIDExt, error) {
	if m.currentMasterchainInfo != nil {
		return m.currentMasterchainInfo(ctx)
	}
	return &ton.BlockIDExt{SeqNo: 1}, nil
}

func (m *nftAPIMock) RunGetMethod(ctx context.Context, blockInfo *ton.BlockIDExt, addr *address.Address, method string, params ...interface{}) (*ton.ExecutionResult, error) {
	if m.runGetMethod == nil {
		return nil, errors.New("runGetMethod is not configured")
	}
	return m.runGetMethod(ctx, blockInfo, addr, method, params...)
}

func (m *nftAPIMock) Client() ton.LiteClient {
	return nil
}

func (m *nftAPIMock) GetTime(ctx context.Context) (uint32, error) {
	return 0, nil
}

func (m *nftAPIMock) GetLibraries(ctx context.Context, list ...[]byte) ([]*cell.Cell, error) {
	return nil, nil
}

func (m *nftAPIMock) LookupBlock(ctx context.Context, workchain int32, shard int64, seqno uint32) (*ton.BlockIDExt, error) {
	return nil, nil
}

func (m *nftAPIMock) GetBlockData(ctx context.Context, block *ton.BlockIDExt) (*tlb.Block, error) {
	return nil, nil
}

func (m *nftAPIMock) GetBlockHeader(ctx context.Context, block *ton.BlockIDExt) (*tlb.BlockHeader, error) {
	return nil, nil
}

func (m *nftAPIMock) GetBlockTransactionsV2(ctx context.Context, block *ton.BlockIDExt, count uint32, after ...*ton.TransactionID3) ([]ton.TransactionShortInfo, bool, error) {
	return nil, false, nil
}

func (m *nftAPIMock) GetBlockShardsInfo(ctx context.Context, master *ton.BlockIDExt) ([]*ton.BlockIDExt, error) {
	return nil, nil
}

func (m *nftAPIMock) GetBlockchainConfig(ctx context.Context, block *ton.BlockIDExt, onlyParams ...int32) (*ton.BlockchainConfig, error) {
	return nil, nil
}

func (m *nftAPIMock) GetMasterchainInfo(ctx context.Context) (*ton.BlockIDExt, error) {
	return m.CurrentMasterchainInfo(ctx)
}

func (m *nftAPIMock) GetAccount(ctx context.Context, block *ton.BlockIDExt, addr *address.Address) (*tlb.Account, error) {
	return nil, nil
}

func (m *nftAPIMock) SendExternalMessage(ctx context.Context, msg *tlb.ExternalMessage) error {
	return nil
}

func (m *nftAPIMock) SendExternalMessageWaitTransaction(ctx context.Context, msg *tlb.ExternalMessage) (*tlb.Transaction, *ton.BlockIDExt, []byte, error) {
	return nil, nil, nil, nil
}

func (m *nftAPIMock) ListTransactions(ctx context.Context, addr *address.Address, num uint32, lt uint64, txHash []byte) ([]*tlb.Transaction, error) {
	return nil, nil
}

func (m *nftAPIMock) GetTransaction(ctx context.Context, block *ton.BlockIDExt, addr *address.Address, lt uint64) (*tlb.Transaction, error) {
	return nil, nil
}

func (m *nftAPIMock) GetBlockProof(ctx context.Context, known, target *ton.BlockIDExt) (*ton.PartialBlockProof, error) {
	return nil, nil
}

func (m *nftAPIMock) SubscribeOnTransactions(workerCtx context.Context, addr *address.Address, lastProcessedLT uint64, channel chan<- *tlb.Transaction) {
}

func (m *nftAPIMock) VerifyProofChain(ctx context.Context, from, to *ton.BlockIDExt) error {
	return nil
}

func (m *nftAPIMock) WithRetry(maxRetries ...int) ton.APIClientWrapped {
	return m
}

func (m *nftAPIMock) WithRetryTimeout(maxRetries int, timeout time.Duration) ton.APIClientWrapped {
	return m
}

func (m *nftAPIMock) WithTimeout(timeout time.Duration) ton.APIClientWrapped {
	return m
}

func (m *nftAPIMock) WithLSInfoInErrors() ton.APIClientWrapped {
	return m
}

func (m *nftAPIMock) SetTrustedBlock(block *ton.BlockIDExt) {
}

func (m *nftAPIMock) SetTrustedBlockFromConfig(cfg *liteclient.GlobalConfig) {
}

func (m *nftAPIMock) FindLastTransactionByInMsgHash(ctx context.Context, addr *address.Address, msgHash []byte, maxTxNumToScan ...int) (*tlb.Transaction, error) {
	return nil, nil
}

func (m *nftAPIMock) FindLastTransactionByOutMsgHash(ctx context.Context, addr *address.Address, msgHash []byte, maxTxNumToScan ...int) (*tlb.Transaction, error) {
	return nil, nil
}

func (m *nftAPIMock) FindLastTransactionByInMsgHashAfterTime(ctx context.Context, addr *address.Address, msgHash []byte, after time.Time) (*tlb.Transaction, error) {
	return nil, nil
}

func (m *nftAPIMock) FindLastTransactionByOutMsgHashAfterTime(ctx context.Context, addr *address.Address, msgHash []byte, after time.Time) (*tlb.Transaction, error) {
	return nil, nil
}

func (m *nftAPIMock) GetOutMsgQueueSizes(ctx context.Context, wc *int32, shard *int64) (*ton.OutMsgQueueSizes, error) {
	return nil, nil
}

func (m *nftAPIMock) GetBlockOutMsgQueueSize(ctx context.Context, block *ton.BlockIDExt) (*ton.BlockOutMsgQueueSize, error) {
	return nil, nil
}

func (m *nftAPIMock) GetDispatchQueueInfo(ctx context.Context, block *ton.BlockIDExt, afterAddr *address.Address, maxAccounts int) (*ton.DispatchQueueInfo, error) {
	return nil, nil
}

func (m *nftAPIMock) GetDispatchQueueMessages(ctx context.Context, block *ton.BlockIDExt, addr *address.Address, afterLT uint64, maxMessages int, options ...func(*ton.GetDispatchQueueMessages)) (*ton.DispatchQueueMessages, error) {
	return nil, nil
}

type failingContent struct {
	err error
}

func (f *failingContent) ContentCell() (*cell.Cell, error) {
	return nil, f.err
}

func addrSlice(addr *address.Address) *cell.Slice {
	return cell.BeginCell().MustStoreAddr(addr).EndCell().BeginParse()
}

func offchainCell(t *testing.T, uri string) *cell.Cell {
	t.Helper()

	cl, err := (&ContentOffchain{URI: uri}).ContentCell()
	if err != nil {
		t.Fatalf("failed to build offchain content cell: %v", err)
	}
	return cl
}

func decode[T any](t *testing.T, cl *cell.Cell, dst *T) {
	t.Helper()
	if err := tlb.LoadFromCell(dst, cl.BeginParse()); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
}

func TestContentFromSlice_ExtraBranches(t *testing.T) {
	t.Run("empty offchain from tiny slice", func(t *testing.T) {
		cl := cell.BeginCell().MustStoreUInt(1, 1).EndCell()
		content, err := ContentFromCell(cl)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		off, ok := content.(*ContentOffchain)
		if !ok {
			t.Fatalf("unexpected content type: %T", content)
		}
		if off.URI != "" {
			t.Fatalf("expected empty uri, got: %q", off.URI)
		}
	})

	t.Run("unknown prefix becomes offchain uri", func(t *testing.T) {
		cl := cell.BeginCell().MustStoreUInt(uint64('A'), 8).MustStoreStringSnake("bc").EndCell()
		content, err := ContentFromCell(cl)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		off, ok := content.(*ContentOffchain)
		if !ok {
			t.Fatalf("unexpected content type: %T", content)
		}
		if off.URI != "Abc" {
			t.Fatalf("unexpected uri: %q", off.URI)
		}
	})
}

func TestContentOnchain_SetAttributeCellChunked(t *testing.T) {
	on := &ContentOnchain{}

	if err := on.SetAttributeCell("chunked", cell.BeginCell().MustStoreUInt(0x01, 8).EndCell()); err != nil {
		t.Fatalf("set attribute cell failed: %v", err)
	}

	if v := on.GetAttributeBinary("chunked"); v != nil {
		t.Fatalf("expected nil for chunked placeholder, got: %x", v)
	}
}

func TestToNftContent(t *testing.T) {
	t.Run("nil content", func(t *testing.T) {
		cl, err := toNftContent(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cl.BitsSize() != 0 || cl.RefsNum() != 0 {
			t.Fatal("nil content should produce an empty cell")
		}
	})

	t.Run("offchain content", func(t *testing.T) {
		cl, err := toNftContent(&ContentOffchain{URI: "https://nft.example/item.json"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		uri, err := cl.BeginParse().LoadStringSnake()
		if err != nil {
			t.Fatalf("failed to parse offchain uri: %v", err)
		}
		if uri != "https://nft.example/item.json" {
			t.Fatalf("unexpected uri: %q", uri)
		}
	})

	t.Run("custom content error", func(t *testing.T) {
		expectedErr := errors.New("content failed")
		_, err := toNftContent(&failingContent{err: expectedErr})
		if !errors.Is(err, expectedErr) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestCollectionClient_GetAtBlockMethods(t *testing.T) {
	collectionAddr := address.MustParseAddr("EQBTObWUuWTb5ECnLI4x6a3szzstmMDOcc5Kdo-CpbUY9Y5K")
	itemAddr := address.MustParseAddr("EQC9bWZd29foipyPOGWlVNVCQzpGAjvi1rGWF7EbNcSVClpA")
	royaltyAddr := address.MustParseAddr("EQCyMa4xOmcr6H0OWbcYtwOTsf6YUSW3KuGj010WgWBVQhoJ")
	ownerAddr := address.MustParseAddr("EQB9ElEc88x6kOZytvUp0_18U-W1V-lBdvPcZ-BXdBWVrmeA")

	m := &nftAPIMock{
		runGetMethod: func(ctx context.Context, blockInfo *ton.BlockIDExt, addr *address.Address, method string, params ...interface{}) (*ton.ExecutionResult, error) {
			if blockInfo.SeqNo != 333 {
				t.Fatalf("unexpected block seqno: %d", blockInfo.SeqNo)
			}
			if !addr.Equals(collectionAddr) {
				t.Fatalf("unexpected collection address: %s", addr.String())
			}

			switch method {
			case "get_nft_address_by_index":
				if len(params) != 1 || params[0].(*big.Int).Cmp(big.NewInt(7)) != 0 {
					t.Fatalf("unexpected params for %s: %#v", method, params)
				}
				return ton.NewExecutionResult([]any{addrSlice(itemAddr)}), nil
			case "royalty_params":
				return ton.NewExecutionResult([]any{big.NewInt(5), big.NewInt(100), addrSlice(royaltyAddr)}), nil
			case "get_nft_content":
				if len(params) != 2 {
					t.Fatalf("unexpected params for %s: %#v", method, params)
				}
				return ton.NewExecutionResult([]any{offchainCell(t, "https://cdn.example/full.json")}), nil
			case "get_collection_data":
				return ton.NewExecutionResult([]any{
					big.NewInt(42),
					offchainCell(t, "https://cdn.example/collection.json"),
					addrSlice(ownerAddr),
				}), nil
			default:
				return nil, errors.New("unexpected method: " + method)
			}
		},
	}

	c := NewCollectionClient(m, collectionAddr)
	block := &ton.BlockIDExt{SeqNo: 333}

	addr, err := c.GetNFTAddressByIndexAtBlock(context.Background(), big.NewInt(7), block)
	if err != nil {
		t.Fatalf("GetNFTAddressByIndexAtBlock failed: %v", err)
	}
	if !addr.Equals(itemAddr) {
		t.Fatalf("unexpected nft addr: %s", addr.String())
	}
	if m.waitedSeq != 333 {
		t.Fatalf("unexpected WaitForBlock seqno: %d", m.waitedSeq)
	}

	roy, err := c.RoyaltyParamsAtBlock(context.Background(), block)
	if err != nil {
		t.Fatalf("RoyaltyParamsAtBlock failed: %v", err)
	}
	if roy.Factor != 5 || roy.Base != 100 || !roy.Address.Equals(royaltyAddr) {
		t.Fatalf("unexpected royalty params: %+v", roy)
	}

	fullContent, err := c.GetNFTContentAtBlock(context.Background(), big.NewInt(7), &ContentOffchain{URI: "item.json"}, block)
	if err != nil {
		t.Fatalf("GetNFTContentAtBlock failed: %v", err)
	}
	off, ok := fullContent.(*ContentOffchain)
	if !ok {
		t.Fatalf("unexpected content type: %T", fullContent)
	}
	if off.URI != "https://cdn.example/full.json" {
		t.Fatalf("unexpected content uri: %q", off.URI)
	}

	data, err := c.GetCollectionDataAtBlock(context.Background(), block)
	if err != nil {
		t.Fatalf("GetCollectionDataAtBlock failed: %v", err)
	}
	if data.NextItemIndex.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("unexpected next index: %s", data.NextItemIndex.String())
	}
	if !data.OwnerAddress.Equals(ownerAddr) {
		t.Fatalf("unexpected owner address: %s", data.OwnerAddress.String())
	}
	off, ok = data.Content.(*ContentOffchain)
	if !ok || off.URI != "https://cdn.example/collection.json" {
		t.Fatalf("unexpected collection content: %#v", data.Content)
	}
}

func TestCollectionClient_WrapperMethods(t *testing.T) {
	collectionAddr := address.MustParseAddr("EQBTObWUuWTb5ECnLI4x6a3szzstmMDOcc5Kdo-CpbUY9Y5K")
	itemAddr := address.MustParseAddr("EQC9bWZd29foipyPOGWlVNVCQzpGAjvi1rGWF7EbNcSVClpA")
	ownerAddr := address.MustParseAddr("EQB9ElEc88x6kOZytvUp0_18U-W1V-lBdvPcZ-BXdBWVrmeA")

	m := &nftAPIMock{
		currentMasterchainInfo: func(ctx context.Context) (*ton.BlockIDExt, error) {
			return &ton.BlockIDExt{SeqNo: 777}, nil
		},
		runGetMethod: func(ctx context.Context, blockInfo *ton.BlockIDExt, addr *address.Address, method string, params ...interface{}) (*ton.ExecutionResult, error) {
			switch method {
			case "get_nft_address_by_index":
				return ton.NewExecutionResult([]any{addrSlice(itemAddr)}), nil
			case "royalty_params":
				return ton.NewExecutionResult([]any{big.NewInt(1), big.NewInt(2), addrSlice(ownerAddr)}), nil
			case "get_nft_content":
				return ton.NewExecutionResult([]any{offchainCell(t, "https://cdn.example/full2.json")}), nil
			case "get_collection_data":
				return ton.NewExecutionResult([]any{big.NewInt(1), offchainCell(t, "https://cdn.example/main2.json"), addrSlice(ownerAddr)}), nil
			default:
				return nil, errors.New("unexpected method: " + method)
			}
		},
	}

	c := NewCollectionClient(m, collectionAddr)

	if _, err := c.GetNFTAddressByIndex(context.Background(), big.NewInt(1)); err != nil {
		t.Fatalf("GetNFTAddressByIndex failed: %v", err)
	}
	if m.waitedSeq != 777 {
		t.Fatalf("unexpected WaitForBlock seqno after GetNFTAddressByIndex: %d", m.waitedSeq)
	}

	if _, err := c.RoyaltyParams(context.Background()); err != nil {
		t.Fatalf("RoyaltyParams failed: %v", err)
	}
	if m.waitedSeq != 777 {
		t.Fatalf("unexpected WaitForBlock seqno after RoyaltyParams: %d", m.waitedSeq)
	}

	if _, err := c.GetNFTContent(context.Background(), big.NewInt(1), &ContentOffchain{URI: "x"}); err != nil {
		t.Fatalf("GetNFTContent failed: %v", err)
	}
	if m.waitedSeq != 777 {
		t.Fatalf("unexpected WaitForBlock seqno after GetNFTContent: %d", m.waitedSeq)
	}

	if _, err := c.GetCollectionData(context.Background()); err != nil {
		t.Fatalf("GetCollectionData failed: %v", err)
	}
	if m.waitedSeq != 777 {
		t.Fatalf("unexpected WaitForBlock seqno after GetCollectionData: %d", m.waitedSeq)
	}
}

func TestCollectionClient_BuildMintPayloads(t *testing.T) {
	owner := address.MustParseAddr("EQCyMa4xOmcr6H0OWbcYtwOTsf6YUSW3KuGj010WgWBVQhoJ")
	editor := address.MustParseAddr("EQB9ElEc88x6kOZytvUp0_18U-W1V-lBdvPcZ-BXdBWVrmeA")

	c := NewCollectionClient(&nftAPIMock{}, owner)

	t.Run("BuildMintPayload", func(t *testing.T) {
		body, err := c.BuildMintPayload(big.NewInt(9), owner, tlb.FromNanoTONU(123), &ContentOffchain{URI: "https://nft.example/9.json"})
		if err != nil {
			t.Fatalf("BuildMintPayload failed: %v", err)
		}

		var payload ItemMintPayload
		decode(t, body, &payload)

		if payload.Index.Cmp(big.NewInt(9)) != 0 {
			t.Fatalf("unexpected index: %s", payload.Index.String())
		}
		if payload.TonAmount.Nano().Cmp(big.NewInt(123)) != 0 {
			t.Fatalf("unexpected amount: %s", payload.TonAmount.Nano().String())
		}

		s := payload.Content.BeginParse()
		parsedOwner, err := s.LoadAddr()
		if err != nil {
			t.Fatalf("failed to parse owner: %v", err)
		}
		if !parsedOwner.Equals(owner) {
			t.Fatalf("unexpected owner: %s", parsedOwner.String())
		}

		content, err := ContentFromCell(s.MustLoadRef().MustToCell())
		if err != nil {
			t.Fatalf("failed to parse nested content: %v", err)
		}
		off := content.(*ContentOffchain)
		if off.URI != "https://nft.example/9.json" {
			t.Fatalf("unexpected nested uri: %q", off.URI)
		}
	})

	t.Run("BuildMintEditablePayload", func(t *testing.T) {
		body, err := c.BuildMintEditablePayload(big.NewInt(11), owner, editor, tlb.FromNanoTONU(456), &ContentOffchain{URI: "https://nft.example/11.json"})
		if err != nil {
			t.Fatalf("BuildMintEditablePayload failed: %v", err)
		}

		var payload ItemMintPayload
		decode(t, body, &payload)

		if payload.Index.Cmp(big.NewInt(11)) != 0 {
			t.Fatalf("unexpected index: %s", payload.Index.String())
		}
		if payload.TonAmount.Nano().Cmp(big.NewInt(456)) != 0 {
			t.Fatalf("unexpected amount: %s", payload.TonAmount.Nano().String())
		}

		s := payload.Content.BeginParse()
		parsedOwner, err := s.LoadAddr()
		if err != nil {
			t.Fatalf("failed to parse owner: %v", err)
		}
		if !parsedOwner.Equals(owner) {
			t.Fatalf("unexpected owner: %s", parsedOwner.String())
		}

		if _, err = ContentFromCell(s.MustLoadRef().MustToCell()); err != nil {
			t.Fatalf("failed to parse nested content: %v", err)
		}

		parsedEditor, err := s.LoadAddr()
		if err != nil {
			t.Fatalf("failed to parse editor: %v", err)
		}
		if !parsedEditor.Equals(editor) {
			t.Fatalf("unexpected editor: %s", parsedEditor.String())
		}
	})

	t.Run("toNftContent error from custom content", func(t *testing.T) {
		expectedErr := errors.New("custom content failed")
		_, err := c.BuildMintPayload(big.NewInt(1), owner, tlb.FromNanoTONU(1), &failingContent{err: expectedErr})
		if !errors.Is(err, expectedErr) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestItemClient_GetNFTData(t *testing.T) {
	nftAddr := address.MustParseAddr("EQC9bWZd29foipyPOGWlVNVCQzpGAjvi1rGWF7EbNcSVClpA")
	collectionAddr := address.MustParseAddr("EQBTObWUuWTb5ECnLI4x6a3szzstmMDOcc5Kdo-CpbUY9Y5K")
	ownerAddr := address.MustParseAddr("EQB9ElEc88x6kOZytvUp0_18U-W1V-lBdvPcZ-BXdBWVrmeA")

	t.Run("GetNFTDataAtBlock with owner and content", func(t *testing.T) {
		m := &nftAPIMock{
			runGetMethod: func(ctx context.Context, blockInfo *ton.BlockIDExt, addr *address.Address, method string, params ...interface{}) (*ton.ExecutionResult, error) {
				if method != "get_nft_data" {
					t.Fatalf("unexpected method: %s", method)
				}
				return ton.NewExecutionResult([]any{
					big.NewInt(1),
					big.NewInt(12),
					addrSlice(collectionAddr),
					addrSlice(ownerAddr),
					offchainCell(t, "https://cdn.example/item12.json"),
				}), nil
			},
		}

		item := NewItemClient(m, nftAddr)
		if !item.GetNFTAddress().Equals(nftAddr) {
			t.Fatalf("unexpected nft address: %s", item.GetNFTAddress().String())
		}

		data, err := item.GetNFTDataAtBlock(context.Background(), &ton.BlockIDExt{SeqNo: 888})
		if err != nil {
			t.Fatalf("GetNFTDataAtBlock failed: %v", err)
		}
		if m.waitedSeq != 888 {
			t.Fatalf("unexpected WaitForBlock seqno: %d", m.waitedSeq)
		}
		if !data.Initialized || data.Index.Cmp(big.NewInt(12)) != 0 {
			t.Fatalf("unexpected basic data: initialized=%v index=%s", data.Initialized, data.Index.String())
		}
		if !data.CollectionAddress.Equals(collectionAddr) {
			t.Fatalf("unexpected collection address: %s", data.CollectionAddress.String())
		}
		if !data.OwnerAddress.Equals(ownerAddr) {
			t.Fatalf("unexpected owner address: %s", data.OwnerAddress.String())
		}
		off := data.Content.(*ContentOffchain)
		if off.URI != "https://cdn.example/item12.json" {
			t.Fatalf("unexpected content uri: %q", off.URI)
		}
	})

	t.Run("GetNFTDataAtBlock with nil owner and content", func(t *testing.T) {
		m := &nftAPIMock{
			runGetMethod: func(ctx context.Context, blockInfo *ton.BlockIDExt, addr *address.Address, method string, params ...interface{}) (*ton.ExecutionResult, error) {
				return ton.NewExecutionResult([]any{
					big.NewInt(0),
					big.NewInt(1),
					addrSlice(collectionAddr),
					nil,
					nil,
				}), nil
			},
		}

		item := NewItemClient(m, nftAddr)
		data, err := item.GetNFTDataAtBlock(context.Background(), &ton.BlockIDExt{SeqNo: 999})
		if err != nil {
			t.Fatalf("GetNFTDataAtBlock failed: %v", err)
		}
		if data.Initialized {
			t.Fatal("item should be uninitialized")
		}
		if !data.OwnerAddress.IsAddrNone() {
			t.Fatalf("expected NONE owner, got: %s", data.OwnerAddress.String())
		}
		if data.Content != nil {
			t.Fatalf("expected nil content, got: %#v", data.Content)
		}
	})

	t.Run("GetNFTData wrapper", func(t *testing.T) {
		m := &nftAPIMock{
			currentMasterchainInfo: func(ctx context.Context) (*ton.BlockIDExt, error) {
				return &ton.BlockIDExt{SeqNo: 4321}, nil
			},
			runGetMethod: func(ctx context.Context, blockInfo *ton.BlockIDExt, addr *address.Address, method string, params ...interface{}) (*ton.ExecutionResult, error) {
				return ton.NewExecutionResult([]any{
					big.NewInt(1),
					big.NewInt(2),
					addrSlice(collectionAddr),
					nil,
					nil,
				}), nil
			},
		}

		item := NewItemClient(m, nftAddr)
		if _, err := item.GetNFTData(context.Background()); err != nil {
			t.Fatalf("GetNFTData failed: %v", err)
		}
		if m.waitedSeq != 4321 {
			t.Fatalf("unexpected WaitForBlock seqno: %d", m.waitedSeq)
		}
	})
}

func TestItemClient_BuildTransferPayload(t *testing.T) {
	nftAddr := address.MustParseAddr("EQC9bWZd29foipyPOGWlVNVCQzpGAjvi1rGWF7EbNcSVClpA")
	newOwner := address.MustParseAddr("EQB9ElEc88x6kOZytvUp0_18U-W1V-lBdvPcZ-BXdBWVrmeA")
	respTo := address.MustParseAddr("EQCyMa4xOmcr6H0OWbcYtwOTsf6YUSW3KuGj010WgWBVQhoJ")

	item := NewItemClient(&nftAPIMock{}, nftAddr)

	t.Run("default response destination and nil forward payload", func(t *testing.T) {
		body, err := item.BuildTransferPayload(newOwner, tlb.FromNanoTONU(7), nil)
		if err != nil {
			t.Fatalf("BuildTransferPayload failed: %v", err)
		}

		var payload TransferPayload
		decode(t, body, &payload)

		if !payload.NewOwner.Equals(newOwner) {
			t.Fatalf("unexpected new owner: %s", payload.NewOwner.String())
		}
		if !payload.ResponseDestination.Equals(newOwner) {
			t.Fatalf("unexpected response destination: %s", payload.ResponseDestination.String())
		}
		if payload.ForwardAmount.Nano().Cmp(big.NewInt(7)) != 0 {
			t.Fatalf("unexpected forward amount: %s", payload.ForwardAmount.Nano().String())
		}
		if payload.ForwardPayload == nil {
			t.Fatal("forward payload should not be nil")
		}
	})

	t.Run("custom response destination", func(t *testing.T) {
		body, err := item.BuildTransferPayload(newOwner, tlb.FromNanoTONU(9), cell.BeginCell().MustStoreUInt(1, 1).EndCell(), respTo)
		if err != nil {
			t.Fatalf("BuildTransferPayload failed: %v", err)
		}

		var payload TransferPayload
		decode(t, body, &payload)

		if !payload.ResponseDestination.Equals(respTo) {
			t.Fatalf("unexpected response destination: %s", payload.ResponseDestination.String())
		}
	})

	t.Run("panic on too many response destinations", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic for too many response destinations")
			}
		}()
		_, _ = item.BuildTransferPayload(newOwner, tlb.FromNanoTONU(1), nil, newOwner, respTo)
	})
}

func TestItemEditableClient_GetEditorAndBuildEditPayload(t *testing.T) {
	nftAddr := address.MustParseAddr("EQC9bWZd29foipyPOGWlVNVCQzpGAjvi1rGWF7EbNcSVClpA")
	editorAddr := address.MustParseAddr("EQCyMa4xOmcr6H0OWbcYtwOTsf6YUSW3KuGj010WgWBVQhoJ")

	t.Run("GetEditorAtBlock and wrapper", func(t *testing.T) {
		m := &nftAPIMock{
			currentMasterchainInfo: func(ctx context.Context) (*ton.BlockIDExt, error) {
				return &ton.BlockIDExt{SeqNo: 654}, nil
			},
			runGetMethod: func(ctx context.Context, blockInfo *ton.BlockIDExt, addr *address.Address, method string, params ...interface{}) (*ton.ExecutionResult, error) {
				if method != "get_editor" {
					t.Fatalf("unexpected method: %s", method)
				}
				return ton.NewExecutionResult([]any{addrSlice(editorAddr)}), nil
			},
		}

		editable := NewItemEditableClient(m, nftAddr)
		ed, err := editable.GetEditorAtBlock(context.Background(), &ton.BlockIDExt{SeqNo: 7777})
		if err != nil {
			t.Fatalf("GetEditorAtBlock failed: %v", err)
		}
		if !ed.Equals(editorAddr) {
			t.Fatalf("unexpected editor: %s", ed.String())
		}
		if m.waitedSeq != 7777 {
			t.Fatalf("unexpected WaitForBlock seqno: %d", m.waitedSeq)
		}

		ed, err = editable.GetEditor(context.Background())
		if err != nil {
			t.Fatalf("GetEditor failed: %v", err)
		}
		if !ed.Equals(editorAddr) {
			t.Fatalf("unexpected editor: %s", ed.String())
		}
		if m.waitedSeq != 654 {
			t.Fatalf("unexpected WaitForBlock seqno after wrapper: %d", m.waitedSeq)
		}
	})

	t.Run("BuildEditPayload offchain without 0x01 prefix", func(t *testing.T) {
		editable := NewItemEditableClient(&nftAPIMock{}, nftAddr)
		body, err := editable.BuildEditPayload(&ContentOffchain{URI: "https://nft.example/item-1.json"})
		if err != nil {
			t.Fatalf("BuildEditPayload failed: %v", err)
		}

		var payload ItemEditPayload
		decode(t, body, &payload)

		uri, err := payload.Content.BeginParse().LoadStringSnake()
		if err != nil {
			t.Fatalf("failed to parse offchain uri: %v", err)
		}
		if uri != "https://nft.example/item-1.json" {
			t.Fatalf("unexpected uri: %q", uri)
		}
	})

	t.Run("BuildEditPayload regular content and error", func(t *testing.T) {
		editable := NewItemEditableClient(&nftAPIMock{}, nftAddr)

		body, err := editable.BuildEditPayload(&ContentOnchain{Name: "Name1"})
		if err != nil {
			t.Fatalf("BuildEditPayload failed: %v", err)
		}

		var payload ItemEditPayload
		decode(t, body, &payload)

		content, err := ContentFromCell(payload.Content)
		if err != nil {
			t.Fatalf("failed to parse onchain content: %v", err)
		}
		on := content.(*ContentOnchain)
		if on.GetAttribute("name") != "Name1" {
			t.Fatalf("unexpected onchain name: %q", on.GetAttribute("name"))
		}

		expectedErr := errors.New("edit content failed")
		_, err = editable.BuildEditPayload(&failingContent{err: expectedErr})
		if !errors.Is(err, expectedErr) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
