package tlb

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

const (
	valueFlowV1Tag uint64 = 0xb8e48dfb
	valueFlowV2Tag uint64 = 0x3ebf98b7
)

// ValueFlow describes all currency entering and leaving a block.
type ValueFlow struct {
	FromPrevBlock CurrencyCollection
	ToNextBlock   CurrencyCollection
	Imported      CurrencyCollection
	Exported      CurrencyCollection
	FeesCollected CurrencyCollection
	FeesImported  CurrencyCollection
	Recovered     CurrencyCollection
	Created       CurrencyCollection
	Minted        CurrencyCollection
	Burned        CurrencyCollection
}

func (v *ValueFlow) LoadFromCell(loader *cell.Slice) error {
	tag, err := loader.LoadUInt(32)
	if err != nil {
		return fmt.Errorf("failed to load value flow tag: %w", err)
	}
	if tag != valueFlowV1Tag && tag != valueFlowV2Tag {
		return fmt.Errorf("unknown value flow tag %08x", tag)
	}

	first, err := loader.LoadRef()
	if err != nil {
		return fmt.Errorf("failed to load value flow first group: %w", err)
	}

	var flow ValueFlow
	if flow.FromPrevBlock, err = loadValueFlowCurrency(first, "from previous block"); err != nil {
		return err
	}
	if flow.ToNextBlock, err = loadValueFlowCurrency(first, "to next block"); err != nil {
		return err
	}
	if flow.Imported, err = loadValueFlowCurrency(first, "imported"); err != nil {
		return err
	}
	if flow.Exported, err = loadValueFlowCurrency(first, "exported"); err != nil {
		return err
	}
	if err = requireEmptyValueFlowSlice(first, "first group"); err != nil {
		return err
	}

	if flow.FeesCollected, err = loadValueFlowCurrency(loader, "fees collected"); err != nil {
		return err
	}
	if tag == valueFlowV2Tag {
		if flow.Burned, err = loadValueFlowCurrency(loader, "burned"); err != nil {
			return err
		}
	}

	second, err := loader.LoadRef()
	if err != nil {
		return fmt.Errorf("failed to load value flow second group: %w", err)
	}
	if flow.FeesImported, err = loadValueFlowCurrency(second, "fees imported"); err != nil {
		return err
	}
	if flow.Recovered, err = loadValueFlowCurrency(second, "recovered"); err != nil {
		return err
	}
	if flow.Created, err = loadValueFlowCurrency(second, "created"); err != nil {
		return err
	}
	if flow.Minted, err = loadValueFlowCurrency(second, "minted"); err != nil {
		return err
	}
	if err = requireEmptyValueFlowSlice(second, "second group"); err != nil {
		return err
	}
	if err = requireEmptyValueFlowSlice(loader, "root"); err != nil {
		return err
	}

	*v = flow
	return nil
}

// ToCell serializes the flow without requiring a balanced equation, matching
// C++ ValueFlow::store; run Validate separately to mirror block validation.
func (v ValueFlow) ToCell() (*cell.Cell, error) {
	first := cell.BeginCell()
	if err := storeCurrencyCollection(first, v.FromPrevBlock); err != nil {
		return nil, fmt.Errorf("failed to store from previous block: %w", err)
	}
	if err := storeCurrencyCollection(first, v.ToNextBlock); err != nil {
		return nil, fmt.Errorf("failed to store to next block: %w", err)
	}
	if err := storeCurrencyCollection(first, v.Imported); err != nil {
		return nil, fmt.Errorf("failed to store imported: %w", err)
	}
	if err := storeCurrencyCollection(first, v.Exported); err != nil {
		return nil, fmt.Errorf("failed to store exported: %w", err)
	}

	second := cell.BeginCell()
	if err := storeCurrencyCollection(second, v.FeesImported); err != nil {
		return nil, fmt.Errorf("failed to store fees imported: %w", err)
	}
	if err := storeCurrencyCollection(second, v.Recovered); err != nil {
		return nil, fmt.Errorf("failed to store recovered: %w", err)
	}
	if err := storeCurrencyCollection(second, v.Created); err != nil {
		return nil, fmt.Errorf("failed to store created: %w", err)
	}
	if err := storeCurrencyCollection(second, v.Minted); err != nil {
		return nil, fmt.Errorf("failed to store minted: %w", err)
	}

	builder := cell.BeginCell()
	tag := valueFlowV1Tag
	if !valueFlowCurrencyIsZero(v.Burned) {
		tag = valueFlowV2Tag
	}
	builder.MustStoreUInt(tag, 32)
	if err := builder.StoreRef(first.EndCell()); err != nil {
		return nil, fmt.Errorf("failed to store value flow first group: %w", err)
	}
	if err := storeCurrencyCollection(builder, v.FeesCollected); err != nil {
		return nil, fmt.Errorf("failed to store fees collected: %w", err)
	}
	if tag == valueFlowV2Tag {
		if err := storeCurrencyCollection(builder, v.Burned); err != nil {
			return nil, fmt.Errorf("failed to store burned: %w", err)
		}
	}
	if err := builder.StoreRef(second.EndCell()); err != nil {
		return nil, fmt.Errorf("failed to store value flow second group: %w", err)
	}
	return builder.EndCell(), nil
}

