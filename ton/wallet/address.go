package wallet

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

const DefaultSubwallet = 698983191

func AddressFromPubKey(key ed25519.PublicKey, spec VersionConfig, subwallet uint32) (*address.Address, error) {
	state, err := GetStateInit(key, spec, subwallet)
	if err != nil {
		return nil, fmt.Errorf("failed to get state: %w", err)
	}

	stateCell, err := tlb.ToCell(state)
	if err != nil {
		return nil, fmt.Errorf("failed to get state cell: %w", err)
	}

	addr := address.NewAddress(0, 0, stateCell.Hash())

	return addr, nil
}

func GetWalletVersion(account *tlb.Account) Version {
	if !account.IsActive || account.State.Status != tlb.AccountStatusActive {
		return Unknown
	}

	for v := range walletCodeHex {
		code, ok := walletCode[v]
		if !ok {
			continue
		}
		if bytes.Equal(account.Code.Hash(), code.Hash()) {
			return v
		}
	}

	return Unknown
}

func GetStateInit(pubKey ed25519.PublicKey, spec VersionConfig, subWallet uint32) (*tlb.StateInit, error) {
	var ver Version
	switch v := spec.(type) {
	case Version:
		ver = v
		switch ver {
		case HighloadV3:
			return nil, fmt.Errorf("use ConfigHighloadV3 for highload v3 spec")
		}
	case ConfigHighloadV3:
		ver = HighloadV3
	}

	code, ok := walletCode[ver]
	if !ok {
		return nil, fmt.Errorf("cannot get code: %w", ErrUnsupportedWalletVersion)
	}

	var data *cell.Cell
	switch ver {
	case V3R1, V3R2:
		data = cell.BeginCell().
			MustStoreUInt(0, 32).                 // seqno
			MustStoreUInt(uint64(subWallet), 32). // sub wallet
			MustStoreSlice(pubKey, 256).
			EndCell()
	case V4R1, V4R2:
		data = cell.BeginCell().
			MustStoreUInt(0, 32). // seqno
			MustStoreUInt(uint64(subWallet), 32).
			MustStoreSlice(pubKey, 256).
			MustStoreDict(nil). // empty dict of plugins
			EndCell()
	case HighloadV2R2, HighloadV2Verified:
		data = cell.BeginCell().
			MustStoreUInt(uint64(subWallet), 32).
			MustStoreUInt(0, 64). // last cleaned
			MustStoreSlice(pubKey, 256).
			MustStoreDict(nil). // old queries
			EndCell()
	case HighloadV3:
		timeout := spec.(ConfigHighloadV3).MessageTTL
		if timeout >= 1<<22 {
			return nil, fmt.Errorf("too big timeout")
		}

		data = cell.BeginCell().
			MustStoreSlice(pubKey, 256).
			MustStoreUInt(uint64(subWallet), 32).
			MustStoreUInt(0, 66).
			MustStoreUInt(uint64(timeout), 22).
			EndCell()
	default:
		return nil, ErrUnsupportedWalletVersion
	}

	return &tlb.StateInit{
		Data: data,
		Code: code,
	}, nil
}

func GetPublicKey(ctx context.Context, api TonAPI, addr *address.Address) (ed25519.PublicKey, error) {
	master, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get current master block: %w", err)
	}

	res, err := api.WaitForBlock(master.SeqNo).RunGetMethod(ctx, master, addr, "get_public_key")
	if err != nil {
		return nil, fmt.Errorf("failed to execute get_public_key contract method: %w", err)
	}

	key, err := res.Int(0)
	if err != nil {
		return nil, fmt.Errorf("failed to parse get_public_key execution result: %w", err)
	}

	b := key.Bytes()
	pubKey := make([]byte, 32)
	copy(pubKey[32-len(b):], b)

	return pubKey, nil
}
