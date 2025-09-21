package tvm

import (
	"encoding/hex"
	"errors"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
	"math/big"
	"testing"
	"time"
)

var MainContractCode = func() *cell.Cell {
	contractCodeBytes, _ := hex.DecodeString("b5ee9c724102180100019a000114ff00f4a413f4bcf2c80b0102016202070202ce0306020120040500691b088831c02456f8007434c0cc1caa42644c383c0074c7f4cfcc4060841fa1d93beea6f4c7cc3e1080683e18bc00b80c2103fcbc20001d3b513434c7c07e1874c7c07e18b460001d4c8f84101cb1ff84201cb1fc9ed548020120080f020120090e0201200a0b000db7203e003f08300201200c0d0047b2e9c160c235c61981fe44407e08efac3ca600fe800c1480aed4cc6eec3ca696284068200057b057bb68bb7efb507b50fb513b517b51e556dc76cc4c3b59fb597b593b58fb5864e936cc7b507b7c407cbfe0002fb829a708e10709320c1059402a402a4e830a420c204e630802012010110099bbfe470ed41ed43ed44ed45ed47935b8064ed67ed65ed64ed63ed618e28758e237a9320c2008e18a5738e1202a420c20524c200b092f237de02a520c101e630e830fe00e431ed41edf101f2ff8020148121702012013140021ad1cbacdb84b80d200d2106102731872400201201516000caad0f001f8420022aacd759c709320c1059401a401a4e830e4000db3aedd646939209e25fbf5")
	code, _ := cell.FromBOC(contractCodeBytes)
	return code
}()

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

