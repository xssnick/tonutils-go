package nft

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func emptySlice() *cell.Slice {
	return cell.BeginCell().EndCell().BeginParse()
}

func malformedOnchainCell() *cell.Cell {
	return cell.BeginCell().MustStoreUInt(0x00, 8).MustStoreBoolBit(true).EndCell()
}

func assertWrappedErr(t *testing.T, err error, expected error, contains string) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected error with fragment %q", contains)
	}
	if expected != nil && !errors.Is(err, expected) {
		t.Fatalf("expected wrapped error %v, got: %v", expected, err)
	}
	if contains != "" && !strings.Contains(err.Error(), contains) {
		t.Fatalf("expected error to contain %q, got: %v", contains, err)
	}
}

func TestContentFromSlice_ErrorBranches(t *testing.T) {
	t.Run("fallback to ref when bits are too small", func(t *testing.T) {
		root := cell.BeginCell().MustStoreRef(offchainCell(t, "https://cdn.example/ref.json")).EndCell()
		content, err := ContentFromCell(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		off, ok := content.(*ContentOffchain)
		if !ok {
			t.Fatalf("unexpected content type: %T", content)
		}
		if off.URI != "https://cdn.example/ref.json" {
			t.Fatalf("unexpected uri: %q", off.URI)
		}
	})

	t.Run("failed to load type", func(t *testing.T) {
		root := cell.BeginCell().MustStoreRef(cell.BeginCell().EndCell()).EndCell()
		_, err := ContentFromCell(root)
		assertWrappedErr(t, err, nil, "failed to load type")
	})

	t.Run("failed to load dict onchain data", func(t *testing.T) {
		_, err := ContentFromCell(malformedOnchainCell())
		assertWrappedErr(t, err, nil, "failed to load dict onchain data")
	})

	t.Run("failed to load snake offchain data for type 0x01", func(t *testing.T) {
		cl := cell.BeginCell().
			MustStoreUInt(0x01, 8).
			MustStoreRef(cell.BeginCell().EndCell()).
			MustStoreRef(cell.BeginCell().EndCell()).
			EndCell()
		_, err := ContentFromCell(cl)
		assertWrappedErr(t, err, nil, "failed to load snake offchain data")
	})

	t.Run("failed to load snake offchain data for unknown type", func(t *testing.T) {
		cl := cell.BeginCell().
			MustStoreUInt(0x7F, 8).
			MustStoreRef(cell.BeginCell().EndCell()).
			MustStoreRef(cell.BeginCell().EndCell()).
			EndCell()
		_, err := ContentFromCell(cl)
		assertWrappedErr(t, err, nil, "failed to load snake offchain data")
	})
}

func TestContentSemichain_ContentCellBranches(t *testing.T) {
	t.Run("skip uri injection when uri is empty", func(t *testing.T) {
		semi := &ContentSemichain{
			ContentOffchain: ContentOffchain{URI: ""},
			ContentOnchain:  ContentOnchain{Name: "NoURI"},
		}
		cl, err := semi.ContentCell()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		content, err := ContentFromCell(cl)
		if err != nil {
			t.Fatalf("unexpected parse error: %v", err)
		}
		on, ok := content.(*ContentOnchain)
		if !ok {
			t.Fatalf("expected onchain content, got: %T", content)
		}
		if on.GetAttribute("uri") != "" {
			t.Fatalf("uri should stay empty, got: %q", on.GetAttribute("uri"))
		}
	})

	t.Run("keep existing uri attribute", func(t *testing.T) {
		semi := &ContentSemichain{
			ContentOffchain: ContentOffchain{URI: "https://new.example/ignored.json"},
			ContentOnchain:  ContentOnchain{Name: "HasURI"},
		}
		if err := semi.ContentOnchain.SetAttribute("uri", "https://preset.example/value.json"); err != nil {
			t.Fatalf("failed to preset uri: %v", err)
		}

		cl, err := semi.ContentCell()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		content, err := ContentFromCell(cl)
		if err != nil {
			t.Fatalf("unexpected parse error: %v", err)
		}
		sem, ok := content.(*ContentSemichain)
		if !ok {
			t.Fatalf("expected semichain content, got: %T", content)
		}
		if sem.URI != "https://preset.example/value.json" {
			t.Fatalf("expected preset uri to remain, got: %q", sem.URI)
		}
	})
}

func TestCollectionClient_ErrorPaths(t *testing.T) {
	collectionAddr := address.MustParseAddr("EQBTObWUuWTb5ECnLI4x6a3szzstmMDOcc5Kdo-CpbUY9Y5K")
	ownerAddr := address.MustParseAddr("EQB9ElEc88x6kOZytvUp0_18U-W1V-lBdvPcZ-BXdBWVrmeA")
	block := &ton.BlockIDExt{SeqNo: 10}
	baseErr := errors.New("boom")

	t.Run("wrapper methods masterchain error", func(t *testing.T) {
		m := &nftAPIMock{
			currentMasterchainInfo: func(ctx context.Context) (*ton.BlockIDExt, error) {
				return nil, baseErr
			},
		}
		c := NewCollectionClient(m, collectionAddr)

		_, err := c.GetNFTAddressByIndex(context.Background(), big.NewInt(1))
		assertWrappedErr(t, err, baseErr, "failed to get masterchain info")

		_, err = c.RoyaltyParams(context.Background())
		assertWrappedErr(t, err, baseErr, "failed to get masterchain info")

		_, err = c.GetNFTContent(context.Background(), big.NewInt(1), &ContentOffchain{URI: "x"})
		assertWrappedErr(t, err, baseErr, "failed to get masterchain info")

		_, err = c.GetCollectionData(context.Background())
		assertWrappedErr(t, err, baseErr, "failed to get masterchain info")
	})

	t.Run("GetNFTAddressByIndexAtBlock errors", func(t *testing.T) {
		cases := []struct {
			name     string
			result   *ton.ExecutionResult
			runErr   error
			contains string
		}{
			{name: "run method error", runErr: baseErr, contains: "failed to run get_nft_address_by_index method"},
			{name: "result not slice", result: ton.NewExecutionResult([]any{big.NewInt(1)}), contains: "result get err"},
			{name: "bad address in slice", result: ton.NewExecutionResult([]any{emptySlice()}), contains: "failed to load address from result slice"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				m := &nftAPIMock{
					runGetMethod: func(ctx context.Context, blockInfo *ton.BlockIDExt, addr *address.Address, method string, params ...interface{}) (*ton.ExecutionResult, error) {
						if tc.runErr != nil {
							return nil, tc.runErr
						}
						return tc.result, nil
					},
				}
				c := NewCollectionClient(m, collectionAddr)
				_, err := c.GetNFTAddressByIndexAtBlock(context.Background(), big.NewInt(1), block)
				assertWrappedErr(t, err, tc.runErr, tc.contains)
			})
		}
	})

	t.Run("RoyaltyParamsAtBlock errors", func(t *testing.T) {
		cases := []struct {
			name     string
			result   *ton.ExecutionResult
			runErr   error
			contains string
		}{
			{name: "run method error", runErr: baseErr, contains: "failed to run royalty_params method"},
			{name: "factor parse error", result: ton.NewExecutionResult([]any{"bad"}), contains: "factor get err"},
			{name: "base parse error", result: ton.NewExecutionResult([]any{big.NewInt(1), "bad"}), contains: "base get err"},
			{name: "address slice parse error", result: ton.NewExecutionResult([]any{big.NewInt(1), big.NewInt(2), "bad"}), contains: "addr slice get err"},
			{name: "address load error", result: ton.NewExecutionResult([]any{big.NewInt(1), big.NewInt(2), emptySlice()}), contains: "failed to load address from result slice"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				m := &nftAPIMock{
					runGetMethod: func(ctx context.Context, blockInfo *ton.BlockIDExt, addr *address.Address, method string, params ...interface{}) (*ton.ExecutionResult, error) {
						if tc.runErr != nil {
							return nil, tc.runErr
						}
						return tc.result, nil
					},
				}
				c := NewCollectionClient(m, collectionAddr)
				_, err := c.RoyaltyParamsAtBlock(context.Background(), block)
				assertWrappedErr(t, err, tc.runErr, tc.contains)
			})
		}
	})

	t.Run("GetNFTContentAtBlock errors", func(t *testing.T) {
		cases := []struct {
			name      string
			content   ContentAny
			result    *ton.ExecutionResult
			runErr    error
			contains  string
			expected  error
			skipRunGM bool
		}{
			{name: "content convert error", content: &failingContent{err: baseErr}, contains: "failed to convert nft content to cell", expected: baseErr, skipRunGM: true},
			{name: "run method error", content: &ContentOffchain{URI: "x"}, runErr: baseErr, contains: "failed to run get_nft_content method", expected: baseErr},
			{name: "result get error", content: &ContentOffchain{URI: "x"}, result: ton.NewExecutionResult([]any{"bad"}), contains: "result get err"},
			{name: "content parse error", content: &ContentOffchain{URI: "x"}, result: ton.NewExecutionResult([]any{malformedOnchainCell()}), contains: "failed to parse content"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				m := &nftAPIMock{
					runGetMethod: func(ctx context.Context, blockInfo *ton.BlockIDExt, addr *address.Address, method string, params ...interface{}) (*ton.ExecutionResult, error) {
						if tc.skipRunGM {
							t.Fatal("RunGetMethod should not be called")
						}
						if tc.runErr != nil {
							return nil, tc.runErr
						}
						return tc.result, nil
					},
				}
				c := NewCollectionClient(m, collectionAddr)
				_, err := c.GetNFTContentAtBlock(context.Background(), big.NewInt(1), tc.content, block)
				assertWrappedErr(t, err, tc.expected, tc.contains)
			})
		}
	})

	t.Run("GetCollectionDataAtBlock errors", func(t *testing.T) {
		cases := []struct {
			name     string
			result   *ton.ExecutionResult
			runErr   error
			contains string
		}{
			{name: "run method error", runErr: baseErr, contains: "failed to run get_collection_data method"},
			{name: "next index parse error", result: ton.NewExecutionResult([]any{"bad"}), contains: "next index get err"},
			{name: "content parse error on result index", result: ton.NewExecutionResult([]any{big.NewInt(1), "bad"}), contains: "content get err"},
			{name: "owner slice parse error", result: ton.NewExecutionResult([]any{big.NewInt(1), offchainCell(t, "https://x"), "bad"}), contains: "owner get err"},
			{name: "owner address load error", result: ton.NewExecutionResult([]any{big.NewInt(1), offchainCell(t, "https://x"), emptySlice()}), contains: "failed to load owner address from result slice"},
			{name: "content parse error", result: ton.NewExecutionResult([]any{big.NewInt(1), malformedOnchainCell(), addrSlice(ownerAddr)}), contains: "failed to parse content"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				m := &nftAPIMock{
					runGetMethod: func(ctx context.Context, blockInfo *ton.BlockIDExt, addr *address.Address, method string, params ...interface{}) (*ton.ExecutionResult, error) {
						if tc.runErr != nil {
							return nil, tc.runErr
						}
						return tc.result, nil
					},
				}
				c := NewCollectionClient(m, collectionAddr)
				_, err := c.GetCollectionDataAtBlock(context.Background(), block)
				assertWrappedErr(t, err, tc.runErr, tc.contains)
			})
		}
	})
}

