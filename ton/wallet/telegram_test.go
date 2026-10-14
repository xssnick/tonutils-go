package wallet

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTelegramWalletStateInit(t *testing.T) {
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	pubKey := key.Public().(ed25519.PublicKey)
	w, err := FromPrivateKey(nil, key, Telegram)
	if err != nil {
		t.Fatal(err)
	}
	if w.GetSubwalletID() != TelegramMainnetSubwallet || Telegram.String() != "Telegram" {
		t.Fatal("unexpected default subwallet or version name")
	}

	// Independently calculated cell representation hashes for the zero-seed public key.
	tests := []struct {
		name      string
		subwallet uint32
		dataHash  string
		addrHash  string
	}{
		{"mainnet", TelegramMainnetSubwallet,
			"20fbd31cba19b846f593cfdcd0e51fa2f740386c5b9fb500cb44aad902feab33",
			"4ddd4aee907e58856c4b30b78924897199c66f0077c4411841e1d80af1166ee6"},
		{"testnet", TelegramTestnetSubwallet,
			"5d630a2657726c2b6237289d8cf0163e01c2ce7f8aa3815cdc35cd49a63b1ab7",
			"652414200b5f7a069f5aed0a6a39cfaf9ea8031d624b99e8018fb8f8b273f71a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			si, err := GetStateInit(pubKey, Telegram, tt.subwallet)
			if err != nil {
				t.Fatal(err)
			}
			if si.Code.HashKey() != telegramWalletTestCode(t).HashKey() {
				t.Fatal("StateInit does not contain the canonical trampoline")
			}
			if got := hex.EncodeToString(si.Data.Hash()); got != tt.dataHash {
				t.Fatalf("data hash = %s, want %s", got, tt.dataHash)
			}
			parsedKey, err := ParsePubKeyFromData(Telegram, si.Data)
			if err != nil || !bytes.Equal(parsedKey, pubKey) {
				t.Fatalf("ParsePubKeyFromData = %x, %v", parsedKey, err)
			}
			acc := &tlb.Account{
				IsActive: true,
				State:    &tlb.AccountState{AccountStorage: tlb.AccountStorage{Status: tlb.AccountStatusActive}},
				Code:     si.Code,
				Data:     si.Data,
			}
			if GetWalletVersion(acc) != Telegram {
				t.Fatal("wallet version was not recognized")
			}
			sub, err := w.GetSubwallet(tt.subwallet)
			if err != nil {
				t.Fatal(err)
			}
			if got := hex.EncodeToString(sub.WalletAddress().Data()); got != tt.addrHash {
				t.Fatalf("address hash = %s, want %s", got, tt.addrHash)
			}
			addr, err := AddressFromPubKey(pubKey, Telegram, tt.subwallet, -1)
			if err != nil || addr.Workchain() != -1 || !bytes.Equal(addr.Data(), sub.WalletAddress().Data()) {
				t.Fatalf("unexpected workchain address: %v, %v", addr, err)
			}
		})
	}
}

