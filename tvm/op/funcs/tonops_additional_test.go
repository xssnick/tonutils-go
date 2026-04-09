package funcs

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	localec "github.com/xssnick/tonutils-go/tvm/internal/secp256k1"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func mustConfigRoot(t *testing.T, idx uint64, value *cell.Cell) *cell.Cell {
	t.Helper()

	dict := cell.NewDict(32)
	key := cell.BeginCell().MustStoreUInt(idx, 32).EndCell()
	if err := dict.SetRef(key, value); err != nil {
		t.Fatalf("failed to build config dict: %v", err)
	}
	return dict.AsCell()
}

func mustUnpackedConfig(t *testing.T, globalID int32) tuple.Tuple {
	t.Helper()

	cfg := tuple.NewTupleSized(2)
	if err := cfg.Set(1, cell.BeginCell().MustStoreUInt(uint64(uint32(globalID)), 32).ToSlice()); err != nil {
		t.Fatalf("failed to set unpacked config: %v", err)
	}
	return cfg
}

func mustSetFuncParam(t *testing.T, st *vm.State, idx int, val any) {
	t.Helper()

	paramsAny, err := st.Reg.C7.Index(0)
	if err != nil {
		t.Fatalf("failed to get params tuple: %v", err)
	}
	params, ok := paramsAny.(tuple.Tuple)
	if !ok {
		t.Fatalf("params slot has type %T", paramsAny)
	}
	if idx >= params.Len() {
		params.Resize(idx + 1)
	}
	if err = params.Set(idx, val); err != nil {
		t.Fatalf("failed to set param %d: %v", idx, err)
	}
	c7 := st.Reg.C7.Copy()
	if err = c7.Set(0, params); err != nil {
		t.Fatalf("failed to update c7 params slot: %v", err)
	}
	if err = st.SetC7(c7); err != nil {
		t.Fatalf("failed to apply c7 update: %v", err)
	}
}

