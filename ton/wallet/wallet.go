package wallet

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/xssnick/tonutils-go/adnl"

	"github.com/xssnick/tonutils-go/ton"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type Version int

// Network IDs
const MainnetGlobalID = -239
const TestnetGlobalID = -3

const (
	V1R1               Version = 11
	V1R2               Version = 12
	V1R3               Version = 13
	V2R1               Version = 21
	V2R2               Version = 22
	V3R1               Version = 31
	V3R2               Version = 32
	V3                         = V3R2
	V4R1               Version = 41
	V4R2               Version = 42
	V5R1Beta           Version = 51 // W5 Beta
	V5R1Final          Version = 52 // W5 Final
	HighloadV2R2       Version = 122
	HighloadV2Verified Version = 123
	HighloadV3         Version = 300
	Lockup             Version = 200
	Unknown            Version = 0
)

const (
	CarryAllRemainingBalance       = 128
	CarryAllRemainingIncomingValue = 64
	DestroyAccountIfZero           = 32
	IgnoreErrors                   = 2
	PayGasSeparately               = 1
)

func (v Version) String() string {
	if v == Unknown {
		return "unknown"
	}

	switch v {
	case HighloadV2R2:
		return fmt.Sprintf("highload V2R2")
	case HighloadV2Verified:
		return fmt.Sprintf("highload V2R2 verified")
	case HighloadV3:
		return fmt.Sprintf("highload V3")
	}

	if v/100 == 2 {
		return fmt.Sprintf("lockup")
	}
	if v/10 > 0 && v/10 < 10 {
		return fmt.Sprintf("V%dR%d", v/10, v%10)
	}
	return fmt.Sprintf("%d", v)
}

var (
	walletCodeHex = map[Version]string{
		V1R1: _V1R1CodeHex, V1R2: _V1R2CodeHex, V1R3: _V1R3CodeHex,
		V2R1: _V2R1CodeHex, V2R2: _V2R2CodeHex,
		V3R1: _V3R1CodeHex, V3R2: _V3R2CodeHex,
		V4R1: _V4R1CodeHex, V4R2: _V4R2CodeHex,
		V5R1Beta:     _V5R1BetaCodeHex,
		V5R1Final:    _V5R1FinalCodeHex,
		HighloadV2R2: _HighloadV2R2CodeHex, HighloadV2Verified: _HighloadV2VerifiedCodeHex,
		HighloadV3: _HighloadV3CodeHex,
		Lockup:     _LockupCodeHex,
	}
	walletCodeBOC = map[Version][]byte{}
	walletCode    = map[Version]*cell.Cell{}
)

func init() {
	var err error

	for ver, codeHex := range walletCodeHex {
		walletCodeBOC[ver], err = hex.DecodeString(codeHex)
		if err != nil {
			panic(err)
		}
		walletCode[ver], err = cell.FromBOC(walletCodeBOC[ver])
		if err != nil {
			panic(err)
		}
	}
}

// defining some funcs this way to mock for tests
var randUint32 = func() uint32 {
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return binary.LittleEndian.Uint32(buf)
}

var timeNow = time.Now

var (
	ErrUnsupportedWalletVersion = errors.New("wallet version is not supported")
)

type TonAPI interface {
	WaitForBlock(seqno uint32) ton.APIClientWrapped
	Client() ton.LiteClient
	CurrentMasterchainInfo(ctx context.Context) (*ton.BlockIDExt, error)
	GetAccount(ctx context.Context, block *ton.BlockIDExt, addr *address.Address) (*tlb.Account, error)
	SendExternalMessage(ctx context.Context, msg *tlb.ExternalMessage) error
	SendExternalMessageWaitTransaction(ctx context.Context, ext *tlb.ExternalMessage) (*tlb.Transaction, *ton.BlockIDExt, []byte, error)
	RunGetMethod(ctx context.Context, blockInfo *ton.BlockIDExt, addr *address.Address, method string, params ...interface{}) (*ton.ExecutionResult, error)
	ListTransactions(ctx context.Context, addr *address.Address, num uint32, lt uint64, txHash []byte) ([]*tlb.Transaction, error)
	FindLastTransactionByInMsgHash(ctx context.Context, addr *address.Address, msgHash []byte, maxTxNumToScan ...int) (*tlb.Transaction, error)
	FindLastTransactionByOutMsgHash(ctx context.Context, addr *address.Address, msgHash []byte, maxTxNumToScan ...int) (*tlb.Transaction, error)
}

