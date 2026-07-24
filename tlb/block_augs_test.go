package tlb

import (
	"bytes"
	_ "embed"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// Real mainnet basechain block 0:8000000000000000 seqno 71398501
// (root hash 244d0a2f58561995bd57943b0db945cb9fa6a5181d501efc334636aa4401c241),
// referenced by masterchain seqno 66519406. It contains 269 InMsgDescr entries
// (msg_import_ext, msg_import_imm, msg_import_deferred_fin), 57 OutMsgDescr
// entries (msg_export_imm) and 234 account blocks with 269 transactions.
//
//go:embed testdata/block_0_8000000000000000_71398501.boc
var mainnetBlockBOC []byte

const legacyMsgDescrGlobalVersion = uint32(11)

func loadMainnetBlock(t *testing.T) *Block {
	t.Helper()

	c, err := cell.FromBOC(mainnetBlockBOC)
	if err != nil {
		t.Fatalf("failed to parse block boc: %v", err)
	}

	wantHash, _ := hexBytes("244d0a2f58561995bd57943b0db945cb9fa6a5181d501efc334636aa4401c241")
	if !bytes.Equal(c.Hash(), wantHash) {
		t.Fatalf("block fixture root hash mismatch: %x", c.Hash())
	}

	var blk Block
	if err = LoadFromCell(&blk, c.MustBeginParse()); err != nil {
		t.Fatalf("failed to parse block: %v", err)
	}
	return &blk
}

func hexBytes(s string) ([]byte, error) {
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		var b byte
		for j := 0; j < 2; j++ {
			c := s[i*2+j]
			b <<= 4
			switch {
			case c >= '0' && c <= '9':
				b |= c - '0'
			case c >= 'a' && c <= 'f':
				b |= c - 'a' + 10
			case c >= 'A' && c <= 'F':
				b |= c - 'A' + 10
			}
		}
		out[i] = b
	}
	return out, nil
}

// mustCellHashEqual asserts both cells serialize to the same hash.
func mustCellHashEqual(t *testing.T, name string, got, want *cell.Cell) {
	t.Helper()
	if got == nil || want == nil {
		if got != want {
			t.Fatalf("%s: nil mismatch: got %v, want %v", name, got, want)
		}
		return
	}
	if !bytes.Equal(got.Hash(), want.Hash()) {
		t.Fatalf("%s mismatch:\n got  %s\n want %s", name, got.Dump(), want.Dump())
	}
}

// rebuildAugDictAndCompare re-creates an augmented dictionary from scratch by
// Set()ting every key/value of the original with the writable augmentation,
// then requires the rebuilt root cell hash and root extra to be identical to
// the original ones. It also cross-checks every leaf extra derived by the
// augmentation against the stored one.
func rebuildAugDictAndCompare(t *testing.T, name string, original, rebuilt *cell.AugmentedDictionary, aug cell.Augmentation) {
	t.Helper()

	items, err := original.RangeExtra(false, false)
	if err != nil {
		t.Fatalf("%s: failed to list original dict: %v", name, err)
	}
	if len(items) == 0 {
		t.Fatalf("%s: original dict is empty, nothing to validate", name)
	}

	for i, item := range items {
		valueCell, err := item.Value.Copy().ToCell()
		if err != nil {
			t.Fatalf("%s: failed to capture value %d: %v", name, i, err)
		}
		storedExtra, err := item.Extra.Copy().ToCell()
		if err != nil {
			t.Fatalf("%s: failed to capture extra %d: %v", name, i, err)
		}

		derivedExtra, err := aug.LeafExtra(item.Value.Copy())
		if err != nil {
			t.Fatalf("%s: failed to derive leaf extra %d (key %x): %v", name, i, item.Key.Hash(), err)
		}
		mustCellHashEqual(t, name+" leaf extra", derivedExtra, storedExtra)

		if err = rebuilt.Set(item.Key, valueCell); err != nil {
			t.Fatalf("%s: failed to set item %d: %v", name, i, err)
		}
	}

	origCell, err := original.ToCell()
	if err != nil {
		t.Fatalf("%s: failed to serialize original: %v", name, err)
	}
	rebuiltCell, err := rebuilt.ToCell()
	if err != nil {
		t.Fatalf("%s: failed to serialize rebuilt: %v", name, err)
	}
	mustCellHashEqual(t, name+" wrapped root cell (hash+extra)", rebuiltCell, origCell)
	mustCellHashEqual(t, name+" root extra", rebuilt.GetRootExtra(), original.GetRootExtra())
}

