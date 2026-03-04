package ton

import (
	"context"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tl"
)

func init() {
	tl.Register(OutMsgQueueSizes{}, "liteServer.outMsgQueueSizes shards:(vector liteServer.outMsgQueueSize) ext_msg_queue_size_limit:int = liteServer.OutMsgQueueSizes")
	tl.Register(OutMsgQueueSize{}, "liteServer.outMsgQueueSize id:tonNode.blockIdExt size:int = liteServer.OutMsgQueueSize")
	tl.Register(BlockOutMsgQueueSize{}, "liteServer.blockOutMsgQueueSize mode:# id:tonNode.blockIdExt size:long proof:mode.0?bytes = liteServer.BlockOutMsgQueueSize")
	tl.Register(DispatchQueueInfo{}, "liteServer.dispatchQueueInfo mode:# id:tonNode.blockIdExt account_dispatch_queues:(vector liteServer.accountDispatchQueueInfo) complete:Bool proof:mode.0?bytes = liteServer.DispatchQueueInfo")
	tl.Register(AccountDispatchQueueInfo{}, "liteServer.accountDispatchQueueInfo addr:int256 size:long min_lt:long max_lt:long = liteServer.AccountDispatchQueueInfo")
	tl.Register(DispatchQueueMessages{}, "liteServer.dispatchQueueMessages mode:# id:tonNode.blockIdExt messages:(vector liteServer.dispatchQueueMessage) complete:Bool proof:mode.0?bytes messages_boc:mode.2?bytes = liteServer.DispatchQueueMessages")
	tl.Register(DispatchQueueMessage{}, "liteServer.dispatchQueueMessage addr:int256 lt:long hash:int256 metadata:liteServer.transactionMetadata = liteServer.DispatchQueueMessage")
	tl.Register(TransactionMetadata{}, "liteServer.transactionMetadata mode:# depth:int initiator:liteServer.accountId initiator_lt:long = liteServer.TransactionMetadata")

	tl.Register(GetOutMsgQueueSizes{}, "liteServer.getOutMsgQueueSizes mode:# wc:mode.0?int shard:mode.0?long = liteServer.OutMsgQueueSizes")
	tl.Register(GetBlockOutMsgQueueSize{}, "liteServer.getBlockOutMsgQueueSize mode:# id:tonNode.blockIdExt want_proof:mode.0?true = liteServer.BlockOutMsgQueueSize")
	tl.Register(GetDispatchQueueInfo{}, "liteServer.getDispatchQueueInfo mode:# id:tonNode.blockIdExt after_addr:mode.1?int256 max_accounts:int want_proof:mode.0?true = liteServer.DispatchQueueInfo")
	tl.Register(GetDispatchQueueMessages{}, "liteServer.getDispatchQueueMessages mode:# id:tonNode.blockIdExt addr:int256 after_lt:long max_messages:int want_proof:mode.0?true one_account:mode.1?true messages_boc:mode.2?true = liteServer.DispatchQueueMessages")
}

type OutMsgQueueSizes struct {
	Shards               []OutMsgQueueSize `tl:"vector struct"`
	ExtMsgQueueSizeLimit int32             `tl:"int"`
}

type OutMsgQueueSize struct {
	ID   *BlockIDExt `tl:"struct"`
	Size int32       `tl:"int"`
}

type BlockOutMsgQueueSize struct {
	Mode  uint32      `tl:"flags"`
	ID    *BlockIDExt `tl:"struct"`
	Size  int64       `tl:"long"`
	Proof []byte      `tl:"?0 bytes"`
}

type DispatchQueueInfo struct {
	Mode                  uint32                     `tl:"flags"`
	ID                    *BlockIDExt                `tl:"struct"`
	AccountDispatchQueues []AccountDispatchQueueInfo `tl:"vector struct"`
	Complete              bool                       `tl:"bool"`
	Proof                 []byte                     `tl:"?0 bytes"`
}

type AccountDispatchQueueInfo struct {
	Addr  []byte `tl:"int256"`
	Size  int64  `tl:"long"`
	MinLT uint64 `tl:"long"`
	MaxLT uint64 `tl:"long"`
}

type DispatchQueueMessages struct {
	Mode        uint32                 `tl:"flags"`
	ID          *BlockIDExt            `tl:"struct"`
	Messages    []DispatchQueueMessage `tl:"vector struct"`
	Complete    bool                   `tl:"bool"`
	Proof       []byte                 `tl:"?0 bytes"`
	MessagesBOC []byte                 `tl:"?2 bytes"`
}

type DispatchQueueMessage struct {
	Addr     []byte              `tl:"int256"`
	LT       uint64              `tl:"long"`
	Hash     []byte              `tl:"int256"`
	Metadata TransactionMetadata `tl:"struct"`
}

type TransactionMetadata struct {
	Mode        uint32    `tl:"flags"`
	Depth       int32     `tl:"int"`
	Initiator   AccountId `tl:"struct"`
	InitiatorLT uint64    `tl:"long"`
}

type AccountId struct {
	Workchain int32  `tl:"int"`
	ID        []byte `tl:"int256"`
}

// Requests

