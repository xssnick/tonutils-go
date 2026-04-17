package cell

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log"
	"math"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"
)

func skipTestCurrencyCollectionExtra(loader *Slice) error {
	if _, err := loader.LoadCoins(); err != nil {
		return err
	}
	_, err := loader.LoadMaybeRef()
	return err
}

func skipTestDepthBalanceInfoExtra(loader *Slice) error {
	if _, err := loader.LoadUInt(5); err != nil {
		return err
	}
	return skipTestCurrencyCollectionExtra(loader)
}

func TestLoadCell_LoadAugDict(t *testing.T) {
	boc, _ := hex.DecodeString("b5ee9c724102340100062200235b9023afe2ffffff1100000000000000000000000000019db8c60000000162c4845200001aab34c426c6014d575c2001020300480101e5415dd4e865179eb82b1edff31c8408a095e7474e3a1d3d68a061fe03b8ac62000102138209bd22c691124a3630043301d90000000000000000ffffffffffffffff826f48b1a444928d8bb1146f3ef442e4900001aab34b4e484014d575ccf1f8c5fab66850786114e421ecc97a16833bbe2b5a034e49fabcb583022173aafcda01a0c373515a9ff299743ebb7bd974a8f82ce51986a23d2831fb034189d83303130104de91634889251b180506330048010179219a4240635a4ad6f3a6a65275701912edea7893798f57af1b4f9778ca721b021503130101b271de28950272b8070809031301011898a010da960e780a0b0c004801010ab44385631582c1108ad75ebfc884e257b8cf6cdfbbe9155b6c12807f9149e4002a00480101bb06f3506745c5f6a6239d132a70b38439cb60ff95f62e45261ba12e844e889b0001031301007443d8525c3939180d0e0f00480101ae5aa36e6c6acae1db9b2ab1a33cf9859af8ecea96fd2e78b60eb24e0f4f2e53003100480101ec90a44eee02bed840c10e88351163ee9e3613eb9dbe8da760783da449714e2800010213010070971eff39146f88101100480101ee5c34562b83c7c32cb6033f90ce4637a9f59073428032d2be0cb276414b1d50002700480101aaed7ccc3904836f362ae06eb234b71d64e02eb4ba6d6b7869197a9ed5c4b0b800010213010044a99d11861d4b68121300480101596621878c7465344345dcefa4803ee2fb224fcb1cbd6aa09a10f53ab38914e90026021301002f239c10ff1cc72814150048010170c159783d1ae77f702595ce6d7a25acd37ffaaa8c12293ec9e6e81206cc6c310023004801012b5f1d1614fcb15ebd3d3d489b2895f5cda5fdbc48e658556643fd8c10c9c2c30024021301002b46ef1908757d6816170048010115bf77a14a73e704bc99a04417ee8985285a2939bb45211b707d533f57ebc10b001b021100fca881a1128a4c0818190048010113b9aff02e187ceba81ee40ca27df256723a0d8780cab4d94fcd6777b44468ad001d021100fc1790148d13b5281a1b00480101150c62b460866814e89011659974790cbc4490e707066c4c6f464ac63c2e7f41001c021100fc1655ff5d35bd881c1d021100fc15b673f38f9fe81e1f0048010161d2396ee5844f18376658a740d0e64c1574e15435d898b11e6895fb9e366c7e00160048010123a38921c8a3df0be51e86008e789246c5d42acfce14e2f92ae20fd323228b0a0014021100fc14f672784a4108202100480101d064c22bd7b908f0583e76124b8f79cd2ae12a2b6c7f314866841ab69f6d08fd0011021100fc14e70d89bdac28222300480101015cf021221ff8bfe080a84c140f55b7df241dacd892dfae49cd21b9b21838450011021100fc14d960a69113c82425004801012bca1f9584151841c927fb8b9a6bf887c09ba1e0cc5330b9427a96e3aee7b8a00010021100fc14d5376aba99082627004801012e4427dfe24435652c5b10f4d6a533aa22b8c6b3b0cf9564843fb3234aadd95c000d021100fc14d29ab7e9aca82829004801011d4467b1885043dd00b94cd83318975cb2d140fdd722084503c5e4f53d6bdd3e000f021100fc14d275986a11482a2b0048010160f1f53a819b9663e6cf4a7f2ad05f14473c1040cf8625144a425db0e3d1fbe10001021100fc14d2678e94cc082c2d0211503f05347a6cd0c5c22e2f00480101528a31734c0cd0914e0e5b24837094ce137ba183f79df2ba90a97d7909b95e9b00090212680fc14d1e633be2523031004801013fb8e8144a95214d6762cd8d7359fa7d8d7ec2fb6965cb839b8e43d624ab60e8000900480101a034fc34e1f147eb3f9c031c44e890a05e7194d2856e55653466736c40edfd95000a019dba14b98dca6d1cbf2f323117af319a45c09562da3b1d49f86e900e83cc6a00fc14cb1acdbfba4c9832d0d1105cb368bb9f085ac369478347a67b0c52b690cc3902a961c799c2500001aa4cd2961c58320048010155d04ccb9e1eef0374eafb7ce62e26fb6b5d1d17353a9ad12bbbe04406241e6b000100480101b3e9649d10ccb379368e81a3a7e8e49c8eb53f6acc69b0ba2ffa80082f70ee390001ee8406d7")
	c, err := FromBOC(boc)
	if err != nil {
		t.Fatal(err)
		return
	}

	ld := c.BeginParse()
	ld.MustLoadRef()
	ld = ld.MustLoadRef()

	for i := 0; i < 3; i++ {
		dict, err := ld.LoadAugDict(256, skipTestDepthBalanceInfoExtra)
		if err != nil {
			t.Fatal(err, i)
			return
		}

		if dict.IsEmpty() {
			t.Fatal("keys empty")
		}

		addr := address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N")
		data := dict.GetWithExtra(BeginCell().MustStoreSlice(addr.Data(), 256).EndCell())
		if data == nil {
			t.Fatal("not in dict", i)
			return
		}

		if hex.EncodeToString(data.Hash()) != "3ff114a9563416a6fb36b5ec7ec57e2be353af2129a696f66c115a0e7c14a889" {
			t.Fatal("incorrect value")
		}

		value, err := dict.LoadValue(BeginCell().MustStoreSlice(addr.Data(), 256).EndCell())
		if err != nil {
			t.Fatal(err, i)
			return
		}
		if value.BitsLeft() != 320 || value.RefsNum() != 1 {
			t.Fatalf("value should not include augmentation extra, got %d bits and %d refs", value.BitsLeft(), value.RefsNum())
		}

		data = dict.Get(BeginCell().MustStoreSlice(addr.Data(), 32).EndCell())
		if data != nil {
			t.Fatal("in dict", i)
			return
		}

		addr2 := address.MustParseAddr("kQB3P0cDOtkFDdxB77YX-F2DGkrIszmZkmyauMnsP1gg0inM")
		data = dict.Get(BeginCell().MustStoreSlice(addr2.Data(), 256).EndCell())
		if data != nil {
			t.Fatal("in dict", i)
			return
		}

		ld = dict.MustToCell().BeginParse()
	}
}

