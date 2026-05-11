package cell

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
)

func TestSliceLoadAddrVariantsAndPreloadBigInt(t *testing.T) {
	t.Run("AddressRoundTrips", func(t *testing.T) {
		tests := []struct {
			name string
			addr *address.Address
		}{
			{name: "None", addr: nil},
			{name: "Ext", addr: address.NewAddressExt(0, 20, []byte{0xAB, 0xC0, 0x00})},
			{name: "Std", addr: address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N")},
			{name: "Var", addr: address.NewAddressVar(0, -3, 20, []byte{0xDE, 0xA0, 0x00})},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				sl := BeginCell().MustStoreAddr(tc.addr).EndCell().BeginParse()
				got, err := sl.LoadAddr()
				if err != nil {
					t.Fatal(err)
				}

				want := tc.addr
				if want == nil {
					want = address.NewAddressNone()
				}

				if got.Type() != want.Type() || got.Workchain() != want.Workchain() || got.BitsLen() != want.BitsLen() || !bytes.Equal(got.Data(), want.Data()) {
					t.Fatalf("unexpected address roundtrip: got=%v want=%v", got, want)
				}

				sl = BeginCell().MustStoreAddr(tc.addr).EndCell().BeginParse()
				got = sl.MustLoadAddr()
				if got.Type() != want.Type() {
					t.Fatalf("must-loader returned wrong type: got=%v want=%v", got.Type(), want.Type())
				}
			})
		}
	})

	t.Run("AnycastForms", func(t *testing.T) {
		stdAnycast := BeginCell().
			MustStoreUInt(0b10, 2).
			MustStoreBoolBit(true).
			MustStoreUInt(3, 5).
			MustStoreSlice([]byte{0b10100000}, 3).
			MustStoreUInt(0xFF, 8).
			MustStoreSlice(bytes.Repeat([]byte{0x11}, 32), 256).
			EndCell().
			BeginParse()

		stdAddr, err := stdAnycast.LoadAddr()
		if err != nil {
			t.Fatal(err)
		}
		if stdAddr.Type() != address.StdAddress || stdAddr.Workchain() != -1 {
			t.Fatalf("unexpected std anycast parse result: type=%v wc=%d", stdAddr.Type(), stdAddr.Workchain())
		}

		varAnycast := BeginCell().
			MustStoreUInt(0b11, 2).
			MustStoreBoolBit(true).
			MustStoreUInt(4, 5).
			MustStoreSlice([]byte{0b11110000}, 4).
			MustStoreUInt(20, 9).
			MustStoreInt(-7, 32).
			MustStoreSlice([]byte{0xAB, 0xC0, 0x00}, 20).
			EndCell().
			BeginParse()

		varAddr, err := varAnycast.LoadAddr()
		if err != nil {
			t.Fatal(err)
		}
		if varAddr.Type() != address.VarAddress || varAddr.Workchain() != -7 || varAddr.BitsLen() != 20 {
			t.Fatalf("unexpected var anycast parse result: type=%v wc=%d bits=%d", varAddr.Type(), varAddr.Workchain(), varAddr.BitsLen())
		}
	})

	t.Run("PreloadBigIntAndSnakeErrors", func(t *testing.T) {
		val := new(big.Int).Neg(big.NewInt(12345))
		sl := BeginCell().MustStoreBigInt(new(big.Int).Set(val), 32).EndCell().BeginParse()
		before := sl.BitsLeft()
		preloaded, err := sl.PreloadBigInt(32)
		if err != nil {
			t.Fatal(err)
		}
		if preloaded.Cmp(val) != 0 {
			t.Fatalf("unexpected preloaded value: got=%s want=%s", preloaded.String(), val.String())
		}
		if sl.BitsLeft() != before {
			t.Fatalf("preload should not advance slice: before=%d after=%d", before, sl.BitsLeft())
		}

		badSnake := BeginCell().
			MustStoreSlice([]byte("a"), 8).
			MustStoreRef(BeginCell().MustStoreSlice([]byte("b"), 8).EndCell()).
			MustStoreRef(BeginCell().MustStoreSlice([]byte("c"), 8).EndCell()).
			EndCell().
			BeginParse()
		if _, err := badSnake.LoadBinarySnake(); err == nil {
			t.Fatal("snake with multiple refs should fail")
		}
	})
}
