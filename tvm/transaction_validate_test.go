package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestBuiltTransactionFinalValidationRejectsTrailingData(t *testing.T) {
	params := transactionSerializeTestBuildParams()
	tx, err := buildTransactionCell(params)
	if err != nil {
		t.Fatal(err)
	}

	badRoot := tx.ToBuilder().MustStoreBoolBit(true).EndCell()
	if err = validateBuiltTransactionCell(badRoot, params.inMsg, params.outMsgs); err == nil {
		t.Fatal("expected trailing transaction data to be rejected")
	}

	io := tx.MustPeekRef(0)
	badIO := io.ToBuilder().MustStoreBoolBit(true).EndCell()
	if err = validateBuiltTransactionIO(badIO, nil, nil, 0); err == nil {
		t.Fatal("expected trailing transaction IO data to be rejected")
	}

	stateUpdate := tx.MustPeekRef(1)
	badStateUpdate := stateUpdate.ToBuilder().MustStoreBoolBit(true).EndCell()
	if err = validateBuiltTransactionHashUpdate(badStateUpdate); err == nil {
		t.Fatal("expected trailing state update data to be rejected")
	}

	description := tx.MustPeekRef(2)
	badDescription := description.ToBuilder().MustStoreBoolBit(true).EndCell()
	if err = validateBuiltTransactionDescription(badDescription); err == nil {
		t.Fatal("expected trailing transaction description data to be rejected")
	}
}

func TestBuiltTransactionFinalValidationRejectsWrongOutMessageCount(t *testing.T) {
	params := transactionSerializeTestBuildParams()
	tx, err := buildTransactionCell(params)
	if err != nil {
		t.Fatal(err)
	}

	const countOffset = 4 + 256 + 64 + 256 + 64 + 32
	loader := tx.MustBeginParse()
	prefix := loader.MustLoadSlice(countOffset)
	loader.MustLoadUInt(15)
	tailBits := loader.BitsLeft()
	tail := loader.MustLoadSlice(tailBits)
	builder := cell.BeginCell().
		MustStoreSlice(prefix, countOffset).
		MustStoreUInt(1, 15).
		MustStoreSlice(tail, tailBits)
	for loader.RefsNum() > 0 {
		ref, err := loader.LoadRefCell()
		if err != nil {
			t.Fatal(err)
		}
		builder.MustStoreRef(ref)
	}

	if err = validateBuiltTransactionCell(builder.EndCell(), params.inMsg, params.outMsgs); err == nil {
		t.Fatal("expected mismatching output message count to be rejected")
	}
}

func TestBuiltTransactionFinalValidationChecksActualOutMessageDictionary(t *testing.T) {
	valid, err := transactionInternalMessageToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     tonopsTestAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.ZeroCoins,
		IHRFee:      tlb.ZeroCoins,
		FwdFee:      tlb.ZeroCoins,
	}, transactionOutboundLayout{})
	if err != nil {
		t.Fatal(err)
	}
	malformed := cell.BeginCell().MustStoreUInt(0, 1).EndCell()
	outDict := cell.NewDict(15)
	if err = outDict.SetBuilder(
		cell.BeginCell().MustStoreUInt(0, 15).EndCell(),
		cell.BeginCell().MustStoreRef(malformed),
	); err != nil {
		t.Fatal(err)
	}

	err = validateBuiltTransactionIO(
		buildTransactionIOCell(nil, outDict),
		nil,
		[]OutMessage{{Cell: valid}},
		1,
	)
	if err == nil {
		t.Fatal("expected malformed message stored in output dictionary to be rejected")
	}

	oneMessageDict := cell.NewDict(15)
	if err = oneMessageDict.SetBuilder(
		cell.BeginCell().MustStoreUInt(0, 15).EndCell(),
		cell.BeginCell().MustStoreRef(valid),
	); err != nil {
		t.Fatal(err)
	}
	err = validateBuiltTransactionIO(
		buildTransactionIOCell(nil, oneMessageDict),
		nil,
		[]OutMessage{{Cell: valid}, {Cell: valid}},
		2,
	)
	if err == nil {
		t.Fatal("expected output dictionary leaf count mismatch to be rejected")
	}
}

func TestBuiltTransactionMessageValidationAcceptsEitherLayouts(t *testing.T) {
	msg := &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     tonopsTestAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.ZeroCoins,
		IHRFee:      tlb.ZeroCoins,
		FwdFee:      tlb.ZeroCoins,
		StateInit:   &tlb.StateInit{},
		Body:        cell.BeginCell().MustStoreUInt(0xabc, 12).EndCell(),
	}

	for _, layout := range []transactionOutboundLayout{
		{stateInitInRef: false, bodyInRef: false},
		{stateInitInRef: true, bodyInRef: true},
	} {
		root, err := transactionInternalMessageToCell(msg, layout)
		if err != nil {
			t.Fatal(err)
		}
		if err = validateBuiltTransactionMessage(root); err != nil {
			t.Fatalf("layout %+v rejected: %v", layout, err)
		}
	}
}