func TestDictionary_ToCell(t *testing.T) {
	d := NewDict(47)

	for u := 0; u < 150; u++ {
		for x, i := range []uint64{2, 3, 1, 88, 1273, 2211} {
			val := BeginCell().MustStoreUInt(16+uint64(x), 32).EndCell()

			key := BeginCell().MustStoreUInt(i+uint64(u*10000), 47).EndCell()
			err := d.Set(key, val)
			if err != nil {
				t.Fatal("set err:", err)
				return
			}
		}
	}

	c, err := d.ToCell()
	if err != nil {
		t.Fatal("cell err:", err)
		return
	}

	d2, err := c.BeginParse().ToDict(47)
	if err != nil {
		t.Fatal("load err:", err)
		return
	}

	c2, err := d2.ToCell()
	if err != nil {
		t.Fatal("to cell err:", err)
		return
	}

	if !bytes.Equal(c2.Hash(), c.Hash()) {
		t.Fatal("repack not match")
	}
}

func TestDictionary_ToCellNilReceiver(t *testing.T) {
	var dict *Dictionary

	c, err := dict.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if c != nil {
		t.Fatal("nil dict should serialize to nil root")
	}
}

func TestLoadCell_EmptyDict(t *testing.T) {
	d := NewDict(256)
	c := BeginCell().MustStoreDict(d).EndCell()

	s := c.BeginParse().MustLoadMaybeRef()
	if s != nil {
		t.Fatal("dict format incorrect")
	}

	d2 := c.BeginParse().MustLoadDict(256)

	if !d2.IsEmpty() {
		t.Fatal("dict len incorrect")
	}
}

func TestLoadCell_TypedNilDictStoresEmpty(t *testing.T) {
	var d *Dictionary

	c := BeginCell().MustStoreDict(d).EndCell()
	if c.BeginParse().MustLoadMaybeRef() != nil {
		t.Fatal("typed nil dict should serialize as empty maybe-ref")
	}
}

func TestLoadCell_LoadDictEdgeCase(t *testing.T) {
	boc, _ := base64.StdEncoding.DecodeString("te6cckEBEwEAVwACASABAgIC2QMEAgm3///wYBESAgEgBQYCAWIODwIBIAcIAgHODQ0CAdQNDQIBIAkKAgEgCxACASAQDAABWAIBIA0NAAEgAgEgEBAAAdQAAUgAAfwAAdwXk+eF")
	c, err := FromBOC(boc)
	if err != nil {
		t.Fatal(err)
		return
	}

	dict, err := c.BeginParse().ToDict(32)
	if err != nil {
		t.Fatal(err)
	}

	should := map[int64]bool{
		0: true,
		1: true, 9: true, 10: true, 12: true,
		14: true, 15: true, 16: true,
		17: true, 32: true, 34: true,
		36: true, -1001: true, -1000: true,
	}

	items, err := dict.LoadAll()
	if err != nil {
		t.Fatal(err)
	}

	for i, kv := range items {
		if !should[kv.Key.MustLoadInt(32)] {
			t.Fatal(i, "bad key")
		}
	}
}