func TestAugInMsgDescrRebuildFromMainnetBlock(t *testing.T) {
	blk := loadMainnetBlock(t)

	original, err := LoadInMsgDescrAugDict(blk.Extra.InMsgDesc.MustBeginParse(), legacyMsgDescrGlobalVersion)
	if err != nil {
		t.Fatalf("failed to load in msg descr: %v", err)
	}

	rebuilt, err := NewInMsgDescrAugDict(legacyMsgDescrGlobalVersion)
	if err != nil {
		t.Fatal(err)
	}
	rebuildAugDictAndCompare(t, "InMsgDescr", original.AugmentedDictionary, rebuilt.AugmentedDictionary,
		AugInMsgDescr{GlobalVersion: legacyMsgDescrGlobalVersion})
}

func TestAugOutMsgDescrRebuildFromMainnetBlock(t *testing.T) {
	blk := loadMainnetBlock(t)

	original, err := LoadOutMsgDescrAugDict(blk.Extra.OutMsgDesc.MustBeginParse(), legacyMsgDescrGlobalVersion)
	if err != nil {
		t.Fatalf("failed to load out msg descr: %v", err)
	}

	rebuilt, err := NewOutMsgDescrAugDict(legacyMsgDescrGlobalVersion)
	if err != nil {
		t.Fatal(err)
	}
	rebuildAugDictAndCompare(t, "OutMsgDescr", original.AugmentedDictionary, rebuilt.AugmentedDictionary,
		AugOutMsgDescr{GlobalVersion: legacyMsgDescrGlobalVersion})
}

func TestAugShardAccountBlocksRebuildFromMainnetBlock(t *testing.T) {
	blk := loadMainnetBlock(t)

	var original ShardAccountBlocksAugDict
	if err := LoadFromCell(&original, blk.Extra.ShardAccountBlocks.MustBeginParse()); err != nil {
		t.Fatalf("failed to load shard account blocks: %v", err)
	}

	rebuilt, err := NewShardAccountBlocksAugDict()
	if err != nil {
		t.Fatal(err)
	}
	rebuildAugDictAndCompare(t, "ShardAccountBlocks", original.AugmentedDictionary, rebuilt.AugmentedDictionary, AugShardAccountBlocks{})

	// a dictionary parsed with LoadFromCell must be directly writable: delete
	// an entry and re-insert it, ending up with the original root again
	items, err := original.RangeExtra(false, false)
	if err != nil {
		t.Fatal(err)
	}
	key := items[0].Key
	value, err := items[0].Value.Copy().ToCell()
	if err != nil {
		t.Fatal(err)
	}
	wantRoot, err := original.AugmentedDictionary.ToCell()
	if err != nil {
		t.Fatal(err)
	}

	if err = original.Delete(key); err != nil {
		t.Fatalf("failed to delete from parsed dict: %v", err)
	}
	afterDelete, err := original.AugmentedDictionary.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(afterDelete.Hash(), wantRoot.Hash()) {
		t.Fatal("delete did not change the dict")
	}
	if err = original.Set(key, value); err != nil {
		t.Fatalf("failed to set into parsed dict: %v", err)
	}
	restored, err := original.AugmentedDictionary.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	mustCellHashEqual(t, "parsed ShardAccountBlocks delete+set restore", restored, wantRoot)
}