func TestTonParamAliasesAndGlobals(t *testing.T) {
	configValue := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	configRoot := mustConfigRoot(t, 17, configValue)
	myAddr := cell.BeginCell().MustStoreUInt(0xCC, 8).ToSlice()
	myCode := cell.BeginCell().MustStoreUInt(0xDD, 8).EndCell()

	params := map[int]any{
		4:  int64(111),
		5:  uint64(222),
		6:  int64(333),
		7:  int64(444),
		8:  myAddr,
		9:  configRoot,
		10: myCode,
		11: int64(555),
		12: int64(666),
		14: mustUnpackedConfig(t, -7),
	}

	intAliases := []struct {
		name string
		op   vm.OP
		want int64
	}{
		{name: "BLOCKLT", op: BLOCKLT(), want: 111},
		{name: "LTIME", op: LTIME(), want: 222},
		{name: "RANDSEED", op: RANDSEED(), want: 333},
		{name: "BALANCE", op: BALANCE(), want: 444},
		{name: "INCOMINGVALUE", op: INCOMINGVALUE(), want: 555},
		{name: "STORAGEFEES", op: STORAGEFEES(), want: 666},
	}

	for _, tc := range intAliases {
		t.Run(tc.name, func(t *testing.T) {
			st := newFuncTestState(t, params)
			if err := tc.op.Interpret(st); err != nil {
				t.Fatalf("%s failed: %v", tc.name, err)
			}
			got, err := st.Stack.PopIntFinite()
			if err != nil {
				t.Fatalf("failed to pop %s result: %v", tc.name, err)
			}
			if got.Int64() != tc.want {
				t.Fatalf("%s = %v, want %d", tc.name, got, tc.want)
			}
		})
	}

	t.Run("MYADDR", func(t *testing.T) {
		st := newFuncTestState(t, params)
		if err := MYADDR().Interpret(st); err != nil {
			t.Fatalf("MYADDR failed: %v", err)
		}
		got, err := st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop MYADDR result: %v", err)
		}
		if !bytes.Equal(mustSliceData(t, got), mustSliceData(t, myAddr)) {
			t.Fatal("unexpected MYADDR result")
		}
	})

	t.Run("MYCODE", func(t *testing.T) {
		st := newFuncTestState(t, params)
		if err := MYCODE().Interpret(st); err != nil {
			t.Fatalf("MYCODE failed: %v", err)
		}
		got, err := st.Stack.PopCell()
		if err != nil {
			t.Fatalf("failed to pop MYCODE result: %v", err)
		}
		if !bytes.Equal(got.Hash(), myCode.Hash()) {
			t.Fatal("unexpected MYCODE result")
		}
	})

	t.Run("ConfigOps", func(t *testing.T) {
		st := newFuncTestState(t, params)
		st.InitForExecution()
		if err := CONFIGROOT().Interpret(st); err != nil {
			t.Fatalf("CONFIGROOT failed: %v", err)
		}
		gotRoot, err := st.Stack.PopCell()
		if err != nil {
			t.Fatalf("failed to pop CONFIGROOT result: %v", err)
		}
		if !bytes.Equal(gotRoot.Hash(), configRoot.Hash()) {
			t.Fatal("unexpected CONFIGROOT result")
		}

		if err := CONFIGDICT().Interpret(st); err != nil {
			t.Fatalf("CONFIGDICT failed: %v", err)
		}
		width, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop CONFIGDICT width: %v", err)
		}
		gotCfg, err := st.Stack.PopCell()
		if err != nil {
			t.Fatalf("failed to pop CONFIGDICT root: %v", err)
		}
		if width.Int64() != 32 || !bytes.Equal(gotCfg.Hash(), configRoot.Hash()) {
			t.Fatalf("unexpected CONFIGDICT output: width=%v", width)
		}

		if err := st.Stack.PushInt(big.NewInt(17)); err != nil {
			t.Fatalf("failed to push config key: %v", err)
		}
		if err := CONFIGPARAM().Interpret(st); err != nil {
			t.Fatalf("CONFIGPARAM failed: %v", err)
		}
		ok, err := st.Stack.PopBool()
		if err != nil {
			t.Fatalf("failed to pop CONFIGPARAM flag: %v", err)
		}
		gotCfgVal, err := st.Stack.PopCell()
		if err != nil {
			t.Fatalf("failed to pop CONFIGPARAM value: %v", err)
		}
		if !ok || !bytes.Equal(gotCfgVal.Hash(), configValue.Hash()) {
			t.Fatal("unexpected CONFIGPARAM result")
		}

		if err := st.Stack.PushInt(big.NewInt(18)); err != nil {
			t.Fatalf("failed to push missing config key: %v", err)
		}
		if err := CONFIGPARAM().Interpret(st); err != nil {
			t.Fatalf("CONFIGPARAM missing failed: %v", err)
		}
		ok, err = st.Stack.PopBool()
		if err != nil {
			t.Fatalf("failed to pop missing CONFIGPARAM flag: %v", err)
		}
		if ok {
			t.Fatal("CONFIGPARAM should report false for a missing key")
		}

		if err := st.Stack.PushInt(big.NewInt(17)); err != nil {
			t.Fatalf("failed to push CONFIGOPTPARAM key: %v", err)
		}
		if err := CONFIGOPTPARAM().Interpret(st); err != nil {
			t.Fatalf("CONFIGOPTPARAM failed: %v", err)
		}
		gotOptCfg, err := st.Stack.PopCell()
		if err != nil {
			t.Fatalf("failed to pop CONFIGOPTPARAM value: %v", err)
		}
		if !bytes.Equal(gotOptCfg.Hash(), configValue.Hash()) {
			t.Fatal("unexpected CONFIGOPTPARAM value")
		}

		if err := st.Stack.PushInt(big.NewInt(18)); err != nil {
			t.Fatalf("failed to push missing CONFIGOPTPARAM key: %v", err)
		}
		if err := CONFIGOPTPARAM().Interpret(st); err != nil {
			t.Fatalf("CONFIGOPTPARAM missing failed: %v", err)
		}
		gotAny, err := st.Stack.PopAny()
		if err != nil {
			t.Fatalf("failed to pop missing CONFIGOPTPARAM value: %v", err)
		}
		if gotAny != nil {
			t.Fatalf("missing CONFIGOPTPARAM should push nil, got %T", gotAny)
		}

		if err := GLOBALID().Interpret(st); err != nil {
			t.Fatalf("GLOBALID failed: %v", err)
		}
		gotGlobalID, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop GLOBALID: %v", err)
		}
		if gotGlobalID.Int64() != -7 {
			t.Fatalf("GLOBALID = %v, want -7", gotGlobalID)
		}
	})

	t.Run("GlobalSetGetOps", func(t *testing.T) {
		st := newFuncTestState(t, params)
		st.InitForExecution()

		if err := st.Stack.PushInt(big.NewInt(777)); err != nil {
			t.Fatalf("failed to push SETGLOBVAR value: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(5)); err != nil {
			t.Fatalf("failed to push SETGLOBVAR index: %v", err)
		}
		if err := SETGLOBVAR().Interpret(st); err != nil {
			t.Fatalf("SETGLOBVAR failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(5)); err != nil {
			t.Fatalf("failed to push GETGLOBVAR index: %v", err)
		}
		if err := GETGLOBVAR().Interpret(st); err != nil {
			t.Fatalf("GETGLOBVAR failed: %v", err)
		}
		got, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop GETGLOBVAR result: %v", err)
		}
		if got.Int64() != 777 {
			t.Fatalf("GETGLOBVAR = %v, want 777", got)
		}

		setGlob := SETGLOB(6)
		getGlob := GETGLOB(6)
		if err := st.Stack.PushInt(big.NewInt(888)); err != nil {
			t.Fatalf("failed to push SETGLOB value: %v", err)
		}
		if err := setGlob.Interpret(st); err != nil {
			t.Fatalf("SETGLOB failed: %v", err)
		}
		if err := getGlob.Interpret(st); err != nil {
			t.Fatalf("GETGLOB failed: %v", err)
		}
		got, err = st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop GETGLOB result: %v", err)
		}
		if got.Int64() != 888 {
			t.Fatalf("GETGLOB = %v, want 888", got)
		}
	})
}

