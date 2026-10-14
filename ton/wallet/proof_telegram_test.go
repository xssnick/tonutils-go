package wallet

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTonConnectVerifier_TelegramWallet(t *testing.T) {
	code := telegramWalletTestCode(t)
	initialKey := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	currentSeed := [ed25519.SeedSize]byte{1}
	currentKey := ed25519.NewKeyFromSeed(currentSeed[:])
	initialData := telegramWalletTestData(initialKey)
	currentData := telegramWalletTestData(currentKey)
	addr, stateInit := proofTestStateInit(t, code, initialData)

	active := func(data *cell.Cell) *tlb.Account {
		return &tlb.Account{
			IsActive: true,
			State:    &tlb.AccountState{AccountStorage: tlb.AccountStorage{Status: tlb.AccountStatusActive}},
			Code:     code,
			Data:     data,
		}
	}
	uninit := &tlb.Account{
		IsActive: true,
		State:    &tlb.AccountState{AccountStorage: tlb.AccountStorage{Status: tlb.AccountStatusUninit}},
	}
	frozen := &tlb.Account{
		IsActive: true,
		State:    &tlb.AccountState{AccountStorage: tlb.AccountStorage{Status: tlb.AccountStatusFrozen}},
	}
	unknownCode := active(currentData)
	unknownCode.Code = cell.BeginCell().EndCell()
	v4State, err := GetStateInit(initialKey.Public().(ed25519.PublicKey), V4R2, DefaultSubwallet)
	if err != nil {
		t.Fatal(err)
	}
	wrongWalletCode := active(v4State.Data)
	wrongWalletCode.Code = v4State.Code
	missingCode := active(currentData)
	missingCode.Code = nil
	unknownRevision := cell.BeginCell().MustStoreUInt(1, 8).
		MustStoreUInt(0, 64).MustStoreSlice(currentKey.Public().(ed25519.PublicKey), 256).EndCell()
	requestErr := errors.New("account request failed")

	tests := []struct {
		name          string
		account       *tlb.Account
		key           ed25519.PrivateKey
		withStateInit bool
		masterErr     error
		accountErr    error
		wantErr       string
	}{
		{name: "nonexistent", account: &tlb.Account{}, key: initialKey, withStateInit: true},
		{name: "uninit", account: uninit, key: initialKey, withStateInit: true},
		{name: "active initial key", account: active(initialData), key: initialKey, withStateInit: true},
		{name: "rotated key with StateInit", account: active(currentData), key: currentKey, withStateInit: true},
		{name: "old key with StateInit", account: active(currentData), key: initialKey, withStateInit: true,
			wantErr: "signature verification failed"},
		{name: "rotated key without StateInit", account: active(currentData), key: currentKey},
		{name: "old key without StateInit", account: active(currentData), key: initialKey,
			wantErr: "signature verification failed"},
		{name: "nonexistent without StateInit", account: &tlb.Account{}, key: initialKey,
			wantErr: "state init was not passed"},
		{name: "uninit without StateInit", account: uninit, key: initialKey,
			wantErr: "state init was not passed"},
		{name: "frozen", account: frozen, key: initialKey, withStateInit: true,
			wantErr: "account status FROZEN"},
		{name: "unknown current code", account: unknownCode, key: initialKey, withStateInit: true,
			wantErr: "unexpected code"},
		{name: "different known wallet code", account: wrongWalletCode, key: initialKey, withStateInit: true,
			wantErr: "unexpected code"},
		{name: "missing current code", account: missingCode, key: initialKey, withStateInit: true,
			wantErr: "account has no code or data"},
		{name: "missing current data", account: active(nil), key: initialKey, withStateInit: true,
			wantErr: "account has no code or data"},
		{name: "unknown current revision", account: active(unknownRevision), key: initialKey, withStateInit: true,
			wantErr: "unsupported Telegram Wallet revision"},
		{name: "truncated current data", account: active(cell.BeginCell().MustStoreUInt(0, 8).EndCell()),
			key: initialKey, withStateInit: true, wantErr: "failed to parse Telegram wallet data"},
		{name: "master request error", key: initialKey, withStateInit: true, masterErr: requestErr,
			wantErr: "failed to get current master block"},
		{name: "account request error", key: initialKey, withStateInit: true, accountErr: requestErr,
			wantErr: "failed to get account"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &proofTestClient{account: tt.account, masterErr: tt.masterErr, accountErr: tt.accountErr}
			verifier := NewTonConnectVerifier("example.com", time.Minute, client)
			var init []byte
			if tt.withStateInit {
				init = stateInit
			}

			err := verifier.VerifyProof(context.Background(), addr, proofTestSign(addr, tt.key), "challenge", init)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("VerifyProof failed: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
			if (tt.masterErr != nil || tt.accountErr != nil) && !errors.Is(err, requestErr) {
				t.Fatalf("request error was not preserved: %v", err)
			}

			wantAccountCalls := 1
			if tt.masterErr != nil {
				wantAccountCalls = 0
			}
			if client.masterCalls != 1 || client.accountCalls != wantAccountCalls {
				t.Fatalf("master/account calls = %d/%d, want 1/%d", client.masterCalls, client.accountCalls, wantAccountCalls)
			}
		})
	}
}

