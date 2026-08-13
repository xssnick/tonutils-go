package tvm

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/internal/bigint"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func transactionCoinsNano(coins *tlb.Coins) *big.Int {
	if coins == nil {
		return nil
	}
	return coins.Nano()
}

// transactionCoinsNanoRef is the read-only sibling of transactionCoinsNano, for
// operands that are compared, serialised or added into somebody else's
// accumulator. The result MUST NOT be mutated.
func transactionCoinsNanoRef(coins *tlb.Coins) *big.Int {
	if coins == nil {
		return nil
	}
	return coins.NanoRef()
}

func transactionCoinsPtr(nano *big.Int) *tlb.Coins {
	if nano == nil || nano.Sign() == 0 {
		return nil
	}
	coins := tlb.FromNanoTON(nano)
	return &coins
}

// transactionCoinsClonePtr is transactionCoinsPtr(transactionCoinsNano(coins))
// with one copy instead of two: the intermediate the second copy was taken from
// was itself freshly made and visible to nobody else.
func transactionCoinsClonePtr(coins *tlb.Coins) *tlb.Coins {
	if coins == nil {
		return nil
	}
	nano := coins.NanoRef()
	if nano.Sign() == 0 {
		return nil
	}
	out := tlb.FromOwnedNanoTON(bigint.Set(nano))
	return &out
}

// transactionBigOrZero returns a private copy the caller owns and may mutate in
// place: transactionCurrencyBalance.copy feeds it straight into Add/Sub (see
// transaction_currency.go add/sub and transaction_bounce.go), and
// transactionSendActionFineFunds accumulates into its result. It therefore must
// keep allocating; use transactionSharedBigOrZero for read-only c7 leaves.
func transactionBigOrZero(v *big.Int) *big.Int {
	if v == nil {
		return bigint.FromInt64(0)
	}
	return bigint.Set(v)
}

// transactionSharedBigOrZero is the read-only sibling of transactionBigOrZero
// for values that are stored into a c7 tuple and never mutated. Small values
// come from the VM's shared static pool; c7 leaves are only ever handed out by
// tuple.Tuple.Index, which clones every *big.Int leaf, so a pooled instance
// cannot reach an in-place mutation. The result MUST NOT be mutated.
func transactionSharedBigOrZero(v *big.Int) *big.Int {
	if v == nil {
		return vm.StaticInt(0)
	}
	if v.IsInt64() {
		if shared := vm.StaticInt(v.Int64()); shared != nil {
			return shared
		}
	}
	return bigint.Set(v)
}

func transactionCollectUsage(root *cell.Cell) (transactionUsage, error) {
	return newTransactionUsageCollector().addCell(root, false)
}

func transactionLoadedCell(root *cell.Cell) (*cell.Cell, error) {
	if root == nil {
		return nil, nil
	}

	sl, err := root.BeginParseWithoutTrace()
	if err != nil {
		return nil, err
	}
	return sl.BaseCell(), nil
}

type transactionUsageCollector struct {
	seen map[cell.Hash]struct{}
}

func newTransactionUsageCollector() *transactionUsageCollector {
	return &transactionUsageCollector{seen: map[cell.Hash]struct{}{}}
}

func (c *transactionUsageCollector) addCell(root *cell.Cell, skipRoot bool) (transactionUsage, error) {
	if root == nil {
		return transactionUsage{}, nil
	}

	var sl cell.Slice
	if err := root.BeginParseIntoWithoutTrace(&sl); err != nil {
		return transactionUsage{}, err
	}

	loaded := sl.BaseCell()
	key := loaded.HashKey()
	if _, ok := c.seen[key]; ok {
		return transactionUsage{}, nil
	}
	c.seen[key] = struct{}{}

	res := transactionUsage{}
	if !skipRoot {
		res = transactionUsage{
			cells: 1,
			bits:  uint64(loaded.BitsSize()),
		}
	}

	for sl.RefsNum() > 0 {
		ref, err := sl.LoadRefCell()
		if err != nil {
			return transactionUsage{}, err
		}

		usage, err := c.addCell(ref, false)
		if err != nil {
			return transactionUsage{}, err
		}
		res = transactionAddUsage(res, usage)
	}
	return res, nil
}

func transactionAddUsage(a, b transactionUsage) transactionUsage {
	return transactionUsage{
		cells: a.cells + b.cells,
		bits:  a.bits + b.bits,
	}
}

// transactionZeroCurrencyBalance leaves extra nil rather than allocating an
// empty map: every reader of extra (range, index, len, delete) treats a nil map
// as empty, and add is the only writer — it materialises the map on demand.
// copy already returned balances with a nil extra, so nil is not a new state.
func transactionZeroCurrencyBalance() *transactionCurrencyBalance {
	return &transactionCurrencyBalance{
		grams: bigint.FromInt64(0),
	}
}

