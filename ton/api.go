package ton

import (
	"context"
	"encoding/binary"

	"github.com/xssnick/tonutils-go/liteclient"
)

// requests
const (
	_GetMasterchainInfo   int32 = -1984567762
	_RunContractGetMethod int32 = 1556504018
	_GetAccountState      int32 = 1804144165
	_SendMessage          int32 = 1762317442
	_GetTransactions      int32 = 474015649
)

// responses
const (
	_RunQueryResult    int32 = -1550163605
	_AccountState      int32 = 1887029073
	_SendMessageResult int32 = 961602967
	_TransactionsList  int32 = 1864812043
	_LSError           int32 = -1146494648
)

type LiteClient interface {
	Do(ctx context.Context, typeID int32, payload []byte) (*liteclient.LiteResponse, error)
}

type APIClient struct {
	client LiteClient
}

func NewAPIClient(client LiteClient) *APIClient {
	return &APIClient{
		client: client,
	}
}

func loadBytes(data []byte) (loaded []byte, buffer []byte) {
	offset := 1
	ln := int(data[0])
	if ln == 0xFE {
		ln = int(binary.LittleEndian.Uint32(data)) >> 8
		offset = 4
	}

	// bytes length should be dividable by 4, add additional offset to buffer if it is not
	bufSz := ln
	if add := ln % 4; add != 0 {
		bufSz += 4 - add
	}

	// if its end, we don't need to align by 4
	if offset+bufSz >= len(data) {
		return data[offset : offset+ln], nil
	}

	return data[offset : offset+ln], data[offset+bufSz:]
}

func storableBytes(buf []byte) []byte {
	var data []byte

	// store buf length
	if len(buf) >= 0xFE {
		ln := make([]byte, 4)
		binary.LittleEndian.PutUint32(ln, uint32(len(buf)<<8)|0xFE)
		data = append(data, ln...)
	} else {
		data = append(data, byte(len(buf)))
	}

	data = append(data, buf...)

	// adjust actual length to fit % 4 = 0
	if round := (len(buf) + 1) % 4; round != 0 {
		data = append(data, make([]byte, 4-round)...)
	}

	return data
}
