package wallet

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

const _SubWalletV3 = 698983191

func AddressFromPubKey(key ed25519.PublicKey, ver Version) (*address.Address, error) {
	state, err := GetStateInit(key, ver)
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

func GetStateInit(pubKey ed25519.PublicKey, ver Version) (*tlb.StateInit, error) {
	var code, data *cell.Cell

	switch ver {
	case V3:
		v3boc, err := hex.DecodeString("B5EE9C724101010100710000DEFF0020DD2082014C97BA218201339CBAB19F71B0ED44D0D31FD31F31D70BFFE304E0A4F2608308D71820D31FD31FD31FF82313BBF263ED44D0D31FD31FD3FFD15132BAF2A15144BAF2A204F901541055F910F2A3F8009320D74A96D307D402FB00E8D101A4C8CB1FCB1FCBFFC9ED5410BD6DAD")
		if err != nil {
			return nil, fmt.Errorf("failed to decode hex of wallet boc: %w", err)
		}

		code, err = cell.FromBOC(v3boc)
		if err != nil {
			return nil, fmt.Errorf("failed to decode wallet code boc: %w", err)
		}

		data = cell.BeginCell().
			MustStoreUInt(0, 32).            // seqno
			MustStoreUInt(_SubWalletV3, 32). // sub wallet, hardcoded everywhere
			MustStoreSlice(pubKey, 256).
			EndCell()
	default:
		return nil, errors.New("wallet version is not supported")
	}

	return &tlb.StateInit{
		Data: data,
		Code: code,
	}, nil
}