func TestLoadCell_LoadDictRejectsOverlongLabel(t *testing.T) {
	root := BeginCell().
		MustStoreUInt(0, 1).
		MustStoreUInt(0b110, 3).
		MustStoreUInt(0, 2).
		EndCell()

	_, err := BeginCell().MustStoreMaybeRef(root).EndCell().BeginParse().LoadDict(1)
	if err == nil || !strings.Contains(err.Error(), "label exceeds remaining key bits") {
		t.Fatalf("expected overlong label error, got %v", err)
	}
}

func TestLoadCell_LoadDictRejectsForkWithExtraBits(t *testing.T) {
	leaf0 := BeginCell().MustStoreUInt(0, 2).EndCell()
	leaf1 := BeginCell().MustStoreUInt(0, 2).EndCell()

	root := BeginCell().
		MustStoreUInt(0, 2).
		MustStoreUInt(1, 1).
		MustStoreRef(leaf0).
		MustStoreRef(leaf1).
		EndCell()

	_, err := BeginCell().MustStoreMaybeRef(root).EndCell().BeginParse().LoadDict(1)
	if err == nil || !strings.Contains(err.Error(), "invalid dict fork node") {
		t.Fatalf("expected malformed fork error, got %v", err)
	}
}

func TestLoadCell_LoadDictRejectsForkWithWrongRefCount(t *testing.T) {
	root := BeginCell().
		MustStoreUInt(0, 2).
		MustStoreRef(BeginCell().MustStoreUInt(0, 1).EndCell()).
		EndCell()

	_, err := BeginCell().MustStoreMaybeRef(root).EndCell().BeginParse().LoadDict(1)
	if err == nil || !strings.Contains(err.Error(), "invalid dict fork node") {
		t.Fatalf("expected wrong ref count error, got %v", err)
	}
}

func TestStoreDictLabel_SameBitNonByteAligned(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value uint64
		bits  uint
	}{
		{name: "zeroes", value: 0, bits: 5},
		{name: "ones", value: 0b11111, bits: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			builder := BeginCell()
			label := BeginCell().MustStoreUInt(tc.value, tc.bits).ToSlice()

			if err := storeDictLabel(builder, label, tc.bits); err != nil {
				t.Fatal(err)
			}

			loader := builder.EndCell().BeginParse()
			if typ := loader.MustLoadUInt(2); typ != 0b11 {
				t.Fatalf("expected hml_same encoding, got %02b", typ)
			}

			loader = builder.EndCell().BeginParse()
			labelLen, loaded, err := loadLabel(tc.bits, loader, BeginCell())
			if err != nil {
				t.Fatal(err)
			}
			if labelLen != tc.bits {
				t.Fatalf("unexpected label length: %d", labelLen)
			}
			if got := mustLoadTestValue(t, loaded.ToSlice(), tc.bits); got != tc.value {
				t.Fatalf("unexpected label value: %b", got)
			}
		})
	}
}

func TestLoadCell_DictAll(t *testing.T) {
	empty := BeginCell().EndCell()
	mm := NewDict(64)
	mm.SetIntKey(big.NewInt(0), empty)
	mm.SetIntKey(new(big.Int).SetUint64(math.MaxUint64), empty)
	mm.SetIntKey(new(big.Int).SetUint64(math.MaxUint64-1), empty)
	for i := 0; i < 100000; i++ {
		mm.SetIntKey(big.NewInt(int64(i)), empty)
	}
	mm.SetIntKey(big.NewInt(255), empty)
	mm.SetIntKey(big.NewInt(9223372036854775807), empty)
	mm.SetIntKey(big.NewInt(9223372036854775806), empty)
	hh, _ := mm.AsCell().BeginParse().ToDict(64)

	items, err := mm.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, kv := range items {
		key := kv.Key.MustToCell()
		if hh.Get(key) == nil {
			t.Fatal("invalid key", key.Dump())
		}
	}
}

func TestLoadCell_DictShuffle(t *testing.T) {
	empty := BeginCell().EndCell()
	mm := NewDict(64)
	for i := 0; i < 120000; i++ {
		rnd := make([]byte, 8)
		_, _ = rand.Read(rnd)
		_ = mm.SetIntKey(new(big.Int).Mod(new(big.Int).SetBytes(rnd), big.NewInt(65000)), empty)
	}
	hh, _ := mm.AsCell().BeginParse().ToDict(64)

	items, err := mm.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, kv := range items {
		key := kv.Key.MustToCell()
		if hh.Get(key) == nil {
			t.Fatal("invalid key", key.Dump())
		}
	}

	items, err = hh.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, kv := range items {
		key := kv.Key.MustToCell()
		if mm.Get(key) == nil {
			t.Fatal("invalid key 2", key.Dump())
		}
	}
}