func TestTVM_ExecuteJetton(t *testing.T) {
	v := NewTVM()

	jettonCodeBytes, _ := hex.DecodeString("b5ee9c72010214010003b7000114ff00f4a413f4bcf2c80b0102016202030202ca0405020120121302012006070201d2101101ddd0831c02497c138007434c0c05c6c2544d7c0fc08783e903e900c7e800c5c75c87e800c7e800c1cea6d0000b4c7e08403e29fa954882ea54c4d167c07b8208405e3514654882ea58c511100fc07f80d60841657c1ef14842ea50c167c08381b08a0842cf6ecbe2eb8c096e103fcbc208020120090a0060ed44d0d300fa00fa40fa40d430345144c705f28d048040d721d30030443404c8cb005003fa0201cf1601cf16ccc9ed540011bdf48860e175e5c29b0201f40b0c01f700f4cffe803e90087c05bb513434c03e803e903e90350c093c96d44de8548af1c17cb8b04a70bffcb8b0950d549c15180104d50541413232c01400fe808073c58073c5b332487232c044fd0004bd0032c032483e401c1d3232c0b281f2fff274017e903d010c7e800835d270803cb8b11de0063232c1540273c59c200d01f33b513434c03e803e903e90350c0274cffe80145468017e903e9014d731c1551cdb9c15180104d50541413232c01400fe808073c58073c5b332487232c044fd0004bd0032c0327e401c1d3232c0b281f2fff2741403b1c1476c7cb8b0c2fe80146e6860822625a020822625a004ad822860823938702806684a200e00c2fa0218cb6b13cc8210178d4519c8cb1f1acb3f5008fa0222cf165007cf1626fa025004cf16c95006cc2491729171e25009a814a08208e4e1c0aa008208989680a0a015bcf2e2c505c98040fb00504404c8cb005003fa0201cf1601cf16ccc9ed5401f68e38528aa019a182107362d09cc8cb1f5230cb3f58fa025008cf165008cf16c9718010c8cb0524cf165007fa0216cb6a15ccc971fb001035103497104a1039385f04e226d70b01c30024c200b08e238210d53276db708010c8cb055009cf165005fa0217cb6a13cb1f13cb3fc972fb0050039410266c32e24003040f002404c8cb005003fa0201cf1601cf16ccc9ed5400e53b513434c03e803e903e90350c0234cffe803e900c1454685492b1c17cb8b04a30bffcb8b0a0823938702a8005e805ef3cb8b0e0841ef765f7b232c7c5b2cfd4013e8088f3c58073c5b25c60063232c14973c59c3e80b2dab33260103ec01409013232c01400fe808073c58073c5b3327b5520008f200835c87b513434c03e803e903e90350c0174c7e08405e3514654882ea0841ef765f784ee84ac7cb8b174cfcc7e800c04e800d409013232c01400fe808073c58073c5b3327b55200023bfd8176a26869807d007d207d206a18360a40023be1b576a26869807d007d207d206a182f824")
	code, _ := cell.FromBOC(jettonCodeBytes)

	jettonDataBytes, _ := hex.DecodeString("b5ee9c720102150100040200018f21dcd65004001e5bddddeed8df4a41249e4659ad2f258fa34d2219ac52c6f12e4b71e2d145618005401b901d3598d73c65306697791a78e2bdd28b1d06508007239279ff2b33df30010114ff00f4a413f4bcf2c80b0202016203040202ca050602012013140201200708008fd600835c87b513434c03e803e903e90350c0174c7e08405e3514654882ea0841ef765f784ee84ac7cb8b174cfcc7e800c04e800d409013232c01400fe808073c58073c5b3327b55201ddd0831c02497c138007434c0c05c6c2544d7c0fc08383e903e900c7e800c5c75c87e800c7e800c1cea6d0000b4c7e08403e29fa954882ea54c4d167c0778208405e3514654882ea58c511100fc07b80d60841657c1ef14842ea50c167c07f81b08a0842cf6ecbe2eb8c096e103fcbc2090201200a0b0060ed44d0d300fa00fa40fa40d430345144c705f28d048040d721d30030443404c8cb005003fa0201cf1601cf16ccc9ed540011bdf48860e175e5c29b0201580c0d01f7503d33ffa00fa4021f016ed44d0d300fa00fa40fa40d43024f25b5137a1522bc705f2e2c129c2fff2e2c2543552705460041354150504c8cb005003fa0201cf1601cf16ccc921c8cb0113f40012f400cb00c920f9007074c8cb02ca07cbffc9d005fa40f40431fa0020d749c200f2e2c4778018c8cb055009cf167080e0201200f1000c2fa0218cb6b13cc8210178d4519c8cb1f1acb3f5008fa0222cf165007cf1626fa025004cf16c95006cc2491729171e25009a814a08208e4e1c0aa008208989680a0a015bcf2e2c505c98040fb00504404c8cb005003fa0201cf1601cf16ccc9ed5401f33b513434c03e803e903e90350c0274cffe80145468017e903e9014d731c1551cdb9c15180104d50541413232c01400fe808073c58073c5b332487232c044fd0004bd0032c0327e401c1d3232c0b281f2fff2741403b1c1476c7cb8b0c2fe80146e6860822625a020822625a004ad822860823938702806684a201100e53b513434c03e803e903e90350c0234cffe803e900c1454685492b1c17cb8b04a30bffcb8b0a0823938702a8005e805ef3cb8b0e0841ef765f7b232c7c5b2cfd4013e8088f3c58073c5b25c60063232c14973c59c3e80b2dab33260103ec01409013232c01400fe808073c58073c5b3327b552001f68e38528aa019a182107362d09cc8cb1f5230cb3f58fa025008cf165008cf16c9718010c8cb0524cf165007fa0216cb6a15ccc971fb001035103497104a1039385f04e226d70b01c30024c200b08e238210d53276db708010c8cb055009cf165005fa0217cb6a13cb1f13cb3fc972fb0050039410266c32e240030412002404c8cb005003fa0201cf1601cf16ccc9ed540023bfd8176a26869807d007d207d206a18360a40023be1b576a26869807d007d207d206a182f824")
	data, _ := cell.FromBOC(jettonDataBytes)

	s := vm.NewStack()
	_ = s.PushInt(big.NewInt(115562))

	err := v.Execute(code, data, tuple.Tuple{}, vm.Gas{}, s)
	if err != nil {
		t.Fatal(err)
	}

	t.Log(s.String())
}

