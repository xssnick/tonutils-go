package wallet

import (
	"crypto/ed25519"
	"fmt"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

const DefaultSubwallet = 698983191

func AddressFromPubKey(key ed25519.PublicKey, ver Version, subwallet uint32) (*address.Address, error) {
	state, err := GetStateInit(key, ver, subwallet)
	if err != nil {
		return nil, fmt.Errorf("failed to get state: %w", err)
	}

	stateCell, err := state.ToCell()
	if err != nil {
		return nil, fmt.Errorf("failed to get state cell: %w", err)
	}

	addr := address.NewAddress(0, 0, stateCell.Hash())

	return addr, nil
}

func GetStateInit(pubKey ed25519.PublicKey, ver Version, subWallet uint32) (*tlb.StateInit, error) {
	code, err := getCode(ver)
	if err != nil {
		return nil, err
	}

	var data *cell.Cell
	switch ver {
	case V3:
		data = cell.BeginCell().
			MustStoreUInt(0, 32).                 // seqno
			MustStoreUInt(uint64(subWallet), 32). // sub wallet
			MustStoreSlice(pubKey, 256).
			EndCell()
	case V4R2:
		data = cell.BeginCell().
			MustStoreUInt(0, 32). // seqno
			MustStoreUInt(uint64(subWallet), 32).
			MustStoreSlice(pubKey, 256).
			MustStoreDict(nil). // empty dict of plugins
			EndCell()
	case HighloadV2R2:
		data = cell.BeginCell().
			MustStoreUInt(uint64(subWallet), 32).
			MustStoreUInt(0, 64). // last cleaned
			MustStoreSlice(pubKey, 256).
			MustStoreDict(nil). // old queries
			EndCell()
	default:
		return nil, ErrUnsupportedWalletVersion
	}

	return &tlb.StateInit{
		Data: data,
		Code: code,
	}, nil
}

func getCode(ver Version) (*cell.Cell, error) {
	var boc []byte

	switch ver {
	case V3:
		boc = _V3CodeBOC
	case V4R2:
		boc = _V4R2CodeBOC
	case HighloadV2R2:
		boc = _HighloadV2R2CodeBOC
	default:
		return nil, fmt.Errorf("cannot get code: %w", ErrUnsupportedWalletVersion)
	}

	code, err := cell.FromBOC(boc)
	if err != nil {
		return nil, fmt.Errorf("failed to convert code boc to cell: %w", err)
	}

	return code, nil
}