func TestCollectionClient_BuildMintEditablePayload_ErrorPath(t *testing.T) {
	owner := address.MustParseAddr("EQCyMa4xOmcr6H0OWbcYtwOTsf6YUSW3KuGj010WgWBVQhoJ")
	editor := address.MustParseAddr("EQB9ElEc88x6kOZytvUp0_18U-W1V-lBdvPcZ-BXdBWVrmeA")
	baseErr := errors.New("content failed")

	c := NewCollectionClient(&nftAPIMock{}, owner)
	_, err := c.BuildMintEditablePayload(big.NewInt(1), owner, editor, tlb.FromNanoTONU(1), &failingContent{err: baseErr})
	assertWrappedErr(t, err, baseErr, "failed to convert nft content to cell")
}

func TestItemClient_ErrorPaths(t *testing.T) {
	nftAddr := address.MustParseAddr("EQC9bWZd29foipyPOGWlVNVCQzpGAjvi1rGWF7EbNcSVClpA")
	collectionAddr := address.MustParseAddr("EQBTObWUuWTb5ECnLI4x6a3szzstmMDOcc5Kdo-CpbUY9Y5K")
	baseErr := errors.New("boom")

	t.Run("GetNFTData wrapper masterchain error", func(t *testing.T) {
		m := &nftAPIMock{
			currentMasterchainInfo: func(ctx context.Context) (*ton.BlockIDExt, error) {
				return nil, baseErr
			},
		}
		c := NewItemClient(m, nftAddr)
		_, err := c.GetNFTData(context.Background())
		assertWrappedErr(t, err, baseErr, "failed to get masterchain info")
	})

	t.Run("GetNFTDataAtBlock errors", func(t *testing.T) {
		block := &ton.BlockIDExt{SeqNo: 99}
		cases := []struct {
			name     string
			result   *ton.ExecutionResult
			runErr   error
			contains string
		}{
			{name: "run method error", runErr: baseErr, contains: "failed to run get_nft_data method"},
			{name: "init parse error", result: ton.NewExecutionResult([]any{"bad"}), contains: "err get init value"},
			{name: "index parse error", result: ton.NewExecutionResult([]any{big.NewInt(1), "bad"}), contains: "err get index value"},
			{name: "collection slice parse error", result: ton.NewExecutionResult([]any{big.NewInt(1), big.NewInt(1), "bad"}), contains: "err get collection slice value"},
			{name: "collection address load error", result: ton.NewExecutionResult([]any{big.NewInt(1), big.NewInt(1), emptySlice()}), contains: "failed to load collection address from result slice"},
			{name: "owner nil check error", result: ton.NewExecutionResult([]any{big.NewInt(1), big.NewInt(1), addrSlice(collectionAddr)}), contains: "err check for nil owner slice value"},
			{name: "owner slice parse error", result: ton.NewExecutionResult([]any{big.NewInt(1), big.NewInt(1), addrSlice(collectionAddr), "bad"}), contains: "err get owner slice value"},
			{name: "owner address load error", result: ton.NewExecutionResult([]any{big.NewInt(1), big.NewInt(1), addrSlice(collectionAddr), emptySlice()}), contains: "failed to load owner address from result slice"},
			{name: "content nil check error", result: ton.NewExecutionResult([]any{big.NewInt(1), big.NewInt(1), addrSlice(collectionAddr), nil}), contains: "err check for nil content cell value"},
			{name: "content cell parse error", result: ton.NewExecutionResult([]any{big.NewInt(1), big.NewInt(1), addrSlice(collectionAddr), nil, "bad"}), contains: "err get content cell value"},
			{name: "content parse error", result: ton.NewExecutionResult([]any{big.NewInt(1), big.NewInt(1), addrSlice(collectionAddr), nil, malformedOnchainCell()}), contains: "failed to parse content"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				m := &nftAPIMock{
					runGetMethod: func(ctx context.Context, blockInfo *ton.BlockIDExt, addr *address.Address, method string, params ...interface{}) (*ton.ExecutionResult, error) {
						if tc.runErr != nil {
							return nil, tc.runErr
						}
						return tc.result, nil
					},
				}
				c := NewItemClient(m, nftAddr)
				_, err := c.GetNFTDataAtBlock(context.Background(), block)
				assertWrappedErr(t, err, tc.runErr, tc.contains)
			})
		}
	})
}