func TestTelegramWalletSigning(t *testing.T) {
	const now = int64(1_740_000_000)
	previousTimeNow := timeNow
	timeNow = func() time.Time { return time.Unix(now, 0) }
	t.Cleanup(func() { timeNow = previousTimeNow })
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))

	for _, tt := range []struct {
		name  string
		count int
		op    uint64
	}{
		{"single", 1, 0x63896E75},
		{"bulk", 5, 0x73896E75},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var signed *cell.Cell
			w, err := FromPubKeyWithOptions(key.Public().(ed25519.PublicKey), Telegram,
				WithSigner(func(ctx context.Context, payload *cell.Cell, subwallet uint32) ([]byte, error) {
					if subwallet != TelegramMainnetSubwallet {
						t.Fatalf("unexpected signer subwallet %d", subwallet)
					}
					signed = payload
					return payload.Sign(key), nil
				}))
			if err != nil {
				t.Fatal(err)
			}
			spec := w.GetSpec().(*SpecTelegram)
			spec.SetMessagesTTL(90)
			spec.SetSeqnoFetcher(func(context.Context, uint32) (uint32, error) { return 9, nil })
			messages := make([]*Message, tt.count)
			for i := range messages {
				messages[i] = SimpleMessage(w.WalletAddress(), tlb.FromNanoTONU(1),
					cell.BeginCell().MustStoreUInt(uint64(i), 16).EndCell())
			}
			ext, err := w.PrepareExternalMessageForMany(context.Background(), false, messages)
			if err != nil {
				t.Fatal(err)
			}
			body := ext.Body.MustBeginParse()
			signature := body.MustLoadSlice(512)
			request := body.MustToCell()
			requestHash := request.HashKey()
			if !ed25519.Verify(key.Public().(ed25519.PublicKey), requestHash[:], signature) || signed.HashKey() != requestHash {
				t.Fatal("signature does not cover the complete request")
			}
			if body.MustLoadUInt(32) != tt.op || body.MustLoadUInt(32) != uint64(w.GetSubwalletID()) ||
				body.MustLoadUInt(32) != uint64(now+90) || body.MustLoadUInt(32) != 9 {
				t.Fatal("unexpected opcode, subwallet, TTL or seqno")
			}
			if ext.StateInit != nil || !ext.DstAddr.Equals(w.WalletAddress()) {
				t.Fatal("unexpected external message destination or StateInit")
			}

			var decoded TelegramSignedRequest
			if err = tlb.Parse(&decoded, ext.Body); err != nil {
				t.Fatal(err)
			}
			var decodedMessages []TelegramMessageToSend
			if tt.count == 1 {
				var request TelegramSendOneRequest
				if err = tlb.Parse(&request, decoded.Request); err != nil {
					t.Fatal(err)
				}
				decodedMessages = []TelegramMessageToSend{request.Message}
			} else {
				var request TelegramSendBulkRequest
				if err = tlb.Parse(&request, decoded.Request); err != nil {
					t.Fatal(err)
				}
				decodedMessages = request.Messages
			}
			if len(decodedMessages) != tt.count {
				t.Fatalf("decoded %d messages, want %d", len(decodedMessages), tt.count)
			}
			for i, message := range decodedMessages {
				if message.SendMode != messages[i].Mode ||
					message.Message.Body.HashKey() != messages[i].InternalMessage.Body.HashKey() {
					t.Fatalf("decoded message %d differs from requested message", i)
				}
			}
		})
	}
}

func TestTelegramWalletSend(t *testing.T) {
	requestErr := errors.New("request failed")
	state := func(status tlb.AccountStatus) *tlb.Account {
		return &tlb.Account{IsActive: true, State: &tlb.AccountState{
			AccountStorage: tlb.AccountStorage{Status: status},
		}}
	}
	tests := []struct {
		name          string
		account       *tlb.Account
		seqno         uint32
		masterErr     error
		accountErr    error
		seqnoErr      error
		sendErr       error
		wantStateInit bool
		wantErr       string
	}{
		{name: "nonexistent", account: &tlb.Account{}, wantStateInit: true,
			seqnoErr: ton.ContractExecError{Code: ton.ErrCodeContractNotInitialized}},
		{name: "uninit", account: state(tlb.AccountStatusUninit), wantStateInit: true,
			seqnoErr: ton.ContractExecError{Code: ton.ErrCodeContractNotInitialized}},
		{name: "active", account: state(tlb.AccountStatusActive), seqno: 9},
		{name: "frozen", account: state(tlb.AccountStatusFrozen), wantErr: "frozen Telegram wallet"},
		{name: "master error", masterErr: requestErr, wantErr: "failed to get block"},
		{name: "account error", accountErr: requestErr, wantErr: "failed to get account state"},
		{name: "seqno error", account: state(tlb.AccountStatusActive), seqnoErr: requestErr, wantErr: "failed to fetch seqno"},
		{name: "send error", account: state(tlb.AccountStatusActive), sendErr: requestErr, wantErr: "failed to send message"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &telegramTestClient{account: tt.account, seqno: tt.seqno, masterErr: tt.masterErr,
				accountErr: tt.accountErr, seqnoErr: tt.seqnoErr, sendErr: tt.sendErr}
			w, err := FromPrivateKey(client, ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)), Telegram)
			if err != nil {
				t.Fatal(err)
			}
			err = w.TransferNoBounce(context.Background(), w.WalletAddress(), tlb.FromNanoTONU(1), "deploy")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				if tt.wantErr != "frozen Telegram wallet" && !errors.Is(err, requestErr) {
					t.Fatalf("request error was not preserved: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if client.sent == nil {
				t.Fatal("external message was not sent")
			}
			if (client.sent.StateInit != nil) != tt.wantStateInit {
				t.Fatalf("StateInit presence = %v, want %v", client.sent.StateInit != nil, tt.wantStateInit)
			}
			body := client.sent.Body.MustBeginParse()
			body.MustLoadSlice(512)
			body.MustLoadSlice(96)
			if seqno := body.MustLoadUInt(32); seqno != uint64(tt.seqno) {
				t.Fatalf("seqno = %d, want %d", seqno, tt.seqno)
			}
		})
	}
}