type GetOutMsgQueueSizes struct {
	Mode  uint32 `tl:"flags"`
	WC    int32  `tl:"?0 int"`
	Shard int64  `tl:"?0 long"`
}

type GetBlockOutMsgQueueSize struct {
	Mode      uint32      `tl:"flags"`
	ID        *BlockIDExt `tl:"struct"`
	WantProof *True       `tl:"?0 struct"`
}

type GetDispatchQueueInfo struct {
	Mode        uint32      `tl:"flags"`
	ID          *BlockIDExt `tl:"struct"`
	AfterAddr   []byte      `tl:"?1 int256"`
	MaxAccounts int32       `tl:"int"`
	WantProof   *True       `tl:"?0 struct"`
}

type GetDispatchQueueMessages struct {
	Mode        uint32      `tl:"flags"`
	ID          *BlockIDExt `tl:"struct"`
	Addr        []byte      `tl:"int256"`
	AfterLT     uint64      `tl:"long"`
	MaxMessages int32       `tl:"int"`
	WantProof   *True       `tl:"?0 struct"`
	OneAccount  *True       `tl:"?1 struct"`
	MessagesBOC *True       `tl:"?2 struct"`
}

func (c *APIClient) GetOutMsgQueueSizes(ctx context.Context, wc *int32, shard *int64) (*OutMsgQueueSizes, error) {
	req := GetOutMsgQueueSizes{}
	if wc != nil && shard != nil {
		req.Mode = 1
		req.WC = *wc
		req.Shard = *shard
	}

	var resp tl.Serializable
	err := c.client.QueryLiteserver(ctx, req, &resp)
	if err != nil {
		return nil, err
	}

	switch t := resp.(type) {
	case OutMsgQueueSizes:
		return &t, nil
	case LSError:
		return nil, t
	}
	return nil, errUnexpectedResponse(resp)
}

func (c *APIClient) GetBlockOutMsgQueueSize(ctx context.Context, block *BlockIDExt) (*BlockOutMsgQueueSize, error) {
	// TODO: support proofs
	req := GetBlockOutMsgQueueSize{
		ID: block,
	}

	var resp tl.Serializable
	err := c.client.QueryLiteserver(ctx, req, &resp)
	if err != nil {
		return nil, err
	}

	switch t := resp.(type) {
	case BlockOutMsgQueueSize:
		return &t, nil
	case LSError:
		return nil, t
	}
	return nil, errUnexpectedResponse(resp)
}

func (c *APIClient) GetDispatchQueueInfo(ctx context.Context, block *BlockIDExt, afterAddr *address.Address, maxAccounts int) (*DispatchQueueInfo, error) {
	// TODO: support proofs
	req := GetDispatchQueueInfo{
		ID:          block,
		MaxAccounts: int32(maxAccounts),
	}

	if afterAddr != nil {
		req.Mode |= 1 << 1
		req.AfterAddr = afterAddr.Data()
	}

	var resp tl.Serializable
	err := c.client.QueryLiteserver(ctx, req, &resp)
	if err != nil {
		return nil, err
	}

	switch t := resp.(type) {
	case DispatchQueueInfo:
		return &t, nil
	case LSError:
		return nil, t
	}
	return nil, errUnexpectedResponse(resp)
}

func (c *APIClient) GetDispatchQueueMessages(ctx context.Context, block *BlockIDExt, addr *address.Address, afterLT uint64, maxMessages int, options ...func(*GetDispatchQueueMessages)) (*DispatchQueueMessages, error) {
	// TODO: support proofs
	req := GetDispatchQueueMessages{
		ID:          block,
		Addr:        addr.Data(),
		AfterLT:     afterLT,
		MaxMessages: int32(maxMessages),
	}

	for _, opt := range options {
		opt(&req)
	}

	var resp tl.Serializable
	err := c.client.QueryLiteserver(ctx, req, &resp)
	if err != nil {
		return nil, err
	}

	switch t := resp.(type) {
	case DispatchQueueMessages:
		return &t, nil
	case LSError:
		return nil, t
	}
	return nil, errUnexpectedResponse(resp)
}

func (m *AccountDispatchQueueInfo) Address() *address.Address {
	addr := address.NewAddress(0, 0, m.Addr)
	return addr
}

func (m *DispatchQueueMessage) Address() *address.Address {
	addr := address.NewAddress(0, 0, m.Addr)
	return addr
}

func (m *TransactionMetadata) InitiatorAddress() *address.Address {
	addr := address.NewAddress(0, byte(m.Initiator.Workchain), m.Initiator.ID)
	return addr
}

func (m *DispatchQueueMessages) GetTotalMessagesCount() int {
	// If messages BOC is present, we need to parse it to get exact count if it differs from []Messages
	// For now just return slice length
	return len(m.Messages)
}

// Options for GetDispatchQueueMessages

func WithDispatchQueueMessagesBOC() func(*GetDispatchQueueMessages) {
	return func(req *GetDispatchQueueMessages) {
		req.Mode |= 1 << 2
		req.MessagesBOC = &True{}
	}
}

func WithDispatchQueueOneAccount() func(*GetDispatchQueueMessages) {
	return func(req *GetDispatchQueueMessages) {
		req.Mode |= 1 << 1
		req.OneAccount = &True{}
	}
}
