package tlb

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestWorkchainDescrLoadBasic(t *testing.T) {
	var descr WorkchainDescr
	if err := LoadFromCell(&descr, testBasicWorkchainDescrCell().MustBeginParse()); err != nil {
		t.Fatal(err)
	}

	fields := descr.Fields()
	if !fields.Basic || !fields.AcceptMsgs || !fields.Active {
		t.Fatalf("unexpected descriptor flags: basic=%t accept=%t active=%t", fields.Basic, fields.AcceptMsgs, fields.Active)
	}
	if !descr.ValidAddressLength(256) {
		t.Fatal("basic workchain should accept 256-bit addresses")
	}
	if descr.ValidAddressLength(255) {
		t.Fatal("basic workchain should reject non-256-bit addresses")
	}

	format, ok := fields.Format.(WorkchainFormatBasic)
	if !ok {
		t.Fatalf("unexpected format type %T", fields.Format)
	}
	if format.VMVersion != -1 || format.VMMode != 3 {
		t.Fatalf("unexpected basic format: %+v", format)
	}
}

func TestWorkchainDescrLoadExtended(t *testing.T) {
	var descr WorkchainDescr
	if err := LoadFromCell(&descr, testExtendedWorkchainDescrCell(0xa7).MustBeginParse()); err != nil {
		t.Fatal(err)
	}

	fields := descr.Fields()
	if fields.Basic || !fields.AcceptMsgs {
		t.Fatalf("unexpected descriptor flags: basic=%t accept=%t", fields.Basic, fields.AcceptMsgs)
	}
	if !descr.ValidAddressLength(64) || !descr.ValidAddressLength(128) || !descr.ValidAddressLength(256) {
		t.Fatal("extended workchain should accept min, stepped, and max address lengths")
	}
	if descr.ValidAddressLength(95) || descr.ValidAddressLength(257) {
		t.Fatal("extended workchain accepted invalid address length")
	}

	format, ok := fields.Format.(WorkchainFormatExtended)
	if !ok {
		t.Fatalf("unexpected format type %T", fields.Format)
	}
	if format.MinAddrLen != 64 || format.MaxAddrLen != 256 || format.AddrLenStep != 32 || format.WorkchainTypeID != 1 {
		t.Fatalf("unexpected extended format: %+v", format)
	}
}

func TestBlockchainConfigGetWorkchainDescr(t *testing.T) {
	workchains := cell.NewDict(32)
	if err := workchains.SetIntKey(big.NewInt(0), testExtendedWorkchainDescrCell(0xa6)); err != nil {
		t.Fatal(err)
	}

	workchainsCell, err := ToCell(&WorkchainsConfig{Workchains: workchains})
	if err != nil {
		t.Fatal(err)
	}

	cfgDict := cell.NewDict(32)
	param := cell.BeginCell().MustStoreRef(workchainsCell).EndCell()
	if err = cfgDict.SetIntKey(big.NewInt(int64(ConfigParamWorkchains)), param); err != nil {
		t.Fatal(err)
	}

	descr, err := (BlockchainConfig{Root: cfgDict.AsCell()}).GetWorkchainDescr(0)
	if err != nil {
		t.Fatal(err)
	}
	if descr == nil || descr.Fields().Basic || !descr.ValidAddressLength(128) {
		t.Fatalf("unexpected workchain descriptor: %+v", descr)
	}
}

func testBasicWorkchainDescrCell() *cell.Cell {
	return cell.BeginCell().
		MustStoreUInt(0xa6, 8).
		MustStoreUInt(123, 32).
		MustStoreUInt(0, 8).
		MustStoreUInt(0, 8).
		MustStoreUInt(4, 8).
		MustStoreBoolBit(true).
		MustStoreBoolBit(true).
		MustStoreBoolBit(true).
		MustStoreUInt(0, 13).
		MustStoreSlice(make([]byte, 32), 256).
		MustStoreSlice(make([]byte, 32), 256).
		MustStoreUInt(7, 32).
		MustStoreUInt(1, 4).
		MustStoreInt(-1, 32).
		MustStoreUInt(3, 64).
		EndCell()
}

func testExtendedWorkchainDescrCell(tag uint64) *cell.Cell {
	return cell.BeginCell().
		MustStoreUInt(tag, 8).
		MustStoreUInt(123, 32).
		MustStoreUInt(0, 8).
		MustStoreUInt(0, 8).
		MustStoreUInt(4, 8).
		MustStoreBoolBit(false).
		MustStoreBoolBit(true).
		MustStoreBoolBit(true).
		MustStoreUInt(0, 13).
		MustStoreSlice(make([]byte, 32), 256).
		MustStoreSlice(make([]byte, 32), 256).
		MustStoreUInt(7, 32).
		MustStoreUInt(0, 4).
		MustStoreUInt(64, 12).
		MustStoreUInt(256, 12).
		MustStoreUInt(32, 12).
		MustStoreUInt(1, 32).
		EndCell()
}
