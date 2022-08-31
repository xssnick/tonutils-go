package wallet

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

// converted to hex from https://github.com/toncenter/tonweb/blob/0a5effd36a3f342f4aacabab728b1f9747085ad1/src/contract/wallet/WalletSourcesFromCPP.txt#L18
const _HighloadV2R2CodeHex = "B5EE9C720101090100E9000114FF00F4A413F4BCF2C80B010201200203020148040501EEF28308D71820D31FD33FF823AA1F5320B9F263ED44D0D31FD33FD3FFF404D153608040F40E6FA131F2605173BAF2A207F901541087F910F2A302F404D1F8007F8E18218010F4786FA16FA1209802D307D43001FB009132E201B3E65B8325A1C840348040F4438AE631C812CB1F13CB3FCBFFF400C9ED54080004D03002012006070017BD9CE76A26869AF98EB85FFC0041BE5F976A268698F98E99FE9FF98FA0268A91040207A0737D098C92DBFC95DD1F140038208040F4966FA16FA132511094305303B9DE2093333601923230E2B3"

var (
	_HighloadV2R2CodeBOC []byte
)

func init() {
	var err error

	_HighloadV2R2CodeBOC, err = hex.DecodeString(_HighloadV2R2CodeHex)
	if err != nil {
		panic(err)
	}
}

type SpecHighloadV2R2 struct {
	SpecRegular
}

func (s *SpecHighloadV2R2) BuildMessage(_ context.Context, queryID uint32, messages []*Message) (*cell.Cell, error) {
	if len(messages) > 254 {
		return nil, errors.New("for this type of wallet max 254 messages can be sent in the same time")
	}

	dict := cell.NewDict(16)

	for i, message := range messages {
		msg, err := message.InternalMessage.ToCell()
		if err != nil {
			return nil, fmt.Errorf("failed to convert msg to cell: %w", err)
		}

		data := cell.BeginCell().
			MustStoreUInt(uint64(message.Mode), 8).
			MustStoreRef(msg).
			EndCell()

		if err = dict.SetIntKey(big.NewInt(int64(i)), data); err != nil {
			return nil, fmt.Errorf("failed to add msg to dict: %w", err)
		}
	}

	boundedID := uint64(timeNow().Add(time.Duration(s.messagesTTL)*time.Second).UTC().Unix()<<32) + uint64(queryID)
	payload := cell.BeginCell().MustStoreUInt(uint64(s.wallet.subwallet), 32).
		MustStoreUInt(boundedID, 64).
		MustStoreDict(dict)

	sign := payload.EndCell().Sign(s.wallet.key)
	msg := cell.BeginCell().MustStoreSlice(sign, 512).MustStoreBuilder(payload).EndCell()

	return msg, nil
}