type Message struct {
	Mode            uint8
	InternalMessage *tlb.InternalMessage
}

type Signer func(context.Context, *cell.Cell) ([]byte, error)

type Wallet struct {
	api    TonAPI
	key    ed25519.PrivateKey
	pubKey ed25519.PublicKey
	addr   *address.Address
	ver    VersionConfig

	// Can be used to operate multiple wallets with the same key and version.
	// use GetSubwallet if you need it.
	subwallet uint32

	// Stores a pointer to implementation of the version related functionality
	spec any

	signer Signer
}

type walletOption func(*Wallet)

func FromPrivateKey(api TonAPI, key ed25519.PrivateKey, version VersionConfig) (*Wallet, error) {
	return newWallet(
		api,
		key.Public().(ed25519.PublicKey),
		version,
		withPrivateKey(key),
		withSigner(func(ctx context.Context, c *cell.Cell) ([]byte, error) {
			if c == nil {
				return nil, fmt.Errorf("cannot sign: cell is nil")
			}
			return c.Sign(key), nil
		}))
}

func FromSigner(api TonAPI, publicKey ed25519.PublicKey, version VersionConfig, signer Signer) (*Wallet, error) {
	return newWallet(
		api,
		publicKey,
		version,
		withSigner(signer))
}

func newWallet(api TonAPI, publicKey ed25519.PublicKey, version VersionConfig, options ...walletOption) (*Wallet, error) {
	var subwallet uint32 = DefaultSubwallet

	// default subwallet depends on wallet type
	switch version.(type) {
	case ConfigV5R1Beta:
	case ConfigV5R1Final:
		subwallet = 0
	}

	addr, err := AddressFromPubKey(publicKey, version, subwallet)
	if err != nil {
		return nil, err
	}

	w := &Wallet{
		api:       api,
		addr:      addr,
		ver:       version,
		subwallet: subwallet,
		pubKey:    publicKey,
	}

	for _, opt := range options {
		opt(w)
	}

	w.spec, err = getSpec(w)
	if err != nil {
		return nil, err
	}

	return w, nil
}

func withPrivateKey(privateKey ed25519.PrivateKey) walletOption {
	return func(w *Wallet) {
		w.key = privateKey
	}
}

func withSigner(signer Signer) walletOption {
	return func(w *Wallet) {
		w.signer = signer
	}
}

