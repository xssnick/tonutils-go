package funcs

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
)

func TestSendMsgFeeUint64WrapParity(t *testing.T) {
	forwardCases := []struct {
		name        string
		prices      tlb.ConfigMsgForwardPrices
		cells, bits uint64
	}{
		{name: "zero"},
		{
			name:   "lump addition wraps",
			prices: tlb.ConfigMsgForwardPrices{LumpPrice: ^uint64(0), CellPrice: 1 << 16},
			cells:  1,
		},
		{
			name: "both products fill uint128",
			prices: tlb.ConfigMsgForwardPrices{
				LumpPrice: ^uint64(0) - 17,
				BitPrice:  ^uint64(0),
				CellPrice: ^uint64(0) - 1,
			},
			cells: ^uint64(0) - 2,
			bits:  ^uint64(0),
		},
		{
			name: "low-word carry",
			prices: tlb.ConfigMsgForwardPrices{
				LumpPrice: 0xF000000000000001,
				BitPrice:  0xFFFFFFFF00000001,
				CellPrice: 0x80000000FFFFFFFF,
			},
			cells: 0xFFFFFFFFFFFFFFFD,
			bits:  0x8000000000000003,
		},
	}
	for _, tc := range forwardCases {
		t.Run("forward/"+tc.name, func(t *testing.T) {
			want := sendMsgForwardFeeBigReference(&tc.prices, tc.cells, tc.bits)
			if got := sendMsgForwardFeeShort(&tc.prices, tc.cells, tc.bits); got != want {
				t.Fatalf("forward fee = %d, want %d", got, want)
			}
		})
	}

	ihrCases := []struct {
		name   string
		fwdFee uint64
		factor uint32
	}{
		{name: "zero"},
		{name: "maximum", fwdFee: ^uint64(0), factor: ^uint32(0)},
		{name: "high-word shift", fwdFee: 0xF123456789ABCDEF, factor: 0xFEDCBA98},
	}
	for _, tc := range ihrCases {
		t.Run("ihr/"+tc.name, func(t *testing.T) {
			want := new(big.Int).Mul(new(big.Int).SetUint64(tc.fwdFee), new(big.Int).SetUint64(uint64(tc.factor)))
			want.Rsh(want, 16)
			if got := sendMsgIHRFeeShort(tc.fwdFee, tc.factor); got != want.Uint64() {
				t.Fatalf("IHR fee = %d, want %d", got, want.Uint64())
			}
		})
	}
}

func sendMsgForwardFeeBigReference(prices *tlb.ConfigMsgForwardPrices, cells, bits uint64) uint64 {
	total := new(big.Int).Mul(new(big.Int).SetUint64(prices.BitPrice), new(big.Int).SetUint64(bits))
	total.Add(total, new(big.Int).Mul(new(big.Int).SetUint64(prices.CellPrice), new(big.Int).SetUint64(cells)))
	total.Add(total, big.NewInt(0xFFFF))
	total.Mod(total, new(big.Int).Lsh(big.NewInt(1), 128))
	total.Rsh(total, 16)
	return prices.LumpPrice + total.Uint64()
}
