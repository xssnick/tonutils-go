package funcs

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	localec "github.com/xssnick/tonutils-go/tvm/internal/secp256k1"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func makeConfigRootRefDict(t *testing.T, entries map[uint32]*cell.Cell) *cell.Cell {
	t.Helper()

	dict := cell.NewDict(32)
	for idx, value := range entries {
		key := cell.BeginCell().MustStoreUInt(uint64(idx), 32).EndCell()
		if err := dict.SetRef(key, value); err != nil {
			t.Fatalf("failed to set config ref %d: %v", idx, err)
		}
	}
	return dict.AsCell()
}

func makeExtraBalanceDict(t *testing.T, entries map[uint32]uint64) *cell.Cell {
	t.Helper()

	dict := cell.NewDict(32)
	for idx, amount := range entries {
		key := cell.BeginCell().MustStoreUInt(uint64(idx), 32).EndCell()
		val := cell.BeginCell().MustStoreVarUInt(amount, 32).EndCell()
		if err := dict.Set(key, val); err != nil {
			t.Fatalf("failed to set extra balance %d: %v", idx, err)
		}
	}
	return dict.AsCell()
}

func makeFeeState(t *testing.T) *vm.State {
	t.Helper()

	gasCfg := tuple.NewTupleSized(7)
	gasSlice := cell.BeginCell().
		MustStoreUInt(0xD1, 8).
		MustStoreUInt(10, 64).
		MustStoreUInt(3, 64).
		MustStoreUInt(0xDE, 8).
		MustStoreUInt(1<<16, 64).
		MustStoreUInt(100, 64).
		MustStoreUInt(80, 64).
		MustStoreUInt(7, 64).
		MustStoreUInt(1000, 64).
		MustStoreUInt(11, 64).
		MustStoreUInt(12, 64).
		ToSlice()
	msgSlice := cell.BeginCell().
		MustStoreUInt(0xEA, 8).
		MustStoreUInt(100, 64).
		MustStoreUInt(1<<16, 64).
		MustStoreUInt(2<<16, 64).
		MustStoreUInt(77, 32).
		MustStoreUInt(9, 16).
		MustStoreUInt(10, 16).
		ToSlice()
	storageSlice := cell.BeginCell().
		MustStoreUInt(0xCC, 8).
		MustStoreUInt(1, 32).
		MustStoreUInt(2, 64).
		MustStoreUInt(3, 64).
		MustStoreUInt(4, 64).
		MustStoreUInt(5, 64).
		ToSlice()
	if err := gasCfg.Set(0, storageSlice); err != nil {
		t.Fatalf("set storage cfg failed: %v", err)
	}
	if err := gasCfg.Set(2, gasSlice); err != nil {
		t.Fatalf("set gas cfg failed: %v", err)
	}
	if err := gasCfg.Set(5, msgSlice); err != nil {
		t.Fatalf("set msg cfg failed: %v", err)
	}

	balance := *tuple.NewTuple(big.NewInt(1000), makeExtraBalanceDict(t, map[uint32]uint64{7: 55}))
	st := newFuncTestState(t, map[int]any{
		paramIdxUnpackedConfig: gasCfg,
		7:                      balance,
	})
	st.InitForExecution()
	return st
}