func getSpec(w *Wallet) (any, error) {
	switch v := w.ver.(type) {
	case Version, ConfigV5R1Beta, ConfigV5R1Final:
		regular := SpecRegular{
			wallet:      w,
			messagesTTL: 60 * 3, // default ttl 3 min
		}

		seqnoFetcher := func(ctx context.Context, subWallet uint32) (uint32, error) {
			block, err := w.api.CurrentMasterchainInfo(ctx)
			if err != nil {
				return 0, fmt.Errorf("failed to get block: %w", err)
			}

			resp, err := w.api.WaitForBlock(block.SeqNo).RunGetMethod(ctx, block, w.addr, "seqno")
			if err != nil {
				if cErr, ok := err.(ton.ContractExecError); ok && cErr.Code == ton.ErrCodeContractNotInitialized {
					return 0, nil
				}
				return 0, fmt.Errorf("get seqno err: %w", err)
			}

			iSeq, err := resp.Int(0)
			if err != nil {
				return 0, fmt.Errorf("failed to parse seqno: %w", err)
			}
			return uint32(iSeq.Uint64()), nil
		}

		switch x := w.ver.(type) {
		case ConfigV5R1Beta:
			if x.NetworkGlobalID == 0 {
				return nil, fmt.Errorf("NetworkGlobalID should be set in V5 config")
			}
			return &SpecV5R1Beta{SpecRegular: regular, SpecSeqno: SpecSeqno{seqnoFetcher: seqnoFetcher}, config: x}, nil
		case ConfigV5R1Final:
			if x.NetworkGlobalID == 0 {
				return nil, fmt.Errorf("NetworkGlobalID should be set in V5 config")
			}
			return &SpecV5R1Final{SpecRegular: regular, SpecSeqno: SpecSeqno{seqnoFetcher: seqnoFetcher}, config: x}, nil
		}

		switch v {
		case V3R1, V3R2:
			return &SpecV3{regular, SpecSeqno{seqnoFetcher: seqnoFetcher}}, nil
		case V4R1, V4R2:
			return &SpecV4R2{regular, SpecSeqno{seqnoFetcher: seqnoFetcher}}, nil
		case HighloadV2R2, HighloadV2Verified:
			return &SpecHighloadV2R2{regular, SpecQuery{}}, nil
		case HighloadV3:
			return nil, fmt.Errorf("use ConfigHighloadV3 for highload v3 spec")
		case V5R1Beta:
			return nil, fmt.Errorf("use ConfigV5R1Beta for V5 Beta spec")
		case V5R1Final:
			return nil, fmt.Errorf("use ConfigV5R1Final for V5 spec")
		}
	case ConfigHighloadV3:
		return &SpecHighloadV3{wallet: w, config: v}, nil
	case ConfigCustom:
		return v.GetSpec(w), nil
	}

	return nil, fmt.Errorf("cannot init spec: %w", ErrUnsupportedWalletVersion)
}

// Address - returns old (bounce) version of wallet address
// DEPRECATED: because of address reform, use WalletAddress,
// it will return UQ format
func (w *Wallet) Address() *address.Address {
	return w.addr
}

// WalletAddress - returns new standard non bounce address
func (w *Wallet) WalletAddress() *address.Address {
	return w.addr.Bounce(false)
}

func (w *Wallet) PrivateKey() ed25519.PrivateKey {
	return w.key
}

func (w *Wallet) GetSubwallet(subwallet uint32) (*Wallet, error) {
	addr, err := AddressFromPubKey(w.pubKey, w.ver, subwallet)
	if err != nil {
		return nil, err
	}

	sub := &Wallet{
		api:       w.api,
		key:       w.key,
		pubKey:    w.pubKey,
		addr:      addr,
		ver:       w.ver,
		subwallet: subwallet,
		signer:    w.signer,
	}

	sub.spec, err = getSpec(sub)
	if err != nil {
		return nil, err
	}

	return sub, nil
}

func (w *Wallet) GetSubwalletID() uint32 {
	return w.subwallet
}

func (w *Wallet) GetBalance(ctx context.Context, block *ton.BlockIDExt) (tlb.Coins, error) {
	acc, err := w.api.WaitForBlock(block.SeqNo).GetAccount(ctx, block, w.addr)
	if err != nil {
		return tlb.Coins{}, fmt.Errorf("failed to get account state: %w", err)
	}

	if !acc.IsActive {
		return tlb.Coins{}, nil
	}

	return acc.State.Balance, nil
}

func (w *Wallet) GetSpec() any {
	return w.spec
}

func (w *Wallet) BuildExternalMessage(ctx context.Context, message *Message) (*tlb.ExternalMessage, error) {
	return w.BuildExternalMessageForMany(ctx, []*Message{message})
}

// Deprecated: use BuildExternalMessageForMany
func (w *Wallet) BuildMessageForMany(ctx context.Context, messages []*Message) (*tlb.ExternalMessage, error) {
	return w.BuildExternalMessageForMany(ctx, messages)
}