func TestDict_CornerSame(t *testing.T) {
	mm := NewDict(64)
	mm.SetIntKey(big.NewInt(255), BeginCell().EndCell())
	hh, _ := mm.AsCell().BeginParse().ToDict(64)

	if _, err := hh.LoadValueByIntKey(big.NewInt(255)); err != nil {
		t.Fatal("invalid key")
	}
}

func TestDict_Delete(t *testing.T) {
	mm := NewDict(64)
	mm.SetIntKey(big.NewInt(255), BeginCell().EndCell())
	mm.SetIntKey(big.NewInt(777), BeginCell().EndCell())
	mm.SetIntKey(big.NewInt(333), BeginCell().EndCell())
	mm.SetIntKey(big.NewInt(334), BeginCell().EndCell())
	mm.SetIntKey(big.NewInt(331), BeginCell().EndCell())
	mm.SetIntKey(big.NewInt(1000), BeginCell().EndCell())
	mm.SetIntKey(big.NewInt(1001), BeginCell().EndCell())
	hh, _ := mm.AsCell().BeginParse().ToDict(64)

	kProof := BeginCell().MustStoreBigInt(big.NewInt(332), 64).EndCell()
	trace := NewProofTrace()
	hh.SetObserver(trace)
	_, err := hh.LoadValue(kProof)
	if !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatal("no such key")
	}
	proof, err := hh.AsCell().CreateProof(trace.Skeleton())
	if err != nil {
		t.Fatal("failed to proof no key")
	}

	if _, err = hh.LoadValue(kProof); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatal("no key orig", err)
	}

	proof, err = UnwrapProof(proof, hh.AsCell().Hash())
	if err != nil {
		t.Fatal("bad proof hash", err)
	}
	if _, err = proof.AsDict(64).LoadValue(kProof); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatal("no no key proof", err)
	}

	hh.DeleteIntKey(big.NewInt(255))
	hh2, _ := hh.AsCell().BeginParse().ToDict(64)

	if _, err := hh2.LoadValueByIntKey(big.NewInt(255)); err == nil {
		t.Fatal("invalid key")
	}

	if _, err := hh2.LoadValueByIntKey(big.NewInt(777)); err != nil {
		t.Fatal("invalid key")
	}
}

func TestDictionary_Make(t *testing.T) {
	d := NewDict(32)
	err := d.SetIntKey(big.NewInt(100), BeginCell().MustStoreInt(777, 16).EndCell())
	if err != nil {
		t.Fatal(err.Error())
	}

	err = d.SetIntKey(big.NewInt(101), BeginCell().MustStoreInt(777, 16).EndCell())
	if err != nil {
		t.Fatal(err.Error())
	}
	err = d.SetIntKey(big.NewInt(102), BeginCell().MustStoreInt(777, 16).EndCell())
	if err != nil {
		t.Fatal(err.Error())
	}
	for i := int64(0); i < 30000; i++ {
		err = d.SetIntKey(big.NewInt(111+i), BeginCell().MustStoreInt(777, 60).EndCell())
		if err != nil {
			t.Fatal(err.Error())
		}
	}

	for i := int64(0); i < 20000; i++ {
		err = d.SetIntKey(big.NewInt(111+i), nil)
		if err != nil {
			t.Fatal(err.Error())
		}
	}

	for i := int64(25000); i < 30000; i++ {
		err = d.SetIntKey(big.NewInt(111+i), nil)
		if err != nil {
			t.Fatal(err.Error())
		}
	}

	d2, err := d.AsCell().BeginParse().ToDict(32)
	if err != nil {
		t.Fatal(err.Error())
	}

	sl, err := d2.LoadValueByIntKey(big.NewInt(100))
	if err != nil {
		t.Fatal(err.Error())
	}
	println(sl.MustToCell().Dump())
}

func Test_ReplaceDict(t *testing.T) {
	k := big.NewInt(1)
	k2 := big.NewInt(2)
	v := BeginCell().EndCell()

	dict := NewDict(64)

	if err := dict.SetIntKey(k, v); err != nil {
		panic(err)
	}
	if err := dict.SetIntKey(k2, v); err != nil {
		panic(err)
	}

	if err := dict.SetIntKey(k, v); err != nil {
		panic(err)
	}
	if err := dict.SetIntKey(k2, v); err != nil {
		panic(err)
	}

	if err := dict.SetIntKey(k, v); err != nil {
		panic(err)
	}
	if err := dict.SetIntKey(k2, v); err != nil {
		panic(err)
	}
}

