//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	localec "github.com/xssnick/tonutils-go/tvm/internal/secp256k1"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorTonOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}
	// This suite targets the subset of tonops that is both run_method-compatible
	// and currently stable against the official reference harness. Transaction-only
	// gas ops such as ACCEPT/SETGASLIMIT/COMMIT stay local-only. A few additional
	// tonops paths are intentionally excluded here because the harness exposes
	// known semantic mismatches that still need opcode/runtime fixes.

	configValue := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	configRoot := mustConfigDictCell(t, map[uint32]*cell.Cell{
		7: configValue,
	})
	version12Config := mustConfigDictCell(t, map[uint32]*cell.Cell{
		8: cell.BeginCell().
			MustStoreUInt(0xC4, 8).
			MustStoreUInt(12, 32).
			MustStoreUInt(0, 64).
			EndCell(),
	})
	version12RefCfg := &referenceGetMethodConfig{
		Address:    tonopsTestAddr,
		Now:        uint32(tonopsTestTime.Unix()),
		Balance:    uint64(tonopsTestBalance.Int64()),
		RandSeed:   tonopsTestSeed,
		ConfigRoot: version12Config,
	}
	feeC7 := feeTestC7(t)
	myCode := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	c7 := makeTonopsTestC7(t, tonopsTestC7Config{
		ConfigRoot:    configRoot,
		MyCode:        myCode,
		StorageFees:   tonopsTestStorageFees,
		IncomingValue: *tuple.NewTuple(big.NewInt(555), cell.BeginCell().MustStoreUInt(0xCD, 8).EndCell()),
		Balance:       *tuple.NewTuple(big.NewInt(123456789), cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()),
		Globals: map[int]any{
			1: int64(111),
			2: int64(222),
		},
		ExtraParams: map[int]any{
			2: int64(42),
		},
	})
	sendMsgCell, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     tonopsTestAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.MustFromNano(big.NewInt(1000), 9),
		IHRFee:      tlb.MustFromNano(big.NewInt(0), 9),
		FwdFee:      tlb.MustFromNano(big.NewInt(0), 9),
		Body:        cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build sendmsg test message: %v", err)
	}
	dataSizeLeaf := cell.BeginCell().MustStoreUInt(1, 1).EndCell()
	dataSizeRoot := cell.BeginCell().MustStoreRef(dataSizeLeaf).MustStoreRef(dataSizeLeaf).EndCell()
	stdAddrSlice := cell.BeginCell().MustStoreAddr(tonopsTestAddr).ToSlice()
	addrNoneTail := cell.BeginCell().MustStoreUInt(0, 2).MustStoreUInt(0xA, 4).ToSlice()
	invalidAnycast := cell.BeginCell().
		MustStoreUInt(0b10, 2).
		MustStoreBoolBit(true).
		MustStoreUInt(31, 5).
		MustStoreSlice(bytes.Repeat([]byte{0xFF}, 4), 31).
		MustStoreInt(int64(tonopsTestAddr.Workchain()), 8).
		MustStoreSlice(tonopsTestAddr.Data(), 256).
		ToSlice()

	ecrecoverHash := bytes.Repeat([]byte{0x42}, 32)
	ecrecoverV, ecrecoverR, ecrecoverS, _, ok := localec.SignRecoverable(bytes.Repeat([]byte{0x31}, 32), bytes.Repeat([]byte{0x57}, 32), ecrecoverHash)
	if !ok {
		t.Fatal("failed to build secp256k1 recovery fixture")
	}
	_, _, _, xonlyBasePub, ok := localec.SignRecoverable(bytes.Repeat([]byte{0x41}, 32), bytes.Repeat([]byte{0x67}, 32), bytes.Repeat([]byte{0x22}, 32))
	if !ok {
		t.Fatal("failed to build secp256k1 xonly fixture")
	}
	secpXOnlyKey := new(big.Int).SetBytes(xonlyBasePub[1:33])
	secpTooLargeTweak := new(big.Int).SetBytes(localec.CurveOrderBytes())

	p256Priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate p256 key: %v", err)
	}
	p256HashBytes := bytes.Repeat([]byte{0x55}, 32)
	p256HashDigest := sha256.Sum256(p256HashBytes)
	p256R, p256S, err := ecdsa.Sign(rand.Reader, p256Priv, p256HashDigest[:])
	if err != nil {
		t.Fatalf("failed to sign p256 hash fixture: %v", err)
	}
	p256SigU := make([]byte, 64)
	copy(p256SigU[32-len(p256R.Bytes()):32], p256R.Bytes())
	copy(p256SigU[64-len(p256S.Bytes()):64], p256S.Bytes())
	p256Pub := elliptic.MarshalCompressed(elliptic.P256(), p256Priv.PublicKey.X, p256Priv.PublicKey.Y)

	p256SliceData := []byte("p256-signed-slice")
	p256SliceDigest := sha256.Sum256(p256SliceData)
	p256SliceR, p256SliceS, err := ecdsa.Sign(rand.Reader, p256Priv, p256SliceDigest[:])
	if err != nil {
		t.Fatalf("failed to sign p256 slice fixture: %v", err)
	}
	p256SigS := make([]byte, 64)
	copy(p256SigS[32-len(p256SliceR.Bytes()):32], p256SliceR.Bytes())
	copy(p256SigS[64-len(p256SliceS.Bytes()):64], p256SliceS.Bytes())
	badP256Key := append([]byte{0x05}, bytes.Repeat([]byte{0x01}, 32)...)

	type testCase struct {
		name          string
		code          *cell.Cell
		stack         []any
		exit          int32
		c7            tuple.Tuple
		globalVersion int
		refCfg        *referenceGetMethodConfig
	}

	tests := []testCase{
		{
			name: "ecrecover_success",
			code: codeFromBuilders(t, funcsop.ECRECOVER().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(ecrecoverHash),
				int64(ecrecoverV),
				ecrecoverR,
				ecrecoverS,
			},
			exit: 0,
		},
		{
			name: "ecrecover_invalid_v",
			code: codeFromBuilders(t, funcsop.ECRECOVER().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(ecrecoverHash),
				int64(4),
				ecrecoverR,
				ecrecoverS,
			},
			exit: 0,
		},
		{
			name: "secp256k1_xonly_pubkey_tweak_add_success",
			code: codeFromBuilders(t, funcsop.SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Serialize()),
			stack: []any{
				secpXOnlyKey,
				int64(7),
			},
			exit: 0,
		},
		{
			name: "secp256k1_xonly_pubkey_tweak_add_invalid_key",
			code: codeFromBuilders(t, funcsop.SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(bytes.Repeat([]byte{0xFF}, 32)),
				int64(1),
			},
			exit: 0,
		},
		{
			name: "secp256k1_xonly_pubkey_tweak_add_tweak_ge_n",
			code: codeFromBuilders(t, funcsop.SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Serialize()),
			stack: []any{
				secpXOnlyKey,
				secpTooLargeTweak,
			},
			exit: 0,
		},
		{
			name: "p256_chksignu_success",
			code: codeFromBuilders(t, funcsop.P256_CHKSIGNU().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(p256HashBytes),
				cell.BeginCell().MustStoreSlice(p256SigU, 512).ToSlice(),
				cell.BeginCell().MustStoreSlice(p256Pub, 264).ToSlice(),
			},
			exit: 0,
		},
		{
			name: "p256_chksignu_bad_key",
			code: codeFromBuilders(t, funcsop.P256_CHKSIGNU().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(p256HashBytes),
				cell.BeginCell().MustStoreSlice(p256SigU, 512).ToSlice(),
				cell.BeginCell().MustStoreSlice(badP256Key, 264).ToSlice(),
			},
			exit: 0,
		},
		{
			name: "p256_chksigns_success",
			code: codeFromBuilders(t, funcsop.P256_CHKSIGNS().Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice(p256SliceData, uint(len(p256SliceData))*8).ToSlice(),
				cell.BeginCell().MustStoreSlice(p256SigS, 512).ToSlice(),
				cell.BeginCell().MustStoreSlice(p256Pub, 264).ToSlice(),
			},
			exit: 0,
		},
		{
			name: "p256_chksigns_unaligned_slice",
			code: codeFromBuilders(t, funcsop.P256_CHKSIGNS().Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreUInt(0x7F, 7).ToSlice(),
				cell.BeginCell().MustStoreSlice(p256SigS, 512).ToSlice(),
				cell.BeginCell().MustStoreSlice(p256Pub, 264).ToSlice(),
			},
			exit: vmerr.CodeCellUnderflow,
		},
		{
			name: "getparam_generic",
			code: codeFromBuilders(t, funcsop.GETPARAM(2).Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "blocklt",
			code: codeFromBuilders(t, funcsop.BLOCKLT().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "ltime",
			code: codeFromBuilders(t, funcsop.LTIME().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "randseed",
			code: codeFromBuilders(t, funcsop.RANDSEED().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "balance",
			code: codeFromBuilders(t, funcsop.BALANCE().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "myaddr",
			code: codeFromBuilders(t, funcsop.MYADDR().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "configroot",
			code: codeFromBuilders(t, funcsop.CONFIGROOT().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "mycode",
			code: codeFromBuilders(t, funcsop.MYCODE().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "incomingvalue",
			code: codeFromBuilders(t, funcsop.INCOMINGVALUE().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "storagefees",
			code: codeFromBuilders(t, funcsop.STORAGEFEES().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "configdict",
			code: codeFromBuilders(t, funcsop.CONFIGDICT().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name:  "configparam_hit",
			code:  codeFromBuilders(t, funcsop.CONFIGPARAM().Serialize()),
			stack: []any{int64(7)},
			exit:  0,
			c7:    c7,
		},
		{
			name:  "configparam_miss",
			code:  codeFromBuilders(t, funcsop.CONFIGPARAM().Serialize()),
			stack: []any{int64(8)},
			exit:  0,
			c7:    c7,
		},
		{
			name:  "configoptparam_hit",
			code:  codeFromBuilders(t, funcsop.CONFIGOPTPARAM().Serialize()),
			stack: []any{int64(7)},
			exit:  0,
			c7:    c7,
		},
		{
			name:  "configoptparam_miss",
			code:  codeFromBuilders(t, funcsop.CONFIGOPTPARAM().Serialize()),
			stack: []any{int64(8)},
			exit:  0,
			c7:    c7,
		},
		{
			name: "getglob_fixed",
			code: codeFromBuilders(t, funcsop.GETGLOB(1).Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name:  "getglobvar",
			code:  codeFromBuilders(t, funcsop.GETGLOBVAR().Serialize()),
			stack: []any{int64(2)},
			exit:  0,
			c7:    c7,
		},
		{
			name:  "setglob_fixed_then_get",
			code:  codeFromBuilders(t, funcsop.SETGLOB(2).Serialize(), funcsop.GETGLOB(2).Serialize()),
			stack: []any{int64(456)},
			exit:  0,
			c7:    c7,
		},
		{
			name:  "setglobvar_then_get",
			code:  codeFromBuilders(t, funcsop.SETGLOBVAR().Serialize(), stackop.PUSHINT(big.NewInt(5)).Serialize(), funcsop.GETGLOBVAR().Serialize()),
			stack: []any{int64(999), int64(5)},
			exit:  0,
			c7:    c7,
		},
		{
			name:  "setglobvar_nil_noop",
			code:  codeFromBuilders(t, funcsop.SETGLOBVAR().Serialize(), stackop.PUSHINT(big.NewInt(10)).Serialize(), funcsop.GETGLOBVAR().Serialize()),
			stack: []any{nil, int64(10)},
			exit:  0,
			c7:    c7,
		},
		{
			name: "prevblocksinfotuple",
			code: codeFromBuilders(t, funcsop.PREVBLOCKSINFOTUPLE().Serialize()),
			exit: 0,
			c7:   feeC7,
		},
		{
			name: "prevmcblocks",
			code: codeFromBuilders(t, funcsop.PREVMCBLOCKS().Serialize()),
			exit: 0,
			c7:   feeC7,
		},
		{
			name: "prevkeyblock",
			code: codeFromBuilders(t, funcsop.PREVKEYBLOCK().Serialize()),
			exit: 0,
			c7:   feeC7,
		},
		{
			name: "prevmcblocks_100",
			code: codeFromBuilders(t, funcsop.PREVMCBLOCKS_100().Serialize()),
			exit: 0,
			c7:   feeC7,
		},
		{
			name: "getprecompiledgas",
			code: codeFromBuilders(t, funcsop.GETPRECOMPILEDGAS().Serialize()),
			exit: 0,
			c7:   feeC7,
		},
		{
			name: "randu256",
			code: codeFromBuilders(t, funcsop.RANDU256().Serialize()),
			exit: 0,
			c7:   feeC7,
		},
		{
			name:  "rand",
			code:  codeFromBuilders(t, funcsop.RAND().Serialize()),
			stack: []any{int64(7)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "setrand",
			code:  codeFromBuilders(t, funcsop.SETRAND().Serialize(), funcsop.RANDSEED().Serialize()),
			stack: []any{big.NewInt(7)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "addrand",
			code:  codeFromBuilders(t, funcsop.ADDRAND().Serialize(), funcsop.RANDSEED().Serialize()),
			stack: []any{big.NewInt(7)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "getstoragefee",
			code:  codeFromBuilders(t, funcsop.GETSTORAGEFEE().Serialize()),
			stack: []any{int64(2), int64(3), int64(10), int64(0)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "getgasfee",
			code:  codeFromBuilders(t, funcsop.GETGASFEE().Serialize()),
			stack: []any{int64(250), int64(0)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "getforwardfee",
			code:  codeFromBuilders(t, funcsop.GETFORWARDFEE().Serialize()),
			stack: []any{int64(2), int64(8), int64(0)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "getoriginalfwdfee",
			code:  codeFromBuilders(t, funcsop.GETORIGINALFWDFEE().Serialize()),
			stack: []any{big.NewInt(3200), int64(0)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "getforwardfeesimple",
			code:  codeFromBuilders(t, funcsop.GETFORWARDFEESIMPLE().Serialize()),
			stack: []any{int64(2), int64(8), int64(0)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "getgasfeesimple",
			code:  codeFromBuilders(t, funcsop.GETGASFEESIMPLE().Serialize()),
			stack: []any{int64(250), int64(0)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "sha256u",
			code:  codeFromBuilders(t, funcsop.SHA256U().Serialize()),
			stack: []any{cell.BeginCell().MustStoreSlice([]byte("hello world"), 88).ToSlice()},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "hashext",
			code:  codeFromBuilders(t, funcsop.HASHEXT(0).Serialize()),
			stack: []any{cell.BeginCell().MustStoreSlice([]byte("hello world"), 88).ToSlice(), int64(1)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "cdatasize",
			code:  codeFromBuilders(t, funcsop.CDATASIZE().Serialize()),
			stack: []any{dataSizeRoot, int64(10)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "sdatasize",
			code:  codeFromBuilders(t, funcsop.SDATASIZE().Serialize()),
			stack: []any{dataSizeRoot.BeginParse(), int64(10)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "ldvarint16",
			code:  codeFromBuilders(t, funcsop.LDVARINT16().Serialize()),
			stack: []any{cell.BeginCell().MustStoreUInt(1, 4).MustStoreInt(-17, 8).ToSlice()},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "ldvaruint32",
			code:  codeFromBuilders(t, funcsop.LDVARUINT32().Serialize()),
			stack: []any{cell.BeginCell().MustStoreUInt(1, 5).MustStoreUInt(17, 8).ToSlice()},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "ldvarint32",
			code:  codeFromBuilders(t, funcsop.LDVARINT32().Serialize()),
			stack: []any{cell.BeginCell().MustStoreUInt(1, 5).MustStoreInt(-17, 8).ToSlice()},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "stvarint16",
			code:  codeFromBuilders(t, funcsop.STVARINT16().Serialize()),
			stack: []any{cell.BeginCell(), int64(-17)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "stvarint32",
			code:  codeFromBuilders(t, funcsop.STVARINT32().Serialize()),
			stack: []any{cell.BeginCell(), int64(-17)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "ldmsgaddr_ok",
			code:  codeFromBuilders(t, funcsop.LDMSGADDR().Serialize()),
			stack: []any{stdAddrSlice},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "ldmsgaddrq_ok",
			code:  codeFromBuilders(t, funcsop.LDMSGADDRQ().Serialize()),
			stack: []any{stdAddrSlice},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "ldmsgaddrq_fail",
			code:  codeFromBuilders(t, funcsop.LDMSGADDRQ().Serialize()),
			stack: []any{cell.BeginCell().MustStoreUInt(0b11, 2).ToSlice()},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "parsemsgaddr",
			code:  codeFromBuilders(t, funcsop.PARSEMSGADDR().Serialize()),
			stack: []any{stdAddrSlice},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "parsemsgaddrq_invalid_anycast",
			code:  codeFromBuilders(t, funcsop.PARSEMSGADDRQ().Serialize()),
			stack: []any{invalidAnycast},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "rewritestdaddr",
			code:  codeFromBuilders(t, funcsop.REWRITESTDADDR().Serialize()),
			stack: []any{stdAddrSlice},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "rewritestdaddrq",
			code:  codeFromBuilders(t, funcsop.REWRITESTDADDRQ().Serialize()),
			stack: []any{stdAddrSlice},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "rewritevaraddr",
			code:  codeFromBuilders(t, funcsop.REWRITEVARADDR().Serialize()),
			stack: []any{stdAddrSlice},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "rewritevaraddrq",
			code:  codeFromBuilders(t, funcsop.REWRITEVARADDRQ().Serialize()),
			stack: []any{stdAddrSlice},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:          "ldstdaddr",
			code:          codeFromBuilders(t, funcsop.LDSTDADDR().Serialize()),
			stack:         []any{stdAddrSlice},
			exit:          0,
			c7:            feeC7,
			globalVersion: 12,
			refCfg:        version12RefCfg,
		},
		{
			name:          "ldstdaddrq",
			code:          codeFromBuilders(t, funcsop.LDSTDADDRQ().Serialize()),
			stack:         []any{stdAddrSlice},
			exit:          0,
			c7:            feeC7,
			globalVersion: 12,
			refCfg:        version12RefCfg,
		},
		{
			name:          "ldoptstdaddr_none",
			code:          codeFromBuilders(t, funcsop.LDOPTSTDADDR().Serialize()),
			stack:         []any{addrNoneTail},
			exit:          0,
			c7:            feeC7,
			globalVersion: 12,
			refCfg:        version12RefCfg,
		},
		{
			name:          "ldoptstdaddrq_none",
			code:          codeFromBuilders(t, funcsop.LDOPTSTDADDRQ().Serialize()),
			stack:         []any{addrNoneTail},
			exit:          0,
			c7:            feeC7,
			globalVersion: 12,
			refCfg:        version12RefCfg,
		},
		{
			name:          "ststdaddr",
			code:          codeFromBuilders(t, funcsop.STSTDADDR().Serialize()),
			stack:         []any{stdAddrSlice, cell.BeginCell()},
			exit:          0,
			c7:            feeC7,
			globalVersion: 12,
			refCfg:        version12RefCfg,
		},
		{
			name:          "ststdaddrq",
			code:          codeFromBuilders(t, funcsop.STSTDADDRQ().Serialize()),
			stack:         []any{stdAddrSlice, cell.BeginCell()},
			exit:          0,
			c7:            feeC7,
			globalVersion: 12,
			refCfg:        version12RefCfg,
		},
		{
			name:          "stoptstdaddr_none",
			code:          codeFromBuilders(t, funcsop.STOPTSTDADDR().Serialize()),
			stack:         []any{nil, cell.BeginCell()},
			exit:          0,
			c7:            feeC7,
			globalVersion: 12,
			refCfg:        version12RefCfg,
		},
		{
			name:          "stoptstdaddrq_none",
			code:          codeFromBuilders(t, funcsop.STOPTSTDADDRQ().Serialize()),
			stack:         []any{nil, cell.BeginCell()},
			exit:          0,
			c7:            feeC7,
			globalVersion: 12,
			refCfg:        version12RefCfg,
		},
		{
			name:  "rawreserve",
			code:  codeFromBuilders(t, funcsop.RAWRESERVE().Serialize()),
			stack: []any{big.NewInt(777), int64(0)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "rawreservex",
			code:  codeFromBuilders(t, funcsop.RAWRESERVEX().Serialize()),
			stack: []any{big.NewInt(777), nil, int64(0)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "setcode",
			code:  codeFromBuilders(t, funcsop.SETCODE().Serialize()),
			stack: []any{sendMsgCell},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "setlibcode",
			code:  codeFromBuilders(t, funcsop.SETLIBCODE().Serialize()),
			stack: []any{sendMsgCell, int64(1)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "changelib",
			code:  codeFromBuilders(t, funcsop.CHANGELIB().Serialize()),
			stack: []any{big.NewInt(1), int64(1)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "sendmsg_fee_only",
			code:  codeFromBuilders(t, funcsop.SENDMSG().Serialize()),
			stack: []any{sendMsgCell, int64(1024)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "sendmsg_send",
			code:  codeFromBuilders(t, funcsop.SENDMSG().Serialize()),
			stack: []any{sendMsgCell, int64(1)},
			exit:  0,
			c7:    feeC7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := prependRawMethodDrop(tt.code)
			goStack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build go stack: %v", err)
			}
			refStack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build reference stack: %v", err)
			}

			goVersion := tt.globalVersion
			if goVersion == 0 {
				goVersion = vm.DefaultGlobalVersion
			}
			goRes, err := runGoCrossCodeWithVersion(code, cell.BeginCell().EndCell(), tt.c7, goStack, goVersion)
			if err != nil {
				t.Fatalf("go tvm execution failed: %v", err)
			}
			var refRes *crossRunResult
			if tt.refCfg != nil {
				refRes, err = runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, *tt.refCfg)
			} else {
				refRes, err = runReferenceCrossCode(code, cell.BeginCell().EndCell(), tt.c7, refStack)
			}
			if err != nil {
				t.Fatalf("reference tvm execution failed: %v", err)
			}

			if goRes.exitCode != tt.exit || refRes.exitCode != tt.exit {
				t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, tt.exit)
			}
			if goRes.exitCode != refRes.exitCode {
				t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
			}
			if goRes.gasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
			}

			goStackCell, err := normalizeStackCell(goRes.stack)
			if err != nil {
				t.Fatalf("failed to normalize go stack: %v", err)
			}
			refStackCell, err := normalizeStackCell(refRes.stack)
			if err != nil {
				t.Fatalf("failed to normalize reference stack: %v", err)
			}
			if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
				t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
			}
		})
	}
}
