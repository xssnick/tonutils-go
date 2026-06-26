package tvm

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func transactionCoinsNano(coins *tlb.Coins) *big.Int {
	if coins == nil {
		return nil
	}
	return new(big.Int).Set(coins.Nano())
}

func transactionCoinsPtr(nano *big.Int) *tlb.Coins {
	if nano == nil || nano.Sign() == 0 {
		return nil
	}
	coins := tlb.FromNanoTON(nano)
	return &coins
}

func transactionBigOrZero(v *big.Int) *big.Int {
	if v == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(v)
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

	sl, err := root.BeginParseWithoutTrace()
	if err != nil {
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

func transactionZeroCurrencyBalance() *transactionCurrencyBalance {
	return &transactionCurrencyBalance{
		grams: big.NewInt(0),
		extra: map[uint32]*big.Int{},
	}
}

func transactionCurrencyFromCollection(cc tlb.CurrencyCollection) (*transactionCurrencyBalance, error) {
	return transactionCurrencyFromParts(cc.Coins.Nano(), cc.ExtraCurrencies)
}

func transactionCurrencyFromParts(grams *big.Int, extraDict *cell.Dictionary) (*transactionCurrencyBalance, error) {
	extra, err := transactionLoadExtraCurrencies(extraDict)
	if err != nil {
		return nil, err
	}
	return &transactionCurrencyBalance{
		grams: transactionBigOrZero(grams),
		extra: extra,
	}, nil
}

func (c *transactionCurrencyBalance) copy() *transactionCurrencyBalance {
	if c == nil {
		return transactionZeroCurrencyBalance()
	}
	out := &transactionCurrencyBalance{
		grams: transactionBigOrZero(c.grams),
		extra: make(map[uint32]*big.Int, len(c.extra)),
	}
	for id, amount := range c.extra {
		if amount != nil {
			out.extra[id] = new(big.Int).Set(amount)
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
	if c.extra == nil {
		c.extra = map[uint32]*big.Int{}
	}
	for id, amount := range other.extra {
		if amount == nil || amount.Sign() == 0 {
			continue
		}
		if c.extra[id] == nil {
			c.extra[id] = big.NewInt(0)
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

func transactionLoadExtraCurrencies(dict *cell.Dictionary) (map[uint32]*big.Int, error) {
	if dict == nil || dict.IsEmpty() {
		return map[uint32]*big.Int{}, nil
	}
	items, err := dict.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to load extra currencies: %w", err)
	}

	out := make(map[uint32]*big.Int, len(items))
	for _, item := range items {
		key, err := item.Key.LoadUInt(32)
		if err != nil {
			return nil, fmt.Errorf("failed to load extra currency id: %w", err)
		}
		amount, err := item.Value.LoadVarUInt(32)
		if err != nil {
			return nil, fmt.Errorf("failed to load extra currency amount: %w", err)
		}
		if item.Value.BitsLeft() != 0 || item.Value.RefsNum() != 0 {
			return nil, errors.New("extra currency amount has trailing data")
		}
		if amount.Sign() > 0 {
			out[uint32(key)] = amount
		}
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
		value := cell.BeginCell().MustStoreBigVarUInt(amount, 32).EndCell()
		if err := dict.SetIntKey(new(big.Int).SetUint64(uint64(id)), value); err != nil {
			return nil, fmt.Errorf("failed to store extra currency: %w", err)
		}
	}
	return dict, nil
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
		return big.NewInt(0)
	}
	if b == nil || a.Cmp(b) <= 0 {
		return new(big.Int).Set(a)
	}
	return new(big.Int).Set(b)
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