func TestTonConnectVerifier_TelegramWalletInvalidStateInit(t *testing.T) {
	code := telegramWalletTestCode(t)
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	data := telegramWalletTestData(key)
	tests := []struct {
		name      string
		code      *cell.Cell
		data      *cell.Cell
		wantErr   string
		wantCalls int
	}{
		{name: "missing code", data: data, wantErr: "account has no code or data"},
		{name: "missing data", code: code, wantErr: "account has no code or data", wantCalls: 1},
		{name: "unknown code", code: cell.BeginCell().EndCell(), data: data, wantErr: "unknown code"},
		{name: "missing revision", code: code, data: cell.BeginCell().EndCell(),
			wantErr: "failed to parse Telegram wallet data", wantCalls: 1},
		{name: "unknown revision", code: code, data: cell.BeginCell().MustStoreUInt(1, 8).MustStoreUInt(0, 64).
			MustStoreSlice(key.Public().(ed25519.PublicKey), 256).EndCell(),
			wantErr: "unsupported Telegram Wallet revision", wantCalls: 1},
		{name: "missing prefix", code: code, data: cell.BeginCell().MustStoreUInt(0, 8).EndCell(),
			wantErr: "failed to parse Telegram wallet data", wantCalls: 1},
		{name: "truncated key", code: code, data: cell.BeginCell().MustStoreUInt(0, 8).MustStoreUInt(0, 64).
			MustStoreSlice(key.Public().(ed25519.PublicKey), 248).EndCell(),
			wantErr: "failed to parse Telegram wallet data", wantCalls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, stateInit := proofTestStateInit(t, tt.code, tt.data)
			client := &proofTestClient{account: &tlb.Account{}}
			verifier := NewTonConnectVerifier("example.com", time.Minute, client)
			err := verifier.VerifyProof(context.Background(), addr, proofTestSign(addr, key), "challenge", stateInit)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
			if client.masterCalls != tt.wantCalls || client.accountCalls != tt.wantCalls {
				t.Fatalf("master/account calls = %d/%d, want %d/%d", client.masterCalls, client.accountCalls, tt.wantCalls, tt.wantCalls)
			}
		})
	}

	addr, stateInit := proofTestStateInit(t, code, data)
	t.Run("address mismatch", func(t *testing.T) {
		wrongAddr := address.NewAddress(0, 0, make([]byte, 32))
		verifier := NewTonConnectVerifier("example.com", time.Minute, nil)
		err := verifier.VerifyProof(context.Background(), wrongAddr, proofTestSign(wrongAddr, key), "challenge", stateInit)
		if err == nil || !strings.Contains(err.Error(), "state init hash does not match address") {
			t.Fatalf("expected address mismatch, got %v", err)
		}
	})
	t.Run("invalid boc", func(t *testing.T) {
		verifier := NewTonConnectVerifier("example.com", time.Minute, nil)
		err := verifier.VerifyProof(context.Background(), addr, proofTestSign(addr, key), "challenge", []byte{1, 2, 3})
		if err == nil || !strings.Contains(err.Error(), "failed to parse state init boc") {
			t.Fatalf("expected invalid StateInit BoC, got %v", err)
		}
	})
	t.Run("missing blockchain client", func(t *testing.T) {
		verifier := NewTonConnectVerifier("example.com", time.Minute, nil)
		err := verifier.VerifyProof(context.Background(), addr, proofTestSign(addr, key), "challenge", stateInit)
		if err == nil || !strings.Contains(err.Error(), "blockchain client is required") {
			t.Fatalf("expected missing blockchain client error, got %v", err)
		}
	})
}