func TestTonopsGasConfigAndGlobals(t *testing.T) {
	st := newFuncTestState(t, map[int]any{0: int64(21), 4: int64(44), 5: int64(55), 6: big.NewInt(66), 9: (*cell.Cell)(nil), 14: *tuple.NewTuple(nil, cell.BeginCell().MustStoreUInt(0xFFFFFFFF, 32).ToSlice())})
	st.Gas = vm.GasWithLimit(50)

	if err := st.Gas.Consume(5); err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if err := GASCONSUMED().Interpret(st); err != nil {
		t.Fatalf("GASCONSUMED failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Int64() != 5 {
		t.Fatalf("GASCONSUMED = (%v, %v)", got, err)
	}

	if err := st.Stack.PushInt(big.NewInt(25)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := SETGASLIMIT().Interpret(st); err != nil {
		t.Fatalf("SETGASLIMIT failed: %v", err)
	}
	if st.Gas.Limit != 25 {
		t.Fatalf("unexpected gas limit after SETGASLIMIT: %d", st.Gas.Limit)
	}

	if err := ACCEPT().Interpret(st); err != nil {
		t.Fatalf("ACCEPT failed: %v", err)
	}
	if st.Gas.Limit != vm.GasInfinite {
		t.Fatalf("ACCEPT should set infinite gas, got %d", st.Gas.Limit)
	}

	if err := COMMIT().Interpret(st); err != nil {
		t.Fatalf("COMMIT failed: %v", err)
	}
	if !st.Committed.Committed {
		t.Fatal("COMMIT should mark the state as committed")
	}

	paramOps := []struct {
		name string
		op   *vm.OP
		want int64
	}{
		{name: "BLOCKLT", op: opPtr(BLOCKLT()), want: 44},
		{name: "LTIME", op: opPtr(LTIME()), want: 55},
	}
	for _, tc := range paramOps {
		t.Run(tc.name, func(t *testing.T) {
			local := newFuncTestState(t, map[int]any{4: int64(44), 5: int64(55)})
			if err := (*tc.op).Interpret(local); err != nil {
				t.Fatalf("%s failed: %v", tc.name, err)
			}
			got, err := local.Stack.PopIntFinite()
			if err != nil || got.Int64() != tc.want {
				t.Fatalf("%s = (%v, %v), want %d", tc.name, got, err, tc.want)
			}
		})
	}

	st = newFuncTestState(t, map[int]any{
		9:  makeConfigRootRefDict(t, map[uint32]*cell.Cell{7: cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()}),
		14: *tuple.NewTuple(nil, cell.BeginCell().MustStoreUInt(0xFFFFFFFF, 32).ToSlice()),
	})
	st.InitForExecution()
	if root, err := configRootFromC7(st); err != nil || root == nil {
		t.Fatalf("configRootFromC7 = (%v, %v)", root, err)
	}
	if value, err := loadConfigValue(st, big.NewInt(7)); err != nil || value == nil {
		t.Fatalf("loadConfigValue = (%v, %v)", value, err)
	}
	if value, err := loadConfigValue(st, big.NewInt(-1)); err != nil || value != nil {
		t.Fatalf("loadConfigValue(negative) = (%v, %v)", value, err)
	}

	if err := st.Stack.PushInt(big.NewInt(7)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := CONFIGPARAM().Interpret(st); err != nil {
		t.Fatalf("CONFIGPARAM failed: %v", err)
	}
	ok, err := st.Stack.PopBool()
	if err != nil || !ok {
		t.Fatalf("CONFIGPARAM ok = (%v, %v)", ok, err)
	}
	if _, err = st.Stack.PopCell(); err != nil {
		t.Fatalf("PopCell failed: %v", err)
	}

	if err := st.Stack.PushInt(big.NewInt(8)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := CONFIGOPTPARAM().Interpret(st); err != nil {
		t.Fatalf("CONFIGOPTPARAM failed: %v", err)
	}
	if raw, err := st.Stack.PopAny(); err != nil || raw != nil {
		t.Fatalf("CONFIGOPTPARAM missing = (%v, %v)", raw, err)
	}

	if err := GLOBALID().Interpret(st); err != nil {
		t.Fatalf("GLOBALID failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Int64() != -1 {
		t.Fatalf("GLOBALID = (%v, %v)", got, err)
	}

	st = newFuncTestState(t, map[int]any{
		14: *tuple.NewTuple(nil, cell.BeginCell().MustStoreUInt(1, 8).ToSlice()),
	})
	if err := GLOBALID().Interpret(st); err == nil {
		t.Fatal("GLOBALID should reject short config slices")
	}

	st = newFuncTestState(t, nil)
	if err := st.Stack.PushInt(big.NewInt(99)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := st.Stack.PushInt(big.NewInt(3)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := SETGLOBVAR().Interpret(st); err != nil {
		t.Fatalf("SETGLOBVAR failed: %v", err)
	}
	if got, err := st.GetGlobal(3); err != nil || got.(*big.Int).Int64() != 99 {
		t.Fatalf("GetGlobal(3) = (%v, %v)", got, err)
	}

	if err := st.Stack.PushInt(big.NewInt(3)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := GETGLOBVAR().Interpret(st); err != nil {
		t.Fatalf("GETGLOBVAR failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Int64() != 99 {
		t.Fatalf("GETGLOBVAR = (%v, %v)", got, err)
	}

	if err := st.Stack.PushInt(big.NewInt(123)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := SETGLOB(4).Interpret(st); err != nil {
		t.Fatalf("SETGLOB failed: %v", err)
	}
	if err := GETGLOB(4).Interpret(st); err != nil {
		t.Fatalf("GETGLOB failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Int64() != 123 {
		t.Fatalf("GETGLOB = (%v, %v)", got, err)
	}

	if err := SETCP(0).Interpret(st); err != nil {
		t.Fatalf("SETCP failed: %v", err)
	}
}

func TestTonopsSignatureAndCurveOps(t *testing.T) {
	seed := bytes.Repeat([]byte{0x42}, 32)
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	keyInt := new(big.Int).SetBytes(pub)
	dataHash := bytes.Repeat([]byte{0x55}, 32)
	sig := ed25519.Sign(priv, dataHash)

	st := newFuncTestState(t, nil)
	if err := st.Stack.PushInt(new(big.Int).SetBytes(dataHash)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig, 512).ToSlice()); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err := st.Stack.PushInt(keyInt); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := CHKSIGNU().Interpret(st); err != nil {
		t.Fatalf("CHKSIGNU failed: %v", err)
	}
	if ok, err := st.Stack.PopBool(); err != nil || !ok {
		t.Fatalf("CHKSIGNU = (%v, %v)", ok, err)
	}

	msg := []byte("hello ed25519")
	sig = ed25519.Sign(priv, msg)
	st = newFuncTestState(t, nil)
	if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(msg, uint(len(msg))*8).ToSlice()); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig, 512).ToSlice()); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err := st.Stack.PushInt(keyInt); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := CHKSIGNS().Interpret(st); err != nil {
		t.Fatalf("CHKSIGNS failed: %v", err)
	}
	if ok, err := st.Stack.PopBool(); err != nil || !ok {
		t.Fatalf("CHKSIGNS = (%v, %v)", ok, err)
	}

	hash := sha256.Sum256([]byte("secp256k1"))
	privBytes := make([]byte, 32)
	privBytes[31] = 9
	nonceBytes := make([]byte, 32)
	nonceBytes[31] = 7
	v, r, s, pubBytes, ok := localec.SignRecoverable(privBytes, nonceBytes, hash[:])
	if !ok {
		t.Fatal("SignRecoverable failed")
	}

	st = newFuncTestState(t, nil)
	if err := st.Stack.PushInt(new(big.Int).SetBytes(hash[:])); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := st.Stack.PushInt(big.NewInt(int64(v))); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := st.Stack.PushInt(r); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := st.Stack.PushInt(s); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := ECRECOVER().Interpret(st); err != nil {
		t.Fatalf("ECRECOVER failed: %v", err)
	}
	if ok, err := st.Stack.PopBool(); err != nil || !ok {
		t.Fatalf("ECRECOVER ok = (%v, %v)", ok, err)
	}
	y, err := st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite(y) failed: %v", err)
	}
	x, err := st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite(x) failed: %v", err)
	}
	pfx, err := st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite(prefix) failed: %v", err)
	}
	if pfx.Int64() != int64(pubBytes[0]) || !bytes.Equal(x.FillBytes(make([]byte, 32)), pubBytes[1:33]) || !bytes.Equal(y.FillBytes(make([]byte, 32)), pubBytes[33:]) {
		t.Fatalf("unexpected ECRECOVER output")
	}

	st = newFuncTestState(t, nil)
	if err := st.Stack.PushInt(new(big.Int).SetBytes(pubBytes[1:33])); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Interpret(st); err != nil {
		t.Fatalf("SECP256K1_XONLY_PUBKEY_TWEAK_ADD failed: %v", err)
	}
	if ok, err := st.Stack.PopBool(); err != nil || !ok {
		t.Fatalf("tweak-add ok = (%v, %v)", ok, err)
	}
	y, err = st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite(y) failed: %v", err)
	}
	x, err = st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite(x) failed: %v", err)
	}
	pfx, err = st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite(prefix) failed: %v", err)
	}
	expectedTweaked, ok := localec.XOnlyPubkeyTweakAddUncompressed(pubBytes[1:33], make([]byte, 32))
	if !ok {
		t.Fatal("expected canonical x-only tweak-add result")
	}
	if pfx.Int64() != int64(expectedTweaked[0]) || !bytes.Equal(x.FillBytes(make([]byte, 32)), expectedTweaked[1:33]) || !bytes.Equal(y.FillBytes(make([]byte, 32)), expectedTweaked[33:]) {
		t.Fatalf("unexpected tweak-add output")
	}

	curve := elliptic.P256()
	d := big.NewInt(7)
	xPub, yPub := curve.ScalarBaseMult(d.Bytes())
	p256Priv := &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: curve, X: xPub, Y: yPub}, D: d}
	pubCompressed := elliptic.MarshalCompressed(curve, xPub, yPub)

	p256Data := bytes.Repeat([]byte{0x33}, 32)
	digest := sha256.Sum256(p256Data)
	rP256, sP256, err := ecdsa.Sign(bytes.NewReader(bytes.Repeat([]byte{0x77}, 64)), p256Priv, digest[:])
	if err != nil {
		t.Fatalf("ecdsa.Sign failed: %v", err)
	}
	sigBytes := append(rP256.FillBytes(make([]byte, 32)), sP256.FillBytes(make([]byte, 32))...)

	st = newFuncTestState(t, nil)
	if err := st.Stack.PushInt(new(big.Int).SetBytes(p256Data)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sigBytes, 512).ToSlice()); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pubCompressed, 264).ToSlice()); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err := P256_CHKSIGNU().Interpret(st); err != nil {
		t.Fatalf("P256_CHKSIGNU failed: %v", err)
	}
	if ok, err := st.Stack.PopBool(); err != nil || !ok {
		t.Fatalf("P256_CHKSIGNU = (%v, %v)", ok, err)
	}

	msgP256 := []byte("hello p256")
	digest = sha256.Sum256(msgP256)
	rP256, sP256, err = ecdsa.Sign(bytes.NewReader(bytes.Repeat([]byte{0x88}, 64)), p256Priv, digest[:])
	if err != nil {
		t.Fatalf("ecdsa.Sign failed: %v", err)
	}
	sigBytes = append(rP256.FillBytes(make([]byte, 32)), sP256.FillBytes(make([]byte, 32))...)

	st = newFuncTestState(t, nil)
	if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(msgP256, uint(len(msgP256))*8).ToSlice()); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sigBytes, 512).ToSlice()); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pubCompressed, 264).ToSlice()); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err := P256_CHKSIGNS().Interpret(st); err != nil {
		t.Fatalf("P256_CHKSIGNS failed: %v", err)
	}
	if ok, err := st.Stack.PopBool(); err != nil || !ok {
		t.Fatalf("P256_CHKSIGNS = (%v, %v)", ok, err)
	}
}

