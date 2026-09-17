package tvm

import (
	"errors"
	"math"
	"math/big"
	"math/bits"

	"github.com/xssnick/tonutils-go/tlb"
)

// historicalStoragePayment keeps the positive, unnormalized base-2^52 limbs
// used by TON's old add_partial_storage_payment. Addition preserves limb count;
// multiplication can leave a leading zero. The old rounded shift also kept that
// zero, causing sgn() to discard a nonzero fee. A normalized sum loses this state.
type historicalStoragePayment struct {
	digits [5]uint64
	size   int
}

var errHistoricalStorageOverflow = errors.New("historical storage payment exceeds signed reference arithmetic")

func (p *historicalStoragePayment) add(other historicalStoragePayment) error {
	for i := range other.size {
		if other.digits[i] > math.MaxInt64-p.digits[i] {
			return errHistoricalStorageOverflow
		}
		p.digits[i] += other.digits[i]
	}
	p.size = max(p.size, other.size)
	return nil
}

func (p *historicalStoragePayment) multiply(factor uint64) error {
	if factor > math.MaxInt64 {
		return errHistoricalStorageOverflow
	}

	const digitMask = 1<<52 - 1
	var carry uint64
	for i := range p.size {
		if p.digits[i] > math.MaxInt64 {
			return errHistoricalStorageOverflow
		}
		hi, lo := bits.Mul64(p.digits[i], factor)
		low := lo & digitMask
		// set_mul stores the product's high part in a signed 64-bit limb.
		// The old operation then adds the previous carry without normalizing.
		if hi > math.MaxInt64>>12 || carry > math.MaxInt64-low {
			return errHistoricalStorageOverflow
		}
		p.digits[i] = low + carry
		carry = hi<<12 | lo>>52
	}
	if carry == 0 {
		return nil
	}
	if p.size == len(p.digits) {
		return errHistoricalStorageOverflow
	}
	p.digits[p.size] = carry
	p.size++
	return nil
}

func (p *historicalStoragePayment) addWindow(price tlb.ConfigStoragePrices, masterchain bool, delta, cellBits, cells uint64) error {
	bitPrice, cellPrice := transactionStoragePricesFor(price, masterchain)
	cellPayment := historicalStoragePayment{digits: [5]uint64{cells}, size: 1}
	bitPayment := historicalStoragePayment{digits: [5]uint64{cellBits}, size: 1}
	if err := cellPayment.multiply(cellPrice); err != nil {
		return err
	}
	if err := bitPayment.multiply(bitPrice); err != nil {
		return err
	}
	if err := bitPayment.add(cellPayment); err != nil {
		return err
	}
	if err := bitPayment.multiply(delta); err != nil {
		return err
	}
	return p.add(bitPayment)
}

func (p *historicalStoragePayment) fee() *big.Int {
	// rshift(16, 1) shifts the top limb without carrying rounding into it or
	// removing a zero. Storage collection checks that malformed sign first.
	if p.size > 1 && p.digits[p.size-1]>>16 == 0 {
		return new(big.Int)
	}

	var total, digit big.Int
	for i := p.size - 1; i >= 0; i-- {
		total.Lsh(&total, 52)
		total.Add(&total, digit.SetUint64(p.digits[i]))
	}
	return transactionCeilShiftRight(&total, 16)
}