func TestDictionary_SetModesAndRefBuilder(t *testing.T) {
	dict := NewDict(8)

	key1 := BeginCell().MustStoreUInt(0x10, 8).EndCell()
	key2 := BeginCell().MustStoreUInt(0x11, 8).EndCell()
	key3 := BeginCell().MustStoreUInt(0x12, 8).EndCell()

	val1 := BeginCell().MustStoreUInt(0xaa, 8).EndCell()
	val2 := BeginCell().MustStoreUInt(0xbb, 8).EndCell()

	changed, err := dict.SetWithMode(key1, val1, DictSetModeReplace)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("replace should not insert a missing key")
	}

	changed, err = dict.SetWithMode(key1, val1, DictSetModeSet)
	if err != nil || !changed {
		t.Fatalf("failed to insert key: changed=%v err=%v", changed, err)
	}

	changed, err = dict.SetWithMode(key1, val2, DictSetModeAdd)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("add should not replace an existing key")
	}

	changed, err = dict.SetWithMode(key1, val2, DictSetModeReplace)
	if err != nil || !changed {
		t.Fatalf("failed to replace key: changed=%v err=%v", changed, err)
	}

	previous, changed, err := dict.LoadValueAndSetWithMode(key1, val1, DictSetModeReplace)
	if err != nil || !changed {
		t.Fatalf("failed to swap key value: changed=%v err=%v", changed, err)
	}
	if got := mustLoadTestValue(t, previous, 8); got != 0xbb {
		t.Fatalf("unexpected previous value: %x", got)
	}

	value, err := dict.LoadValue(key1)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustLoadTestValue(t, value, 8); got != 0xaa {
		t.Fatalf("unexpected replaced value: %x", got)
	}

	refValue := BeginCell().MustStoreUInt(0xfeed, 16).EndCell()
	changed, err = dict.SetBuilderWithMode(key2, BeginCell().MustStoreRef(refValue), DictSetModeSet)
	if err != nil || !changed {
		t.Fatalf("failed to set ref value: changed=%v err=%v", changed, err)
	}

	loadedRefValue, err := dict.LoadValue(key2)
	if err != nil {
		t.Fatal(err)
	}
	loadedRef, err := loadSingleRefValue(loadedRefValue)
	if err != nil {
		t.Fatal(err)
	}
	if !equalCellContents(loadedRef, refValue) {
		t.Fatal("loaded ref does not match stored ref")
	}

	nextRefValue := BeginCell().MustStoreUInt(0xbeef, 16).EndCell()
	previousRefValue, err := dict.LoadValue(key2)
	if err != nil {
		t.Fatal(err)
	}
	previousRef, err := loadSingleRefValue(previousRefValue)
	if err != nil {
		t.Fatal(err)
	}
	changed, err = dict.SetBuilderWithMode(key2, BeginCell().MustStoreRef(nextRefValue), DictSetModeReplace)
	if err != nil || !changed {
		t.Fatalf("failed to replace ref value: changed=%v err=%v", changed, err)
	}
	if !equalCellContents(previousRef, refValue) {
		t.Fatal("unexpected previous ref value")
	}

	builderValue := BeginCell().MustStoreUInt(0x7, 3).MustStoreRef(BeginCell().MustStoreUInt(1, 1).EndCell())
	builderCell := builderValue.EndCell()

	changed, err = dict.SetBuilderWithMode(key3, builderValue, DictSetModeSet)
	if err != nil || !changed {
		t.Fatalf("failed to set builder value: changed=%v err=%v", changed, err)
	}

	loadedBuilderValue, err := dict.LoadValue(key3)
	if err != nil {
		t.Fatal(err)
	}
	loadedBuilderCell, err := loadedBuilderValue.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if !equalCellContents(loadedBuilderCell, builderCell) {
		t.Fatal("loaded builder value does not match stored builder")
	}

	nonRefValue, err := dict.LoadValue(key3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = loadSingleRefValue(nonRefValue); err == nil {
		t.Fatal("expected single-ref extraction to fail for non-ref value")
	}

	removed, err := dict.LoadValueAndDelete(key1)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustLoadTestValue(t, removed, 8); got != 0xaa {
		t.Fatalf("unexpected deleted value: %x", got)
	}

	removedRefValue, err := dict.LoadValueAndDelete(key2)
	if err != nil {
		t.Fatal(err)
	}
	removedRef, err := loadSingleRefValue(removedRefValue)
	if err != nil {
		t.Fatal(err)
	}
	if !equalCellContents(removedRef, nextRefValue) {
		t.Fatal("removed ref does not match stored ref")
	}

	if _, err = dict.LoadValueAndDelete(key1); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("expected ErrNoSuchKeyInDict on second delete, got %v", err)
	}
}

