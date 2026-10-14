package wallet

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTonConnectVerifier_TelegramAccountProof(t *testing.T) {
	ctx := context.Background()
	initialKey := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	currentSeed := [ed25519.SeedSize]byte{1}
	currentKey := ed25519.NewKeyFromSeed(currentSeed[:])
	si, err := GetStateInit(initialKey.Public().(ed25519.PublicKey), Telegram, TelegramMainnetSubwallet)
	if err != nil {
		t.Fatal(err)
	}
	initCell, err := tlb.ToCell(si)
	if err != nil {
		t.Fatal(err)
	}
	addr := address.NewAddress(0, 255, initCell.Hash())
	stateInit := initCell.ToBOC()
	active := proofChainAccount(t, addr, tlb.AccountStorage{
		Status:  tlb.AccountStatusActive,
		Balance: tlb.FromNanoTONU(1),
		StateInit: &tlb.StateInit{
			Code: si.Code,
			Data: telegramWalletTestData(currentKey),
		},
	})
	response, stateCell := proofChainResponse(t, addr, active)

	t.Run("rotated key", func(t *testing.T) {
		client := &proofChainClient{
			APIClientWrapped: ton.NewAPIClient(&proofChainLite{response: response}, ton.ProofCheckPolicySecure),
			block:            response.ID,
		}
		acc, err := client.GetAccount(ctx, response.ID, addr)
		if err != nil || !acc.IsActive || acc.State.Status != tlb.AccountStatusActive {
			t.Fatalf("GetAccount did not authenticate the active account: %v", err)
		}
		verifier := NewTonConnectVerifier("example.com", time.Minute, client)
		if err = verifier.VerifyProof(ctx, addr, proofTestSign(addr, currentKey), "challenge", stateInit); err != nil {
			t.Fatalf("current key was rejected: %v", err)
		}
		if err = verifier.VerifyProof(ctx, addr, proofTestSign(addr, initialKey), "challenge", stateInit); err == nil {
			t.Fatal("initial key was accepted after rotation")
		}
	})

	t.Run("pruned account is not absent", func(t *testing.T) {
		// Reveal the accounts container but hide its dictionary root. Both proofs
		// authenticate the same state hash and use exactly the same trusted block.
		skeleton := cell.CreateProofSkeleton()
		skeleton.ProofRef(1)
		pruned, err := stateCell.CreateProof(skeleton)
		if err != nil {
			t.Fatal(err)
		}
		if err = cell.CheckProof(pruned, stateCell.Hash()); err != nil {
			t.Fatalf("pruned proof does not authenticate the original state: %v", err)
		}
		hidden := response
		hidden.Proof = proofChainRoundTrip(t, []*cell.Cell{response.Proof[0], pruned})
		hidden.State = nil
		_, _, err = ton.CheckAccountStateProof(addr, response.ID, hidden.Proof, nil, nil, false)
		if !errors.Is(err, cell.ErrDictHasSpecialCells) || errors.Is(err, ton.ErrNoAddrInProof) {
			t.Fatalf("pruned lookup must preserve its error instead of proving absence: %v", err)
		}

		client := &proofChainClient{
			APIClientWrapped: ton.NewAPIClient(&proofChainLite{response: hidden}, ton.ProofCheckPolicySecure),
			block:            response.ID,
		}
		if _, err = client.GetAccount(ctx, response.ID, addr); !errors.Is(err, cell.ErrDictHasSpecialCells) {
			t.Fatalf("GetAccount accepted an incomplete account proof: %v", err)
		}
		verifier := NewTonConnectVerifier("example.com", time.Minute, client)
		if err = verifier.VerifyProof(ctx, addr, proofTestSign(addr, initialKey), "challenge", stateInit); !errors.Is(err, cell.ErrDictHasSpecialCells) {
			t.Fatalf("VerifyProof fell back to the initial key on an incomplete account proof: %v", err)
		}
	})

	otherHash := append([]byte(nil), addr.Data()...)
	otherHash[0] ^= 0x80
	otherAddr := address.NewAddress(0, 255, otherHash)
	uninit := tlb.AccountStorage{Status: tlb.AccountStatusUninit, Balance: tlb.FromNanoTONU(1_000_000_000)}
	for _, tt := range []struct {
		name    string
		addr    *address.Address
		account *cell.Cell
		absent  bool
	}{
		{name: "empty accounts", addr: addr, absent: true},
		{name: "another account only", addr: otherAddr, account: proofChainAccount(t, otherAddr, uninit), absent: true},
		{name: "funded uninit", addr: addr, account: proofChainAccount(t, addr, uninit)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			response, _ := proofChainResponse(t, tt.addr, tt.account)
			_, _, err := ton.CheckAccountStateProof(addr, response.ID, response.Proof, nil, nil, false)
			if tt.absent {
				if !errors.Is(err, ton.ErrNoAddrInProof) {
					t.Fatalf("genuine absence was not proven: %v", err)
				}
				response.State = nil
			} else if err != nil {
				t.Fatalf("uninit account proof was rejected: %v", err)
			}
			client := &proofChainClient{
				APIClientWrapped: ton.NewAPIClient(&proofChainLite{response: response}, ton.ProofCheckPolicySecure),
				block:            response.ID,
			}
			acc, err := client.GetAccount(ctx, response.ID, addr)
			if err != nil {
				t.Fatalf("GetAccount rejected the valid proof: %v", err)
			}
			if tt.absent {
				if acc.IsActive {
					t.Fatal("nonexistent account was returned as present")
				}
			} else if !acc.IsActive || acc.State.Status != tlb.AccountStatusUninit || acc.State.Balance.Nano().Sign() <= 0 {
				t.Fatal("funded uninit account was not preserved")
			}
			verifier := NewTonConnectVerifier("example.com", time.Minute, client)
			if err = verifier.VerifyProof(ctx, addr, proofTestSign(addr, initialKey), "challenge", stateInit); err != nil {
				t.Fatalf("initial StateInit authorization was rejected: %v", err)
			}
		})
	}
}

