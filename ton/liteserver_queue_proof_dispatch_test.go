package ton

import (
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// buildTestDispatchQueue returns a DispatchQueue holding one account with two
// enqueued messages. DispatchQueue is `HashmapAugE 256 AccountDispatchQueue
// uint64`, so every leaf carries a 64-bit min-lt augmentation ahead of the
// value.
func buildTestDispatchQueue(t *testing.T, addr []byte) *cell.AugmentedDictionary {
	t.Helper()

	messages := cell.NewDict(64)
	for i, lt := range []uint64{0x1000, 0x2000} {
		enqueued := cell.BeginCell().
			MustStoreUInt(lt, 64).
			MustStoreRef(cell.BeginCell().MustStoreUInt(uint64(0xA0+i), 8).EndCell()).
			EndCell()
		key := cell.BeginCell().MustStoreUInt(lt, 64).EndCell()
		if err := messages.Set(key, enqueued); err != nil {
			t.Fatalf("set dispatch message %d: %v", i, err)
		}
	}

	messagesCell, err := messages.ToCell()
	if err != nil {
		t.Fatalf("serialize dispatch messages: %v", err)
	}
	account := cell.BeginCell().
		MustStoreBoolBit(true).
		MustStoreRef(messagesCell).
		MustStoreUInt(2, 48).
		EndCell()

	queue, err := cell.NewAugDict(256, tlb.AugDispatchQueue{})
	if err != nil {
		t.Fatalf("new dispatch queue: %v", err)
	}
	if err := queue.Set(cell.BeginCell().MustStoreSlice(addr, 256).EndCell(), account); err != nil {
		t.Fatalf("set dispatch queue account: %v", err)
	}
	return queue
}

// TestLookupNextDispatchQueueAccountSkipsAugmentation pins that the dispatch
// queue lookup returns the AccountDispatchQueue value, not the raw HashmapAug
// leaf. The leaf is `extra ++ value`, so returning it raw hands the 64-bit
// min-lt augmentation to AccountDispatchQueue.LoadFromCell, whose first read is
// LoadDict(64).
func TestLookupNextDispatchQueueAccountSkipsAugmentation(t *testing.T) {
	addr := make([]byte, 32)
	addr[31] = 0x42
	queue := buildTestDispatchQueue(t, addr)

	_, value, err := lookupNextDispatchQueueAccount(queue, make([]byte, 32), true)
	if err != nil {
		t.Fatalf("lookup dispatch queue account: %v", err)
	}

	accountQueue, err := loadAccountDispatchQueue(value)
	if err != nil {
		t.Fatalf("load account dispatch queue: %v", err)
	}
	if accountQueue.Count != 2 {
		t.Fatalf("account dispatch queue count = %d, want 2", accountQueue.Count)
	}
	if accountQueue.Messages == nil || accountQueue.Messages.IsEmpty() {
		t.Fatal("account dispatch queue lost its messages")
	}

	minLT, maxLT, err := accountDispatchQueueMinMaxLT(accountQueue)
	if err != nil {
		t.Fatalf("dispatch queue lt bounds: %v", err)
	}
	if minLT != 0x1000 || maxLT != 0x2000 {
		t.Fatalf("dispatch queue lt bounds = [%d, %d], want [4096, 8192]", minLT, maxLT)
	}
}

// TestCollectDispatchQueueInfoReadsNonEmptyQueue covers the proof-checked path
// end to end: it is the code GetDispatchQueueInfo runs under any proof policy
// other than ProofCheckPolicyUnsafe.
func TestCollectDispatchQueueInfoReadsNonEmptyQueue(t *testing.T) {
	addr := make([]byte, 32)
	addr[31] = 0x42
	queue := buildTestDispatchQueue(t, addr)

	infos, complete, err := collectDispatchQueueInfo(queue, nil, 10)
	if err != nil {
		t.Fatalf("collect dispatch queue info: %v", err)
	}
	if !complete {
		t.Fatal("collect dispatch queue info did not complete")
	}
	if len(infos) != 1 {
		t.Fatalf("collected %d accounts, want 1", len(infos))
	}
	if infos[0].Size != 2 {
		t.Fatalf("account queue size = %d, want 2", infos[0].Size)
	}
	if string(infos[0].Addr) != string(addr) {
		t.Fatalf("account addr = %x, want %x", infos[0].Addr, addr)
	}
}

// TestAugmentedDictionaryLookupNearestKeyReturnsRawLeaf pins the documented
// contract of the raw lookup: the augmentation comes first. Callers that want
// the value must use LookupNearestKeyExtra.
func TestAugmentedDictionaryLookupNearestKeyReturnsRawLeaf(t *testing.T) {
	addr := make([]byte, 32)
	addr[31] = 0x42
	queue := buildTestDispatchQueue(t, addr)
	key := cell.BeginCell().MustStoreSlice(make([]byte, 32), 256).EndCell()

	_, raw, err := queue.LookupNearestKey(key, true, true, false)
	if err != nil {
		t.Fatalf("raw lookup: %v", err)
	}
	_, value, extra, err := queue.LookupNearestKeyExtra(key, true, true, false)
	if err != nil {
		t.Fatalf("decomposed lookup: %v", err)
	}

	if got := extra.BitsLeft(); got != 64 {
		t.Fatalf("extra size = %d bits, want the 64-bit min-lt augmentation", got)
	}
	if raw.BitsLeft() != extra.BitsLeft()+value.BitsLeft() {
		t.Fatalf("raw leaf = %d bits, want extra %d + value %d", raw.BitsLeft(), extra.BitsLeft(), value.BitsLeft())
	}

	// The raw leaf starts with the augmentation, which is exactly why feeding it
	// to AccountDispatchQueue misparses.
	augmentation, err := raw.LoadUInt(64)
	if err != nil {
		t.Fatalf("load augmentation prefix: %v", err)
	}
	expected, err := extra.LoadUInt(64)
	if err != nil {
		t.Fatalf("load extra: %v", err)
	}
	if augmentation != expected {
		t.Fatalf("raw leaf prefix = %d, want the augmentation %d", augmentation, expected)
	}
	if augmentation != 0x1000 {
		t.Fatalf("augmentation = %d, want the minimal message lt 4096", augmentation)
	}
}