func TestTonGasCommitAndSignatureOps(t *testing.T) {
	t.Run("GasAndCommitOps", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		st.Gas = vm.GasWithLimit(50)

		if err := st.ConsumeGas(7); err != nil {
			t.Fatalf("failed to consume gas: %v", err)
		}
		if err := GASCONSUMED().Interpret(st); err != nil {
			t.Fatalf("GASCONSUMED failed: %v", err)
		}
		gotUsed, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop GASCONSUMED value: %v", err)
		}
		if gotUsed.Int64() != 7 {
			t.Fatalf("GASCONSUMED = %v, want 7", gotUsed)
		}

		if err := st.Stack.PushInt(big.NewInt(20)); err != nil {
			t.Fatalf("failed to push gas limit: %v", err)
		}
		if err := SETGASLIMIT().Interpret(st); err != nil {
			t.Fatalf("SETGASLIMIT failed: %v", err)
		}
		if st.Gas.Limit != 20 {
			t.Fatalf("unexpected gas limit after SETGASLIMIT: %d", st.Gas.Limit)
		}

		if err := st.Stack.PushInt(new(big.Int).Lsh(big.NewInt(1), 70)); err != nil {
			t.Fatalf("failed to push huge gas limit: %v", err)
		}
		if err := SETGASLIMIT().Interpret(st); err != nil {
			t.Fatalf("SETGASLIMIT(huge) failed: %v", err)
		}
		if st.Gas.Limit != vm.GasInfinite {
			t.Fatalf("huge SETGASLIMIT should clamp to infinity, got %d", st.Gas.Limit)
		}

		st.Gas = vm.GasWithLimit(30)
		if err := st.ConsumeGas(11); err != nil {
			t.Fatalf("failed to consume gas: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(10)); err != nil {
			t.Fatalf("failed to push too-small limit: %v", err)
		}
		if err := SETGASLIMIT().Interpret(st); err == nil {
			t.Fatal("SETGASLIMIT should fail when the limit is below gas already used")
		}

		st.Gas = vm.GasWithLimit(15)
		if err := ACCEPT().Interpret(st); err != nil {
			t.Fatalf("ACCEPT failed: %v", err)
		}
		if st.Gas.Limit != vm.GasInfinite {
			t.Fatalf("ACCEPT should set infinite gas limit, got %d", st.Gas.Limit)
		}

		if err := COMMIT().Interpret(st); err != nil {
			t.Fatalf("COMMIT failed: %v", err)
		}
		if !st.Committed.Committed {
			t.Fatal("COMMIT should snapshot the current data/action cells")
		}
	})

	t.Run("Ed25519AndSecp256k1Ops", func(t *testing.T) {
		seed := bytes.Repeat([]byte{0x11}, ed25519.SeedSize)
		priv := ed25519.NewKeyFromSeed(seed)
		pub := priv.Public().(ed25519.PublicKey)
		pubInt := new(big.Int).SetBytes(pub)

		msg := []byte("tonutils-go funcs")
		msgSig := ed25519.Sign(priv, msg)

		st := newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(msg, uint(len(msg))*8).ToSlice()); err != nil {
			t.Fatalf("failed to push CHKSIGNS message: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(msgSig, 512).ToSlice()); err != nil {
			t.Fatalf("failed to push CHKSIGNS signature: %v", err)
		}
		if err := st.Stack.PushInt(pubInt); err != nil {
			t.Fatalf("failed to push CHKSIGNS key: %v", err)
		}
		if err := CHKSIGNS().Interpret(st); err != nil {
			t.Fatalf("CHKSIGNS failed: %v", err)
		}
		ok, err := st.Stack.PopBool()
		if err != nil || !ok {
			t.Fatalf("CHKSIGNS = (%v, %v), want true", ok, err)
		}

		hash := sha256.Sum256(msg)
		hashSig := ed25519.Sign(priv, hash[:])
		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(new(big.Int).SetBytes(hash[:])); err != nil {
			t.Fatalf("failed to push CHKSIGNU hash: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(hashSig, 512).ToSlice()); err != nil {
			t.Fatalf("failed to push CHKSIGNU signature: %v", err)
		}
		if err := st.Stack.PushInt(pubInt); err != nil {
			t.Fatalf("failed to push CHKSIGNU key: %v", err)
		}
		if err := CHKSIGNU().Interpret(st); err != nil {
			t.Fatalf("CHKSIGNU failed: %v", err)
		}
		ok, err = st.Stack.PopBool()
		if err != nil || !ok {
			t.Fatalf("CHKSIGNU = (%v, %v), want true", ok, err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(1, 1).ToSlice()); err != nil {
			t.Fatalf("failed to push non-byte-aligned message: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(msgSig, 512).ToSlice()); err != nil {
			t.Fatalf("failed to push CHKSIGNS signature: %v", err)
		}
		if err := st.Stack.PushInt(pubInt); err != nil {
			t.Fatalf("failed to push CHKSIGNS key: %v", err)
		}
		if err := CHKSIGNS().Interpret(st); err == nil {
			t.Fatal("CHKSIGNS should reject non-byte-aligned messages")
		}

		privKey := make([]byte, 32)
		privKey[31] = 9
		nonce := make([]byte, 32)
		nonce[31] = 7
		v, r, s, pubUncompressed, okRecover := localec.SignRecoverable(privKey, nonce, hash[:])
		if !okRecover {
			t.Fatal("SignRecoverable should succeed for test vectors")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(new(big.Int).SetBytes(hash[:])); err != nil {
			t.Fatalf("failed to push ECRECOVER hash: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(int64(v))); err != nil {
			t.Fatalf("failed to push ECRECOVER v: %v", err)
		}
		if err := st.Stack.PushInt(r); err != nil {
			t.Fatalf("failed to push ECRECOVER r: %v", err)
		}
		if err := st.Stack.PushInt(s); err != nil {
			t.Fatalf("failed to push ECRECOVER s: %v", err)
		}
		if err := ECRECOVER().Interpret(st); err != nil {
			t.Fatalf("ECRECOVER failed: %v", err)
		}
		ok, err = st.Stack.PopBool()
		if err != nil || !ok {
			t.Fatalf("ECRECOVER = (%v, %v), want true", ok, err)
		}
		gotY, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop ECRECOVER y: %v", err)
		}
		gotX, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop ECRECOVER x: %v", err)
		}
		gotPrefix, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop ECRECOVER prefix: %v", err)
		}
		if gotPrefix.Int64() != int64(pubUncompressed[0]) ||
			gotX.Cmp(new(big.Int).SetBytes(pubUncompressed[1:33])) != 0 ||
			gotY.Cmp(new(big.Int).SetBytes(pubUncompressed[33:65])) != 0 {
			t.Fatal("unexpected ECRECOVER output")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(new(big.Int).SetBytes(hash[:])); err != nil {
			t.Fatalf("failed to push invalid ECRECOVER hash: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(4)); err != nil {
			t.Fatalf("failed to push invalid ECRECOVER v: %v", err)
		}
		if err := st.Stack.PushInt(r); err != nil {
			t.Fatalf("failed to push invalid ECRECOVER r: %v", err)
		}
		if err := st.Stack.PushInt(s); err != nil {
			t.Fatalf("failed to push invalid ECRECOVER s: %v", err)
		}
		if err := ECRECOVER().Interpret(st); err != nil {
			t.Fatalf("ECRECOVER invalid-v failed: %v", err)
		}
		ok, err = st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("ECRECOVER invalid-v = (%v, %v), want false", ok, err)
		}

		xOnly := new(big.Int).SetBytes(pubUncompressed[1:33])
		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(xOnly); err != nil {
			t.Fatalf("failed to push x-only pubkey: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("failed to push zero tweak: %v", err)
		}
		if err := SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Interpret(st); err != nil {
			t.Fatalf("SECP256K1_XONLY_PUBKEY_TWEAK_ADD failed: %v", err)
		}
		ok, err = st.Stack.PopBool()
		if err != nil || !ok {
			t.Fatalf("SECP256K1_XONLY_PUBKEY_TWEAK_ADD = (%v, %v), want true", ok, err)
		}
		gotY, err = st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop tweak-add y: %v", err)
		}
		gotX, err = st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop tweak-add x: %v", err)
		}
		gotPrefix, err = st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop tweak-add prefix: %v", err)
		}
		if gotPrefix.Int64() != 4 || gotX.Cmp(xOnly) != 0 || gotY.Sign() == 0 {
			t.Fatal("unexpected x-only tweak-add output")
		}
	})

	t.Run("P256SignatureOps", func(t *testing.T) {
		curve := elliptic.P256()
		d := big.NewInt(7)
		x, y := curve.ScalarBaseMult(d.FillBytes(make([]byte, 32)))
		priv := &ecdsa.PrivateKey{
			PublicKey: ecdsa.PublicKey{
				Curve: curve,
				X:     x,
				Y:     y,
			},
			D: d,
		}

		msg := []byte("p256-ton-message")
		digest := sha256.Sum256(msg)
		r, s, err := ecdsa.Sign(bytes.NewReader(bytes.Repeat([]byte{0x44}, 128)), priv, digest[:])
		if err != nil {
			t.Fatalf("failed to sign P256 message: %v", err)
		}
		sig := append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...)
		key := elliptic.MarshalCompressed(curve, x, y)

		st := newFuncTestState(t, nil)
		if err = st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(msg, uint(len(msg))*8).ToSlice()); err != nil {
			t.Fatalf("failed to push P256_CHKSIGNS message: %v", err)
		}
		if err = st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig, 512).ToSlice()); err != nil {
			t.Fatalf("failed to push P256_CHKSIGNS signature: %v", err)
		}
		if err = st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(key, uint(len(key))*8).ToSlice()); err != nil {
			t.Fatalf("failed to push P256_CHKSIGNS key: %v", err)
		}
		if err = P256_CHKSIGNS().Interpret(st); err != nil {
			t.Fatalf("P256_CHKSIGNS failed: %v", err)
		}
		ok, err := st.Stack.PopBool()
		if err != nil || !ok {
			t.Fatalf("P256_CHKSIGNS = (%v, %v), want true", ok, err)
		}

		hashInput := sha256.Sum256(msg)
		hashDigest := sha256.Sum256(hashInput[:])
		r, s, err = ecdsa.Sign(bytes.NewReader(bytes.Repeat([]byte{0x55}, 128)), priv, hashDigest[:])
		if err != nil {
			t.Fatalf("failed to sign P256 hash input: %v", err)
		}
		sig = append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...)

		st = newFuncTestState(t, nil)
		if err = st.Stack.PushInt(new(big.Int).SetBytes(hashInput[:])); err != nil {
			t.Fatalf("failed to push P256_CHKSIGNU data: %v", err)
		}
		if err = st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig, 512).ToSlice()); err != nil {
			t.Fatalf("failed to push P256_CHKSIGNU signature: %v", err)
		}
		if err = st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(key, uint(len(key))*8).ToSlice()); err != nil {
			t.Fatalf("failed to push P256_CHKSIGNU key: %v", err)
		}
		if err = P256_CHKSIGNU().Interpret(st); err != nil {
			t.Fatalf("P256_CHKSIGNU failed: %v", err)
		}
		ok, err = st.Stack.PopBool()
		if err != nil || !ok {
			t.Fatalf("P256_CHKSIGNU = (%v, %v), want true", ok, err)
		}
	})
}

