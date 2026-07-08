package tlb

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// Hand-computed leaf vectors for the InMsg / OutMsg variants that do not occur
// in the mainnet block fixture. Expected extras follow InMsg::get_import_fees
// (ton/crypto/block/block-parse.cpp:1741-1835) and OutMsg::get_export_value
// (ton/crypto/block/block-parse.cpp:1918-1955).

type synthMsgParams struct {
	value      int64
	valueExtra *cell.Dictionary
	ihrFee     int64
	fwdFee     int64
}

func synthMsgFull(t *testing.T, p synthMsgParams) *cell.Cell {
	t.Helper()
	src := address.NewAddress(0, 0, bytes.Repeat([]byte{0x51}, 32))
	dst := address.NewAddress(0, 0, bytes.Repeat([]byte{0x62}, 32))
	return cell.BeginCell().
		MustStoreUInt(0b0100, 4). // int_msg_info$0 ihr_disabled
		MustStoreAddr(src).
		MustStoreAddr(dst).
		MustStoreBigCoins(big.NewInt(p.value)).
		MustStoreDict(p.valueExtra).
		MustStoreBigCoins(big.NewInt(p.ihrFee)).
		MustStoreBigCoins(big.NewInt(p.fwdFee)).
		MustStoreUInt(4242, 64).       // created_lt
		MustStoreUInt(1700000002, 32). // created_at
		MustStoreBoolBit(false).       // init:nothing
		MustStoreBoolBit(false).       // body inline empty
		EndCell()
}