func TestTelegramWalletRotatedSigner(t *testing.T) {
	initialKey := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	currentSeed := [ed25519.SeedSize]byte{1}
	currentKey := ed25519.NewKeyFromSeed(currentSeed[:])
	initialPubKey := initialKey.Public().(ed25519.PublicKey)
	w, err := FromPubKeyWithOptions(initialPubKey, Telegram, WithPrivateKey(currentKey))
	if err != nil {
		t.Fatal(err)
	}
	initialAddr, err := AddressFromPubKey(initialPubKey, Telegram, TelegramMainnetSubwallet)
	if err != nil {
		t.Fatal(err)
	}
	if !w.WalletAddress().Equals(initialAddr) {
		t.Fatal("changing the signing key changed the wallet address")
	}
	w.GetSpec().(*SpecTelegram).SetSeqnoFetcher(func(context.Context, uint32) (uint32, error) { return 1, nil })
	ext, err := w.PrepareExternalMessageForMany(context.Background(), false, []*Message{
		SimpleMessage(w.WalletAddress(), tlb.FromNanoTONU(1), nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	body := ext.Body.MustBeginParse()
	signature := body.MustLoadSlice(512)
	requestHash := body.MustToCell().HashKey()
	if !ed25519.Verify(currentKey.Public().(ed25519.PublicKey), requestHash[:], signature) ||
		ed25519.Verify(initialPubKey, requestHash[:], signature) {
		t.Fatal("request was not signed by the current key")
	}
}

func TestTelegramWalletBuildErrors(t *testing.T) {
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	w, err := FromPrivateKey(nil, key, Telegram)
	if err != nil {
		t.Fatal(err)
	}
	message := SimpleMessage(w.WalletAddress(), tlb.FromNanoTONU(1), nil)
	withoutIgnoreErrors := *message
	withoutIgnoreErrors.Mode = PayGasSeparately
	tooMany := make([]*Message, 256)
	for i := range tooMany {
		tooMany[i] = message
	}
	spec := w.GetSpec().(*SpecTelegram)
	spec.SetSeqnoFetcher(func(context.Context, uint32) (uint32, error) {
		t.Fatal("invalid request reached the seqno fetcher")
		return 0, nil
	})
	for _, tt := range []struct {
		name     string
		messages []*Message
		wantErr  string
	}{
		{"empty", nil, "between 1 and 255"},
		{"too many", tooMany, "between 1 and 255"},
		{"single mode", []*Message{&withoutIgnoreErrors}, "IgnoreErrors"},
		{"bulk mode", []*Message{message, &withoutIgnoreErrors}, "IgnoreErrors"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := w.PrepareExternalMessageForMany(context.Background(), true, tt.messages)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}

	t.Run("signer error", func(t *testing.T) {
		signerErr := errors.New("signer failed")
		spec.SetSeqnoFetcher(func(context.Context, uint32) (uint32, error) { return 0, nil })
		WithSigner(func(context.Context, *cell.Cell, uint32) ([]byte, error) { return nil, signerErr })(w)
		_, err := w.PrepareExternalMessageForMany(context.Background(), true, []*Message{message})
		if !errors.Is(err, signerErr) {
			t.Fatalf("signer error was not preserved: %v", err)
		}
	})
}

type telegramTestClient struct {
	ton.APIClientWrapped
	account                                  *tlb.Account
	seqno                                    uint32
	masterErr, accountErr, seqnoErr, sendErr error
	sent                                     *tlb.ExternalMessage
}

func (c *telegramTestClient) CurrentMasterchainInfo(context.Context) (*ton.BlockIDExt, error) {
	return &ton.BlockIDExt{SeqNo: 1}, c.masterErr
}

func (c *telegramTestClient) WaitForBlock(uint32) ton.APIClientWrapped {
	return c
}

func (c *telegramTestClient) GetAccount(context.Context, *ton.BlockIDExt, *address.Address) (*tlb.Account, error) {
	return c.account, c.accountErr
}

func (c *telegramTestClient) RunGetMethod(context.Context, *ton.BlockIDExt, *address.Address, string, ...any) (*ton.ExecutionResult, error) {
	return ton.NewExecutionResult([]any{big.NewInt(int64(c.seqno))}), c.seqnoErr
}

func (c *telegramTestClient) SendExternalMessage(_ context.Context, ext *tlb.ExternalMessage) error {
	c.sent = ext
	return c.sendErr
}