func TestTonWrapperAndMessageOpVariants(t *testing.T) {
	t.Run("SetcpAndGlobalRoundTrips", func(t *testing.T) {
		setCP := SETCP(-1)
		decodedSetCP := SETCP(0)
		if err := decodedSetCP.Deserialize(setCP.Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("SETCP deserialize failed: %v", err)
		}
		if got := decodedSetCP.SerializeText(); got != "SETCP -1" {
			t.Fatalf("unexpected SETCP text: %q", got)
		}
		st := newFuncTestState(t, nil)
		if err := decodedSetCP.Interpret(st); err == nil {
			t.Fatal("SETCP -1 should reject unsupported codepages")
		}

		setGlob := SETGLOB(7)
		decodedSetGlob := SETGLOB(0)
		if err := decodedSetGlob.Deserialize(setGlob.Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("SETGLOB deserialize failed: %v", err)
		}
		if got := decodedSetGlob.SerializeText(); got != "SETGLOB 7" {
			t.Fatalf("unexpected SETGLOB text: %q", got)
		}
		st = newFuncTestState(t, nil)
		st.InitForExecution()
		if err := st.Stack.PushInt(big.NewInt(321)); err != nil {
			t.Fatalf("failed to push SETGLOB value: %v", err)
		}
		if err := decodedSetGlob.Interpret(st); err != nil {
			t.Fatalf("SETGLOB interpret failed: %v", err)
		}

		getGlob := GETGLOB(7)
		decodedGetGlob := GETGLOB(0)
		if err := decodedGetGlob.Deserialize(getGlob.Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("GETGLOB deserialize failed: %v", err)
		}
		if got := decodedGetGlob.SerializeText(); got != "GETGLOB 7" {
			t.Fatalf("unexpected GETGLOB text: %q", got)
		}
		if err := decodedGetGlob.Interpret(st); err != nil {
			t.Fatalf("GETGLOB interpret failed: %v", err)
		}
		got, err := st.Stack.PopIntFinite()
		if err != nil || got.Int64() != 321 {
			t.Fatalf("GETGLOB round-trip = (%v, %v)", got, err)
		}
	})

	t.Run("SliceDataSizeAndVarInt32Ops", func(t *testing.T) {
		ref := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
		root := cell.BeginCell().MustStoreUInt(0xAA, 8).MustStoreRef(ref).EndCell()

		st := newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(root.BeginParse()); err != nil {
			t.Fatalf("failed to push SDATASIZE slice: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(10)); err != nil {
			t.Fatalf("failed to push SDATASIZE limit: %v", err)
		}
		if err := SDATASIZE().Interpret(st); err != nil {
			t.Fatalf("SDATASIZE failed: %v", err)
		}
		refs, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop SDATASIZE refs: %v", err)
		}
		bits, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop SDATASIZE bits: %v", err)
		}
		cellsCnt, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop SDATASIZE cells: %v", err)
		}
		if refs.Int64() != 1 || bits.Int64() != 16 || cellsCnt.Int64() != 1 {
			t.Fatalf("unexpected SDATASIZE result: cells=%v bits=%v refs=%v", cellsCnt, bits, refs)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(root.BeginParse()); err != nil {
			t.Fatalf("failed to push SDATASIZEQ slice: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("failed to push SDATASIZEQ limit: %v", err)
		}
		if err := SDATASIZEQ().Interpret(st); err != nil {
			t.Fatalf("SDATASIZEQ failed: %v", err)
		}
		ok, err := st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("SDATASIZEQ = (%v, %v), want false", ok, err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
			t.Fatalf("failed to push STVARUINT32 builder: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(65535)); err != nil {
			t.Fatalf("failed to push STVARUINT32 value: %v", err)
		}
		if err := STVARUINT32().Interpret(st); err != nil {
			t.Fatalf("STVARUINT32 failed: %v", err)
		}
		unsignedBuilder, err := st.Stack.PopBuilder()
		if err != nil {
			t.Fatalf("failed to pop STVARUINT32 builder: %v", err)
		}
		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(unsignedBuilder.ToSlice()); err != nil {
			t.Fatalf("failed to push LDVARUINT32 slice: %v", err)
		}
		if err := LDVARUINT32().Interpret(st); err != nil {
			t.Fatalf("LDVARUINT32 failed: %v", err)
		}
		rest, err := st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop LDVARUINT32 rest: %v", err)
		}
		unsignedVal, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop LDVARUINT32 value: %v", err)
		}
		if unsignedVal.Int64() != 65535 || rest.BitsLeft() != 0 {
			t.Fatalf("unexpected LDVARUINT32 result: %v rest=%d", unsignedVal, rest.BitsLeft())
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
			t.Fatalf("failed to push STVARINT32 builder: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(-12345)); err != nil {
			t.Fatalf("failed to push STVARINT32 value: %v", err)
		}
		if err := STVARINT32().Interpret(st); err != nil {
			t.Fatalf("STVARINT32 failed: %v", err)
		}
		signedBuilder, err := st.Stack.PopBuilder()
		if err != nil {
			t.Fatalf("failed to pop STVARINT32 builder: %v", err)
		}
		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(signedBuilder.ToSlice()); err != nil {
			t.Fatalf("failed to push LDVARINT32 slice: %v", err)
		}
		if err := LDVARINT32().Interpret(st); err != nil {
			t.Fatalf("LDVARINT32 failed: %v", err)
		}
		rest, err = st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop LDVARINT32 rest: %v", err)
		}
		signedVal, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop LDVARINT32 value: %v", err)
		}
		if signedVal.Int64() != -12345 || rest.BitsLeft() != 0 {
			t.Fatalf("unexpected LDVARINT32 result: %v rest=%d", signedVal, rest.BitsLeft())
		}
	})

	t.Run("AddressQuietAndDynamicParamWrappers", func(t *testing.T) {
		_, stdAddr, addrData := mustStdAddrSlice(t)

		st := newFuncTestState(t, nil)
		badSrc := cell.BeginCell().MustStoreUInt(1, 1).ToSlice()
		if err := st.Stack.PushSlice(badSrc); err != nil {
			t.Fatalf("failed to push LDMSGADDRQ bad src: %v", err)
		}
		if err := LDMSGADDRQ().Interpret(st); err != nil {
			t.Fatalf("LDMSGADDRQ failed: %v", err)
		}
		ok, err := st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("LDMSGADDRQ = (%v, %v), want false", ok, err)
		}
		rest, err := st.Stack.PopSlice()
		if err != nil || rest.BitsLeft() != badSrc.BitsLeft() {
			t.Fatalf("LDMSGADDRQ rest = (%v, %v)", rest, err)
		}

		st = newFuncTestState(t, nil)
		stdWithTail := cell.BeginCell().MustStoreAddr(address.NewAddress(0, 0, addrData)).MustStoreUInt(0x5, 3).ToSlice()
		if err := st.Stack.PushSlice(stdWithTail); err != nil {
			t.Fatalf("failed to push LDSTDADDR src: %v", err)
		}
		if err := LDSTDADDR().Interpret(st); err != nil {
			t.Fatalf("LDSTDADDR failed: %v", err)
		}
		rest, err = st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop LDSTDADDR rest: %v", err)
		}
		addrSlice, err := st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop LDSTDADDR addr: %v", err)
		}
		if rest.BitsLeft() != 3 || !isValidStdMsgAddr(addrSlice) {
			t.Fatalf("unexpected LDSTDADDR result: rest=%d", rest.BitsLeft())
		}

		st = newFuncTestState(t, nil)
		optSrc := cell.BeginCell().MustStoreAddr(address.NewAddressNone()).MustStoreUInt(0x5, 3).ToSlice()
		if err := st.Stack.PushSlice(optSrc); err != nil {
			t.Fatalf("failed to push LDOPTSTDADDRQ src: %v", err)
		}
		if err := LDOPTSTDADDRQ().Interpret(st); err != nil {
			t.Fatalf("LDOPTSTDADDRQ failed: %v", err)
		}
		ok, err = st.Stack.PopBool()
		if err != nil || !ok {
			t.Fatalf("LDOPTSTDADDRQ = (%v, %v), want true", ok, err)
		}
		rest, err = st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop LDOPTSTDADDRQ rest: %v", err)
		}
		raw, err := st.Stack.PopAny()
		if err != nil {
			t.Fatalf("failed to pop LDOPTSTDADDRQ value: %v", err)
		}
		if raw != nil || rest.BitsLeft() != 3 {
			t.Fatalf("unexpected LDOPTSTDADDRQ output: raw=%T rest=%d", raw, rest.BitsLeft())
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(stdAddr); err != nil {
			t.Fatalf("failed to push REWRITEVARADDR src: %v", err)
		}
		if err := REWRITEVARADDR().Interpret(st); err != nil {
			t.Fatalf("REWRITEVARADDR failed: %v", err)
		}
		rewritten, err := st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop REWRITEVARADDR addr: %v", err)
		}
		workchain, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop REWRITEVARADDR wc: %v", err)
		}
		if workchain.Int64() != 0 || rewritten.BitsLeft() != 256 {
			t.Fatalf("unexpected REWRITEVARADDR output: wc=%v bits=%d", workchain, rewritten.BitsLeft())
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreAddr(address.NewAddressNone()).ToSlice()); err != nil {
			t.Fatalf("failed to push REWRITEVARADDRQ src: %v", err)
		}
		if err := REWRITEVARADDRQ().Interpret(st); err != nil {
			t.Fatalf("REWRITEVARADDRQ failed: %v", err)
		}
		ok, err = st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("REWRITEVARADDRQ = (%v, %v), want false", ok, err)
		}

		inMsg := tuple.NewTupleSized(4)
		if err := inMsg.Set(3, big.NewInt(91)); err != nil {
			t.Fatalf("failed to set INMSGPARAM tuple: %v", err)
		}
		st = newFuncTestState(t, map[int]any{paramIdxInMsgParams: inMsg})
		op := INMSGPARAM(3)
		decoded := INMSGPARAM(0)
		if err := decoded.Deserialize(op.Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("INMSGPARAM deserialize failed: %v", err)
		}
		if got := decoded.SerializeText(); got != "INMSGPARAM 3" {
			t.Fatalf("unexpected INMSGPARAM text: %q", got)
		}
		if err := decoded.Interpret(st); err != nil {
			t.Fatalf("INMSGPARAM interpret failed: %v", err)
		}
		got, err := st.Stack.PopIntFinite()
		if err != nil || got.Int64() != 91 {
			t.Fatalf("INMSGPARAM = (%v, %v)", got, err)
		}
	})
}