func TestAugAccountTransactionsRebuildFromMainnetBlock(t *testing.T) {
	blk := loadMainnetBlock(t)

	var accounts ShardAccountBlocksAugDict
	if err := LoadFromCell(&accounts, blk.Extra.ShardAccountBlocks.MustBeginParse()); err != nil {
		t.Fatalf("failed to load shard account blocks: %v", err)
	}

	items, err := accounts.RangeExtra(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("no account blocks in fixture")
	}

	validated := 0
	for _, item := range items {
		var accountBlock AccountBlock
		if err = LoadFromCell(&accountBlock, item.Value.Copy()); err != nil {
			t.Fatalf("failed to load account block: %v", err)
		}

		original := accountBlock.Transactions
		originalRoot, err := original.ToCell() // parsed inline dict serializes to its root
		if err != nil {
			t.Fatalf("failed to serialize original inline dict: %v", err)
		}

		rebuilt, err := NewAccountTransactionsAugDict()
		if err != nil {
			t.Fatal(err)
		}

		txItems, err := original.RangeExtra(false, false)
		if err != nil {
			t.Fatal(err)
		}
		for i, tx := range txItems {
			valueCell, err := tx.Value.Copy().ToCell()
			if err != nil {
				t.Fatal(err)
			}
			storedExtra, err := tx.Extra.Copy().ToCell()
			if err != nil {
				t.Fatal(err)
			}
			derived, err := AugAccountTransactions{}.LeafExtra(tx.Value.Copy())
			if err != nil {
				t.Fatalf("failed to derive tx %d extra: %v", i, err)
			}
			mustCellHashEqual(t, "AccountTransactions leaf extra", derived, storedExtra)

			if err = rebuilt.Set(tx.Key, valueCell); err != nil {
				t.Fatal(err)
			}
		}

		rebuiltRoot, err := rebuilt.InlineCell()
		if err != nil {
			t.Fatalf("failed to serialize rebuilt inline dict: %v", err)
		}
		mustCellHashEqual(t, "AccountTransactions inline root", rebuiltRoot, originalRoot)
		mustCellHashEqual(t, "AccountTransactions root extra", rebuilt.GetRootExtra(), original.GetRootExtra())
		validated++
	}

	if validated < 100 {
		t.Fatalf("expected to validate at least 100 account transaction dicts, got %d", validated)
	}
}