func proofChainAccount(t *testing.T, addr *address.Address, storage tlb.AccountStorage) *cell.Cell {
	t.Helper()
	account, err := (tlb.AccountState{
		IsValid: true,
		Address: addr,
		StorageInfo: tlb.StorageInfo{
			StorageUsed:  tlb.StorageUsed{CellsUsed: big.NewInt(1), BitsUsed: big.NewInt(328)},
			StorageExtra: tlb.StorageExtraNone{},
		},
		AccountStorage: storage,
	}).ToCell()
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func proofChainResponse(t *testing.T, addr *address.Address, account *cell.Cell) (ton.AccountState, *cell.Cell) {
	t.Helper()
	dict, err := tlb.NewShardAccountsAugDict()
	if err != nil {
		t.Fatal(err)
	}
	if account != nil {
		value, err := tlb.ToCell(tlb.ShardAccount{Account: account, LastTransHash: make([]byte, 32), LastTransLT: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err = dict.Set(cell.BeginCell().MustStoreSlice(addr.Data(), 256).EndCell(), value); err != nil {
			t.Fatal(err)
		}
	}
	state := tlb.ShardStateUnsplit{
		GlobalID: -239, ShardIdent: tlb.ShardIdent{WorkchainID: -1}, Seqno: 7,
		OutMsgQueueInfo: cell.BeginCell().EndCell(), Stats: cell.BeginCell().EndCell(),
	}
	state.Accounts.ShardAccounts = dict
	stateCell, err := tlb.ToCell(state)
	if err != nil {
		t.Fatal(err)
	}
	update, err := cell.CreateMerkleUpdate(stateCell, stateCell)
	if err != nil {
		t.Fatal(err)
	}
	empty := cell.BeginCell().EndCell()
	header := tlb.BlockHeader{PrevRef: tlb.BlkPrevInfo{Prev1: tlb.ExtBlkRef{RootHash: make([]byte, 32), FileHash: make([]byte, 32)}}}
	header.SeqNo, header.Shard.WorkchainID = 7, -1
	blockCell, err := tlb.ToCell(tlb.Block{
		GlobalID: -239, BlockInfo: header, ValueFlow: empty, StateUpdate: update,
		Extra: &tlb.BlockExtra{
			InMsgDesc: empty, OutMsgDesc: empty, ShardAccountBlocks: empty,
			RandSeed: make([]byte, 32), CreatedBy: make([]byte, 32),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	block := &ton.BlockIDExt{Workchain: -1, SeqNo: 7, RootHash: blockCell.Hash(), FileHash: make([]byte, 32)}
	blockSkeleton := cell.CreateProofSkeleton()
	blockSkeleton.ProofRef(0).SetRecursive()
	blockSkeleton.ProofRef(2)
	blockProof, err := blockCell.CreateProof(blockSkeleton)
	if err != nil {
		t.Fatal(err)
	}
	stateSkeleton := cell.CreateProofSkeleton()
	stateSkeleton.SetRecursive()
	stateProof, err := stateCell.CreateProof(stateSkeleton)
	if err != nil {
		t.Fatal(err)
	}
	if err = cell.CheckProof(stateProof, stateCell.Hash()); err != nil {
		t.Fatal(err)
	}
	if account != nil {
		account, err = cell.FromBOC(account.ToBOC())
		if err != nil {
			t.Fatal(err)
		}
	}
	return ton.AccountState{
		ID: block, Proof: proofChainRoundTrip(t, []*cell.Cell{blockProof, stateProof}), State: account,
	}, stateCell
}

func proofChainRoundTrip(t *testing.T, roots []*cell.Cell) []*cell.Cell {
	t.Helper()
	parsed, err := cell.FromBOCMultiRoot(cell.ToBOCWithOptions(roots, cell.BOCSerializeOptions{WithCRC32C: true}))
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

type proofChainLite struct {
	ton.LiteClient
	response ton.AccountState
}

func (l *proofChainLite) QueryLiteserver(_ context.Context, request tl.Serializable, result tl.Serializable) error {
	if _, ok := request.(ton.GetAccountState); !ok {
		return fmt.Errorf("unexpected query %T", request)
	}
	*(result.(*tl.Serializable)) = l.response
	return nil
}

// The fixture block is the trusted anchor. The real APIClient still checks all
// block, shard-state, account-dictionary and account-state Merkle proofs.
type proofChainClient struct {
	ton.APIClientWrapped
	block *ton.BlockIDExt
}

func (c *proofChainClient) CurrentMasterchainInfo(context.Context) (*ton.BlockIDExt, error) {
	return c.block, nil
}