func TestTVM_ExecuteTvmTests(t *testing.T) {
	v := NewTVM()

	contractCodeBytes, _ := hex.DecodeString("b5ee9c7241010c0100bf000114ff00f4a413f4bcf2c80b0102016202070202ce0306020120040500691b088831c02456f8007434c0cc1caa42644c383c0074c7f4cfcc4060841fa1d93beea6f4c7cc3e1080683e18bc00b80c2103fcbc20001d3b513434c7c07e1874c7c07e18b460001d4c8f84101cb1ff84201cb1fc9ed548020120080b020148090a000db7203e003f08300057b62bddb45dbf7da83da87da89da8bda8f2ab6e3b66261dacfdacbdac9dac7dac32749b663da83dbe203e5ff0000dbe5687800fc2144543bc0e")
	code, _ := cell.FromBOC(contractCodeBytes)

	contractDataBytes, _ := hex.DecodeString("b5ee9c720102150100040200018f21dcd65004001e5bddddeed8df4a41249e4659ad2f258fa34d2219ac52c6f12e4b71e2d145618005401b901d3598d73c65306697791a78e2bdd28b1d06508007239279ff2b33df30010114ff00f4a413f4bcf2c80b0202016203040202ca050602012013140201200708008fd600835c87b513434c03e803e903e90350c0174c7e08405e3514654882ea0841ef765f784ee84ac7cb8b174cfcc7e800c04e800d409013232c01400fe808073c58073c5b3327b55201ddd0831c02497c138007434c0c05c6c2544d7c0fc08383e903e900c7e800c5c75c87e800c7e800c1cea6d0000b4c7e08403e29fa954882ea54c4d167c0778208405e3514654882ea58c511100fc07b80d60841657c1ef14842ea50c167c07f81b08a0842cf6ecbe2eb8c096e103fcbc2090201200a0b0060ed44d0d300fa00fa40fa40d430345144c705f28d048040d721d30030443404c8cb005003fa0201cf1601cf16ccc9ed540011bdf48860e175e5c29b0201580c0d01f7503d33ffa00fa4021f016ed44d0d300fa00fa40fa40d43024f25b5137a1522bc705f2e2c129c2fff2e2c2543552705460041354150504c8cb005003fa0201cf1601cf16ccc921c8cb0113f40012f400cb00c920f9007074c8cb02ca07cbffc9d005fa40f40431fa0020d749c200f2e2c4778018c8cb055009cf167080e0201200f1000c2fa0218cb6b13cc8210178d4519c8cb1f1acb3f5008fa0222cf165007cf1626fa025004cf16c95006cc2491729171e25009a814a08208e4e1c0aa008208989680a0a015bcf2e2c505c98040fb00504404c8cb005003fa0201cf1601cf16ccc9ed5401f33b513434c03e803e903e90350c0274cffe80145468017e903e9014d731c1551cdb9c15180104d50541413232c01400fe808073c58073c5b332487232c044fd0004bd0032c0327e401c1d3232c0b281f2fff2741403b1c1476c7cb8b0c2fe80146e6860822625a020822625a004ad822860823938702806684a201100e53b513434c03e803e903e90350c0234cffe803e900c1454685492b1c17cb8b04a30bffcb8b0a0823938702a8005e805ef3cb8b0e0841ef765f7b232c7c5b2cfd4013e8088f3c58073c5b25c60063232c14973c59c3e80b2dab33260103ec01409013232c01400fe808073c58073c5b3327b552001f68e38528aa019a182107362d09cc8cb1f5230cb3f58fa025008cf165008cf16c9718010c8cb0524cf165007fa0216cb6a15ccc971fb001035103497104a1039385f04e226d70b01c30024c200b08e238210d53276db708010c8cb055009cf165005fa0217cb6a13cb1f13cb3fc972fb0050039410266c32e240030412002404c8cb005003fa0201cf1601cf16ccc9ed540023bfd8176a26869807d007d207d206a18360a40023be1b576a26869807d007d207d206a182f824")
	data, _ := cell.FromBOC(contractDataBytes)

	id := tlb.MethodNameHash("tryCatch")
	s := vm.NewStack()
	_ = s.PushInt(big.NewInt(2))
	_ = s.PushInt(big.NewInt(int64(id)))

	err := v.Execute(code, data, tuple.Tuple{}, vm.Gas{}, s)
	if err != nil {
		t.Fatal(err)
	}

	res, err := s.PopInt()
	if err != nil {
		t.Fatal(err)
	}

	if res.Int64() != 3 {
		t.Fatal("result is not 3:", res.Int64())
	}
}

func TestTVM_ExecuteTvmTestLoops(t *testing.T) {
	v := NewTVM()

	/*
		get repeatWhileUntil(x: int): int {
		    var result: int = 0;
		    try {
		        repeat (5) {
		            var v: int = 10;
		            while (v > 0) {
		                v -= 1;
		                var f: int = 3;
		                do {
		                    result += 1;
		                    if ((result > 5) & (x > 0)) {
		                        throw 55;
		                    }
							f -= 1;
		                } while (f > 0);
		            }
					dumpStack();
		        }
		    } catch {
		        result = 100;
		    }
		    return result;
		}
	*/

	s := vm.NewStack()
	_ = s.PushInt(big.NewInt(0))
	_ = s.PushInt(big.NewInt(int64(tlb.MethodNameHash("tryRepeatWhileUntil"))))

	tm := time.Now()
	err := v.Execute(MainContractCode, cell.BeginCell().EndCell(), tuple.Tuple{}, vm.Gas{}, s)
	if err != nil {
		t.Fatal(err)
	}
	println(">>>", time.Since(tm).String())

	res, err := s.PopInt()
	if err != nil {
		t.Fatal(err)
	}

	if res.Int64() != 150 {
		t.Fatal("result is not 150:", res.Int64())
	}
}

