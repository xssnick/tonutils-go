package ton

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"sync"
	"time"
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
	_WaitMasterchainSeqno  int32 = -1159022446
	_GetTime               int32 = 380459572
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
	_CurrentTime       int32 = -380436467

	_BoolTrue  int32 = -1720552011
	_BoolFalse int32 = -1132882121
	_LSError   int32 = -1146494648
)

const (
	ErrCodeContractNotInitialized = 4294967040
)

type LiteClient interface {
	Do(ctx context.Context, typeID int32, payload []byte) (*liteclient.LiteResponse, error)
	StickyContext(ctx context.Context) context.Context
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

	curMasterUpdateTime time.Time
	curMasterLock       sync.RWMutex
	curMaster           *tlb.BlockInfo
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
	var name string
	switch e.Code {
	case 2, 3, 4, 5, 6, 7, 8, 9, 10, 13, 32, 34, 37, 38, ErrCodeContractNotInitialized:
		name += " ("
		switch e.Code {
		case 2:
			name += "stack underflow. Last op-code consume more elements than there are on stacks"
		case 3:
			name += "stack overflow. More values have been stored on a stack than allowed by this version of TVM"
		case 4:
			name += "integer overflow. Integer does not fit into −2256 ≤ x < 2256 or a division by zero has occurred"
		case 5:
			name += "integer out of expected range"
		case 6:
			name += "invalid opcode. Instruction in unknown to current TVM version"
		case 7:
			name += "type check error. An argument to a primitive is of incorrect value type"
		case 8:
			name += "cell overflow. Writing to builder is not possible since after operation there would be more than 1023 bits or 4 references"
		case 9:
			name += "cell underflow. Read from slice primitive tried to read more bits or references than there are"
		case 10:
			name += "dictionary error. Error during manipulation with dictionary (hashmaps)"
		case 13:
			name += "out of gas error. Thrown by TVM when the remaining gas becomes negative"
		case 32:
			name += "action list is invalid. Set during action phase if c5 register after execution contains unparsable object"
		case 34:
			name += "action is invalid or not supported. Set during action phase if current action can not be applied"
		case 37:
			name += "not enough TONs. Message sends too much TON (or there is no enough TONs after deducting fees)"
		case 38:
			name += "not enough extra-currencies"
		case ErrCodeContractNotInitialized:
			name += "contract is not initialized"
		}
		name += ")"
	}

	return fmt.Sprintf("contract exit code: %d%s", e.Code, name)
}

func (e ContractExecError) Is(err error) bool {
	if le, ok := err.(ContractExecError); ok && le.Code == e.Code {
		return true
	}
	return false
}
