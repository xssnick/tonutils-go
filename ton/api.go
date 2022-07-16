package ton

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/xssnick/tonutils-go/liteclient"
)

// requests
const (
	_GetMasterchainInfo    int32 = -1984567762
	_RunContractGetMethod  int32 = 1556504018
	_GetAccountState       int32 = 1804144165
	_SendMessage           int32 = 1762317442
	_GetTransactions       int32 = 474015649
	_GetOneTransaction     int32 = -737205014
	_GetBlock              int32 = 1668796173
	_GetAllShardsInfo      int32 = 1960050027
	_ListBlockTransactions int32 = -1375942694
	_LookupBlock           int32 = -87492834
)

// responses
const (
	_RunQueryResult    int32 = -1550163605
	_AccountState      int32 = 1887029073
	_SendMessageResult int32 = 961602967
	_TransactionsList  int32 = 1864812043
	_TransactionInfo   int32 = 249490759
	_BlockData         int32 = -1519063700
	_BlockTransactions int32 = -1114854101
	_BlockHeader       int32 = 1965916697
	_AllShardsInfo     int32 = 160425773

	_BoolTrue  int32 = -1720552011
	_BoolFalse int32 = -1132882121
	_LSError   int32 = -1146494648
)

type LiteClient interface {
	Do(ctx context.Context, typeID int32, payload []byte) (*liteclient.LiteResponse, error)
}

type ContractExecError struct {
	Code uint32
}

type LSError struct {
	Code int32
	Text string
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

func (e LSError) Error() string {
	return fmt.Sprintf("lite server error, code %d: %s", e.Code, e.Text)
}

func (e LSError) Is(err error) bool {
	if le, ok := err.(LSError); ok && le.Code == e.Code {
		return true
	}
	return false
}

func (e *LSError) Load(data []byte) ([]byte, error) {
	if len(data) < 4+1 {
		return nil, errors.New("not enough length")
	}

	e.Code = int32(binary.LittleEndian.Uint32(data))
	txt, data := loadBytes(data[4:])
	e.Text = string(txt)

	return data, nil
}

func (e ContractExecError) Error() string {
	return fmt.Sprintf("contract exit code: %d", e.Code)
}

func (e ContractExecError) Is(err error) bool {
	if le, ok := err.(ContractExecError); ok && le.Code == e.Code {
		return true
	}
	return false
}