func TestDictionary_MinMax(t *testing.T) {
	dict := NewDict(8)

	values := []struct {
		key uint64
		val uint64
	}{
		{0x01, 0xa1},
		{0x7f, 0xb2},
		{0x80, 0xc3},
		{0xff, 0xd4},
	}

	for _, item := range values {
		if err := dict.Set(
			BeginCell().MustStoreUInt(item.key, 8).EndCell(),
			BeginCell().MustStoreUInt(item.val, 8).EndCell(),
		); err != nil {
			t.Fatal(err)
		}
	}

	assertMinMax := func(fetchMax, invertFirst bool, wantKey, wantVal uint64) {
		t.Helper()

		key, value, err := dict.LoadMinMax(fetchMax, invertFirst)
		if err != nil {
			t.Fatal(err)
		}
		if got := key.BeginParse().MustLoadUInt(8); got != wantKey {
			t.Fatalf("unexpected key: %x", got)
		}
		if got := mustLoadTestValue(t, value, 8); got != wantVal {
			t.Fatalf("unexpected value: %x", got)
		}
	}

	assertMinMax(false, false, 0x01, 0xa1)
	assertMinMax(true, false, 0xff, 0xd4)
	assertMinMax(false, true, 0x80, 0xc3)
	assertMinMax(true, true, 0x7f, 0xb2)

	refDict := NewDict(8)
	refMin := BeginCell().MustStoreUInt(0x1111, 16).EndCell()
	refMax := BeginCell().MustStoreUInt(0x2222, 16).EndCell()

	if err := refDict.SetBuilder(BeginCell().MustStoreUInt(0x01, 8).EndCell(), BeginCell().MustStoreRef(refMin)); err != nil {
		t.Fatal(err)
	}
	if err := refDict.SetBuilder(BeginCell().MustStoreUInt(0xfe, 8).EndCell(), BeginCell().MustStoreRef(refMax)); err != nil {
		t.Fatal(err)
	}

	key, refValue, err := refDict.LoadMinMax(false, false)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := loadSingleRefValue(refValue)
	if err != nil {
		t.Fatal(err)
	}
	if got := key.BeginParse().MustLoadUInt(8); got != 0x01 {
		t.Fatalf("unexpected min ref key: %x", got)
	}
	if !equalCellContents(ref, refMin) {
		t.Fatal("unexpected min ref value")
	}

	key, value, err := dict.LoadMaxAndDelete()
	if err != nil {
		t.Fatal(err)
	}
	if got := key.BeginParse().MustLoadUInt(8); got != 0xff {
		t.Fatalf("unexpected deleted max key: %x", got)
	}
	if got := mustLoadTestValue(t, value, 8); got != 0xd4 {
		t.Fatalf("unexpected deleted max value: %x", got)
	}

	key, refValue, err = refDict.LoadMinMaxAndDelete(true, false)
	if err != nil {
		t.Fatal(err)
	}
	ref, err = loadSingleRefValue(refValue)
	if err != nil {
		t.Fatal(err)
	}
	if got := key.BeginParse().MustLoadUInt(8); got != 0xfe {
		t.Fatalf("unexpected deleted max ref key: %x", got)
	}
	if !equalCellContents(ref, refMax) {
		t.Fatal("unexpected deleted max ref value")
	}
}

func TestDictionary_WrapperCoverage(t *testing.T) {
	dict := NewDict(8)
	if dict.GetKeySize() != 8 {
		t.Fatalf("unexpected key size: %d", dict.GetKeySize())
	}
	if !dict.IsEmpty() {
		t.Fatal("new dict should be empty")
	}
	if !dict.IsEmpty() {
		t.Fatal("unexpected empty dict state")
	}

	key1 := BeginCell().MustStoreUInt(0x01, 8).EndCell()
	key2 := BeginCell().MustStoreUInt(0x02, 8).EndCell()
	key3 := BeginCell().MustStoreUInt(0x03, 8).EndCell()

	if err := dict.SetBuilder(key1, BeginCell().MustStoreUInt(0xAA, 8)); err != nil {
		t.Fatal(err)
	}

	refValue := BeginCell().MustStoreUInt(0x1234, 16).EndCell()
	if err := dict.SetBuilder(key2, BeginCell().MustStoreRef(refValue)); err != nil {
		t.Fatal(err)
	}

	prev, changed, err := dict.LoadValueAndSet(key1, BeginCell().MustStoreUInt(0xBB, 8).EndCell())
	if err != nil || !changed {
		t.Fatalf("failed to swap direct value: changed=%v err=%v", changed, err)
	}
	if got := mustLoadTestValue(t, prev, 8); got != 0xAA {
		t.Fatalf("unexpected previous direct value: %x", got)
	}

	prev, changed, err = dict.LoadValueAndSetBuilder(key1, BeginCell().MustStoreUInt(0xCC, 8))
	if err != nil || !changed {
		t.Fatalf("failed to swap builder value: changed=%v err=%v", changed, err)
	}
	if got := mustLoadTestValue(t, prev, 8); got != 0xBB {
		t.Fatalf("unexpected previous builder value: %x", got)
	}

	prevRefValue, err := dict.LoadValue(key2)
	if err != nil {
		t.Fatal(err)
	}
	prevRef, err := loadSingleRefValue(prevRefValue)
	if err != nil {
		t.Fatal(err)
	}
	changed, err = dict.SetBuilderWithMode(key2, BeginCell().MustStoreRef(BeginCell().MustStoreUInt(0x5678, 16).EndCell()), DictSetModeSet)
	if err != nil || !changed {
		t.Fatalf("failed to swap ref value: changed=%v err=%v", changed, err)
	}
	if !equalCellContents(prevRef, refValue) {
		t.Fatal("unexpected previous ref value")
	}

	loadedRefValue, err := dict.LoadValueByIntKey(big.NewInt(2))
	if err != nil {
		t.Fatal(err)
	}
	loadedRef, err := loadSingleRefValue(loadedRefValue)
	if err != nil {
		t.Fatal(err)
	}
	if loadedRef.BeginParse().MustLoadUInt(16) != 0x5678 {
		t.Fatalf("unexpected loaded ref by int key")
	}

	copyDict := dict.Copy()
	if err = copyDict.Set(key3, BeginCell().MustStoreUInt(0xDD, 8).EndCell()); err != nil {
		t.Fatal(err)
	}
	if _, err = dict.LoadValue(key3); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("copy should not mutate original dict, got %v", err)
	}

	items, err := dict.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("unexpected dict size: %d", len(items))
	}
	if dict.IsEmpty() {
		t.Fatal("dict with values should not be empty")
	}

	deleted, err := dict.LoadValueAndDelete(BeginCell().MustStoreBigInt(big.NewInt(1), dict.GetKeySize()).EndCell())
	if err != nil {
		t.Fatal(err)
	}
	if got := mustLoadTestValue(t, deleted, 8); got != 0xCC {
		t.Fatalf("unexpected deleted direct value: %x", got)
	}

	deletedRefValue, err := dict.LoadValueAndDelete(BeginCell().MustStoreBigInt(big.NewInt(2), dict.GetKeySize()).EndCell())
	if err != nil {
		t.Fatal(err)
	}
	deletedRef, err := loadSingleRefValue(deletedRefValue)
	if err != nil {
		t.Fatal(err)
	}
	if deletedRef.BeginParse().MustLoadUInt(16) != 0x5678 {
		t.Fatalf("unexpected deleted ref value")
	}

	if !dict.IsEmpty() {
		t.Fatal("dict should be empty after deleting all keys")
	}
	items, err = dict.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("unexpected final dict size: %d", len(items))
	}
}