// Validate checks per-collection canonicality and the balance equation of
// C++ ValueFlow::validate, which the reference runs only during block
// validation.
func (v ValueFlow) Validate() error {
	if err := v.validateCollections(); err != nil {
		return err
	}

	incoming := new(big.Int).Add(v.FromPrevBlock.Coins.nanoValue(), v.Imported.Coins.nanoValue())
	incoming.Add(incoming, v.FeesImported.Coins.nanoValue())
	incoming.Add(incoming, v.Created.Coins.nanoValue())
	incoming.Add(incoming, v.Minted.Coins.nanoValue())
	incoming.Add(incoming, v.Recovered.Coins.nanoValue())

	outgoing := new(big.Int).Add(v.ToNextBlock.Coins.nanoValue(), v.Exported.Coins.nanoValue())
	outgoing.Add(outgoing, v.FeesCollected.Coins.nanoValue())
	outgoing.Add(outgoing, v.Burned.Coins.nanoValue())

	if incoming.Cmp(outgoing) != 0 {
		return fmt.Errorf("value flow is unbalanced")
	}
	return v.validateExtraCurrencyBalance()
}

func (v ValueFlow) validateExtraCurrencyBalance() error {
	incoming := []*cell.Dictionary{
		v.FromPrevBlock.ExtraCurrencies,
		v.Imported.ExtraCurrencies,
		v.FeesImported.ExtraCurrencies,
		v.Created.ExtraCurrencies,
		v.Minted.ExtraCurrencies,
		v.Recovered.ExtraCurrencies,
	}
	outgoing := []*cell.Dictionary{
		v.ToNextBlock.ExtraCurrencies,
		v.Exported.ExtraCurrencies,
		v.FeesCollected.ExtraCurrencies,
		v.Burned.ExtraCurrencies,
	}

	empty := true
	for _, side := range [2][]*cell.Dictionary{incoming, outgoing} {
		for _, d := range side {
			if d != nil && !d.IsEmpty() {
				empty = false
				break
			}
		}
	}
	if empty {
		return nil
	}

	sum := func(dicts []*cell.Dictionary) (*cell.Dictionary, error) {
		total := dicts[0]
		var err error
		for _, d := range dicts[1:] {
			if total, err = addExtraCurrencyDicts(total, d); err != nil {
				return nil, err
			}
		}
		return total, nil
	}

	in, err := sum(incoming)
	if err != nil {
		return fmt.Errorf("failed to add incoming extra currencies: %w", err)
	}
	out, err := sum(outgoing)
	if err != nil {
		return fmt.Errorf("failed to add outgoing extra currencies: %w", err)
	}

	left, right := extraDictRoot(in), extraDictRoot(out)
	if (left == nil) != (right == nil) || (left != nil && left.HashKey(0) != right.HashKey(0)) {
		return fmt.Errorf("value flow is unbalanced")
	}
	return nil
}