func (w *Wallet) BuildExternalMessageForMany(ctx context.Context, messages []*Message) (*tlb.ExternalMessage, error) {
	block, err := w.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	acc, err := w.api.WaitForBlock(block.SeqNo).GetAccount(ctx, block, w.addr)
	if err != nil {
		return nil, fmt.Errorf("failed to get account state: %w", err)
	}

	initialized := acc.IsActive && acc.State.Status == tlb.AccountStatusActive
	return w.PrepareExternalMessageForMany(ctx, !initialized, messages)
}

// PrepareExternalMessageForMany - Prepares external message for wallet
// can be used directly for offline signing but custom fetchers should be defined in this case
func (w *Wallet) PrepareExternalMessageForMany(ctx context.Context, withStateInit bool, messages []*Message) (_ *tlb.ExternalMessage, err error) {
	var stateInit *tlb.StateInit
	if withStateInit {
		stateInit, err = GetStateInit(w.pubKey, w.ver, w.subwallet)
		if err != nil {
			return nil, fmt.Errorf("failed to get state init: %w", err)
		}
	}

	var msg *cell.Cell
	switch v := w.ver.(type) {
	case Version, ConfigV5R1Beta, ConfigV5R1Final:
		switch v.(type) {
		case ConfigV5R1Beta:
			v = V5R1Beta
		case ConfigV5R1Final:
			v = V5R1Final
		}

		switch v {
		case V3R2, V3R1, V4R2, V4R1, V5R1Beta, V5R1Final:
			msg, err = w.spec.(RegularBuilder).BuildMessage(ctx, !withStateInit, nil, messages)
			if err != nil {
				return nil, fmt.Errorf("build message err: %w", err)
			}
		case HighloadV2R2, HighloadV2Verified:
			msg, err = w.spec.(*SpecHighloadV2R2).BuildMessage(ctx, messages)
			if err != nil {
				return nil, fmt.Errorf("build message err: %w", err)
			}
		case HighloadV3:
			return nil, fmt.Errorf("use ConfigHighloadV3 for highload v3 spec")
		default:
			return nil, fmt.Errorf("send is not yet supported: %w", ErrUnsupportedWalletVersion)
		}
	case ConfigHighloadV3:
		msg, err = w.spec.(*SpecHighloadV3).BuildMessage(ctx, messages)
		if err != nil {
			return nil, fmt.Errorf("build message err: %w", err)
		}
	case ConfigCustom:
		msg, err = w.spec.(MessageBuilder).BuildMessage(ctx, messages)
		if err != nil {
			return nil, fmt.Errorf("build message err: %w", err)
		}
	default:
		return nil, fmt.Errorf("send is not yet supported: %w", ErrUnsupportedWalletVersion)
	}

	return &tlb.ExternalMessage{
		DstAddr:   w.addr,
		StateInit: stateInit,
		Body:      msg,
	}, nil
}

func (w *Wallet) BuildTransfer(to *address.Address, amount tlb.Coins, bounce bool, comment string) (_ *Message, err error) {
	var body *cell.Cell
	if comment != "" {
		body, err = CreateCommentCell(comment)
		if err != nil {
			return nil, err
		}
	}

	return &Message{
		Mode: PayGasSeparately + IgnoreErrors,
		InternalMessage: &tlb.InternalMessage{
			IHRDisabled: true,
			Bounce:      bounce,
			DstAddr:     to,
			Amount:      amount,
			Body:        body,
		},
	}, nil
}

func (w *Wallet) BuildTransferEncrypted(ctx context.Context, to *address.Address, amount tlb.Coins, bounce bool, comment string) (_ *Message, err error) {
	var body *cell.Cell
	if comment != "" {
		key, err := GetPublicKey(ctx, w.api, to)
		if err != nil {
			return nil, fmt.Errorf("failed to get destination contract (wallet) public key")
		}

		body, err = CreateEncryptedCommentCell(comment, w.WalletAddress(), w.key, key)
		if err != nil {
			return nil, err
		}
	}

	return &Message{
		Mode: PayGasSeparately + IgnoreErrors,
		InternalMessage: &tlb.InternalMessage{
			IHRDisabled: true,
			Bounce:      bounce,
			DstAddr:     to,
			Amount:      amount,
			Body:        body,
		},
	}, nil
}

