package tlb

import (
	"encoding/hex"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestAccountState_LoadFromCell(t *testing.T) {
	accStateBOC, _ := hex.DecodeString("b5ee9c724101030100d700026fc00c419e2b8a3b6cd81acd3967dbbaf4442e1870e99eaf32278b7814a6ccaac5f802068148c314b1854000006735d812370d00764ce8d340010200deff0020dd2082014c97ba218201339cbab19f71b0ed44d0d31fd31f31d70bffe304e0a4f2608308d71820d31fd31fd31ff82313bbf263ed44d0d31fd31fd3ffd15132baf2a15144baf2a204f901541055f910f2a3f8009320d74a96d307d402fb00e8d101a4c8cb1fcb1fcbffc9ed5400500000000229a9a317d78e2ef9e6572eeaa3f206ae5c3dd4d00ddd2ffa771196dc0ab985fa84daf451c340d7fa")
	acc, err := cell.FromBOC(accStateBOC)
	if err != nil {
		t.Fatal(err)
		return
	}

	var as AccountState
	err = as.LoadFromCell(acc.BeginParse())
	if err != nil {
		t.Fatal(err)
		return
	}

	if !as.IsValid {
		t.Fatal("not valid")
		return
	}

	if as.Address.String() != "EQDEGeK4o7bNgazTln27r0RC4YcOmerzIni3gUpsyqxfgMWk" {
		t.Fatal("address not eq", as.Address)
		return
	}

	if as.Status != AccountStatusActive {
		t.Fatal("status not active", as.Status)
		return
	}

	if as.Balance.NanoTON().Uint64() != 31011747 {
		t.Fatal("balance not eq", as.Balance.NanoTON().String())
		return
	}

	if as.LastTransactionLT != 28370239000003 {
		t.Fatal("LastTransactionLT incorrect", as.LastTransactionLT)
		return
	}
}