func TestItemEditableClient_ErrorPaths(t *testing.T) {
	nftAddr := address.MustParseAddr("EQC9bWZd29foipyPOGWlVNVCQzpGAjvi1rGWF7EbNcSVClpA")
	baseErr := errors.New("boom")
	block := &ton.BlockIDExt{SeqNo: 123}

	t.Run("GetEditor wrapper masterchain error", func(t *testing.T) {
		m := &nftAPIMock{
			currentMasterchainInfo: func(ctx context.Context) (*ton.BlockIDExt, error) {
				return nil, baseErr
			},
		}
		c := NewItemEditableClient(m, nftAddr)
		_, err := c.GetEditor(context.Background())
		assertWrappedErr(t, err, baseErr, "failed to get masterchain info")
	})

	t.Run("GetEditorAtBlock errors", func(t *testing.T) {
		cases := []struct {
			name     string
			result   *ton.ExecutionResult
			runErr   error
			contains string
		}{
			{name: "run method error", runErr: baseErr, contains: "failed to run get_editor method"},
			{name: "result not slice", result: ton.NewExecutionResult([]any{"bad"}), contains: "result is not slice"},
			{name: "address load error", result: ton.NewExecutionResult([]any{emptySlice()}), contains: "failed to load address from result slice"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				m := &nftAPIMock{
					runGetMethod: func(ctx context.Context, blockInfo *ton.BlockIDExt, addr *address.Address, method string, params ...interface{}) (*ton.ExecutionResult, error) {
						if tc.runErr != nil {
							return nil, tc.runErr
						}
						return tc.result, nil
					},
				}
				c := NewItemEditableClient(m, nftAddr)
				_, err := c.GetEditorAtBlock(context.Background(), block)
				assertWrappedErr(t, err, tc.runErr, tc.contains)
			})
		}
	})
}