func (w *Wallet) Send(ctx context.Context, message *Message, waitConfirmation ...bool) error {
	return w.SendMany(ctx, []*Message{message}, waitConfirmation...)
}

func (w *Wallet) SendMany(ctx context.Context, messages []*Message, waitConfirmation ...bool) error {
	_, _, _, err := w.sendMany(ctx, messages, waitConfirmation...)
	return err
}

// SendManyGetInMsgHash returns hash of external incoming message payload.
func (w *Wallet) SendManyGetInMsgHash(ctx context.Context, messages []*Message, waitConfirmation ...bool) ([]byte, error) {
	_, _, inMsgHash, err := w.sendMany(ctx, messages, waitConfirmation...)
	return inMsgHash, err
}

// SendManyWaitTxHash always waits for tx block confirmation and returns found tx hash in block.
func (w *Wallet) SendManyWaitTxHash(ctx context.Context, messages []*Message) ([]byte, error) {
	tx, _, _, err := w.sendMany(ctx, messages, true)
	if err != nil {
		return nil, err
	}
	return tx.Hash, err
}

// SendManyWaitTransaction always waits for tx block confirmation and returns found tx.
func (w *Wallet) SendManyWaitTransaction(ctx context.Context, messages []*Message) (*tlb.Transaction, *ton.BlockIDExt, error) {
	tx, block, _, err := w.sendMany(ctx, messages, true)
	return tx, block, err
}

// SendWaitTransaction always waits for tx block confirmation and returns found tx.
func (w *Wallet) SendWaitTransaction(ctx context.Context, message *Message) (*tlb.Transaction, *ton.BlockIDExt, error) {
	return w.SendManyWaitTransaction(ctx, []*Message{message})
}

// TransferWaitTransaction always waits for tx block confirmation and returns found tx.
func (w *Wallet) TransferWaitTransaction(ctx context.Context, to *address.Address, amount tlb.Coins, comment string) (*tlb.Transaction, *ton.BlockIDExt, error) {
	transfer, err := w.BuildTransfer(to, amount, to.IsBounceable(), comment)
	if err != nil {
		return nil, nil, err
	}

	return w.SendManyWaitTransaction(ctx, []*Message{transfer})
}

func (w *Wallet) sendMany(ctx context.Context, messages []*Message, waitConfirmation ...bool) (tx *tlb.Transaction, block *ton.BlockIDExt, inMsgHash []byte, err error) {
	ext, err := w.BuildExternalMessageForMany(ctx, messages)
	if err != nil {
		return nil, nil, nil, err
	}

	if len(waitConfirmation) > 0 && waitConfirmation[0] {
		return w.api.SendExternalMessageWaitTransaction(ctx, ext)
	}

	if err = w.api.SendExternalMessage(ctx, ext); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to send message: %w", err)
	}
	return nil, nil, ext.Body.Hash(), nil
}

// TransferNoBounce - can be used to transfer TON to not yet initialized contract/wallet
func (w *Wallet) TransferNoBounce(ctx context.Context, to *address.Address, amount tlb.Coins, comment string, waitConfirmation ...bool) error {
	return w.transfer(ctx, to, amount, comment, false, waitConfirmation...)
}

// Transfer - safe transfer, in case of error on smart contract side, you will get coins back,
// cannot be used to transfer TON to not yet initialized contract/wallet
func (w *Wallet) Transfer(ctx context.Context, to *address.Address, amount tlb.Coins, comment string, waitConfirmation ...bool) error {
	return w.transfer(ctx, to, amount, comment, true, waitConfirmation...)
}

