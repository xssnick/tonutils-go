package ton

import (
	"bytes"
	"errors"
	"fmt"
	"math"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

const workchainInvalid = int32(math.MinInt32)

type dispatchQueueMessageProof struct {
	message DispatchQueueMessage
	root    *cell.Cell
}

func checkQueueStateProof(block *BlockIDExt, proof []byte) (*tlb.ShardStateUnsplit, error) {
	if len(proof) == 0 {
		return nil, ErrNoProof
	}

	roots, err := cell.FromBOCMultiRoot(proof)
	if err != nil {
		return nil, fmt.Errorf("failed to parse queue proof: %w", err)
	}
	if len(roots) != 2 {
		return nil, fmt.Errorf("queue proof should have 2 roots")
	}

	shardState, err := CheckBlockShardStateProof(roots, block.RootHash)
	if err != nil {
		return nil, fmt.Errorf("failed to verify queue state proof: %w", err)
	}
	return shardState, nil
}

func loadOutMsgQueueInfoFromProof(shardState *tlb.ShardStateUnsplit) (*tlb.OutMsgQueueInfo, error) {
	if shardState.OutMsgQueueInfo == nil {
		return nil, fmt.Errorf("no out message queue info in proof")
	}

	var info tlb.OutMsgQueueInfo
	loader, err := shardState.OutMsgQueueInfo.BeginParse()
	if err != nil {
		return nil, fmt.Errorf("failed to load out message queue info proof: %w", err)
	}
	if err := tlb.LoadFromCellAsProof(&info, loader); err != nil {
		return nil, fmt.Errorf("failed to load out message queue info from proof: %w", err)
	}
	return &info, nil
}

func dispatchQueueFromInfo(info *tlb.OutMsgQueueInfo) *cell.AugmentedDictionary {
	if info == nil || info.Extra == nil || info.Extra.DispatchQueue == nil {
		return nil
	}
	return info.Extra.DispatchQueue.AugmentedDictionary
}

func verifyBlockOutMsgQueueSizeProof(block *BlockIDExt, res *BlockOutMsgQueueSize) error {
	if res.ID == nil || !res.ID.Equals(block) {
		return fmt.Errorf("response with incorrect block id")
	}

	shardState, err := checkQueueStateProof(block, res.Proof)
	if err != nil {
		return err
	}

	info, err := loadOutMsgQueueInfoFromProof(shardState)
	if err != nil {
		return err
	}
	if info.Extra == nil || info.Extra.OutQueueSize == nil {
		return fmt.Errorf("no out message queue size in proof")
	}
	if int64(*info.Extra.OutQueueSize) != res.Size {
		return fmt.Errorf("out message queue size mismatch")
	}

	return nil
}

func verifyDispatchQueueInfoProof(block *BlockIDExt, afterAddr *address.Address, maxAccounts int, res *DispatchQueueInfo) error {
	if res.ID == nil || !res.ID.Equals(block) {
		return fmt.Errorf("response with incorrect block id")
	}
	if maxAccounts <= 0 {
		return fmt.Errorf("maxAccounts should be positive")
	}

	shardState, err := checkQueueStateProof(block, res.Proof)
	if err != nil {
		return err
	}

	info, err := loadOutMsgQueueInfoFromProof(shardState)
	if err != nil {
		return err
	}

	expected, complete, err := collectDispatchQueueInfo(dispatchQueueFromInfo(info), afterAddr, maxAccounts)
	if err != nil {
		return err
	}
	if complete != res.Complete {
		return fmt.Errorf("dispatch queue completeness mismatch")
	}
	if err = compareDispatchQueueInfo(expected, res.AccountDispatchQueues); err != nil {
		return err
	}

	return nil
}

func verifyDispatchQueueMessagesProof(block *BlockIDExt, addr *address.Address, afterLT uint64, maxMessages int, req GetDispatchQueueMessages, res *DispatchQueueMessages) error {
	if res.ID == nil || !res.ID.Equals(block) {
		return fmt.Errorf("response with incorrect block id")
	}
	if maxMessages <= 0 {
		return fmt.Errorf("maxMessages should be positive")
	}

	shardState, err := checkQueueStateProof(block, res.Proof)
	if err != nil {
		return err
	}

	info, err := loadOutMsgQueueInfoFromProof(shardState)
	if err != nil {
		return err
	}

	withMessagesBOC := req.Mode&(1<<2) != 0
	expected, complete, err := collectDispatchQueueMessages(dispatchQueueFromInfo(info), addr.Data(), afterLT, maxMessages, req.Mode&(1<<1) != 0, withMessagesBOC)
	if err != nil {
		return err
	}
	if complete != res.Complete {
		return fmt.Errorf("dispatch queue messages completeness mismatch")
	}
	if err = compareDispatchQueueMessages(expected, res.Messages); err != nil {
		return err
	}
	if withMessagesBOC {
		if err = verifyDispatchQueueMessagesBOC(expected, res.MessagesBOC); err != nil {
			return err
		}
	}

	return nil
}

func collectDispatchQueueInfo(dispatchQueue *cell.AugmentedDictionary, afterAddr *address.Address, maxAccounts int) ([]AccountDispatchQueueInfo, bool, error) {
	remaining := maxAccounts
	if remaining > 64 {
		remaining = 64
	}

	allowEq := true
	keyData := make([]byte, 32)
	if afterAddr != nil {
		allowEq = false
		keyData = afterAddr.Data()
	}

	result := make([]AccountDispatchQueueInfo, 0, remaining)
	for {
		key, value, err := lookupNextDispatchQueueAccount(dispatchQueue, keyData, allowEq)
		allowEq = false
		if errors.Is(err, cell.ErrNoSuchKeyInDict) {
			return result, true, nil
		}
		if err != nil {
			return nil, false, fmt.Errorf("failed to lookup dispatch queue account: %w", err)
		}
		if remaining == 0 {
			return result, false, nil
		}
		remaining--

		addrData, err := loadBits(key, 256)
		if err != nil {
			return nil, false, fmt.Errorf("failed to load dispatch queue account address: %w", err)
		}

		accountQueue, err := loadAccountDispatchQueue(value)
		if err != nil {
			return nil, false, fmt.Errorf("failed to load account dispatch queue: %w", err)
		}
		if accountQueue.Count == 0 || accountQueue.Messages == nil || accountQueue.Messages.IsEmpty() {
			return nil, false, fmt.Errorf("invalid empty account dispatch queue")
		}

		minLT, maxLT, err := accountDispatchQueueMinMaxLT(accountQueue)
		if err != nil {
			return nil, false, err
		}

		result = append(result, AccountDispatchQueueInfo{
			Addr:  addrData,
			Size:  int64(accountQueue.Count),
			MinLT: minLT,
			MaxLT: maxLT,
		})
		keyData = addrData
	}
}

func collectDispatchQueueMessages(dispatchQueue *cell.AugmentedDictionary, addr []byte, afterLT uint64, maxMessages int, oneAccount bool, withMessagesBOC bool) ([]dispatchQueueMessageProof, bool, error) {
	remaining := maxMessages
	if withMessagesBOC && remaining > 16 {
		remaining = 16
	} else if remaining > 64 {
		remaining = 64
	}

	origAddr := append([]byte{}, addr...)
	currentAddr := append([]byte{}, addr...)
	lt := afterLT
	first := true
	result := make([]dispatchQueueMessageProof, 0, remaining)

	for remaining > 0 {
		key, value, err := lookupNextDispatchQueueAccount(dispatchQueue, currentAddr, first)
		if errors.Is(err, cell.ErrNoSuchKeyInDict) {
			return result, true, nil
		}
		if err != nil {
			return nil, false, fmt.Errorf("failed to lookup dispatch queue account: %w", err)
		}

		currentAddr, err = loadBits(key, 256)
		if err != nil {
			return nil, false, fmt.Errorf("failed to load dispatch queue account address: %w", err)
		}
		if oneAccount && !bytes.Equal(currentAddr, origAddr) {
			return result, true, nil
		}

		accountQueue, err := loadAccountDispatchQueue(value)
		if err != nil {
			return nil, false, fmt.Errorf("failed to load account dispatch queue: %w", err)
		}
		if accountQueue.Count == 0 || accountQueue.Messages == nil || accountQueue.Messages.IsEmpty() {
			return nil, false, fmt.Errorf("invalid empty account dispatch queue")
		}

		for {
			key, value, err := lookupNextAccountDispatchMessage(accountQueue.Messages, lt)
			if errors.Is(err, cell.ErrNoSuchKeyInDict) {
				break
			}
			if err != nil {
				return nil, false, fmt.Errorf("failed to lookup account dispatch message: %w", err)
			}

			lt, err = loadUint64Key(key)
			if err != nil {
				return nil, false, fmt.Errorf("failed to load account dispatch message lt: %w", err)
			}
			if remaining == 0 {
				break
			}
			remaining--

			msg, err := loadDispatchQueueMessage(currentAddr, lt, value)
			if err != nil {
				return nil, false, err
			}
			result = append(result, msg)
		}

		first = false
		lt = 0
	}

	return result, false, nil
}

func lookupNextDispatchQueueAccount(dispatchQueue *cell.AugmentedDictionary, addr []byte, allowEq bool) (*cell.Cell, *cell.Slice, error) {
	if dispatchQueue == nil || dispatchQueue.IsEmpty() {
		return nil, nil, cell.ErrNoSuchKeyInDict
	}
	return dispatchQueue.LookupNearestKey(cell.BeginCell().MustStoreSlice(addr, 256).EndCell(), true, allowEq, false)
}

func lookupNextAccountDispatchMessage(messages *cell.Dictionary, lt uint64) (*cell.Cell, *cell.Slice, error) {
	if messages == nil || messages.IsEmpty() {
		return nil, nil, cell.ErrNoSuchKeyInDict
	}
	return messages.LookupNearestKey(cell.BeginCell().MustStoreUInt(lt, 64).EndCell(), true, false, false)
}

func loadAccountDispatchQueue(value *cell.Slice) (*tlb.AccountDispatchQueue, error) {
	var queue tlb.AccountDispatchQueue
	if err := tlb.LoadFromCellAsProof(&queue, value); err != nil {
		return nil, err
	}
	return &queue, nil
}

func accountDispatchQueueMinMaxLT(queue *tlb.AccountDispatchQueue) (uint64, uint64, error) {
	minKey, _, err := queue.Messages.LoadMinMax(false, false)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to load min dispatch queue lt: %w", err)
	}
	maxKey, _, err := queue.Messages.LoadMinMax(true, false)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to load max dispatch queue lt: %w", err)
	}

	minLT, err := loadUint64Key(minKey)
	if err != nil {
		return 0, 0, err
	}
	maxLT, err := loadUint64Key(maxKey)
	if err != nil {
		return 0, 0, err
	}

	return minLT, maxLT, nil
}

