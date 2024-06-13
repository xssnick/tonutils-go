package ton

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func init() {
	tl.Register(LSError{}, "liteServer.error code:int message:string = liteServer.Error")
}

type ProofCheckPolicy int

const (
	ProofCheckPolicyUnsafe ProofCheckPolicy = iota
	ProofCheckPolicyFast                    // Without master block checks
	ProofCheckPolicySecure
)

const (
	ErrCodeContractNotInitialized = -256
)

type LiteClient interface {
	QueryLiteserver(ctx context.Context, payload tl.Serializable, result tl.Serializable) error
	StickyContext(ctx context.Context) context.Context
	StickyContextNextNode(ctx context.Context) (context.Context, error)
	StickyContextNextNodeBalanced(ctx context.Context) (context.Context, error)
	StickyNodeID(ctx context.Context) uint32
}

type ContractExecError struct {
	Code int32
}

type LSError struct {
	Code int32  `tl:"int"`
	Text string `tl:"string"`
}

// Deprecated: use APIClientWrapped
type APIClientWaiter = APIClientWrapped

type APIClientWrapped interface {
	Client() LiteClient
	GetTime(ctx context.Context) (uint32, error)
	GetLibraries(ctx context.Context, list ...[]byte) ([]*cell.Cell, error)
	LookupBlock(ctx context.Context, workchain int32, shard int64, seqno uint32) (*BlockIDExt, error)
	GetBlockData(ctx context.Context, block *BlockIDExt) (*tlb.Block, error)
	GetBlockTransactionsV2(ctx context.Context, block *BlockIDExt, count uint32, after ...*TransactionID3) ([]TransactionShortInfo, bool, error)
	GetBlockShardsInfo(ctx context.Context, master *BlockIDExt) ([]*BlockIDExt, error)
	GetBlockchainConfig(ctx context.Context, block *BlockIDExt, onlyParams ...int32) (*BlockchainConfig, error)
	GetMasterchainInfo(ctx context.Context) (*BlockIDExt, error)
	GetAccount(ctx context.Context, block *BlockIDExt, addr *address.Address) (*tlb.Account, error)
	SendExternalMessage(ctx context.Context, msg *tlb.ExternalMessage) error
	RunGetMethod(ctx context.Context, blockInfo *BlockIDExt, addr *address.Address, method string, params ...interface{}) (*ExecutionResult, error)
	ListTransactions(ctx context.Context, addr *address.Address, num uint32, lt uint64, txHash []byte) ([]*tlb.Transaction, error)
	GetTransaction(ctx context.Context, block *BlockIDExt, addr *address.Address, lt uint64) (*tlb.Transaction, error)
	GetBlockProof(ctx context.Context, known, target *BlockIDExt) (*PartialBlockProof, error)
	CurrentMasterchainInfo(ctx context.Context) (_ *BlockIDExt, err error)
	SubscribeOnTransactions(workerCtx context.Context, addr *address.Address, lastProcessedLT uint64, channel chan<- *tlb.Transaction)
	VerifyProofChain(ctx context.Context, from, to *BlockIDExt) error
	WaitForBlock(seqno uint32) APIClientWrapped
	WithRetry(maxRetries ...int) APIClientWrapped
	WithTimeout(timeout time.Duration) APIClientWrapped
	SetTrustedBlock(block *BlockIDExt)
	SetTrustedBlockFromConfig(cfg *liteclient.GlobalConfig)
	FindLastTransactionByInMsgHash(ctx context.Context, addr *address.Address, msgHash []byte, maxTxNumToScan ...int) (*tlb.Transaction, error)
	FindLastTransactionByOutMsgHash(ctx context.Context, addr *address.Address, msgHash []byte, maxTxNumToScan ...int) (*tlb.Transaction, error)
}

type APIClient struct {
	client LiteClient
	parent *APIClient

	trustedBlock     *BlockIDExt
	curMasters       map[uint32]*masterInfo
	curMastersLock   sync.RWMutex
	proofCheckPolicy ProofCheckPolicy

	trustedLock sync.RWMutex
}

type masterInfo struct {
	updatedAt time.Time
	mx        sync.RWMutex
	block     *BlockIDExt
}

func NewAPIClient(client LiteClient, proofCheckPolicy ...ProofCheckPolicy) *APIClient {
	policy := ProofCheckPolicyFast
	if len(proofCheckPolicy) > 0 {
		policy = proofCheckPolicy[0]
	}

	return &APIClient{
		curMasters:       map[uint32]*masterInfo{},
		client:           client,
		proofCheckPolicy: policy,
	}
}

// SetTrustedBlock - set starting point to verify master block proofs chain
func (c *APIClient) SetTrustedBlock(block *BlockIDExt) {
	c.root().trustedBlock = block.Copy()
}

// SetTrustedBlockFromConfig - same as SetTrustedBlock but takes init block from config
func (c *APIClient) SetTrustedBlockFromConfig(cfg *liteclient.GlobalConfig) {
	b := BlockIDExt(cfg.Validator.InitBlock)
	c.SetTrustedBlock(&b)
}

// WaitForBlock - waits for the given master block seqno will be available on the requested node
func (c *APIClient) WaitForBlock(seqno uint32) APIClientWrapped {
	return &APIClient{
		parent:           c,
		client:           &waiterClient{original: c.client, seqno: seqno},
		proofCheckPolicy: c.proofCheckPolicy,
	}
}

// WithRetry
// If maxTries = 0
//
//	Automatically retires request to another available liteserver
//	when ADNL timeout, or error code 651 or -400 is received.
//
// If maxTries > 0
//
//	Limits additional attempts to this number.
func (c *APIClient) WithRetry(maxTries ...int) APIClientWrapped {
	tries := 0
	if len(maxTries) > 0 {
		tries = maxTries[0]
	}
	return &APIClient{
		parent:           c,
		client:           &retryClient{original: c.client, maxRetries: tries},
		proofCheckPolicy: c.proofCheckPolicy,
	}
}

// WithTimeout add timeout to each LiteServer request
func (c *APIClient) WithTimeout(timeout time.Duration) APIClientWrapped {
	return &APIClient{
		parent:           c,
		client:           &timeoutClient{original: c.client, timeout: timeout},
		proofCheckPolicy: c.proofCheckPolicy,
	}
}

func (c *APIClient) root() *APIClient {
	if c.parent != nil {
		return c.parent.root()
	}
	return c
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
	return fmt.Errorf("unexpected response received: %v", reflect.TypeOf(resp))
}