// TransferWithEncryptedComment - same as Transfer but encrypts comment, throws error if target contract (address) has no get_public_key method.
func (w *Wallet) TransferWithEncryptedComment(ctx context.Context, to *address.Address, amount tlb.Coins, comment string, waitConfirmation ...bool) error {
	transfer, err := w.BuildTransferEncrypted(ctx, to, amount, true, comment)
	if err != nil {
		return err
	}
	return w.Send(ctx, transfer, waitConfirmation...)
}

func CreateCommentCell(text string) (*cell.Cell, error) {
	// comment ident
	root := cell.BeginCell().MustStoreUInt(0, 32)

	if err := root.StoreStringSnake(text); err != nil {
		return nil, fmt.Errorf("failed to build comment: %w", err)
	}

	return root.EndCell(), nil
}

const EncryptedCommentOpcode = 0x2167da4b

func DecryptCommentCell(commentCell *cell.Cell, sender *address.Address, ourKey ed25519.PrivateKey, theirKey ed25519.PublicKey) ([]byte, error) {
	slc := commentCell.BeginParse()
	op, err := slc.LoadUInt(32)
	if err != nil {
		return nil, fmt.Errorf("failed to load op code: %w", err)
	}

	if op != EncryptedCommentOpcode {
		return nil, fmt.Errorf("opcode not match encrypted comment")
	}

	xorKey, err := slc.LoadSlice(256)
	if err != nil {
		return nil, fmt.Errorf("failed to load xor key: %w", err)
	}
	for i := 0; i < 32; i++ {
		xorKey[i] ^= theirKey[i]
	}

	if !bytes.Equal(xorKey, ourKey.Public().(ed25519.PublicKey)) {
		return nil, fmt.Errorf("message was encrypted not for the given keys")
	}

	msgKey, err := slc.LoadSlice(128)
	if err != nil {
		return nil, fmt.Errorf("failed to load xor key: %w", err)
	}

	sharedKey, err := adnl.SharedKey(ourKey, theirKey)
	if err != nil {
		return nil, fmt.Errorf("failed to compute shared key: %w", err)
	}

	h := hmac.New(sha512.New, sharedKey)
	h.Write(msgKey)
	x := h.Sum(nil)

	data, err := slc.LoadBinarySnake()
	if err != nil {
		return nil, fmt.Errorf("failed to load snake encrypted data: %w", err)
	}

	if len(data) < 32 || len(data)%16 != 0 {
		return nil, fmt.Errorf("invalid data")
	}

	c, err := aes.NewCipher(x[:32])
	if err != nil {
		return nil, err
	}
	enc := cipher.NewCBCDecrypter(c, x[32:48])
	enc.CryptBlocks(data, data)

	if data[0] > 31 {
		return nil, fmt.Errorf("invalid prefix size %d", data[0])
	}

	h = hmac.New(sha512.New, []byte(sender.String()))
	h.Write(data)
	if !bytes.Equal(msgKey, h.Sum(nil)[:16]) {
		return nil, fmt.Errorf("incorrect msg key")
	}

	return data[data[0]:], nil
}

func CreateEncryptedCommentCell(text string, senderAddr *address.Address, ourKey ed25519.PrivateKey, theirKey ed25519.PublicKey) (*cell.Cell, error) {
	// encrypted comment op code
	root := cell.BeginCell().MustStoreUInt(EncryptedCommentOpcode, 32)

	sharedKey, err := adnl.SharedKey(ourKey, theirKey)
	if err != nil {
		return nil, fmt.Errorf("failed to compute shared key: %w", err)
	}

	data := []byte(text)

	pfxSz := 16
	if len(data)%16 != 0 {
		pfxSz += 16 - (len(data) % 16)
	}

	pfx := make([]byte, pfxSz)
	pfx[0] = byte(len(pfx))
	if _, err = rand.Read(pfx[1:]); err != nil {
		return nil, fmt.Errorf("rand gen err: %w", err)
	}
	data = append(pfx, data...)

	h := hmac.New(sha512.New, []byte(senderAddr.String()))
	h.Write(data)
	msgKey := h.Sum(nil)[:16]

	h = hmac.New(sha512.New, sharedKey)
	h.Write(msgKey)
	x := h.Sum(nil)

	c, err := aes.NewCipher(x[:32])
	if err != nil {
		return nil, err
	}

	enc := cipher.NewCBCEncrypter(c, x[32:48])
	enc.CryptBlocks(data, data)

	xorKey := ourKey.Public().(ed25519.PublicKey)
	for i := 0; i < 32; i++ {
		xorKey[i] ^= theirKey[i]
	}

	root.MustStoreSlice(xorKey, 256)
	root.MustStoreSlice(msgKey, 128)

	if err := root.StoreBinarySnake(data); err != nil {
		return nil, fmt.Errorf("failed to build comment: %w", err)
	}

	return root.EndCell(), nil
}

