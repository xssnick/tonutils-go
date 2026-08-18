package tvm

import (
	"bytes"
	"os"
	"slices"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func preparedAccessorsMainnetRoot(t *testing.T) *cell.Cell {
	t.Helper()

	raw, err := os.ReadFile("../tlb/testdata/blockchain_config_mainnet.boc")
	if err != nil {
		t.Fatal(err)
	}
	root, err := cell.FromBOC(raw)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// TestPreparedBlockchainConfigGasPricesMatchTlb keeps the accessor an exposure of
// what preparation already decoded rather than a second decoder that may drift.
func TestPreparedBlockchainConfigGasPricesMatchTlb(t *testing.T) {
	root := preparedAccessorsMainnetRoot(t)
	prepared, err := PrepareBlockchainConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	raw := tlb.BlockchainConfig{Root: root}

	for _, masterchain := range []bool{false, true} {
		want, err := raw.GetGasPrices(masterchain)
		if err != nil {
			t.Fatalf("masterchain=%v: %v", masterchain, err)
		}
		got, ok := prepared.GasPrices(masterchain)
		if !ok {
			t.Fatalf("masterchain=%v: prepared config reports no gas prices", masterchain)
		}
		if got != *want {
			t.Fatalf("masterchain=%v: gas prices %+v, want %+v", masterchain, got, *want)
		}
	}
}

// TestPreparedBlockchainConfigLenientReportsAbsentGasPrices pins the one case ok
// is false for: the strict constructor rejects a config without param 20/21, so
// a caller that ignores ok would silently read a zero limit off a lenient one.
func TestPreparedBlockchainConfigLenientReportsAbsentGasPrices(t *testing.T) {
	params := transactionReportStrictConfigParams(t)
	delete(params, tlb.ConfigParamGasPricesMasterchain)

	prepared, err := PrepareBlockchainConfigLenient(buildTransactionConfigRoot(t, params))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := prepared.GasPrices(true); ok {
		t.Fatal("lenient config without param 20 reported masterchain gas prices")
	}
	if _, ok := prepared.GasPrices(false); !ok {
		t.Fatal("lenient config with param 21 reported no basechain gas prices")
	}
}

// TestPreparedBlockchainConfigSpecialAccountOrder pins the sequence, not just the
// set: a masterchain collator runs tick and tock in this order, so a permutation
// reassigns logical times and changes the block it produces.
func TestPreparedBlockchainConfigSpecialAccountOrder(t *testing.T) {
	fundamentals := [][32]byte{
		{0xf0, 0x01}, {0x0f, 0x02}, {0xa0, 0x03},
	}
	configAddr := [32]byte{0xcc, 0xcc}

	for _, tc := range []struct {
		name       string
		configAddr *[32]byte
		listed     bool
	}{
		{name: "config address absent from param 31", configAddr: &configAddr},
		{name: "config address listed by param 31", configAddr: &fundamentals[1], listed: true},
		{name: "no config address at all"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := transactionReportStrictConfigParams(t)
			dict := cell.NewDict(256)
			// Dictionary order is by key, which is not insertion order: storing an
			// unsorted list is what makes the expectation below a real statement.
			for _, addr := range fundamentals {
				key := cell.BeginCell().MustStoreSlice(addr[:], 256).EndCell()
				if err := dict.Set(key, cell.BeginCell().EndCell()); err != nil {
					t.Fatal(err)
				}
			}
			param, err := tlb.ToCell(&tlb.FundamentalSmartContractAddresses{Addresses: dict})
			if err != nil {
				t.Fatal(err)
			}
			params[tlb.ConfigParamFundamentalSMCAddresses] = param
			if tc.configAddr != nil {
				params[tlb.ConfigParamConfigAddress] = cell.BeginCell().
					MustStoreSlice(tc.configAddr[:], 256).EndCell()
			}

			prepared, err := PrepareBlockchainConfig(buildTransactionConfigRoot(t, params))
			if err != nil {
				t.Fatal(err)
			}

			items, err := dict.LoadAll()
			if err != nil {
				t.Fatal(err)
			}
			want := make([][32]byte, 0, len(items)+1)
			for _, item := range items {
				key, err := item.Key.LoadSlice(256)
				if err != nil {
					t.Fatal(err)
				}
				want = append(want, [32]byte(key))
			}
			if tc.configAddr != nil && !tc.listed {
				want = append(want, *tc.configAddr)
			}

			got := prepared.SpecialAccounts()
			if len(got) != len(want) {
				t.Fatalf("special accounts %x, want %x", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("special account %d = %x, want %x", i, got[i], want[i])
				}
			}

			// The order must enumerate exactly the set isSpecialAccount answers
			// for, or the executor and the collator disagree about which accounts
			// receive the special gas allowance.
			for _, addr := range got {
				if !prepared.IsSpecialAccount(addr) {
					t.Fatalf("enumerated account %x is not a member of the set", addr)
				}
				if !prepared.isSpecialAccount(address.NewAddress(0, 0xff, addr[:])) {
					t.Fatalf("enumerated account %x is not special to the executor", addr)
				}
			}
			if len(prepared.specialAccounts) != len(got) {
				t.Fatalf("set holds %d accounts, order enumerates %d",
					len(prepared.specialAccounts), len(got))
			}

			addr, ok := prepared.ConfigAddress()
			if ok != (tc.configAddr != nil) {
				t.Fatalf("config address present = %v, want %v", ok, tc.configAddr != nil)
			}
			if ok && addr != *tc.configAddr {
				t.Fatalf("config address %x, want %x", addr, *tc.configAddr)
			}
		})
	}
}

// TestPreparedBlockchainConfigSpecialAccountsMatchMainnet runs the same equality
// against the real config, where param 31 is neither empty nor synthetic.
func TestPreparedBlockchainConfigSpecialAccountsMatchMainnet(t *testing.T) {
	root := preparedAccessorsMainnetRoot(t)
	prepared, err := PrepareBlockchainConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	raw := tlb.BlockchainConfig{Root: root}

	fundamental, err := raw.GetFundamentalSmartContractAddresses()
	if err != nil {
		t.Fatal(err)
	}
	items, err := fundamental.Addresses.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	configAddr, err := raw.GetConfigAddress()
	if err != nil {
		t.Fatal(err)
	}

	want := make([][32]byte, 0, len(items)+1)
	listed := false
	for _, item := range items {
		key, err := item.Key.LoadSlice(256)
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, [32]byte(key))
		listed = listed || bytes.Equal(key, configAddr)
	}
	if !listed {
		want = append(want, [32]byte(configAddr))
	}

	got := prepared.SpecialAccounts()
	if len(got) != len(want) {
		t.Fatalf("mainnet special accounts: %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mainnet special account %d = %x, want %x", i, got[i], want[i])
		}
	}
	if addr, ok := prepared.ConfigAddress(); !ok || !bytes.Equal(addr[:], configAddr) {
		t.Fatalf("mainnet config address %x/%v, want %x", addr, ok, configAddr)
	}
	if len(want) < 2 {
		t.Fatalf("mainnet fixture lists %d special accounts, too few to prove an order", len(want))
	}
}