func telegramWalletTestCode(t *testing.T) *cell.Cell {
	t.Helper()
	// WalletTrampoline.boc from ton-blockchain/tg-wallet-contract.
	boc, err := hex.DecodeString("b5ee9c7241010101001a000030ff00209821d7498308b9f240df8085f833d0ed1e20ed53d969427e39")
	if err != nil {
		t.Fatal(err)
	}
	code, err := cell.FromBOC(boc)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(code.Hash()) != "9149ae51c1e4689710cebf7830297b16acfbadb363a920a537893e7ffeeca768" {
		t.Fatalf("unexpected Telegram Wallet trampoline hash: %x", code.Hash())
	}
	return code
}

func telegramWalletTestData(key ed25519.PrivateKey) *cell.Cell {
	return cell.BeginCell().MustStoreUInt(0, 8).MustStoreUInt(7, 32).MustStoreUInt(0x7FFF7F11, 32).
		MustStoreSlice(key.Public().(ed25519.PublicKey), 256).EndCell()
}

func proofTestStateInit(t *testing.T, code, data *cell.Cell) (*address.Address, []byte) {
	t.Helper()
	si, err := tlb.ToCell(tlb.StateInit{Code: code, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	return address.NewAddress(0, 0, si.Hash()), si.ToBOC()
}

func proofTestSign(addr *address.Address, key ed25519.PrivateKey) TonConnectProof {
	proof := TonConnectProof{Timestamp: timeNow().Unix(), Payload: "challenge"}
	proof.Domain.Value = "example.com"
	proof.Domain.LengthBytes = uint32(len(proof.Domain.Value))
	return proofTestSignMessage(addr, key, proof)
}

func proofTestSignMessage(addr *address.Address, key ed25519.PrivateKey, proof TonConnectProof) TonConnectProof {
	msg := binary.BigEndian.AppendUint32([]byte("ton-proof-item-v2/"), uint32(addr.Workchain()))
	msg = append(msg, addr.Data()...)
	msg = binary.LittleEndian.AppendUint32(msg, proof.Domain.LengthBytes)
	msg = append(msg, proof.Domain.Value...)
	msg = binary.LittleEndian.AppendUint64(msg, uint64(proof.Timestamp))
	msg = append(msg, proof.Payload...)
	msgHash := sha256.Sum256(msg)
	fullHash := sha256.Sum256(append([]byte("\xff\xffton-connect"), msgHash[:]...))
	proof.Signature = ed25519.Sign(key, fullHash[:])
	return proof
}

type proofTestClient struct {
	ton.APIClientWrapped
	account                   *tlb.Account
	masterErr, accountErr     error
	masterCalls, accountCalls int
}

func (c *proofTestClient) CurrentMasterchainInfo(context.Context) (*ton.BlockIDExt, error) {
	c.masterCalls++
	return &ton.BlockIDExt{SeqNo: 1}, c.masterErr
}

func (c *proofTestClient) GetAccount(context.Context, *ton.BlockIDExt, *address.Address) (*tlb.Account, error) {
	c.accountCalls++
	return c.account, c.accountErr
}