func TestTVM_ExecuteTvmTestSimpleRepeat(t *testing.T) {
	v := NewTVM()

	s := vm.NewStack()
	_ = s.PushInt(big.NewInt(2))
	_ = s.PushInt(big.NewInt(int64(tlb.MethodNameHash("simpleRepeat"))))

	err := v.Execute(MainContractCode, cell.BeginCell().EndCell(), tuple.Tuple{}, vm.Gas{}, s)
	if err != nil {
		t.Fatal(err)
	}

	res, err := s.PopInt()
	if err != nil {
		t.Fatal(err)
	}

	if res.Int64() != 7 {
		t.Fatal("result is not 7:", res.Int64())
	}
}

func TestTVM_ExecuteTvmTestSimpleRepeatWhile(t *testing.T) {
	v := NewTVM()

	s := vm.NewStack()
	_ = s.PushInt(big.NewInt(2))
	_ = s.PushInt(big.NewInt(int64(tlb.MethodNameHash("simpleRepeatWhile"))))

	err := v.Execute(MainContractCode, cell.BeginCell().EndCell(), tuple.Tuple{}, vm.Gas{}, s)
	if err != nil {
		t.Fatal(err)
	}

	res, err := s.PopInt()
	if err != nil {
		t.Fatal(err)
	}

	if res.Int64() != 27 {
		t.Fatal("result is not 7:", res.Int64())
	}
}

func TestTVM_ExecuteTvmTestSimpleUntilWhile(t *testing.T) {
	v := NewTVM()

	s := vm.NewStack()
	_ = s.PushInt(big.NewInt(2))
	_ = s.PushInt(big.NewInt(int64(tlb.MethodNameHash("simpleUntilWhile"))))

	err := v.Execute(MainContractCode, cell.BeginCell().EndCell(), tuple.Tuple{}, vm.Gas{}, s)
	if err != nil {
		t.Fatal(err)
	}

	res, err := s.PopInt()
	if err != nil {
		t.Fatal(err)
	}

	if res.Int64() != 27 {
		t.Fatal("result is not 7:", res.Int64())
	}
}

func TestTVM_ExecuteTvmTestSimpleRepeatUntil(t *testing.T) {
	v := NewTVM()

	s := vm.NewStack()
	_ = s.PushInt(big.NewInt(2))
	_ = s.PushInt(big.NewInt(int64(tlb.MethodNameHash("simpleRepeatUntil"))))

	err := v.Execute(MainContractCode, cell.BeginCell().EndCell(), tuple.Tuple{}, vm.Gas{}, s)
	if err != nil {
		t.Fatal(err)
	}

	res, err := s.PopInt()
	if err != nil {
		t.Fatal(err)
	}

	if res.Int64() != 27 {
		t.Fatal("result is not 7:", res.Int64())
	}
}

func TestTVM_SuperContract(t *testing.T) {
	type args struct {
		name   string
		input  int64
		result int64
	}
	tests := []struct {
		args    args
		wantErr int64
	}{
		{
			args: args{
				name:   "simpleRepeatUntil",
				input:  2,
				result: 27,
			},
			wantErr: 0,
		},
		{
			args: args{
				name:   "simpleUntilWhile",
				input:  3,
				result: 28,
			},
			wantErr: 0,
		},
		{
			args: args{
				name:   "simpleRepeatWhile",
				input:  2,
				result: 27,
			},
			wantErr: 0,
		},
		{
			args: args{
				name:   "simpleRepeat",
				input:  2,
				result: 7,
			},
			wantErr: 0,
		},
		{
			args: args{
				name:   "tryRepeatWhileUntil",
				input:  0,
				result: 150,
			},
			wantErr: 0,
		},
		{
			args: args{
				name:   "tryRepeatWhileUntil",
				input:  1,
				result: 100,
			},
			wantErr: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.args.name, func(t *testing.T) {
			v := NewTVM()

			s := vm.NewStack()
			_ = s.PushInt(big.NewInt(tt.args.input))
			_ = s.PushInt(big.NewInt(int64(tlb.MethodNameHash(tt.args.name))))

			err := v.Execute(MainContractCode, cell.BeginCell().EndCell(), tuple.Tuple{}, vm.Gas{}, s)
			if err != nil {
				if tt.wantErr >= 1 {
					var e vmerr.VMError
					if errors.As(err, &e) {
						if e.Code == tt.wantErr {
							return
						}
					}
				}
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr >= 1 {
				t.Errorf("Execute() no error, wantErr %v", tt.wantErr)
			}

			res, err := s.PopInt()
			if err != nil {
				t.Errorf("PopInt() error = %v", err)
			}

			if res.Int64() != tt.args.result {
				t.Errorf("result %v, but want %v", res.Int64(), tt.args.result)
			}
		})
	}
}