func TestBuiltTransactionMessageValidationRequiresExactReferencedStateInit(t *testing.T) {
	exact := cell.BeginCell().MustStoreUInt(0, 5).EndCell()
	for _, external := range []bool{false, true} {
		if err := validateBuiltTransactionMessage(transactionTestMessageWithReferencedStateInit(exact, external)); err != nil {
			t.Fatalf("external=%t exact StateInit rejected: %v", external, err)
		}
		for _, malformed := range []*cell.Cell{
			exact.ToBuilder().MustStoreBoolBit(true).EndCell(),
			exact.ToBuilder().MustStoreRef(cell.BeginCell().EndCell()).EndCell(),
		} {
			if err := validateBuiltTransactionMessage(transactionTestMessageWithReferencedStateInit(malformed, external)); err == nil {
				t.Fatalf("external=%t malformed referenced StateInit accepted", external)
			}
		}
	}
}

func TestReferencedStateInitValidationKeepsPayloadRefsLazy(t *testing.T) {
	refs := []*cell.Cell{
		cell.BeginCell().MustStoreUInt(1, 1).EndCell(),
		cell.BeginCell().MustStoreUInt(2, 2).EndCell(),
		cell.BeginCell().MustStoreUInt(3, 2).EndCell(),
	}
	stateInit := cell.BeginCell().
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		MustStoreBoolBit(true).MustStoreRef(refs[0]).
		MustStoreBoolBit(true).MustStoreRef(refs[1]).
		MustStoreBoolBit(true).MustStoreRef(refs[2]).
		EndCell()

	byHash := make(map[cell.Hash]*cell.Cell, len(refs))
	lazyRefs := make([]cell.LazyRef, 0, len(refs))
	for _, ref := range refs {
		hash := ref.HashKey()
		byHash[hash] = ref
		lazyRefs = append(lazyRefs, cell.LazyRef{
			Hashes: hash[:],
			Depths: []uint16{ref.Depth()},
		})
	}
	loads := 0
	// Ordinary cell with three refs and the five StateInit presence bits 00111.
	lazyStateInit, err := cell.CreateWithLazyRefsUnsafe(
		0x0301,
		[]byte{0x3c},
		stateInit.Hash(),
		[]uint16{stateInit.Depth()},
		lazyRefs,
		func(hash cell.Hash) (*cell.Cell, error) {
			loads++
			return byHash[hash], nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	msg := transactionTestMessageWithReferencedStateInit(lazyStateInit, false)
	if err = validateBuiltTransactionMessage(msg); err != nil {
		t.Fatal(err)
	}
	if _, err = transactionValidateRelaxedActionMessageCurrencies(msg); err != nil {
		t.Fatal(err)
	}
	if loads != 0 {
		t.Fatalf("StateInit payload refs loaded %d times", loads)
	}
}

func TestBuiltTransactionMessageValidationRejectsNonCanonicalVariableAddress(t *testing.T) {
	for _, tc := range []struct {
		name      string
		workchain int64
		addrBits  uint
		wantError bool
	}{
		{name: "standard-sized int8 workchain", workchain: 1, addrBits: 256, wantError: true},
		{name: "basechain is reserved", workchain: 0, addrBits: 255, wantError: true},
		{name: "masterchain is reserved", workchain: -1, addrBits: 255, wantError: true},
		{name: "non-standard size", workchain: 1, addrBits: 255},
		{name: "wide workchain", workchain: 128, addrBits: 256},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateBuiltTransactionMessage(transactionTestInternalMessageWithVariableDestination(tc.workchain, tc.addrBits))
			if (err != nil) != tc.wantError {
				t.Fatalf("validation error = %v, want error %t", err, tc.wantError)
			}
		})
	}
}

func TestBuiltTransactionMessageValidationRejectsNonCanonicalExtraCurrency(t *testing.T) {
	extra := cell.NewDict(32)
	err := extra.SetBuilder(
		cell.BeginCell().MustStoreUInt(1, 32).EndCell(),
		cell.BeginCell().MustStoreUInt(1, 5).MustStoreUInt(0, 8),
	)
	if err != nil {
		t.Fatal(err)
	}

	root, err := transactionInternalMessageToCell(&tlb.InternalMessage{
		IHRDisabled:     true,
		SrcAddr:         tonopsTestAddr,
		DstAddr:         tonopsTestAddr,
		Amount:          tlb.ZeroCoins,
		ExtraCurrencies: extra,
		IHRFee:          tlb.ZeroCoins,
		FwdFee:          tlb.ZeroCoins,
	}, transactionOutboundLayout{})
	if err != nil {
		t.Fatal(err)
	}
	if err = validateBuiltTransactionMessage(root); err == nil {
		t.Fatal("expected zero-valued extra currency encoding to be rejected")
	}
}