func loadDispatchQueueMessage(addr []byte, lt uint64, value *cell.Slice) (dispatchQueueMessageProof, error) {
	var enqueued tlb.EnqueuedMsg
	if err := tlb.LoadFromCellAsProof(&enqueued, value); err != nil {
		return dispatchQueueMessageProof{}, fmt.Errorf("failed to load enqueued message: %w", err)
	}

	var envelope tlb.MsgEnvelope
	loader, err := enqueued.Msg.BeginParse()
	if err != nil {
		return dispatchQueueMessageProof{}, fmt.Errorf("failed to load message envelope cell: %w", err)
	}
	if err := tlb.LoadFromCellAsProof(&envelope, loader); err != nil {
		return dispatchQueueMessageProof{}, fmt.Errorf("failed to load message envelope: %w", err)
	}
	if envelope.Msg == nil {
		return dispatchQueueMessageProof{}, fmt.Errorf("message envelope has no message")
	}

	msg := DispatchQueueMessage{
		Addr:     append([]byte{}, addr...),
		LT:       lt,
		Hash:     envelope.Msg.Hash(),
		Metadata: transactionMetadataFromEnvelope(envelope.Metadata),
	}
	return dispatchQueueMessageProof{
		message: msg,
		root:    envelope.Msg,
	}, nil
}

