package wallet

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func telegramVMConfig(t *testing.T) *tvm.PreparedBlockchainConfig {
	t.Helper()

	// Canonical revision 0 backend, stored under config[-123] by WalletTrampoline.
	// https://github.com/ton-blockchain/tg-wallet-contract/blob/1a29ae5d90e03c3d585d3e42e6ecef691d727a3c/contracts/revisions-boc/WalletTg-rev00.boc
	boc, err := os.ReadFile("testdata/telegram-rev00.boc")
	if err != nil {
		t.Fatal(err)
	}
	backend, err := cell.FromBOC(boc)
	if err != nil {
		t.Fatal(err)
	}
	if hash := fmt.Sprintf("%x", backend.HashKey()); hash != "e30911420bef1191c09dce58b9df2b4ca4c2d9c383cc3b6a91170349ffa70e2c" {
		t.Fatalf("unexpected revision 0 backend hash: %s", hash)
	}

	version, err := tlb.ToCell(&tlb.GlobalVersion{Version: uint32(vm.MaxSupportedGlobalVersion)})
	if err != nil {
		t.Fatal(err)
	}
	dict := cell.NewDict(32)
	for id, param := range map[int64]*cell.Cell{8: version, -123: backend} {
		if err = dict.SetIntKey(big.NewInt(id), cell.BeginCell().MustStoreRef(param).EndCell()); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := tvm.PrepareBlockchainConfigLenient(dict.AsCell())
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestTelegramWalletVM(t *testing.T) {
	const now = uint32(1_740_000_000)
	previousTimeNow := timeNow
	timeNow = func() time.Time { return time.Unix(int64(now), 0) }
	t.Cleanup(func() { timeNow = previousTimeNow })

	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x37}, ed25519.SeedSize))
	config := telegramVMConfig(t)
	dst := address.NewAddress(0, 0, bytes.Repeat([]byte{0x42}, 32))

	for _, count := range []int{1, 7, 255} {
		t.Run(fmt.Sprintf("send %d messages", count), func(t *testing.T) {
			w, err := FromPrivateKeyWithOptions(key, Telegram)
			if err != nil {
				t.Fatal(err)
			}
			w.GetSpec().(*SpecTelegram).SetSeqnoFetcher(func(context.Context, uint32) (uint32, error) {
				return 0, nil
			})
			messages := make([]*Message, count)
			for i := range messages {
				messages[i] = SimpleMessage(dst, tlb.FromNanoTONU(uint64(i+1)),
					cell.BeginCell().MustStoreUInt(uint64(i), 16).EndCell())
			}
			ext, err := w.PrepareExternalMessageForMany(context.Background(), true, messages)
			if err != nil {
				t.Fatal(err)
			}

			res, err := tvm.NewTVM().EmulateExternalMessage(ext.StateInit.Code, ext.StateInit.Data, ext,
				tvm.EmulateExternalMessageConfig{
					Now:      now,
					Balance:  big.NewInt(10_000_000_000),
					RandSeed: make([]byte, 32),
					Config:   config,
				})
			if err != nil {
				t.Fatal(err)
			}
			if res.ExitCode != 0 || !res.Accepted || !res.Committed {
				t.Fatalf("exit=%d, accepted=%v, committed=%v", res.ExitCode, res.Accepted, res.Committed)
			}
			wantData := cell.BeginCell().MustStoreUInt(0, 8).MustStoreUInt(1, 32).
				MustStoreUInt(uint64(w.GetSubwalletID()), 32).
				MustStoreSlice(key.Public().(ed25519.PublicKey), 256).EndCell()
			if res.Data.HashKey() != wantData.HashKey() {
				t.Fatalf("unexpected storage after first send: %s", res.Data.Dump())
			}
			if res.Code.HashKey() != ext.StateInit.Code.HashKey() {
				t.Fatal("execution changed trampoline code")
			}

			actions, err := tlb.LoadOutList(res.Actions)
			if err != nil {
				t.Fatal(err)
			}
			if len(actions) != count {
				t.Fatalf("got %d outgoing actions, want %d", len(actions), count)
			}
			for i, action := range actions {
				send, ok := action.(tlb.ActionSendMsg)
				if !ok {
					t.Fatalf("action %d has unexpected type %T", i, action)
				}
				wantMsg, err := tlb.ToCell(messages[i].InternalMessage)
				if err != nil {
					t.Fatal(err)
				}
				if send.Mode != messages[i].Mode || send.Msg.HashKey() != wantMsg.HashKey() {
					t.Fatalf("outgoing action %d differs from requested message", i)
				}
			}
		})
	}

	t.Run("send after key rotation", func(t *testing.T) {
		initialPubKey := key.Public().(ed25519.PublicKey)
		currentKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x38}, ed25519.SeedSize))
		currentPubKey := currentKey.Public().(ed25519.PublicKey)
		w, err := FromPubKeyWithOptions(initialPubKey, Telegram, WithPrivateKey(currentKey))
		if err != nil {
			t.Fatal(err)
		}
		initialAddr, err := AddressFromPubKey(initialPubKey, Telegram, TelegramMainnetSubwallet)
		if err != nil {
			t.Fatal(err)
		}
		w.GetSpec().(*SpecTelegram).SetSeqnoFetcher(func(context.Context, uint32) (uint32, error) {
			return 1, nil
		})
		ext, err := w.PrepareExternalMessageForMany(context.Background(), false, []*Message{
			SimpleMessage(dst, tlb.FromNanoTONU(1), cell.BeginCell().EndCell()),
		})
		if err != nil {
			t.Fatal(err)
		}
		if !ext.DstAddr.Equals(initialAddr) || ext.StateInit != nil {
			t.Fatal("send after rotation must target the original address without StateInit")
		}

		// Rotation updates storage, while the original address and trampoline stay fixed.
		currentData := cell.BeginCell().MustStoreUInt(0, 8).MustStoreUInt(1, 32).
			MustStoreUInt(uint64(TelegramMainnetSubwallet), 32).MustStoreSlice(currentPubKey, 256).EndCell()
		res, err := tvm.NewTVM().EmulateExternalMessage(walletCode[Telegram], currentData, ext,
			tvm.EmulateExternalMessageConfig{
				Now:      now,
				Balance:  big.NewInt(10_000_000_000),
				RandSeed: make([]byte, 32),
				Config:   config,
			})
		if err != nil {
			t.Fatal(err)
		}
		if res.ExitCode != 0 || !res.Accepted || !res.Committed {
			t.Fatalf("exit=%d, accepted=%v, committed=%v", res.ExitCode, res.Accepted, res.Committed)
		}
		wantData := cell.BeginCell().MustStoreUInt(0, 8).MustStoreUInt(2, 32).
			MustStoreUInt(uint64(TelegramMainnetSubwallet), 32).MustStoreSlice(currentPubKey, 256).EndCell()
		if res.Data.HashKey() != wantData.HashKey() {
			t.Fatalf("unexpected storage after send with rotated key: %s", res.Data.Dump())
		}
	})

	t.Run("invalid signature", func(t *testing.T) {
		w, err := FromPrivateKeyWithOptions(key, Telegram)
		if err != nil {
			t.Fatal(err)
		}
		w.GetSpec().(*SpecTelegram).SetSeqnoFetcher(func(context.Context, uint32) (uint32, error) {
			return 0, nil
		})
		ext, err := w.PrepareExternalMessageForMany(context.Background(), true, []*Message{
			SimpleMessage(dst, tlb.FromNanoTONU(1), cell.BeginCell().EndCell()),
		})
		if err != nil {
			t.Fatal(err)
		}
		body := ext.Body.MustBeginParse()
		signature := body.MustLoadSlice(512)
		signature[0] ^= 1
		ext.Body = cell.BeginCell().MustStoreSlice(signature, 512).MustStoreBuilder(body.ToBuilder()).EndCell()

		res, err := tvm.NewTVM().EmulateExternalMessage(ext.StateInit.Code, ext.StateInit.Data, ext,
			tvm.EmulateExternalMessageConfig{
				Now:      now,
				Balance:  big.NewInt(10_000_000_000),
				RandSeed: make([]byte, 32),
				Config:   config,
			})
		if err != nil {
			t.Fatal(err)
		}
		if res.ExitCode == 0 || res.Accepted || res.Committed || res.Actions != nil {
			t.Fatalf("invalid signature accepted: exit=%d, accepted=%v, committed=%v",
				res.ExitCode, res.Accepted, res.Committed)
		}
		if res.Data.HashKey() != ext.StateInit.Data.HashKey() {
			t.Fatal("invalid signature changed account data")
		}
	})
}