func (w *Wallet) transfer(ctx context.Context, to *address.Address, amount tlb.Coins, comment string, bounce bool, waitConfirmation ...bool) (err error) {
	transfer, err := w.BuildTransfer(to, amount, bounce, comment)
	if err != nil {
		return err
	}
	return w.Send(ctx, transfer, waitConfirmation...)
}

func (w *Wallet) DeployContractWaitTransaction(ctx context.Context, amount tlb.Coins, msgBody, contractCode, contractData *cell.Cell) (*address.Address, *tlb.Transaction, *ton.BlockIDExt, error) {
	state := &tlb.StateInit{
		Data: contractData,
		Code: contractCode,
	}

	stateCell, err := tlb.ToCell(state)
	if err != nil {
		return nil, nil, nil, err
	}

	addr := address.NewAddress(0, 0, stateCell.Hash())

	tx, block, err := w.SendWaitTransaction(ctx, &Message{
		Mode: PayGasSeparately + IgnoreErrors,
		InternalMessage: &tlb.InternalMessage{
			IHRDisabled: true,
			Bounce:      false,
			DstAddr:     addr,
			Amount:      amount,
			Body:        msgBody,
			StateInit:   state,
		},
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return addr, tx, block, nil
}

// Deprecated: use DeployContractWaitTransaction
func (w *Wallet) DeployContract(ctx context.Context, amount tlb.Coins, msgBody, contractCode, contractData *cell.Cell, waitConfirmation ...bool) (*address.Address, error) {
	state := &tlb.StateInit{
		Data: contractData,
		Code: contractCode,
	}

	stateCell, err := tlb.ToCell(state)
	if err != nil {
		return nil, err
	}

	addr := address.NewAddress(0, 0, stateCell.Hash())

	if err = w.Send(ctx, &Message{
		Mode: PayGasSeparately + IgnoreErrors,
		InternalMessage: &tlb.InternalMessage{
			IHRDisabled: true,
			Bounce:      false,
			DstAddr:     addr,
			Amount:      amount,
			Body:        msgBody,
			StateInit:   state,
		},
	}, waitConfirmation...); err != nil {
		return nil, err
	}

	return addr, nil
}

func SimpleMessage(to *address.Address, amount tlb.Coins, payload *cell.Cell) *Message {
	return &Message{
		Mode: PayGasSeparately + IgnoreErrors,
		InternalMessage: &tlb.InternalMessage{
			IHRDisabled: true,
			Bounce:      true,
			DstAddr:     to,
			Amount:      amount,
			Body:        payload,
		},
	}
}

// SimpleMessageAutoBounce - will determine bounce flag from address
func SimpleMessageAutoBounce(to *address.Address, amount tlb.Coins, payload *cell.Cell) *Message {
	return &Message{
		Mode: PayGasSeparately + IgnoreErrors,
		InternalMessage: &tlb.InternalMessage{
			IHRDisabled: true,
			Bounce:      to.IsBounceable(),
			DstAddr:     to,
			Amount:      amount,
			Body:        payload,
		},
	}
}