func transactionCurrencyFromCollection(cc tlb.CurrencyCollection) (*transactionCurrencyBalance, error) {
	return transactionCurrencyFromOwnedParts(cc.Coins.Nano(), cc.ExtraCurrencies)
}

func transactionCurrencyFromParts(grams *big.Int, extraDict *cell.Dictionary) (*transactionCurrencyBalance, error) {
	return transactionCurrencyFromOwnedParts(transactionBigOrZero(grams), extraDict)
}

// transactionCurrencyFromOwnedParts takes ownership of grams (must be non-nil);
// the caller must not retain or mutate it afterwards.
func transactionCurrencyFromOwnedParts(grams *big.Int, extraDict *cell.Dictionary) (*transactionCurrencyBalance, error) {
	extra, err := transactionLoadExtraCurrencies(extraDict)
	if err != nil {
		return nil, err
	}
	return &transactionCurrencyBalance{
		grams: grams,
		extra: extra,
	}, nil
}

func (c *transactionCurrencyBalance) copy() *transactionCurrencyBalance {
	if c == nil {
		return transactionZeroCurrencyBalance()
	}
	out := &transactionCurrencyBalance{
		grams: transactionBigOrZero(c.grams),
	}
	if len(c.extra) > 0 {
		out.extra = make(map[uint32]*big.Int, len(c.extra))
		for id, amount := range c.extra {
			if amount != nil {
				out.extra[id] = bigint.Set(amount)
			}
		}
	}
	return out
}

func (c *transactionCurrencyBalance) add(other *transactionCurrencyBalance) {
	if c == nil || other == nil {
		return
	}
	if other.grams != nil {
		c.grams.Add(c.grams, other.grams)
	}
	for id, amount := range other.extra {
		if amount == nil || amount.Sign() == 0 {
			continue
		}
		if c.extra == nil {
			c.extra = make(map[uint32]*big.Int, len(other.extra))
		}
		if c.extra[id] == nil {
			c.extra[id] = bigint.FromInt64(0)
		}
		c.extra[id].Add(c.extra[id], amount)
	}
	c.removeZeroExtra()
}

func (c *transactionCurrencyBalance) sub(other *transactionCurrencyBalance) bool {
	if c == nil || other == nil {
		return true
	}
	if other.grams != nil && c.grams.Cmp(other.grams) < 0 {
		return false
	}
	if !c.hasExtra(other.extra) {
		return false
	}
	if other.grams != nil {
		c.grams.Sub(c.grams, other.grams)
	}
	for id, amount := range other.extra {
		if amount == nil || amount.Sign() == 0 {
			continue
		}
		c.extra[id].Sub(c.extra[id], amount)
	}
	c.removeZeroExtra()
	return true
}

func (c *transactionCurrencyBalance) clamp(max *transactionCurrencyBalance) {
	if c == nil || max == nil {
		return
	}
	if c.grams.Cmp(max.grams) > 0 {
		c.grams.Set(max.grams)
	}
	for id, amount := range c.extra {
		maxAmount := max.extra[id]
		if maxAmount == nil || maxAmount.Sign() <= 0 {
			delete(c.extra, id)
			continue
		}
		if amount.Cmp(maxAmount) > 0 {
			amount.Set(maxAmount)
		}
	}
	c.removeZeroExtra()
}

func (c *transactionCurrencyBalance) hasExtra(need map[uint32]*big.Int) bool {
	for id, amount := range need {
		if amount == nil || amount.Sign() == 0 {
			continue
		}
		have := c.extra[id]
		if have == nil || have.Cmp(amount) < 0 {
			return false
		}
	}
	return true
}

func (c *transactionCurrencyBalance) extraEmpty() bool {
	return transactionExtraCount(c.extra) == 0
}

func (c *transactionCurrencyBalance) removeZeroExtra() {
	if c == nil {
		return
	}
	for id, amount := range c.extra {
		if amount == nil || amount.Sign() == 0 {
			delete(c.extra, id)
		}
	}
}

func (c *transactionCurrencyBalance) extraDict() (*cell.Dictionary, error) {
	if c == nil {
		return nil, nil
	}
	return transactionStoreExtraCurrencies(c.extra)
}

// transactionExtraDictAbsent reports a dictionary that carries no entries to
// read at all, as opposed to one whose entries turn out to be unreadable.
func transactionExtraDictAbsent(dict *cell.Dictionary) bool {
	return dict == nil || dict.AsCell() == nil
}

