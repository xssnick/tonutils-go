package ton

import (
	"context"
	"fmt"
	"github.com/xssnick/tonutils-go/tl"
	"reflect"
	"sync"
	"time"
)

func init() {
	tl.Register(LSError{}, "liteServer.error code:int message:string = liteServer.Error")
}

const (
	ErrCodeContractNotInitialized = -256
)

type LiteClient interface {
	QueryLiteserver(ctx context.Context, payload tl.Serializable, result tl.Serializable) error
	StickyContext(ctx context.Context) context.Context
	StickyNodeID(ctx context.Context) uint32
}

type ContractExecError struct {
	Code int32
}

type LSError struct {
	Code int32  `tl:"int"`
	Text string `tl:"string"`
}

type APIClient struct {
	client LiteClient

	curMasters     map[uint32]*masterInfo
	curMastersLock sync.RWMutex
}

type masterInfo struct {
	updatedAt time.Time
	mx        sync.RWMutex
	block     *BlockIDExt
}

func NewAPIClient(client LiteClient) *APIClient {
	return &APIClient{
		curMasters: map[uint32]*masterInfo{},
		client:     client,
	}
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

func errUnexpectedResponse(resp tl.Serializable) error {
	return fmt.Errorf("unexpected response received: %s", reflect.TypeOf(resp))
}