func TestDictionary_MinMaxWrappers(t *testing.T) {
	dict := NewDict(8)
	for key, val := range map[uint64]uint64{
		0x01: 0xA1,
		0x80: 0xB2,
		0xFF: 0xC3,
	} {
		if err := dict.Set(
			BeginCell().MustStoreUInt(key, 8).EndCell(),
			BeginCell().MustStoreUInt(val, 8).EndCell(),
		); err != nil {
			t.Fatal(err)
		}
	}

	minKey, minValue, err := dict.LoadMin()
	if err != nil {
		t.Fatal(err)
	}
	if got := minKey.BeginParse().MustLoadUInt(8); got != 0x01 {
		t.Fatalf("unexpected min key: %x", got)
	}
	if got := mustLoadTestValue(t, minValue, 8); got != 0xA1 {
		t.Fatalf("unexpected min value: %x", got)
	}

	maxKey, maxValue, err := dict.LoadMax()
	if err != nil {
		t.Fatal(err)
	}
	if got := maxKey.BeginParse().MustLoadUInt(8); got != 0xFF {
		t.Fatalf("unexpected max key: %x", got)
	}
	if got := mustLoadTestValue(t, maxValue, 8); got != 0xC3 {
		t.Fatalf("unexpected max value: %x", got)
	}

	removedKey, removedValue, err := dict.LoadMinAndDelete()
	if err != nil {
		t.Fatal(err)
	}
	if got := removedKey.BeginParse().MustLoadUInt(8); got != 0x01 {
		t.Fatalf("unexpected removed min key: %x", got)
	}
	if got := mustLoadTestValue(t, removedValue, 8); got != 0xA1 {
		t.Fatalf("unexpected removed min value: %x", got)
	}

	refDict := NewDict(8)
	refMin := BeginCell().MustStoreUInt(0x1111, 16).EndCell()
	refMax := BeginCell().MustStoreUInt(0x2222, 16).EndCell()
	if err := refDict.SetBuilder(BeginCell().MustStoreUInt(0x02, 8).EndCell(), BeginCell().MustStoreRef(refMin)); err != nil {
		t.Fatal(err)
	}
	if err := refDict.SetBuilder(BeginCell().MustStoreUInt(0xFE, 8).EndCell(), BeginCell().MustStoreRef(refMax)); err != nil {
		t.Fatal(err)
	}

	maxRefKey, maxRefValue, err := refDict.LoadMinMax(true, false)
	if err != nil {
		t.Fatal(err)
	}
	maxRef, err := loadSingleRefValue(maxRefValue)
	if err != nil {
		t.Fatal(err)
	}
	if got := maxRefKey.BeginParse().MustLoadUInt(8); got != 0xFE {
		t.Fatalf("unexpected max ref key: %x", got)
	}
	if !equalCellContents(maxRef, refMax) {
		t.Fatal("unexpected max ref value")
	}

	minRefKey, minRefValue, err := refDict.LoadMinMaxAndDelete(false, false)
	if err != nil {
		t.Fatal(err)
	}
	minRef, err := loadSingleRefValue(minRefValue)
	if err != nil {
		t.Fatal(err)
	}
	if got := minRefKey.BeginParse().MustLoadUInt(8); got != 0x02 {
		t.Fatalf("unexpected removed min ref key: %x", got)
	}
	if !equalCellContents(minRef, refMin) {
		t.Fatal("unexpected removed min ref value")
	}
}