func transactionMetadataFromEnvelope(metadata *tlb.MsgMetadata) TransactionMetadata {
	if metadata == nil {
		return TransactionMetadata{
			Depth: -1,
			Initiator: AccountID{
				Workchain: workchainInvalid,
				ID:        make([]byte, 32),
			},
			InitiatorLT: math.MaxUint64,
		}
	}

	return TransactionMetadata{
		Depth: int32(metadata.Depth),
		Initiator: AccountID{
			Workchain: metadata.Initiator.Workchain(),
			ID:        metadata.Initiator.Data(),
		},
		InitiatorLT: metadata.InitiatorLT,
	}
}

func compareDispatchQueueInfo(expected, got []AccountDispatchQueueInfo) error {
	if len(expected) != len(got) {
		return fmt.Errorf("dispatch queue account count mismatch")
	}
	for i := range expected {
		if !bytes.Equal(expected[i].Addr, got[i].Addr) ||
			expected[i].Size != got[i].Size ||
			expected[i].MinLT != got[i].MinLT ||
			expected[i].MaxLT != got[i].MaxLT {
			return fmt.Errorf("dispatch queue account %d mismatch", i)
		}
	}
	return nil
}

func compareDispatchQueueMessages(expected []dispatchQueueMessageProof, got []DispatchQueueMessage) error {
	if len(expected) != len(got) {
		return fmt.Errorf("dispatch queue message count mismatch")
	}
	for i := range expected {
		if !bytes.Equal(expected[i].message.Addr, got[i].Addr) ||
			expected[i].message.LT != got[i].LT ||
			!bytes.Equal(expected[i].message.Hash, got[i].Hash) ||
			!transactionMetadataEqual(expected[i].message.Metadata, got[i].Metadata) {
			return fmt.Errorf("dispatch queue message %d mismatch", i)
		}
	}
	return nil
}