func TestAugCombineWithOnMainnetShardAccountBlocks(t *testing.T) {
	blk := loadMainnetBlock(t)

	var original ShardAccountBlocksAugDict
	if err := LoadFromCell(&original, blk.Extra.ShardAccountBlocks.MustBeginParse()); err != nil {
		t.Fatalf("failed to load shard account blocks: %v", err)
	}

	items, err := original.RangeExtra(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 4 {
		t.Fatalf("fixture dict is too small: %d items", len(items))
	}

	left, err := NewShardAccountBlocksAugDict()
	if err != nil {
		t.Fatal(err)
	}
	right, err := NewShardAccountBlocksAugDict()
	if err != nil {
		t.Fatal(err)
	}

	for i, item := range items {
		valueCell, err := item.Value.Copy().ToCell()
		if err != nil {
			t.Fatal(err)
		}
		dst := left
		if i%2 == 1 {
			dst = right
		}
		if err = dst.Set(item.Key, valueCell); err != nil {
			t.Fatal(err)
		}
	}

	ok, err := left.CombineWith(right.AugmentedDictionary)
	if err != nil {
		t.Fatalf("combine failed: %v", err)
	}
	if !ok {
		t.Fatal("combine reported a key conflict")
	}

	combinedCell, err := left.AugmentedDictionary.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	originalCell, err := original.AugmentedDictionary.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	mustCellHashEqual(t, "combined ShardAccountBlocks root", combinedCell, originalCell)
	mustCellHashEqual(t, "combined ShardAccountBlocks extra", left.GetRootExtra(), original.GetRootExtra())
}

func TestImportFeesRoundTripAgainstMainnetExtras(t *testing.T) {
	blk := loadMainnetBlock(t)

	descr, err := LoadInMsgDescrAugDict(blk.Extra.InMsgDesc.MustBeginParse(), legacyMsgDescrGlobalVersion)
	if err != nil {
		t.Fatalf("failed to load in msg descr: %v", err)
	}

	items, err := descr.RangeExtra(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("no in msg descr items")
	}

	for i, item := range items {
		originalExtra, err := item.Extra.Copy().ToCell()
		if err != nil {
			t.Fatal(err)
		}

		var fees ImportFees
		if err = LoadFromCell(&fees, item.Extra.Copy()); err != nil {
			t.Fatalf("failed to parse import fees %d: %v", i, err)
		}
		serialized, err := fees.ToCell()
		if err != nil {
			t.Fatalf("failed to serialize import fees %d: %v", i, err)
		}
		mustCellHashEqual(t, "ImportFees round-trip", serialized, originalExtra)
	}

	// also validate the dict root extra as a whole
	rootExtraCell := descr.GetRootExtra()
	var rootFees ImportFees
	if err = LoadFromCell(&rootFees, rootExtraCell.MustBeginParse()); err != nil {
		t.Fatalf("failed to parse root import fees: %v", err)
	}
	serialized, err := rootFees.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	mustCellHashEqual(t, "ImportFees root extra round-trip", serialized, rootExtraCell)
}

type mutableDescriptorDict interface {
	ToCell() (*cell.Cell, error)
	Set(*cell.Cell, *cell.Cell) error
	Delete(*cell.Cell) error
}

func TestDecodedMessageDescriptorsRemainWritable(t *testing.T) {
	key := cell.BeginCell().MustStoreUInt(0, 256).EndCell()
	dummy := cell.BeginCell().EndCell()

	tests := []struct {
		name       string
		descriptor *cell.Cell
		new        func() (mutableDescriptorDict, error)
		load       func(*cell.Slice) (mutableDescriptorDict, error)
	}{
		{
			name:       "in",
			descriptor: cell.BeginCell().MustStoreUInt(0, 3).EndCell(),
			new: func() (mutableDescriptorDict, error) {
				return NewInMsgDescrAugDict(legacyMsgDescrGlobalVersion)
			},
			load: func(s *cell.Slice) (mutableDescriptorDict, error) {
				return LoadInMsgDescrAugDict(s, legacyMsgDescrGlobalVersion)
			},
		},
		{
			name: "out",
			descriptor: cell.BeginCell().MustStoreUInt(0, 3).
				MustStoreRef(dummy).MustStoreRef(dummy).EndCell(),
			new: func() (mutableDescriptorDict, error) {
				return NewOutMsgDescrAugDict(legacyMsgDescrGlobalVersion)
			},
			load: func(s *cell.Slice) (mutableDescriptorDict, error) {
				return LoadOutMsgDescrAugDict(s, legacyMsgDescrGlobalVersion)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			empty, err := test.new()
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := empty.ToCell()
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := test.load(encoded.MustBeginParse())
			if err != nil {
				t.Fatal(err)
			}
			if err = decoded.Set(key, test.descriptor); err != nil {
				t.Fatalf("set decoded descriptor: %v", err)
			}
			if err = decoded.Delete(key); err != nil {
				t.Fatalf("delete decoded descriptor: %v", err)
			}
		})
	}
}

func TestMsgEnvelopeRoundTripAgainstMainnetEnvelopes(t *testing.T) {
	blk := loadMainnetBlock(t)

	descr, err := LoadOutMsgDescrAugDict(blk.Extra.OutMsgDesc.MustBeginParse(), legacyMsgDescrGlobalVersion)
	if err != nil {
		t.Fatalf("failed to load out msg descr: %v", err)
	}

	items, err := descr.RangeExtra(false, false)
	if err != nil {
		t.Fatal(err)
	}

	checked := 0
	for _, item := range items {
		v := item.Value.Copy()
		if _, err = v.LoadUInt(3); err != nil {
			t.Fatal(err)
		}
		if v.RefsNum() == 0 {
			continue
		}
		envCell, err := v.LoadRefCell()
		if err != nil {
			t.Fatal(err)
		}

		var env MsgEnvelope
		if err = LoadFromCell(&env, envCell.MustBeginParse()); err != nil {
			t.Fatalf("failed to parse mainnet envelope: %v", err)
		}
		serialized, err := env.ToCell()
		if err != nil {
			t.Fatalf("failed to serialize mainnet envelope: %v", err)
		}
		mustCellHashEqual(t, "mainnet MsgEnvelope round-trip", serialized, envCell)
		checked++
	}
	if checked == 0 {
		t.Fatal("no envelopes checked")
	}
}

// ===========================================================================
// synthetic fixtures for the state dictionaries (no state fixture in repo)
// ===========================================================================

func synthAccountCell(t *testing.T, addr *address.Address, balance *big.Int, extra *cell.Dictionary) *cell.Cell {
	t.Helper()

	state := AccountState{
		IsValid: true,
		Address: addr,
		StorageInfo: StorageInfo{
			StorageUsed: StorageUsed{
				CellsUsed: big.NewInt(10),
				BitsUsed:  big.NewInt(2000),
			},
			StorageExtra: StorageExtraNone{},
			LastPaid:     1700000000,
		},
		AccountStorage: AccountStorage{
			Status:            AccountStatusUninit,
			LastTransactionLT: 123456789,
			Balance:           FromNanoTON(balance),
			ExtraCurrencies:   extra,
		},
	}

	c, err := state.ToCell()
	if err != nil {
		t.Fatalf("failed to serialize account: %v", err)
	}
	return c
}

func synthShardAccountValue(t *testing.T, account *cell.Cell, lastLT uint64) *cell.Cell {
	t.Helper()

	sa := ShardAccount{
		Account:       account,
		LastTransHash: bytes.Repeat([]byte{0xAB}, 32),
		LastTransLT:   lastLT,
	}
	c, err := ToCell(sa)
	if err != nil {
		t.Fatalf("failed to serialize shard account: %v", err)
	}
	return c
}

func addrKeyCell(addr *address.Address) *cell.Cell {
	return cell.BeginCell().MustStoreSlice(addr.Data(), 256).EndCell()
}

func mustExtraDict(t *testing.T, entries map[uint32]int64) *cell.Dictionary {
	t.Helper()
	d := cell.NewDict(32)
	for id, amount := range entries {
		err := d.SetIntKey(new(big.Int).SetUint64(uint64(id)),
			cell.BeginCell().MustStoreBigVarUInt(big.NewInt(amount), 32).EndCell())
		if err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func TestAugShardAccountsSynthetic(t *testing.T) {
	addr1 := address.NewAddress(0, 0, bytes.Repeat([]byte{0x11}, 32))
	addr2 := address.NewAddress(0, 0, bytes.Repeat([]byte{0x92}, 32))
	addr3 := address.NewAddress(0, 0, bytes.Repeat([]byte{0xE3}, 32))

	extra2 := mustExtraDict(t, map[uint32]int64{7: 1000, 42: 5})

	acc1 := synthAccountCell(t, addr1, big.NewInt(1_000_000_000), nil)
	acc2 := synthAccountCell(t, addr2, big.NewInt(25), extra2)
	accNone := cell.BeginCell().MustStoreBoolBit(false).EndCell() // account_none$0

	dict, err := NewShardAccountsAugDict()
	if err != nil {
		t.Fatal(err)
	}

	if err = dict.Set(addrKeyCell(addr1), synthShardAccountValue(t, acc1, 100)); err != nil {
		t.Fatalf("failed to set account 1: %v", err)
	}
	if err = dict.Set(addrKeyCell(addr2), synthShardAccountValue(t, acc2, 200)); err != nil {
		t.Fatalf("failed to set account 2: %v", err)
	}
	if err = dict.Set(addrKeyCell(addr3), synthShardAccountValue(t, accNone, 0)); err != nil {
		t.Fatalf("failed to set account none: %v", err)
	}

	// The root extra uses the maximum split_depth, sums balances, and treats
	// account_none as the null DepthBalanceInfo.
	expected := cell.BeginCell().
		MustStoreUInt(0, 5).                          // split_depth
		MustStoreBigCoins(big.NewInt(1_000_000_025)). // grams sum
		MustStoreDict(extra2).                        // merged extra currencies
		EndCell()
	mustCellHashEqual(t, "ShardAccounts synthetic root extra", dict.GetRootExtra(), expected)

	// serialized dict must reload cleanly with the same augmentation (the
	// loader cross-checks the stored root extra against the root node)
	serialized, err := dict.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	var reloaded ShardAccountsAugDict
	if err = LoadFromCell(&reloaded, serialized.MustBeginParse()); err != nil {
		t.Fatalf("failed to reload synthetic shard accounts: %v", err)
	}
	mustCellHashEqual(t, "ShardAccounts reload extra", reloaded.GetRootExtra(), expected)

	// leaf extra of the account with extra currencies must copy the balance
	// bit-exactly
	_, leafExtra, err := reloaded.LoadValueExtra(addrKeyCell(addr2))
	if err != nil {
		t.Fatal(err)
	}
	leafExtraCell, err := leafExtra.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	wantLeaf := cell.BeginCell().
		MustStoreUInt(0, 5).
		MustStoreBigCoins(big.NewInt(25)).
		MustStoreDict(extra2).
		EndCell()
	mustCellHashEqual(t, "ShardAccounts leaf extra", leafExtraCell, wantLeaf)

	// deleting an entry must recompute the root extra
	if err = dict.Delete(addrKeyCell(addr2)); err != nil {
		t.Fatal(err)
	}
	expectedAfterDelete := cell.BeginCell().
		MustStoreUInt(0, 5).
		MustStoreBigCoins(big.NewInt(1_000_000_000)).
		MustStoreBoolBit(false).
		EndCell()
	mustCellHashEqual(t, "ShardAccounts extra after delete", dict.GetRootExtra(), expectedAfterDelete)
}

func synthIntMessage(t *testing.T, src, dst *address.Address, valueNano int64, createdLT uint64) *cell.Cell {
	t.Helper()
	// int_msg_info$0 ihr_disabled:Bool bounce:Bool bounced:Bool src dest value
	// ihr_fee fwd_fee created_lt created_at; init:Maybe nothing; body inline empty
	b := cell.BeginCell().
		MustStoreUInt(0b0100, 4). // tag + ihr_disabled=1
		MustStoreAddr(src).
		MustStoreAddr(dst).
		MustStoreBigCoins(big.NewInt(valueNano)).
		MustStoreBoolBit(false).          // no extra currencies
		MustStoreBigCoins(big.NewInt(0)). // ihr_fee
		MustStoreBigCoins(big.NewInt(1)). // fwd_fee
		MustStoreUInt(createdLT, 64).
		MustStoreUInt(1700000001, 32).
		MustStoreBoolBit(false). // init:nothing
		MustStoreBoolBit(false)  // body inline
	return b.EndCell()
}

func synthEnqueuedMsgValue(t *testing.T, createdLT uint64, emittedLT *uint64) *cell.Cell {
	t.Helper()

	src := address.NewAddress(0, 0, bytes.Repeat([]byte{0x21}, 32))
	dst := address.NewAddress(0, 0, bytes.Repeat([]byte{0x35}, 32))

	env := MsgEnvelope{
		FwdFeeRemaining: FromNanoTON(big.NewInt(7)),
		Msg:             synthIntMessage(t, src, dst, 1000, createdLT),
		EmittedLT:       emittedLT,
	}
	envCell, err := env.ToCell()
	if err != nil {
		t.Fatal(err)
	}

	enq := EnqueuedMsg{EnqueuedLT: createdLT + 5, Msg: envCell}
	c, err := enq.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func queueKey(b byte) *cell.Cell {
	return cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{b}, 44), 352).EndCell()
}

func TestAugOutMsgQueueSynthetic(t *testing.T) {
	dict, err := NewOutMsgQueueAugDict()
	if err != nil {
		t.Fatal(err)
	}

	// A v1 envelope uses the enclosed message's created_lt as its extra.
	if err = dict.Set(queueKey(0x01), synthEnqueuedMsgValue(t, 500, nil)); err != nil {
		t.Fatal(err)
	}
	if err = dict.Set(queueKey(0x85), synthEnqueuedMsgValue(t, 300, nil)); err != nil {
		t.Fatal(err)
	}

	// A fork extra is the minimum of its child extras.
	want := cell.BeginCell().MustStoreUInt(300, 64).EndCell()
	mustCellHashEqual(t, "OutMsgQueue min extra", dict.GetRootExtra(), want)

	// An explicit v2 emitted_lt overrides the message created_lt.
	emitted := uint64(120)
	if err = dict.Set(queueKey(0x44), synthEnqueuedMsgValue(t, 700, &emitted)); err != nil {
		t.Fatal(err)
	}
	want = cell.BeginCell().MustStoreUInt(120, 64).EndCell()
	mustCellHashEqual(t, "OutMsgQueue emitted min extra", dict.GetRootExtra(), want)

	// reload validates stored extras against the augmentation
	serialized, err := dict.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	var reloaded OutMsgQueueAugDict
	if err = LoadFromCell(&reloaded, serialized.MustBeginParse()); err != nil {
		t.Fatalf("failed to reload synthetic out msg queue: %v", err)
	}
	mustCellHashEqual(t, "OutMsgQueue reload extra", reloaded.GetRootExtra(), want)

	// deleting the v2 entry restores the previous minimum
	if err = dict.Delete(queueKey(0x44)); err != nil {
		t.Fatal(err)
	}
	want = cell.BeginCell().MustStoreUInt(300, 64).EndCell()
	mustCellHashEqual(t, "OutMsgQueue extra after delete", dict.GetRootExtra(), want)

	// An empty dictionary has a zero extra.
	empty, err := NewOutMsgQueueAugDict()
	if err != nil {
		t.Fatal(err)
	}
	mustCellHashEqual(t, "OutMsgQueue empty extra", empty.GetRootExtra(),
		cell.BeginCell().MustStoreUInt(0, 64).EndCell())
}

func synthShardFeeCreatedValue(t *testing.T, fees, create int64, feesExtra *cell.Dictionary) *cell.Cell {
	t.Helper()
	b := cell.BeginCell().
		MustStoreBigCoins(big.NewInt(fees)).
		MustStoreDict(feesExtra).
		MustStoreBigCoins(big.NewInt(create)).
		MustStoreBoolBit(false)
	return b.EndCell()
}

func shardFeeKey(wc int32, shardPfx uint64) *cell.Cell {
	return cell.BeginCell().MustStoreInt(int64(wc), 32).MustStoreUInt(shardPfx, 64).EndCell()
}

func TestAugShardFeesSynthetic(t *testing.T) {
	dict, err := NewShardFeesAugDict()
	if err != nil {
		t.Fatal(err)
	}

	extraA := mustExtraDict(t, map[uint32]int64{3: 50})
	extraB := mustExtraDict(t, map[uint32]int64{3: 8, 9: 1})

	valA := synthShardFeeCreatedValue(t, 100, 40, extraA)
	valB := synthShardFeeCreatedValue(t, 23, 60, extraB)

	if err = dict.Set(shardFeeKey(0, 0x2000000000000000), valA); err != nil {
		t.Fatal(err)
	}
	if err = dict.Set(shardFeeKey(0, 0x6000000000000000), valB); err != nil {
		t.Fatal(err)
	}

	// A leaf extra is the value copied verbatim.
	_, leafExtra, err := dict.LoadValueExtra(shardFeeKey(0, 0x2000000000000000))
	if err != nil {
		t.Fatal(err)
	}
	leafCell, err := leafExtra.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	mustCellHashEqual(t, "ShardFees leaf extra", leafCell, valA)

	// A fork extra sums both CurrencyCollections component-wise and merges extra
	// currency dictionaries by key.
	mergedExtra := mustExtraDict(t, map[uint32]int64{3: 58, 9: 1})
	want := cell.BeginCell().
		MustStoreBigCoins(big.NewInt(123)).
		MustStoreDict(mergedExtra).
		MustStoreBigCoins(big.NewInt(100)).
		MustStoreBoolBit(false).
		EndCell()
	mustCellHashEqual(t, "ShardFees root extra", dict.GetRootExtra(), want)

	// round-trip through the ShardFeeCreated TLB type in both directions
	var sfc ShardFeeCreated
	if err = LoadFromCell(&sfc, dict.GetRootExtra().MustBeginParse()); err != nil {
		t.Fatalf("failed to parse root extra as ShardFeeCreated: %v", err)
	}
	if sfc.Fees.Coins.Nano().Int64() != 123 || sfc.Create.Coins.Nano().Int64() != 100 {
		t.Fatalf("unexpected ShardFeeCreated values: %+v", sfc)
	}
	back, err := ToCell(sfc)
	if err != nil {
		t.Fatal(err)
	}
	mustCellHashEqual(t, "ShardFeeCreated round-trip", back, want)

	// reload validates stored extras against the augmentation
	serialized, err := dict.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	var reloaded ShardFeesAugDict
	if err = LoadFromCell(&reloaded, serialized.MustBeginParse()); err != nil {
		t.Fatalf("failed to reload synthetic shard fees: %v", err)
	}
	mustCellHashEqual(t, "ShardFees reload extra", reloaded.GetRootExtra(), want)

	// Values with trailing data are not exact ShardFeeCreated encodings.
	bad := cell.BeginCell().MustStoreBuilder(valA.ToBuilder()).MustStoreUInt(1, 1).EndCell()
	if err = dict.Set(shardFeeKey(0, 0x7000000000000000), bad); err == nil {
		t.Fatal("expected trailing bits in ShardFeeCreated value to be rejected")
	}
}

func TestImportFeesSyntheticRoundTrip(t *testing.T) {
	extra := mustExtraDict(t, map[uint32]int64{100: 7})
	fees := ImportFees{
		FeesCollected: FromNanoTON(big.NewInt(12345)),
		ValueImported: CurrencyCollection{
			Coins:           FromNanoTON(big.NewInt(999999999)),
			ExtraCurrencies: extra,
		},
	}

	c, err := fees.ToCell()
	if err != nil {
		t.Fatal(err)
	}

	var parsed ImportFees
	if err = LoadFromCell(&parsed, c.MustBeginParse()); err != nil {
		t.Fatal(err)
	}
	if parsed.FeesCollected.Nano().Int64() != 12345 {
		t.Fatalf("fees mismatch: %s", parsed.FeesCollected.Nano())
	}
	if parsed.ValueImported.Coins.Nano().Int64() != 999999999 {
		t.Fatalf("value mismatch: %s", parsed.ValueImported.Coins.Nano())
	}

	back, err := parsed.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	mustCellHashEqual(t, "ImportFees synthetic round-trip", back, c)

	// Zero ImportFees is two zero-length Grams fields plus an empty dictionary bit.
	var zero ImportFees
	if err = LoadFromCell(&zero, cell.BeginCell().MustStoreUInt(0, 9).EndCell().MustBeginParse()); err != nil {
		t.Fatal(err)
	}
	zeroCell, err := zero.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if zeroCell.BitsSize() != 9 {
		t.Fatalf("zero ImportFees must serialize to 9 bits, got %d", zeroCell.BitsSize())
	}
}