func TestFeeAndParamAliasOps(t *testing.T) {
	st := makeFeeState(t)
	if err := st.Stack.PushInt(big.NewInt(13)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := st.Stack.PushBool(true); err != nil {
		t.Fatalf("PushBool failed: %v", err)
	}
	if err := GETGASFEE().Interpret(st); err != nil {
		t.Fatalf("GETGASFEE failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Int64() != 6 {
		t.Fatalf("GETGASFEE = (%v, %v)", got, err)
	}

	st = makeFeeState(t)
	if err := st.Stack.PushInt(big.NewInt(3)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := st.Stack.PushInt(big.NewInt(20)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := st.Stack.PushInt(big.NewInt(10)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := st.Stack.PushBool(false); err != nil {
		t.Fatalf("PushBool failed: %v", err)
	}
	if err := GETSTORAGEFEE().Interpret(st); err != nil {
		t.Fatalf("GETSTORAGEFEE failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Int64() != 1 {
		t.Fatalf("GETSTORAGEFEE = (%v, %v)", got, err)
	}

	st = makeFeeState(t)
	if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := st.Stack.PushInt(big.NewInt(3)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := st.Stack.PushBool(false); err != nil {
		t.Fatalf("PushBool failed: %v", err)
	}
	if err := GETFORWARDFEE().Interpret(st); err != nil {
		t.Fatalf("GETFORWARDFEE failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Int64() != 107 {
		t.Fatalf("GETFORWARDFEE = (%v, %v)", got, err)
	}

	st = makeFeeState(t)
	if err := st.Stack.PushInt(big.NewInt(100)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := st.Stack.PushBool(false); err != nil {
		t.Fatalf("PushBool failed: %v", err)
	}
	if err := GETORIGINALFWDFEE().Interpret(st); err != nil {
		t.Fatalf("GETORIGINALFWDFEE failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Sign() <= 0 {
		t.Fatalf("GETORIGINALFWDFEE = (%v, %v)", got, err)
	}

	st = makeFeeState(t)
	if err := st.Stack.PushInt(big.NewInt(13)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := st.Stack.PushBool(true); err != nil {
		t.Fatalf("PushBool failed: %v", err)
	}
	if err := GETGASFEESIMPLE().Interpret(st); err != nil {
		t.Fatalf("GETGASFEESIMPLE failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Int64() != 13 {
		t.Fatalf("GETGASFEESIMPLE = (%v, %v)", got, err)
	}

	st = makeFeeState(t)
	if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := st.Stack.PushInt(big.NewInt(3)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := st.Stack.PushBool(false); err != nil {
		t.Fatalf("PushBool failed: %v", err)
	}
	if err := GETFORWARDFEESIMPLE().Interpret(st); err != nil {
		t.Fatalf("GETFORWARDFEESIMPLE failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Int64() != 7 {
		t.Fatalf("GETFORWARDFEESIMPLE = (%v, %v)", got, err)
	}

	st = makeFeeState(t)
	if err := st.Stack.PushInt(big.NewInt(7)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := GETEXTRABALANCE().Interpret(st); err != nil {
		t.Fatalf("GETEXTRABALANCE failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Int64() != 55 {
		t.Fatalf("GETEXTRABALANCE = (%v, %v)", got, err)
	}

	st = makeFeeState(t)
	if err := st.Stack.PushInt(big.NewInt(99)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := GETEXTRABALANCE().Interpret(st); err != nil {
		t.Fatalf("GETEXTRABALANCE missing failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Sign() != 0 {
		t.Fatalf("GETEXTRABALANCE missing = (%v, %v)", got, err)
	}

	st = newFuncTestState(t, nil)
	builder := cell.BeginCell().MustStoreUInt(0xAA, 8)
	wantHash := builder.EndCell().Hash()
	if err := st.Stack.PushBuilder(builder); err != nil {
		t.Fatalf("PushBuilder failed: %v", err)
	}
	if err := HASHBU().Interpret(st); err != nil {
		t.Fatalf("HASHBU failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || !bytes.Equal(got.FillBytes(make([]byte, 32)), wantHash) {
		t.Fatalf("HASHBU = (%v, %v)", got, err)
	}

	prevBlocks := *tuple.NewTuple(big.NewInt(11), big.NewInt(12), big.NewInt(13))
	inMsg := *tuple.NewTuple(big.NewInt(1), big.NewInt(0), big.NewInt(2), big.NewInt(3), big.NewInt(4), big.NewInt(5), big.NewInt(6), big.NewInt(7), big.NewInt(8), big.NewInt(9))
	cfg := *tuple.NewTuple(big.NewInt(100))
	st = newFuncTestState(t, map[int]any{
		paramIdxPrevBlocksInfo: prevBlocks,
		paramIdxUnpackedConfig: cfg,
		paramIdxDuePayment:     big.NewInt(14),
		paramIdxPrecompiledGas: big.NewInt(15),
		paramIdxInMsgParams:    inMsg,
	})

	simpleChecks := []struct {
		name string
		run  func(*vm.State) error
		want int64
	}{
		{name: "PREVMCBLOCKS", run: PREVMCBLOCKS().Interpret, want: 11},
		{name: "PREVKEYBLOCK", run: PREVKEYBLOCK().Interpret, want: 12},
		{name: "PREVMCBLOCKS_100", run: PREVMCBLOCKS_100().Interpret, want: 13},
		{name: "INMSG_BOUNCE", run: INMSG_BOUNCE().Interpret, want: 1},
		{name: "INMSG_BOUNCED", run: INMSG_BOUNCED().Interpret, want: 0},
		{name: "INMSG_SRC", run: INMSG_SRC().Interpret, want: 2},
		{name: "INMSG_FWDFEE", run: INMSG_FWDFEE().Interpret, want: 3},
		{name: "INMSG_LT", run: INMSG_LT().Interpret, want: 4},
		{name: "INMSG_UTIME", run: INMSG_UTIME().Interpret, want: 5},
		{name: "INMSG_ORIGVALUE", run: INMSG_ORIGVALUE().Interpret, want: 6},
		{name: "INMSG_VALUE", run: INMSG_VALUE().Interpret, want: 7},
		{name: "INMSG_VALUEEXTRA", run: INMSG_VALUEEXTRA().Interpret, want: 8},
		{name: "INMSG_STATEINIT", run: INMSG_STATEINIT().Interpret, want: 9},
		{name: "DUEPAYMENT", run: DUEPAYMENT().Interpret, want: 14},
		{name: "GETPRECOMPILEDGAS", run: GETPRECOMPILEDGAS().Interpret, want: 15},
	}
	for _, tc := range simpleChecks {
		t.Run(tc.name, func(t *testing.T) {
			local := newFuncTestState(t, map[int]any{
				paramIdxPrevBlocksInfo: prevBlocks,
				paramIdxUnpackedConfig: cfg,
				paramIdxDuePayment:     big.NewInt(14),
				paramIdxPrecompiledGas: big.NewInt(15),
				paramIdxInMsgParams:    inMsg,
			})
			if err := tc.run(local); err != nil {
				t.Fatalf("%s failed: %v", tc.name, err)
			}
			got, err := local.Stack.PopIntFinite()
			if err != nil || got.Int64() != tc.want {
				t.Fatalf("%s = (%v, %v), want %d", tc.name, got, err, tc.want)
			}
		})
	}

	st = newFuncTestState(t, map[int]any{paramIdxPrevBlocksInfo: prevBlocks, paramIdxUnpackedConfig: cfg, paramIdxInMsgParams: inMsg})
	if err := PREVBLOCKSINFOTUPLE().Interpret(st); err != nil {
		t.Fatalf("PREVBLOCKSINFOTUPLE failed: %v", err)
	}
	if tup, err := st.Stack.PopTuple(); err != nil || tup.Len() != 3 {
		t.Fatalf("PREVBLOCKSINFOTUPLE = (%v, %v)", tup, err)
	}
	if err := UNPACKEDCONFIGTUPLE().Interpret(st); err != nil {
		t.Fatalf("UNPACKEDCONFIGTUPLE failed: %v", err)
	}
	if tup, err := st.Stack.PopTuple(); err != nil || tup.Len() != 1 {
		t.Fatalf("UNPACKEDCONFIGTUPLE = (%v, %v)", tup, err)
	}
	if err := INMSGPARAMS().Interpret(st); err != nil {
		t.Fatalf("INMSGPARAMS failed: %v", err)
	}
	if tup, err := st.Stack.PopTuple(); err != nil || tup.Len() != 10 {
		t.Fatalf("INMSGPARAMS = (%v, %v)", tup, err)
	}
	if err := INMSGPARAM(2).Interpret(st); err != nil {
		t.Fatalf("INMSGPARAM failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Int64() != 2 {
		t.Fatalf("INMSGPARAM = (%v, %v)", got, err)
	}
}

func TestAdvancedOpSerializationAndCodepages(t *testing.T) {
	setcp := SETCP(-1)
	if setcp.SerializeText() != "SETCP -1" {
		t.Fatalf("unexpected SETCP text: %q", setcp.SerializeText())
	}
	parsedSetCP := SETCP(0)
	if err := parsedSetCP.Deserialize(setcp.Serialize().ToSlice()); err != nil {
		t.Fatalf("SETCP.Deserialize failed: %v", err)
	}
	if parsedSetCP.SerializeText() != "SETCP -1" {
		t.Fatalf("unexpected parsed SETCP text: %q", parsedSetCP.SerializeText())
	}
	if err := parsedSetCP.Interpret(newFuncTestState(t, nil)); err == nil {
		t.Fatal("SETCP(-1) should reject unsupported codepages")
	}

	st := newFuncTestState(t, nil)
	if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := SETCPX().Interpret(st); err == nil {
		t.Fatal("SETCPX should reject unsupported codepages")
	}

	getParam := GETPARAM(14)
	if getParam.SerializeText() != "GETPARAM 14" {
		t.Fatalf("unexpected GETPARAM text: %q", getParam.SerializeText())
	}
	parsedGetParam := GETPARAM(0)
	if err := parsedGetParam.Deserialize(getParam.Serialize().ToSlice()); err != nil {
		t.Fatalf("GETPARAM.Deserialize failed: %v", err)
	}
	st = newFuncTestState(t, map[int]any{14: int64(88)})
	if err := parsedGetParam.Interpret(st); err != nil {
		t.Fatalf("GETPARAM interpreted failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Int64() != 88 {
		t.Fatalf("GETPARAM = (%v, %v)", got, err)
	}

	getParamLong := GETPARAMLONG(200)
	if getParamLong.SerializeText() != "GETPARAMLONG 200" {
		t.Fatalf("unexpected GETPARAMLONG text: %q", getParamLong.SerializeText())
	}
	parsedGetParamLong := GETPARAMLONG(0)
	if err := parsedGetParamLong.Deserialize(getParamLong.Serialize().ToSlice()); err != nil {
		t.Fatalf("GETPARAMLONG.Deserialize failed: %v", err)
	}
	st = newFuncTestState(t, map[int]any{200: int64(99)})
	if err := parsedGetParamLong.Interpret(st); err != nil {
		t.Fatalf("GETPARAMLONG interpreted failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Int64() != 99 {
		t.Fatalf("GETPARAMLONG = (%v, %v)", got, err)
	}

	setGlob := SETGLOB(37)
	if setGlob.SerializeText() != "SETGLOB 5" {
		t.Fatalf("unexpected SETGLOB text: %q", setGlob.SerializeText())
	}
	parsedSetGlob := SETGLOB(0)
	if err := parsedSetGlob.Deserialize(setGlob.Serialize().ToSlice()); err != nil {
		t.Fatalf("SETGLOB.Deserialize failed: %v", err)
	}
	st = newFuncTestState(t, nil)
	if err := st.Stack.PushInt(big.NewInt(456)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := parsedSetGlob.Interpret(st); err != nil {
		t.Fatalf("SETGLOB interpreted failed: %v", err)
	}

	getGlob := GETGLOB(37)
	if getGlob.SerializeText() != "GETGLOB 5" {
		t.Fatalf("unexpected GETGLOB text: %q", getGlob.SerializeText())
	}
	parsedGetGlob := GETGLOB(0)
	if err := parsedGetGlob.Deserialize(getGlob.Serialize().ToSlice()); err != nil {
		t.Fatalf("GETGLOB.Deserialize failed: %v", err)
	}
	if err := parsedGetGlob.Interpret(st); err != nil {
		t.Fatalf("GETGLOB interpreted failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Int64() != 456 {
		t.Fatalf("GETGLOB = (%v, %v)", got, err)
	}

	inMsgParam := INMSGPARAM(18)
	if inMsgParam.SerializeText() != "INMSGPARAM 2" {
		t.Fatalf("unexpected INMSGPARAM text: %q", inMsgParam.SerializeText())
	}
	parsedInMsgParam := INMSGPARAM(0)
	if err := parsedInMsgParam.Deserialize(inMsgParam.Serialize().ToSlice()); err != nil {
		t.Fatalf("INMSGPARAM.Deserialize failed: %v", err)
	}
	st = newFuncTestState(t, map[int]any{
		paramIdxInMsgParams: *tuple.NewTuple(nil, nil, big.NewInt(321)),
	})
	if err := parsedInMsgParam.Interpret(st); err != nil {
		t.Fatalf("INMSGPARAM interpreted failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Int64() != 321 {
		t.Fatalf("INMSGPARAM = (%v, %v)", got, err)
	}
}

func opPtr(op vm.OP) *vm.OP {
	return &op
}