// TestPreparedBlockchainConfigConfigAddressRejectsShortParam covers the silent
// skip: a param 0 that is not 256 bits leaves the set and the accessor agreeing
// that there is no configuration contract, rather than half-adding one.
func TestPreparedBlockchainConfigConfigAddressRejectsShortParam(t *testing.T) {
	params := transactionReportStrictConfigParams(t)
	params[tlb.ConfigParamConfigAddress] = cell.BeginCell().MustStoreUInt(1, 128).EndCell()

	prepared, err := PrepareBlockchainConfig(buildTransactionConfigRoot(t, params))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := prepared.ConfigAddress(); ok {
		t.Fatal("a 128-bit param 0 was reported as a configuration contract address")
	}
	if len(prepared.SpecialAccounts()) != 0 || len(prepared.specialAccounts) != 0 {
		t.Fatalf("a malformed param 0 produced special accounts: %x", prepared.SpecialAccounts())
	}
}

// TestPreparedBlockchainConfigSpecialAccountOrderMatchesReference pins the
// sequence against a written-out expectation rather than against a second
// rendering of the algorithm that produced it.
//
// The two tests above build their `want` by walking the same dictionary in the
// same direction and appending the configuration contract under the same
// condition, which is prepareSpecialAccounts restated: they catch the order
// being dropped, never the order being wrong. The literal here is read off the
// C++ node instead:
//
//   - Config::get_special_smartcontracts (crypto/block/mc-config.cpp) appends
//     parameter-31 keys in ascending dictionary order via check_for_each, then
//     pushes config_addr at the end, and only when the walk did not see it.
//   - Collator::create_ticktock_transactions (validator/impl/collator.cpp)
//     iterates that vector as-is, so its order is the order in which tick and
//     tock transactions enter a masterchain block.
//
// The chosen addresses make both halves visible: inserted as f0…, 0f…, a0…, the
// keys come back ascending, and cc… — which sorts between a0… and f0… — is last
// because it is appended. A sorted, reversed or rotated implementation fails
// here without any dictionary being walked twice.
func TestPreparedBlockchainConfigSpecialAccountOrderMatchesReference(t *testing.T) {
	fundamentals := [][32]byte{{0xf0, 0x01}, {0x0f, 0x02}, {0xa0, 0x03}}
	configAddr := [32]byte{0xcc, 0xcc}
	want := [][32]byte{{0x0f, 0x02}, {0xa0, 0x03}, {0xf0, 0x01}, {0xcc, 0xcc}}

	params := transactionReportStrictConfigParams(t)
	dict := cell.NewDict(256)
	for _, addr := range fundamentals {
		key := cell.BeginCell().MustStoreSlice(addr[:], 256).EndCell()
		if err := dict.Set(key, cell.BeginCell().EndCell()); err != nil {
			t.Fatal(err)
		}
	}
	param, err := tlb.ToCell(&tlb.FundamentalSmartContractAddresses{Addresses: dict})
	if err != nil {
		t.Fatal(err)
	}
	params[tlb.ConfigParamFundamentalSMCAddresses] = param
	params[tlb.ConfigParamConfigAddress] = cell.BeginCell().
		MustStoreSlice(configAddr[:], 256).EndCell()

	prepared, err := PrepareBlockchainConfig(buildTransactionConfigRoot(t, params))
	if err != nil {
		t.Fatal(err)
	}

	got := prepared.SpecialAccounts()
	if len(got) != len(want) {
		t.Fatalf("special accounts %x, want %x", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("special account %d = %x, want %x", i, got[i], want[i])
		}
	}

	// Stated separately because it is the property, not an accident of these
	// addresses: the appended entry is last, not in its sorted place.
	sorted := slices.Clone(got)
	slices.SortFunc(sorted, func(left, right [32]byte) int { return bytes.Compare(left[:], right[:]) })
	if slices.Equal(got, sorted) {
		t.Fatalf("special accounts %x are in ascending order, which cannot distinguish "+
			"the executed order from a sorted one", got)
	}
	if got[len(got)-1] != configAddr {
		t.Fatalf("special accounts end with %x, want the configuration contract %x",
			got[len(got)-1], configAddr)
	}
}
