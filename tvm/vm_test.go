package tvm

import (
	"encoding/hex"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"math/big"
	"testing"
)

func TestTVM_Execute(t *testing.T) {
	v := NewTVM()

	walletV3CodeBytes, _ := hex.DecodeString("b5ee9c720101010100710000deff0020dd2082014c97ba218201339cbab19f71b0ed44d0d31fd31f31d70bffe304e0a4f2608308d71820d31fd31fd31ff82313bbf263ed44d0d31fd31fd3ffd15132baf2a15144baf2a204f901541055f910f2a3f8009320d74a96d307d402fb00e8d101a4c8cb1fcb1fcbffc9ed54")
	code, _ := cell.FromBOC(walletV3CodeBytes)

	walletV3DataBytes, _ := hex.DecodeString("b5ee9c7201010101002a0000500000002a29a9a317f7a26c623ca2429ae80fbb17786b0b523ba71c1dbb13fbcdb8ded762a71cd867")
	data, _ := cell.FromBOC(walletV3DataBytes)

	s := vm.NewStack()
	_ = s.PushInt(big.NewInt(85143))

	err := v.Execute(code, data, tuple.Tuple{}, vm.Gas{}, s)
	if err != nil {
		t.Fatal(err)
	}

	t.Log(s.String())
}