func TestTonActionAndSendMsgOps(t *testing.T) {
	t.Run("ActionInstallers", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		st.InitForExecution()
		emptyActions := st.Reg.D[1]

		if err := st.Stack.PushInt(big.NewInt(777)); err != nil {
			t.Fatalf("failed to push RAWRESERVE amount: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(3)); err != nil {
			t.Fatalf("failed to push RAWRESERVE mode: %v", err)
		}
		if err := RAWRESERVE().Interpret(st); err != nil {
			t.Fatalf("RAWRESERVE failed: %v", err)
		}
		wantReserve := cell.BeginCell().
			MustStoreRef(emptyActions).
			MustStoreUInt(0x36e6b809, 32).
			MustStoreUInt(3, 8).
			MustStoreBigCoins(big.NewInt(777)).
			MustStoreMaybeRef(nil).
			EndCell()
		if !bytes.Equal(st.Reg.D[1].Hash(), wantReserve.Hash()) {
			t.Fatal("unexpected RAWRESERVE action")
		}

		extra := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
		st = newFuncTestState(t, nil)
		st.InitForExecution()
		emptyActions = st.Reg.D[1]
		if err := st.Stack.PushInt(big.NewInt(777)); err != nil {
			t.Fatalf("failed to push RAWRESERVEX amount: %v", err)
		}
		if err := st.Stack.PushCell(extra); err != nil {
			t.Fatalf("failed to push RAWRESERVEX extra: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(3)); err != nil {
			t.Fatalf("failed to push RAWRESERVEX mode: %v", err)
		}
		if err := RAWRESERVEX().Interpret(st); err != nil {
			t.Fatalf("RAWRESERVEX failed: %v", err)
		}
		wantReserveX := cell.BeginCell().
			MustStoreRef(emptyActions).
			MustStoreUInt(0x36e6b809, 32).
			MustStoreUInt(3, 8).
			MustStoreBigCoins(big.NewInt(777)).
			MustStoreMaybeRef(extra).
			EndCell()
		if !bytes.Equal(st.Reg.D[1].Hash(), wantReserveX.Hash()) {
			t.Fatal("unexpected RAWRESERVEX action")
		}

		code := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
		st = newFuncTestState(t, nil)
		st.InitForExecution()
		emptyActions = st.Reg.D[1]
		if err := st.Stack.PushCell(code); err != nil {
			t.Fatalf("failed to push SETCODE code: %v", err)
		}
		if err := SETCODE().Interpret(st); err != nil {
			t.Fatalf("SETCODE failed: %v", err)
		}
		wantSetCode := cell.BeginCell().
			MustStoreRef(emptyActions).
			MustStoreUInt(0xAD4DE08E, 32).
			MustStoreRef(code).
			EndCell()
		if !bytes.Equal(st.Reg.D[1].Hash(), wantSetCode.Hash()) {
			t.Fatal("unexpected SETCODE action")
		}

		st = newFuncTestState(t, nil)
		st.InitForExecution()
		emptyActions = st.Reg.D[1]
		if err := st.Stack.PushCell(code); err != nil {
			t.Fatalf("failed to push SETLIBCODE code: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("failed to push SETLIBCODE mode: %v", err)
		}
		if err := SETLIBCODE().Interpret(st); err != nil {
			t.Fatalf("SETLIBCODE failed: %v", err)
		}
		wantSetLib := cell.BeginCell().
			MustStoreRef(emptyActions).
			MustStoreUInt(0x26FA1DD4, 32).
			MustStoreUInt(3, 8).
			MustStoreRef(code).
			EndCell()
		if !bytes.Equal(st.Reg.D[1].Hash(), wantSetLib.Hash()) {
			t.Fatal("unexpected SETLIBCODE action")
		}

		st = newFuncTestState(t, nil)
		st.InitForExecution()
		emptyActions = st.Reg.D[1]
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("failed to push CHANGELIB hash: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("failed to push CHANGELIB mode: %v", err)
		}
		if err := CHANGELIB().Interpret(st); err != nil {
			t.Fatalf("CHANGELIB failed: %v", err)
		}
		wantChangeLib := cell.BeginCell().
			MustStoreRef(emptyActions).
			MustStoreUInt(0x26FA1DD4, 32).
			MustStoreUInt(2, 8).
			MustStoreBigUInt(big.NewInt(1), 256).
			EndCell()
		if !bytes.Equal(st.Reg.D[1].Hash(), wantChangeLib.Hash()) {
			t.Fatal("unexpected CHANGELIB action")
		}
	})

	t.Run("SendMsgAndSizeLimits", func(t *testing.T) {
		st := makeFeeState(t)
		tonAddr, _, _ := mustStdAddrSlice(t)
		mustSetFuncParam(t, st, 8, cell.BeginCell().MustStoreAddr(tonAddr).ToSlice())

		if got, err := getSizeLimitsMaxMsgCells(st); err != nil || got != 1<<13 {
			t.Fatalf("default size limit = (%d, %v)", got, err)
		}

		cfg := tuple.NewTupleSized(7)
		if err := cfg.Set(6, cell.BeginCell().MustStoreUInt(0x01, 8).MustStoreUInt(0, 32).MustStoreUInt(123, 32).ToSlice()); err != nil {
			t.Fatalf("failed to set size-limits config: %v", err)
		}
		limitState := newFuncTestState(t, map[int]any{paramIdxUnpackedConfig: cfg})
		limitState.InitForExecution()
		if got, err := getSizeLimitsMaxMsgCells(limitState); err != nil || got != 123 {
			t.Fatalf("configured size limit = (%d, %v)", got, err)
		}

		msg := &tlb.InternalMessage{
			IHRDisabled: true,
			SrcAddr:     tonAddr,
			DstAddr:     tonAddr,
			Amount:      tlb.MustFromNano(big.NewInt(1000), 9),
			IHRFee:      tlb.MustFromNano(big.NewInt(0), 9),
			FwdFee:      tlb.MustFromNano(big.NewInt(0), 9),
			Body:        cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell(),
		}
		msgCell, err := tlb.ToCell(msg)
		if err != nil {
			t.Fatalf("failed to build SENDMSG message: %v", err)
		}

		stat := newStorageStat(10, st)
		if !addMessageTailStorage(stat, msgCell, 0) {
			t.Fatalf("addMessageTailStorage should succeed, got %+v", stat)
		}

		emptyActions := st.Reg.D[1]
		if err := st.Stack.PushCell(msgCell); err != nil {
			t.Fatalf("failed to push SENDMSG fee-only msg: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1024)); err != nil {
			t.Fatalf("failed to push SENDMSG fee-only mode: %v", err)
		}
		if err := SENDMSG().Interpret(st); err != nil {
			t.Fatalf("SENDMSG fee-only failed: %v", err)
		}
		fee, err := st.Stack.PopIntFinite()
		if err != nil || fee.Sign() <= 0 {
			t.Fatalf("SENDMSG fee-only fee = (%v, %v)", fee, err)
		}
		if !bytes.Equal(st.Reg.D[1].Hash(), emptyActions.Hash()) {
			t.Fatal("SENDMSG fee-only should leave actions unchanged")
		}

		st = makeFeeState(t)
		mustSetFuncParam(t, st, 8, cell.BeginCell().MustStoreAddr(tonAddr).ToSlice())
		emptyActions = st.Reg.D[1]
		if err := st.Stack.PushCell(msgCell); err != nil {
			t.Fatalf("failed to push SENDMSG msg: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("failed to push SENDMSG mode: %v", err)
		}
		if err := SENDMSG().Interpret(st); err != nil {
			t.Fatalf("SENDMSG send failed: %v", err)
		}
		fee, err = st.Stack.PopIntFinite()
		if err != nil || fee.Sign() <= 0 {
			t.Fatalf("SENDMSG send fee = (%v, %v)", fee, err)
		}
		wantSendMsg := cell.BeginCell().
			MustStoreRef(emptyActions).
			MustStoreUInt(0x0EC3C86D, 32).
			MustStoreUInt(1, 8).
			MustStoreRef(msgCell).
			EndCell()
		if !bytes.Equal(st.Reg.D[1].Hash(), wantSendMsg.Hash()) {
			t.Fatal("unexpected SENDMSG action")
		}

		st = makeFeeState(t)
		mustSetFuncParam(t, st, 8, cell.BeginCell().MustStoreAddr(tonAddr).ToSlice())
		if err := st.Stack.PushCell(msgCell); err != nil {
			t.Fatalf("failed to push invalid-mode SENDMSG msg: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(256)); err != nil {
			t.Fatalf("failed to push invalid-mode SENDMSG mode: %v", err)
		}
		if err := SENDMSG().Interpret(st); err == nil {
			t.Fatal("SENDMSG should reject modes >= 256")
		}

		st = makeFeeState(t)
		mustSetFuncParam(t, st, 8, cell.BeginCell().MustStoreAddr(tonAddr).ToSlice())
		if err := st.Stack.PushCell(cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()); err != nil {
			t.Fatalf("failed to push invalid SENDMSG msg: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("failed to push invalid SENDMSG mode: %v", err)
		}
		if err := SENDMSG().Interpret(st); err == nil {
			t.Fatal("SENDMSG should reject invalid message cells")
		}
	})
}