func synthEnvelope(t *testing.T, msg *cell.Cell, fwdFeeRemaining int64) *cell.Cell {
	t.Helper()
	env := MsgEnvelope{
		FwdFeeRemaining: FromNanoTON(big.NewInt(fwdFeeRemaining)),
		Msg:             msg,
	}
	c, err := env.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func ccCell(t *testing.T, grams int64, extra *cell.Dictionary) *cell.Builder {
	t.Helper()
	return cell.BeginCell().MustStoreBigCoins(big.NewInt(grams)).MustStoreDict(extra)
}

func TestAugInMsgDescrLeafVectors(t *testing.T) {
	dummyTx := cell.BeginCell().MustStoreUInt(1, 8).EndCell()
	dummyProof := cell.BeginCell().MustStoreUInt(2, 8).EndCell()

	extra := mustExtraDict(t, map[uint32]int64{5: 500})
	msg := synthMsgFull(t, synthMsgParams{value: 1_000_000, valueExtra: extra, ihrFee: 30, fwdFee: 20})
	env := synthEnvelope(t, msg, 11)

	cases := []struct {
		name  string
		value *cell.Cell
		want  *cell.Cell
	}{
		{
			// msg_import_ext$000: no value, no fees (block-parse.cpp:1743-1744)
			name: "import_ext",
			value: cell.BeginCell().MustStoreUInt(0b000, 3).
				MustStoreRef(msg).MustStoreRef(dummyTx).EndCell(),
			want: cell.BeginCell().MustStoreUInt(0, 9).EndCell(),
		},
		{
			// msg_import_ihr$010: fees := ihr_fee, imported := ihr_fee + value
			// (block-parse.cpp:1745-1759)
			name: "import_ihr",
			value: cell.BeginCell().MustStoreUInt(0b010, 3).
				MustStoreRef(msg).MustStoreRef(dummyTx).
				MustStoreBigCoins(big.NewInt(30)). // must equal msg ihr_fee
				MustStoreRef(dummyProof).EndCell(),
			want: cell.BeginCell().MustStoreBigCoins(big.NewInt(30)).
				MustStoreBuilder(ccCell(t, 1_000_030, extra)).EndCell(),
		},
		{
			// msg_import_imm$011: fees := fwd_fee, imported := 0
			// (block-parse.cpp:1760-1766)
			name: "import_imm",
			value: cell.BeginCell().MustStoreUInt(0b011, 3).
				MustStoreRef(env).MustStoreRef(dummyTx).
				MustStoreBigCoins(big.NewInt(77)).EndCell(),
			want: cell.BeginCell().MustStoreBigCoins(big.NewInt(77)).
				MustStoreUInt(0, 5).EndCell(),
		},
		{
			// msg_import_fin$100: fees := fwd_fee_remaining, imported :=
			// value + ihr_fee + fwd_fee_remaining (block-parse.cpp:1767-1786)
			name: "import_fin",
			value: cell.BeginCell().MustStoreUInt(0b100, 3).
				MustStoreRef(env).MustStoreRef(dummyTx).
				MustStoreBigCoins(big.NewInt(11)). // must equal fwd_fee_remaining
				EndCell(),
			want: cell.BeginCell().MustStoreBigCoins(big.NewInt(11)).
				MustStoreBuilder(ccCell(t, 1_000_041, extra)).EndCell(),
		},
		{
			// msg_import_tr$101: fees := transit_fee, imported :=
			// value + ihr_fee + fwd_fee_remaining (block-parse.cpp:1787-1807)
			name: "import_tr",
			value: cell.BeginCell().MustStoreUInt(0b101, 3).
				MustStoreRef(env).MustStoreRef(env).
				MustStoreBigCoins(big.NewInt(4)). // transit fee <= fwd_fee_remaining
				EndCell(),
			want: cell.BeginCell().MustStoreBigCoins(big.NewInt(4)).
				MustStoreBuilder(ccCell(t, 1_000_041, extra)).EndCell(),
		},
		{
			// msg_discard_fin$110: fees := fwd_fee, imported := fwd_fee
			// (block-parse.cpp:1808-1816)
			name: "discard_fin",
			value: cell.BeginCell().MustStoreUInt(0b110, 3).
				MustStoreRef(env).MustStoreUInt(123, 64).
				MustStoreBigCoins(big.NewInt(9)).EndCell(),
			want: cell.BeginCell().MustStoreBigCoins(big.NewInt(9)).
				MustStoreBigCoins(big.NewInt(9)).MustStoreBoolBit(false).EndCell(),
		},
		{
			// msg_discard_tr$111: same as discard_fin plus proof ref
			// (block-parse.cpp:1817-1827)
			name: "discard_tr",
			value: cell.BeginCell().MustStoreUInt(0b111, 3).
				MustStoreRef(env).MustStoreUInt(123, 64).
				MustStoreBigCoins(big.NewInt(9)).MustStoreRef(dummyProof).EndCell(),
			want: cell.BeginCell().MustStoreBigCoins(big.NewInt(9)).
				MustStoreBigCoins(big.NewInt(9)).MustStoreBoolBit(false).EndCell(),
		},
		{
			// msg_import_deferred_fin$00100: like msg_import_fin (upstream rule,
			// verified against 200 mainnet leaves in the rebuild test)
			name: "import_deferred_fin",
			value: cell.BeginCell().MustStoreUInt(0b00100, 5).
				MustStoreRef(env).MustStoreRef(dummyTx).
				MustStoreBigCoins(big.NewInt(11)).EndCell(),
			want: cell.BeginCell().MustStoreBigCoins(big.NewInt(11)).
				MustStoreBuilder(ccCell(t, 1_000_041, extra)).EndCell(),
		},
		{
			// msg_import_deferred_tr$00101: fees := 0, imported :=
			// value + ihr_fee + fwd_fee_remaining (upstream rule; NOT covered by
			// the mainnet fixture, pinned here)
			name: "import_deferred_tr",
			value: cell.BeginCell().MustStoreUInt(0b00101, 5).
				MustStoreRef(env).MustStoreRef(env).EndCell(),
			want: cell.BeginCell().MustStoreUInt(0, 4).
				MustStoreBuilder(ccCell(t, 1_000_041, extra)).EndCell(),
		},
	}

	for _, tc := range cases {
		got, err := AugInMsgDescr{}.LeafExtra(tc.value.MustBeginParse())
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		mustCellHashEqual(t, "InMsg leaf vector "+tc.name, got, tc.want)

		// the derived extra must parse as ImportFees and round-trip
		var fees ImportFees
		if err = LoadFromCell(&fees, got.MustBeginParse()); err != nil {
			t.Fatalf("%s: extra does not parse as ImportFees: %v", tc.name, err)
		}
	}

	// consistency checks that C++ enforces must fail loudly
	badFin := cell.BeginCell().MustStoreUInt(0b100, 3).
		MustStoreRef(env).MustStoreRef(dummyTx).
		MustStoreBigCoins(big.NewInt(12)). // != fwd_fee_remaining 11
		EndCell()
	if _, err := (AugInMsgDescr{}).LeafExtra(badFin.MustBeginParse()); err == nil {
		t.Fatal("import_fin with mismatched fwd fee must be rejected")
	}

	badIhr := cell.BeginCell().MustStoreUInt(0b010, 3).
		MustStoreRef(msg).MustStoreRef(dummyTx).
		MustStoreBigCoins(big.NewInt(31)). // != msg ihr_fee 30
		MustStoreRef(dummyProof).EndCell()
	if _, err := (AugInMsgDescr{}).LeafExtra(badIhr.MustBeginParse()); err == nil {
		t.Fatal("import_ihr with mismatched ihr fee must be rejected")
	}

	badTr := cell.BeginCell().MustStoreUInt(0b101, 3).
		MustStoreRef(env).MustStoreRef(env).
		MustStoreBigCoins(big.NewInt(12)). // transit fee > fwd_fee_remaining 11
		EndCell()
	if _, err := (AugInMsgDescr{}).LeafExtra(badTr.MustBeginParse()); err == nil {
		t.Fatal("import_tr with transit fee above remaining fee must be rejected")
	}
}

func TestAugOutMsgDescrLeafVectors(t *testing.T) {
	dummyTx := cell.BeginCell().MustStoreUInt(1, 8).EndCell()

	extra := mustExtraDict(t, map[uint32]int64{8: 80})
	msg := synthMsgFull(t, synthMsgParams{value: 2_000_000, valueExtra: extra, ihrFee: 5, fwdFee: 3})
	env := synthEnvelope(t, msg, 17)

	zero := cell.BeginCell().MustStoreUInt(0, 5).EndCell()
	exportedValue := cell.BeginCell().MustStoreBuilder(ccCell(t, 2_000_022, extra)).EndCell()

	cases := []struct {
		name  string
		value *cell.Cell
		want  *cell.Cell
	}{
		{
			// msg_export_ext$000: no exported value (block-parse.cpp:1920-1924)
			name: "export_ext",
			value: cell.BeginCell().MustStoreUInt(0b000, 3).
				MustStoreRef(msg).MustStoreRef(dummyTx).EndCell(),
			want: zero,
		},
		{
			// msg_export_imm$010 (block-parse.cpp:1925-1926)
			name: "export_imm",
			value: cell.BeginCell().MustStoreUInt(0b010, 3).
				MustStoreRef(env).MustStoreRef(dummyTx).MustStoreRef(dummyTx).EndCell(),
			want: zero,
		},
		{
			// msg_export_new$001: value + ihr + fwd_fee_remaining
			// (block-parse.cpp:1933-1950)
			name: "export_new",
			value: cell.BeginCell().MustStoreUInt(0b001, 3).
				MustStoreRef(env).MustStoreRef(dummyTx).EndCell(),
			want: exportedValue,
		},
		{
			// msg_export_tr$011
			name: "export_tr",
			value: cell.BeginCell().MustStoreUInt(0b011, 3).
				MustStoreRef(env).MustStoreRef(dummyTx).EndCell(),
			want: exportedValue,
		},
		{
			// msg_export_tr_req$111
			name: "export_tr_req",
			value: cell.BeginCell().MustStoreUInt(0b111, 3).
				MustStoreRef(env).MustStoreRef(dummyTx).EndCell(),
			want: exportedValue,
		},
		{
			// msg_export_deq_imm$100 (block-parse.cpp:1927-1928)
			name: "export_deq_imm",
			value: cell.BeginCell().MustStoreUInt(0b100, 3).
				MustStoreRef(env).MustStoreRef(dummyTx).EndCell(),
			want: zero,
		},
		{
			// msg_export_deq$1100 import_block_lt:uint63 (block-parse.cpp:1929-1930)
			name: "export_deq",
			value: cell.BeginCell().MustStoreUInt(0b1100, 4).
				MustStoreRef(env).MustStoreUInt(555, 63).EndCell(),
			want: zero,
		},
		{
			// msg_export_deq_short$1101 (block-parse.cpp:1931-1932)
			name: "export_deq_short",
			value: cell.BeginCell().MustStoreUInt(0b1101, 4).
				MustStoreSlice(bytes.Repeat([]byte{0x77}, 32), 256).
				MustStoreInt(0, 32).MustStoreUInt(0xAA, 64).MustStoreUInt(999, 64).EndCell(),
			want: zero,
		},
		{
			// msg_export_new_defer$10100 (upstream rule, same value formula)
			name: "export_new_defer",
			value: cell.BeginCell().MustStoreUInt(0b10100, 5).
				MustStoreRef(env).MustStoreRef(dummyTx).EndCell(),
			want: exportedValue,
		},
		{
			// msg_export_deferred_tr$10101 (upstream rule, same value formula)
			name: "export_deferred_tr",
			value: cell.BeginCell().MustStoreUInt(0b10101, 5).
				MustStoreRef(env).MustStoreRef(dummyTx).EndCell(),
			want: exportedValue,
		},
	}

	for _, tc := range cases {
		got, err := AugOutMsgDescr{}.LeafExtra(tc.value.MustBeginParse())
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		mustCellHashEqual(t, "OutMsg leaf vector "+tc.name, got, tc.want)
	}

	// truncated variants must be rejected
	truncated := cell.BeginCell().MustStoreUInt(0b1100, 4).MustStoreUInt(1, 10).EndCell()
	if _, err := (AugOutMsgDescr{}).LeafExtra(truncated.MustBeginParse()); err == nil {
		t.Fatal("truncated msg_export_deq must be rejected")
	}
}

// TestAugForkVectors validates the fork (CombineExtra) rules against
// hand-computed values for every augmentation.
func TestAugForkVectors(t *testing.T) {
	slice := func(b *cell.Builder) *cell.Slice { return b.EndCell().MustBeginParse() }

	// CurrencyCollection add: grams sum canonical, extra dicts merged per key
	// (CurrencyCollection::add_values, block-parse.cpp:619-621)
	extraL := mustExtraDict(t, map[uint32]int64{1: 5, 2: 6})
	extraR := mustExtraDict(t, map[uint32]int64{2: 4, 3: 1})
	got, err := AugShardAccountBlocks{}.CombineExtra(
		slice(ccCell(t, 100, extraL)), slice(ccCell(t, 23, extraR)))
	if err != nil {
		t.Fatal(err)
	}
	want := ccCell(t, 123, mustExtraDict(t, map[uint32]int64{1: 5, 2: 10, 3: 1})).EndCell()
	mustCellHashEqual(t, "CC fork", got, want)

	// DepthBalanceInfo add: split_depth max, balance sum
	// (DepthBalanceInfo::add_values, block-parse.cpp:1153-1157)
	dbiL := cell.BeginCell().MustStoreUInt(3, 5).MustStoreBuilder(ccCell(t, 10, nil))
	dbiR := cell.BeginCell().MustStoreUInt(7, 5).MustStoreBuilder(ccCell(t, 15, nil))
	got, err = AugShardAccounts{}.CombineExtra(slice(dbiL), slice(dbiR))
	if err != nil {
		t.Fatal(err)
	}
	want = cell.BeginCell().MustStoreUInt(7, 5).MustStoreBuilder(ccCell(t, 25, nil)).EndCell()
	mustCellHashEqual(t, "DBI fork", got, want)

	// ImportFees add: grams + CC (ImportFees::add_values, block-parse.cpp:1650-1652)
	ifL := cell.BeginCell().MustStoreBigCoins(big.NewInt(3)).MustStoreBuilder(ccCell(t, 30, nil))
	ifR := cell.BeginCell().MustStoreBigCoins(big.NewInt(4)).MustStoreBuilder(ccCell(t, 40, nil))
	got, err = AugInMsgDescr{}.CombineExtra(slice(ifL), slice(ifR))
	if err != nil {
		t.Fatal(err)
	}
	want = cell.BeginCell().MustStoreBigCoins(big.NewInt(7)).MustStoreBuilder(ccCell(t, 70, nil)).EndCell()
	mustCellHashEqual(t, "ImportFees fork", got, want)

	// OutMsgQueue fork: min(left, right) (Aug_OutMsgQueue::eval_fork,
	// block-parse.cpp:1994-1998)
	got, err = AugOutMsgQueue{}.CombineExtra(
		slice(cell.BeginCell().MustStoreUInt(700, 64)),
		slice(cell.BeginCell().MustStoreUInt(300, 64)))
	if err != nil {
		t.Fatal(err)
	}
	mustCellHashEqual(t, "OutMsgQueue fork", got, cell.BeginCell().MustStoreUInt(300, 64).EndCell())

	// ShardFeeCreated add: component-wise CC add (ShardFeeCreated::add_values,
	// block-parse.cpp:2282-2284)
	sfL := cell.BeginCell().MustStoreBuilder(ccCell(t, 1, nil)).MustStoreBuilder(ccCell(t, 2, nil))
	sfR := cell.BeginCell().MustStoreBuilder(ccCell(t, 10, nil)).MustStoreBuilder(ccCell(t, 20, nil))
	got, err = AugShardFees{}.CombineExtra(slice(sfL), slice(sfR))
	if err != nil {
		t.Fatal(err)
	}
	want = cell.BeginCell().MustStoreBuilder(ccCell(t, 11, nil)).MustStoreBuilder(ccCell(t, 22, nil)).EndCell()
	mustCellHashEqual(t, "ShardFeeCreated fork", got, want)

	// empty extras (eval_empty = extra_type null_value)
	checkEmpty := func(name string, aug cell.Augmentation, wantBits uint) {
		c, err := aug.EmptyExtra()
		if err != nil {
			t.Fatalf("%s empty: %v", name, err)
		}
		if c.BitsSize() != wantBits || c.RefsNum() != 0 {
			t.Fatalf("%s empty extra must be %d zero bits, got %d bits %d refs",
				name, wantBits, c.BitsSize(), c.RefsNum())
		}
		s := c.MustBeginParse()
		if v, err := s.LoadBigUInt(wantBits); err != nil || v.Sign() != 0 {
			t.Fatalf("%s empty extra must be all zero", name)
		}
	}
	checkEmpty("ShardAccounts", AugShardAccounts{}, 10)          // DepthBalanceInfo null
	checkEmpty("ShardAccountBlocks", AugShardAccountBlocks{}, 5) // CC null
	checkEmpty("AccountTransactions", AugAccountTransactions{}, 5)
	checkEmpty("InMsgDescr", AugInMsgDescr{}, 9) // ImportFees null (4+4+1)
	checkEmpty("OutMsgDescr", AugOutMsgDescr{}, 5)
	checkEmpty("OutMsgQueue", AugOutMsgQueue{}, 64)
	checkEmpty("ShardFees", AugShardFees{}, 10) // 2 x CC null
}