func (v ValueFlow) validateCollections() error {
	if err := validateValueFlowCurrency("from previous block", v.FromPrevBlock); err != nil {
		return err
	}
	if err := validateValueFlowCurrency("to next block", v.ToNextBlock); err != nil {
		return err
	}
	if err := validateValueFlowCurrency("imported", v.Imported); err != nil {
		return err
	}
	if err := validateValueFlowCurrency("exported", v.Exported); err != nil {
		return err
	}
	if err := validateValueFlowCurrency("fees collected", v.FeesCollected); err != nil {
		return err
	}
	if err := validateValueFlowCurrency("fees imported", v.FeesImported); err != nil {
		return err
	}
	if err := validateValueFlowCurrency("recovered", v.Recovered); err != nil {
		return err
	}
	if err := validateValueFlowCurrency("created", v.Created); err != nil {
		return err
	}
	if err := validateValueFlowCurrency("minted", v.Minted); err != nil {
		return err
	}
	return validateValueFlowCurrency("burned", v.Burned)
}

func loadValueFlowCurrency(loader *cell.Slice, name string) (CurrencyCollection, error) {
	grams, err := loadRawGrams(loader)
	if err != nil {
		return CurrencyCollection{}, fmt.Errorf("failed to load value flow %s coins: %w", name, err)
	}
	coins := Coins{decimals: 9, val: zeroCoinsInt}
	if grams.ln != 0 {
		amount, err := grams.integer()
		if err != nil {
			return CurrencyCollection{}, fmt.Errorf("value flow %s has non-canonical coins: %w", name, err)
		}
		coins = Coins{decimals: 9, val: amount}
	}
	extra, err := loader.LoadDict(32)
	if err != nil {
		return CurrencyCollection{}, fmt.Errorf("failed to load value flow %s extra currencies: %w", name, err)
	}

	// The coins are already canonical and within VarUInteger 16 range here, so
	// only the extra currencies need validation.
	if err = validateValueFlowExtraCurrencies(name, extra); err != nil {
		return CurrencyCollection{}, err
	}
	return CurrencyCollection{
		Coins:           coins,
		ExtraCurrencies: extra,
	}, nil
}

func validateValueFlowCurrency(name string, value CurrencyCollection) error {
	nano := value.Coins.nanoValue()
	if nano.Sign() < 0 {
		return fmt.Errorf("value flow %s is negative", name)
	}
	if tooBigForVarUint16(nano) {
		return fmt.Errorf("value flow %s coins are invalid: %w", name, errTooBigForVarUint16)
	}
	return validateValueFlowExtraCurrencies(name, value.ExtraCurrencies)
}

func validateValueFlowExtraCurrencies(name string, extra *cell.Dictionary) error {
	if extra == nil || extra.IsEmpty() {
		return nil
	}
	if extra.GetKeySize() != 32 {
		return fmt.Errorf("value flow %s extra currency dictionary has key size %d", name, extra.GetKeySize())
	}

	items, err := extra.LoadAll()
	if err != nil {
		return fmt.Errorf("failed to load value flow %s extra currencies: %w", name, err)
	}
	for _, item := range items {
		if _, err = loadExtraCurrencyAmountSlice(item.Value); err != nil {
			return fmt.Errorf("value flow %s extra currency: %w", name, err)
		}
		if item.Value.BitsLeft() != 0 || item.Value.RefsNum() != 0 {
			return fmt.Errorf("value flow %s extra currency amount has trailing data", name)
		}
	}
	return nil
}

func valueFlowCurrencyIsZero(value CurrencyCollection) bool {
	return value.Coins.nanoValue().Sign() == 0 && (value.ExtraCurrencies == nil || value.ExtraCurrencies.IsEmpty())
}

func requireEmptyValueFlowSlice(loader *cell.Slice, name string) error {
	if loader.BitsLeft() != 0 || loader.RefsNum() != 0 {
		return fmt.Errorf("value flow %s has %d trailing bits and %d trailing refs", name, loader.BitsLeft(), loader.RefsNum())
	}
	return nil
}