func TestDict_150KProof(t *testing.T) {
	const sz = 150000
	tm := time.Now()
	dict := NewDict(256)
	keys := make([]*Cell, sz)
	for i := 0; i < sz; i++ {
		b := make([]byte, 32)
		rand.Read(b)

		key := BeginCell().MustStoreSlice(b, 256).EndCell()
		keys[i] = key

		if err := dict.Set(key, BeginCell().MustStoreUInt(uint64(i), 32).EndCell()); err != nil {
			t.Fatal(err.Error())
		}
	}
	log.Println("DICT CREATED:", time.Since(tm).String())

	tm = time.Now()
	proofs := make([]*Cell, sz/10)
	for i := 0; i < sz/10; i++ {
		trace := NewProofTrace()
		observed := dict.AsCell().AsDict(dict.GetKeySize()).SetObserver(trace)
		for y := 0; y < 10; y++ {
			val, err := observed.LoadValue(keys[i*10+y])
			if err != nil {
				t.Fatal(err.Error())
			}
			trace.MarkRecursive(val) // to leave full value in proof
		}

		prf, err := dict.AsCell().CreateProof(trace.Skeleton())
		if err != nil {
			t.Fatal(err.Error())
		}

		proofs[i] = prf
	}

	log.Println("PROOF GROUPS BUILT:", time.Since(tm).String(), len(proofs))
	println(proofs[5].Dump())
}

func TestDictionary_String(t *testing.T) {
	const look = `{
	Key 32[00000000]: Value 32 bits, 0 refs
	Key 32[00000001]: Value 32 bits, 0 refs
	Key 32[00000002]: Value 32 bits, 0 refs
	Key 32[00000003]: Value 32 bits, 0 refs
	Key 32[00000004]: Value 32 bits, 0 refs
	Key 32[00000005]: Value 32 bits, 0 refs
	Key 32[00000006]: Value 32 bits, 0 refs
	Key 32[00000007]: Value 32 bits, 0 refs
	Key 32[00000008]: Value 32 bits, 0 refs
	Key 32[00000009]: Value 32 bits, 0 refs
	Key 32[0000000A]: Value 32 bits, 0 refs
	Key 32[0000000B]: Value 32 bits, 0 refs
	Key 32[0000000C]: Value 32 bits, 0 refs
	Key 32[0000000D]: Value 32 bits, 0 refs
	Key 32[0000000E]: Value 32 bits, 0 refs
}`

	const lookProof = `{
	Key 32[00000006]: Value 32 bits, 0 refs
	Key 32[00000007]: Value 32 bits, 0 refs
}`

	const sz = 15
	dict := NewDict(32)
	keys := make([]*Cell, sz)
	for i := 0; i < sz; i++ {
		key := BeginCell().MustStoreUInt(uint64(i), 32).EndCell()
		keys[i] = key

		if err := dict.Set(key, BeginCell().MustStoreUInt(uint64(i), 32).EndCell()); err != nil {
			t.Fatal(err.Error())
		}
	}

	trace := NewProofTrace()
	observed := dict.AsCell().AsDict(dict.GetKeySize()).SetObserver(trace)
	_, err := observed.LoadValue(BeginCell().MustStoreUInt(uint64(7), 32).EndCell())
	if err != nil {
		t.Fatal(err.Error())
	}

	prf, err := dict.AsCell().CreateProof(trace.Skeleton())
	if err != nil {
		t.Fatal(err.Error())
	}

	if dict.String() != look {
		t.Fatal(dict.String())
	}

	prf, err = UnwrapProof(prf, dict.AsCell().Hash())
	if err != nil {
		t.Fatal(err.Error())
	}

	// 1 more neighbour key could be in proof, it is ok
	if d := prf.AsDict(32); d.String() != lookProof {
		t.Fatal(d.String())
	}
}

func TestDictionary_Sz(t *testing.T) {
	d := NewDict(32)

	for i := 0; i < 14000; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(0xFFFFFFFF))
		d.SetIntKey(n, BeginCell().MustStoreRef(BeginCell().EndCell()).EndCell())
	}

	var calc func(cl *Cell) int
	calc = func(cl *Cell) int {
		var childs int
		for i := 0; i < int(cl.RefsNum()); i++ {
			childs += calc(cl.MustPeekRef(i))
		}
		return childs + 1
	}

	c := d.AsCell()
	println(c.Depth(), calc(c))
}