func verifyDispatchQueueMessagesBOC(expected []dispatchQueueMessageProof, boc []byte) error {
	if len(expected) == 0 && len(boc) == 0 {
		return nil
	}

	roots, err := cell.FromBOCMultiRoot(boc)
	if err != nil {
		return fmt.Errorf("failed to parse dispatch queue messages BOC: %w", err)
	}
	if len(roots) != len(expected) {
		return fmt.Errorf("dispatch queue messages BOC count mismatch")
	}
	for i := range roots {
		if !bytes.Equal(roots[i].Hash(), expected[i].root.Hash()) {
			return fmt.Errorf("dispatch queue messages BOC root %d mismatch", i)
		}
	}
	return nil
}

func transactionMetadataEqual(a, b TransactionMetadata) bool {
	return a.Depth == b.Depth &&
		a.Initiator.Workchain == b.Initiator.Workchain &&
		bytes.Equal(a.Initiator.ID, b.Initiator.ID) &&
		a.InitiatorLT == b.InitiatorLT
}

func loadBits(c *cell.Cell, bits uint) ([]byte, error) {
	loader, err := c.BeginParse()
	if err != nil {
		return nil, err
	}
	return loader.LoadSlice(bits)
}

func loadUint64Key(c *cell.Cell) (uint64, error) {
	loader, err := c.BeginParse()
	if err != nil {
		return 0, err
	}
	return loader.LoadUInt(64)
}
