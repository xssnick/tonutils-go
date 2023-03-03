package tlb

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
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

func Test_MethodNameHash(t *testing.T) {
	hash := MethodNameHash("seqno")
	if hash != 85143 {
		t.Fatal("bad name hash:", hash)
	}
}

func TestAccountStatus_LoadFromCell(t *testing.T) {
	statusActive := 0b10
	statusNone := 0b11
	statusFrozen := 0b01
	statusUninit := 0b00

	tests := []struct {
		name   string
		status int
		want   string
	}{
		{"active acc case", statusActive, "ACTIVE"},
		{"frozen acc case", statusFrozen, "FROZEN"},
		{"not existing acc case", statusNone, "NON_EXIST"},
		{"uninit acc case", statusUninit, "UNINIT"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testAcc := cell.BeginCell()
			err := testAcc.StoreInt(int64(test.status), 2)
			if err != nil {
				t.Fatal(err)
			}
			var accStatus AccountStatus
			err = accStatus.LoadFromCell(testAcc.EndCell().BeginParse())
			if err != nil {
				t.Fatal(err)
			}
			fmt.Println(string(accStatus), test.want)
			if string(accStatus) != test.want {
				t.Errorf("got status %s, want status %s", accStatus, test.want)
			}
		})
	}
}

func TestAccount_HasGetMethod(t *testing.T) {
	contractCode, err := hex.DecodeString("b5ee9c724102140100021f000114ff00f4a413f4bcf2c80b0102016202030202cd04050201200e0f04e7d10638048adf000e8698180b8d848adf07d201800e98fe99ff6a2687d20699fea6a6a184108349e9ca829405d47141baf8280e8410854658056b84008646582a802e78b127d010a65b509e58fe59f80e78b64c0207d80701b28b9e382f970c892e000f18112e001718112e001f181181981e0024060708090201200a0b00603502d33f5313bbf2e1925313ba01fa00d43028103459f0068e1201a44343c85005cf1613cb3fccccccc9ed54925f05e200a6357003d4308e378040f4966fa5208e2906a4208100fabe93f2c18fde81019321a05325bbf2f402fa00d43022544b30f00623ba9302a402de04926c21e2b3e6303250444313c85005cf1613cb3fccccccc9ed54002c323401fa40304144c85005cf1613cb3fccccccc9ed54003c8e15d4d43010344130c85005cf1613cb3fccccccc9ed54e05f04840ff2f00201200c0d003d45af0047021f005778018c8cb0558cf165004fa0213cb6b12ccccc971fb008002d007232cffe0a33c5b25c083232c044fd003d0032c03260001b3e401d3232c084b281f2fff2742002012010110025bc82df6a2687d20699fea6a6a182de86a182c40043b8b5d31ed44d0fa40d33fd4d4d43010245f04d0d431d430d071c8cb0701cf16ccc980201201213002fb5dafda89a1f481a67fa9a9a860d883a1a61fa61ff480610002db4f47da89a1f481a67fa9a9a86028be09e008e003e00b01a500c6e")
	if err != nil {
		t.Fatal(err)
	}
	testCode, err := cell.FromBOC(contractCode)
	if err != nil {
		t.Fatal(err)
	}
	testAcc := Account{
		IsActive:   false,
		State:      nil,
		Data:       nil,
		Code:       testCode,
		LastTxLT:   0,
		LastTxHash: nil,
	}

	res := testAcc.HasGetMethod("get_nft_content")
	if res != true {
		t.Errorf("don`t found existing method")
	}
	res = testAcc.HasGetMethod("get_lol_content")
	if res == true {
		t.Errorf("found not existing method")
	}
}

func TestAccountStatus_ToCell(t *testing.T) {
	statusActive := AccountStatusActive
	statusNone := AccountStatusNonExist
	statusFrozen := AccountStatusFrozen
	statusUninit := AccountStatusUninit

	tests := []struct {
		name   string
		status AccountStatus
		want   uint64
	}{
		{"active acc case", AccountStatus(statusActive), 0b10},
		{"frozen acc case", AccountStatus(statusFrozen), 0b01},
		{"not existing acc case", AccountStatus(statusNone), 0b11},
		{"uninit acc case", AccountStatus(statusUninit), 0b00},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testStatus := test.status
			_cell, err := testStatus.ToCell()
			if err != nil {
				t.Fatal(err)
			}
			res, err := _cell.BeginParse().LoadUInt(2)
			if err != nil {
				t.Fatal(err)
			}

			if res != test.want {
				t.Errorf("got status %d, want status %d", res, test.want)
			}
		})
	}
}

func TestAccountStorage_LoadFromCell(t *testing.T) {
	t.Run("frozen acc case", func(t *testing.T) {
		b := cell.BeginCell()
		b.StoreUInt(123, 64)
		b.StoreBigCoins(big.NewInt(123))
		b.StoreBoolBit(false)
		b.StoreBoolBit(false)
		b.StoreBoolBit(true)
		c := 256
		sl := make([]byte, c)
		_, err := rand.Read(sl)
		if err != nil {
			t.Fatal(err)
		}
		b.StoreSlice(sl, 256)

		var tStor AccountStorage
		err = tStor.LoadFromCell(b.EndCell().BeginParse())
		if err != nil {
			t.Fatal(err)
		}
		if tStor.Status != AccountStatusFrozen {
			t.Errorf("bad acc status")
		}
	})

	t.Run("uinit acc case", func(t *testing.T) {
		b := cell.BeginCell()
		b.StoreUInt(123, 64)
		b.StoreBigCoins(big.NewInt(123))
		b.StoreBoolBit(false)
		b.StoreBoolBit(false)
		b.StoreBoolBit(false)
		c := 256
		sl := make([]byte, c)
		_, err := rand.Read(sl)
		if err != nil {
			t.Fatal(err)
		}
		b.StoreSlice(sl, 256)

		var tStor AccountStorage
		err = tStor.LoadFromCell(b.EndCell().BeginParse())
		if err != nil {
			t.Fatal(err)
		}
		if tStor.Status != AccountStatusUninit {
			t.Errorf("bad acc status")
		}
	})

}