// transactionLoadExtraCurrencies returns nil for a balance that carries no
// extra currencies. Almost every currency balance in a block is grams-only, and
// an empty map is a heap object per balance for nothing: readers cannot tell a
// nil map from an empty one, and add materialises the map before writing.
func transactionLoadExtraCurrencies(dict *cell.Dictionary) (map[uint32]*big.Int, error) {
	if transactionExtraDictAbsent(dict) {
		return nil, nil
	}
	iterator, err := dict.Iterator(false, false)
	if err != nil {
		return nil, fmt.Errorf("failed to load extra currencies: %w", err)
	}

	var out map[uint32]*big.Int
	for iterator.Next() {
		item := iterator.View()
		keySlice := item.Key
		key, err := keySlice.LoadUInt(32)
		if err != nil {
			return nil, fmt.Errorf("failed to load extra currency id: %w", err)
		}
		value := item.Value
		amount, err := value.LoadVarUInt(32)
		if err != nil {
			return nil, fmt.Errorf("failed to load extra currency amount: %w", err)
		}
		if value.BitsLeft() != 0 || value.RefsNum() != 0 {
			return nil, errors.New("extra currency amount has trailing data")
		}
		if amount.Sign() > 0 {
			if out == nil {
				out = make(map[uint32]*big.Int)
			}
			out[uint32(key)] = amount
		}
	}
	if err = iterator.Err(); err != nil {
		return nil, fmt.Errorf("failed to load extra currencies: %w", err)
	}
	return out, nil
}

func transactionStoreExtraCurrencies(extra map[uint32]*big.Int) (*cell.Dictionary, error) {
	for _, amount := range extra {
		if amount != nil && amount.Sign() < 0 {
			return nil, errors.New("negative extra currency amount")
		}
	}
	if transactionExtraCount(extra) == 0 {
		return nil, nil
	}
	dict := cell.NewDict(32)
	for id, amount := range extra {
		if amount == nil || amount.Sign() == 0 {
			continue
		}
		valueBuilder := cell.BeginCell()
		if err := valueBuilder.StoreBigVarUInt(amount, 32); err != nil {
			return nil, fmt.Errorf("failed to store extra currency %d amount: %w", id, err)
		}
		value := valueBuilder.EndCell()
		if err := dict.SetIntKey(bigint.FromUint64(uint64(id)), value); err != nil {
			return nil, fmt.Errorf("failed to store extra currency: %w", err)
		}
	}
	return dict, nil
}

func transactionExtraMapsEqual(a, b map[uint32]*big.Int) bool {
	if transactionExtraCount(a) != transactionExtraCount(b) {
		return false
	}
	for id, amount := range a {
		if amount == nil || amount.Sign() == 0 {
			continue
		}
		other := b[id]
		if other == nil || other.Cmp(amount) != 0 {
			return false
		}
	}
	return true
}

func transactionExtraCount(extra map[uint32]*big.Int) uint64 {
	var count uint64
	for _, amount := range extra {
		if amount != nil && amount.Sign() > 0 {
			count++
		}
	}
	return count
}

func transactionCloneExtraCurrencies(dict *cell.Dictionary) (*cell.Dictionary, error) {
	extra, err := transactionLoadExtraCurrencies(dict)
	if err != nil {
		return nil, err
	}
	return transactionStoreExtraCurrencies(extra)
}

func transactionAddExtraCurrencies(a, b *cell.Dictionary) (*cell.Dictionary, error) {
	// The overwhelmingly common case is two absent dictionaries, whose sum the
	// long way round is two balances, two zero grams and a store that yields
	// nil anyway. The test is the one transactionLoadExtraCurrencies itself
	// uses for "nothing to read", so the shortcut cannot skip a parse that
	// would have failed — transactionExtraDictIsEmpty would, because it reports
	// a dictionary it could not parse as empty.
	if transactionExtraDictAbsent(a) && transactionExtraDictAbsent(b) {
		return nil, nil
	}
	left, err := transactionCurrencyFromParts(nil, a)
	if err != nil {
		return nil, err
	}
	right, err := transactionCurrencyFromParts(nil, b)
	if err != nil {
		return nil, err
	}
	left.add(right)
	return left.extraDict()
}

func transactionExtraDictIsEmpty(dict *cell.Dictionary) bool {
	if dict == nil || dict.IsEmpty() {
		return true
	}
	extra, err := transactionLoadExtraCurrencies(dict)
	return err != nil || transactionExtraCount(extra) == 0
}

func transactionCloneDictShallow(dict *cell.Dictionary) *cell.Dictionary {
	if dict == nil || dict.IsEmpty() {
		return nil
	}
	return dict.Copy()
}

func transactionMinBig(a, b *big.Int) *big.Int {
	if a == nil {
		return bigint.FromInt64(0)
	}
	if b == nil || a.Cmp(b) <= 0 {
		return bigint.Set(a)
	}
	return bigint.Set(b)
}

func transactionNormalizeBits256(src []byte) []byte {
	if len(src) == 32 {
		return src
	}
	out := make([]byte, 32)
	if len(src) > 32 {
		copy(out, src[len(src)-32:])
	} else {
		copy(out[32-len(src):], src)
	}
	return out
}
