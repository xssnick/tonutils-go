package funcs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"fmt"
	"math/big"

	"filippo.io/edwards25519"

	"github.com/xssnick/tonutils-go/tvm/cell"
	localec "github.com/xssnick/tonutils-go/tvm/internal/secp256k1"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return ACCEPT() },
		func() vm.OP { return SETGASLIMIT() },
		func() vm.OP { return GASCONSUMED() },
		func() vm.OP { return COMMIT() },
		func() vm.OP { return GETPARAM(0) },
		func() vm.OP { return BLOCKLT() },
		func() vm.OP { return LTIME() },
		func() vm.OP { return RANDSEED() },
		func() vm.OP { return BALANCE() },
		func() vm.OP { return MYADDR() },
		func() vm.OP { return CONFIGROOT() },
		func() vm.OP { return MYCODE() },
		func() vm.OP { return INCOMINGVALUE() },
		func() vm.OP { return STORAGEFEES() },
		func() vm.OP { return CONFIGDICT() },
		func() vm.OP { return CONFIGPARAM() },
		func() vm.OP { return CONFIGOPTPARAM() },
		func() vm.OP { return GLOBALID() },
		func() vm.OP { return GETGLOBVAR() },
		func() vm.OP { return GETGLOB(1) },
		func() vm.OP { return SETGLOBVAR() },
		func() vm.OP { return SETGLOB(1) },
		func() vm.OP { return GETPARAMLONG(0) },
		func() vm.OP { return CHKSIGNU() },
		func() vm.OP { return CHKSIGNS() },
		func() vm.OP { return ECRECOVER() },
		func() vm.OP { return SECP256K1_XONLY_PUBKEY_TWEAK_ADD() },
		func() vm.OP { return P256_CHKSIGNU() },
		func() vm.OP { return P256_CHKSIGNS() },
	)
}

func pushSmallInt(state *vm.State, v int64) error {
	return state.Stack.PushSmallInt(v)
}

func checkStackDepth(state *vm.State, depth int) error {
	if state.Stack.Len() < depth {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}
	return nil
}

var funcsBigIntOne = big.NewInt(1)

func pushHostValue(state *vm.State, v any) error {
	return state.Stack.PushHostValue(v)
}

func exportUnsignedBytes(x *big.Int, size int, msg string) ([]byte, error) {
	if x == nil || x.Sign() < 0 || x.BitLen() > size*8 {
		return nil, vmerr.Error(vmerr.CodeRangeCheck, msg)
	}
	buf := make([]byte, size)
	x.FillBytes(buf)
	return buf, nil
}

func paramAlias(name string, opcode uint16, idx int) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			v, err := state.GetParam(idx)
			if err != nil {
				return err
			}
			return pushHostValue(state, v)
		},
		Name:      name,
		BitPrefix: helpers.BytesPrefix(byte(opcode>>8), byte(opcode)),
	}
}

func ACCEPT() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			return state.SetGasLimit(vm.GasInfinite)
		},
		Name:      "ACCEPT",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x00),
	}
}

func SETGASLIMIT() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			x, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			limit := int64(0)
			if x.Sign() > 0 {
				if x.BitLen() <= 63 {
					limit = x.Int64()
				} else {
					limit = vm.GasInfinite
				}
			}
			return state.SetGasLimit(limit)
		},
		Name:      "SETGASLIMIT",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x01),
	}
}

func GASCONSUMED() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			return pushSmallInt(state, state.Gas.Used())
		},
		Name:       "GASCONSUMED",
		BitPrefix:  helpers.BytesPrefix(0xF8, 0x07),
		MinVersion: 4,
	}
}

func COMMIT() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			return state.ForceCommitCurrent()
		},
		Name:      "COMMIT",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x0F),
	}
}

func GETPARAM(idx uint8) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		NameSerializer: func() string {
			return fmt.Sprintf("GETPARAM %d", idx)
		},
		BitPrefix:     helpers.UIntPrefix(0xF82, 12),
		FixedSizeBits: 4,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(idx), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			v, err := code.LoadUInt(4)
			if err != nil {
				return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
			}
			idx = uint8(v)
			return nil
		},
		Action: func(state *vm.State) error {
			v, err := state.GetParam(int(idx))
			if err != nil {
				return err
			}
			return pushHostValue(state, v)
		},
	}
}

var getParamLongPrefixes = func() []helpers.BitPrefix {
	prefixes := make([]helpers.BitPrefix, 254)
	idx := 0
	for i := uint64(0); i < 255; i++ {
		if i == 17 {
			continue
		}
		prefixes[idx] = helpers.UIntPrefix(0xF88100|i, 24)
		idx++
	}
	return prefixes
}()

func GETPARAMLONG(idx uint8) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		NameSerializer: func() string {
			return fmt.Sprintf("GETPARAMLONG %d", idx)
		},
		BitPrefix:     helpers.BytesPrefix(0xF8, 0x81),
		Prefixes:      getParamLongPrefixes,
		FixedSizeBits: 8,
		MinVersion:    11,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(idx), 8)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			v, err := code.LoadUInt(8)
			if err != nil {
				return err
			}
			if v == 255 {
				return vm.ErrCorruptedOpcode
			}
			idx = uint8(v)
			return nil
		},
		Action: func(state *vm.State) error {
			v, err := state.GetParam(int(idx))
			if err != nil {
				return err
			}
			return pushHostValue(state, v)
		},
	}
}

func BLOCKLT() *helpers.SimpleOP       { return paramAlias("BLOCKLT", 0xF824, 4) }
func LTIME() *helpers.SimpleOP         { return paramAlias("LTIME", 0xF825, 5) }
func RANDSEED() *helpers.SimpleOP      { return paramAlias("RANDSEED", 0xF826, 6) }
func BALANCE() *helpers.SimpleOP       { return paramAlias("BALANCE", 0xF827, 7) }
func MYADDR() *helpers.SimpleOP        { return paramAlias("MYADDR", 0xF828, 8) }
func CONFIGROOT() *helpers.SimpleOP    { return paramAlias("CONFIGROOT", 0xF829, 9) }
func MYCODE() *helpers.SimpleOP        { return paramAlias("MYCODE", 0xF82A, 10) }
func INCOMINGVALUE() *helpers.SimpleOP { return paramAlias("INCOMINGVALUE", 0xF82B, 11) }
func STORAGEFEES() *helpers.SimpleOP   { return paramAlias("STORAGEFEES", 0xF82C, 12) }

func CONFIGDICT() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			v, err := state.GetParam(9)
			if err != nil {
				return err
			}
			if err = pushHostValue(state, v); err != nil {
				return err
			}
			return pushSmallInt(state, 32)
		},
		Name:      "CONFIGDICT",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x30),
	}
}

func configRootFromC7(state *vm.State) (*cell.Cell, error) {
	v, err := state.GetParam(9)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, nil
	}
	cl, ok := v.(*cell.Cell)
	if !ok {
		return nil, vmerr.Error(vmerr.CodeTypeCheck)
	}
	return cl, nil
}

func fitsSignedBits(x *big.Int, bits uint) bool {
	if x.Sign() >= 0 {
		return x.BitLen() <= int(bits-1)
	}

	absMinusOne := new(big.Int).Neg(x)
	absMinusOne.Sub(absMinusOne, funcsBigIntOne)
	return absMinusOne.BitLen() <= int(bits-1)
}

func loadConfigValue(state *vm.State, idx *big.Int) (*cell.Cell, error) {
	root, err := configRootFromC7(state)
	if err != nil {
		return nil, err
	}
	if root == nil || idx == nil || !fitsSignedBits(idx, 32) {
		return nil, nil
	}
	return loadConfigValueFromRoot(state, root, idx)
}

func loadConfigValueFromRoot(state *vm.State, root *cell.Cell, idx *big.Int) (*cell.Cell, error) {
	key := cell.BeginCell().MustStoreBigInt(idx, 32).EndCell()
	val, err := root.AsDict(32).SetTrace(state.Cells.Trace()).LoadValue(key)
	if err != nil {
		if errors.Is(err, cell.ErrNoSuchKeyInDict) {
			return nil, nil
		}
		return nil, vmerr.Error(vmerr.CodeDict, err.Error())
	}
	if val.BitsLeft() != 0 || val.RefsNum() != 1 {
		return nil, vmerr.Error(vmerr.CodeDict, "value is not a single ref")
	}
	ref, err := val.PeekRefCell()
	if err != nil {
		return nil, vmerr.Error(vmerr.CodeDict, err.Error())
	}
	return ref, nil
}

func CONFIGPARAM() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			idx, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			value, err := loadConfigValue(state, idx)
			if err != nil {
				return err
			}
			if value != nil {
				if err = state.Stack.PushCell(value); err != nil {
					return err
				}
				return state.Stack.PushBool(true)
			}
			return state.Stack.PushBool(false)
		},
		Name:      "CONFIGPARAM",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x32),
	}
}

func CONFIGOPTPARAM() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			idx, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			value, err := loadConfigValue(state, idx)
			if err != nil {
				return err
			}
			return pushHostValue(state, value)
		},
		Name:      "CONFIGOPTPARAM",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x33),
	}
}

func GLOBALID() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if state.GlobalVersion < 6 {
				rootAny, err := state.GetParam(19)
				if err != nil {
					return err
				}
				root, ok := rootAny.(*cell.Cell)
				if !ok || root == nil {
					return vmerr.Error(vmerr.CodeTypeCheck, "intermediate value is not a cell")
				}
				cl, err := loadConfigValueFromRoot(state, root, big.NewInt(19))
				if err != nil {
					return err
				}
				if cl == nil {
					return vmerr.Error(vmerr.CodeUnknown, "invalid global-id config")
				}
				cs, err := cl.BeginParse()
				if err != nil {
					return vmerr.Error(vmerr.CodeUnknown, "invalid global-id config")
				}
				if cs.BitsLeft() < 32 {
					return vmerr.Error(vmerr.CodeUnknown, "invalid global-id config")
				}
				return pushSmallInt(state, int64(int32(cs.MustLoadUInt(32))))
			}

			cfg, err := state.GetUnpackedConfigTuple()
			if err != nil {
				return err
			}
			v, err := cfg.Index(1)
			if err != nil {
				return err
			}
			cs, ok := v.(*cell.Slice)
			if !ok || cs == nil {
				return vmerr.Error(vmerr.CodeTypeCheck)
			}
			if cs.BitsLeft() < 32 {
				return vmerr.Error(vmerr.CodeCellUnderflow, "invalid global-id config")
			}
			return pushSmallInt(state, int64(int32(cs.MustPreloadUInt(32))))
		},
		Name:       "GLOBALID",
		BitPrefix:  helpers.BytesPrefix(0xF8, 0x35),
		MinVersion: 4,
	}
}

func signatureCheckOp(name string, prefix helpers.BitPrefix, fromSlice bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 3); err != nil {
				return err
			}

			keyInt, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			signature, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}

			var data []byte
			if fromSlice {
				cs, popErr := state.Stack.PopSlice()
				if popErr != nil {
					return popErr
				}
				if cs.BitsLeft()%8 != 0 {
					return vmerr.Error(vmerr.CodeCellUnderflow, "slice does not consist of a whole number of bytes")
				}
				data, err = cs.PreloadSlice(cs.BitsLeft())
				if err != nil {
					return vmerr.Error(vmerr.CodeCellUnderflow, "failed to preload signature data")
				}
			} else {
				hashInt, popErr := state.Stack.PopInt()
				if popErr != nil {
					return popErr
				}
				data, err = exportUnsignedBytes(hashInt, 32, "data hash must fit in an unsigned 256-bit integer")
				if err != nil {
					return err
				}
			}

			sigBytes, err := signature.PreloadSlice(512)
			if err != nil {
				return vmerr.Error(vmerr.CodeCellUnderflow, "ed25519 signature must contain at least 512 data bits")
			}
			keyBytes, err := exportUnsignedBytes(keyInt, 32, "ed25519 public key must fit in an unsigned 256-bit integer")
			if err != nil {
				return err
			}

			if err = state.RegisterSignatureCheckCall(); err != nil {
				return err
			}
			if state.GlobalVersion >= 14 && tvmEd25519RejectedPublicKeyV14(keyBytes) {
				return state.Stack.PushBool(state.SignatureCheckAlwaysSucceed)
			}

			return state.Stack.PushBool(tvmEd25519Verify(keyBytes, data, sigBytes) || state.SignatureCheckAlwaysSucceed)
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func tvmEd25519RejectedPublicKeyV14(key []byte) bool {
	if len(key) != 32 || (key[0] != 0 && key[0] != 1) {
		return false
	}
	for i := 1; i < len(key); i++ {
		if key[i] != 0 {
			return false
		}
	}
	return true
}

func tvmEd25519Verify(key, data, sig []byte) bool {
	if len(key) != 32 || len(sig) != 64 {
		return false
	}

	publicKey, err := new(edwards25519.Point).SetBytes(key)
	if err != nil {
		return false
	}
	signatureR, err := new(edwards25519.Point).SetBytes(sig[:32])
	if err != nil {
		return false
	}
	signatureS, err := new(edwards25519.Scalar).SetCanonicalBytes(sig[32:])
	if err != nil {
		return false
	}

	hash := sha512.New()
	_, _ = hash.Write(sig[:32])
	_, _ = hash.Write(key)
	_, _ = hash.Write(data)
	challenge, err := new(edwards25519.Scalar).SetUniformBytes(hash.Sum(nil))
	if err != nil {
		return false
	}

	// s*B == R + k*A  <=>  s*B - k*A == R; a single vartime double-scalar
	// multiplication is much cheaper than two constant-time ones, and inputs
	// are public. Point-level Equal keeps non-canonical R encodings behaving
	// exactly like the previous Add+Equal check.
	minusA := new(edwards25519.Point).Negate(publicKey)
	rCheck := new(edwards25519.Point).VarTimeDoubleScalarBaseMult(challenge, minusA, signatureS)
	return rCheck.Equal(signatureR) == 1
}

func CHKSIGNU() *helpers.SimpleOP {
	return signatureCheckOp("CHKSIGNU", helpers.BytesPrefix(0xF9, 0x10), false)
}

func CHKSIGNS() *helpers.SimpleOP {
	return signatureCheckOp("CHKSIGNS", helpers.BytesPrefix(0xF9, 0x11), true)
}

func preloadFixedBytes(sl *cell.Slice, bits uint, msg string) ([]byte, error) {
	data, err := sl.PreloadSlice(bits)
	if err != nil {
		return nil, vmerr.Error(vmerr.CodeCellUnderflow, msg)
	}
	return data, nil
}

func ECRECOVER() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 4); err != nil {
				return err
			}

			sInt, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			rInt, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			v, err := state.Stack.PopIntRangeInt64(0, 255)
			if err != nil {
				return err
			}
			hashInt, err := state.Stack.PopInt()
			if err != nil {
				return err
			}

			rBytes, err := exportUnsignedBytes(rInt, 32, "r must fit in an unsigned 256-bit integer")
			if err != nil {
				return err
			}
			sBytes, err := exportUnsignedBytes(sInt, 32, "s must fit in an unsigned 256-bit integer")
			if err != nil {
				return err
			}
			hashBytes, err := exportUnsignedBytes(hashInt, 32, "data hash must fit in an unsigned 256-bit integer")
			if err != nil {
				return err
			}

			if err = state.ConsumeGas(vm.EcrecoverGasPrice); err != nil {
				return err
			}

			if state.GlobalVersion >= 14 && (v == 27 || v == 28) {
				v -= 27
			}
			if v > 3 {
				return state.Stack.PushBool(false)
			}

			pub, ok := localec.RecoverUncompressed(hashBytes, new(big.Int).SetBytes(rBytes), new(big.Int).SetBytes(sBytes), byte(v))
			if !ok {
				return state.Stack.PushBool(false)
			}

			x := new(big.Int).SetBytes(pub[1:33])
			y := new(big.Int).SetBytes(pub[33:65])

			if err = pushSmallInt(state, int64(pub[0])); err != nil {
				return err
			}
			if err = state.Stack.PushInt(x); err != nil {
				return err
			}
			if err = state.Stack.PushInt(y); err != nil {
				return err
			}
			return state.Stack.PushBool(true)
		},
		Name:       "ECRECOVER",
		BitPrefix:  helpers.BytesPrefix(0xF9, 0x12),
		MinVersion: 4,
	}
}

func SECP256K1_XONLY_PUBKEY_TWEAK_ADD() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}

			tweakInt, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			keyInt, err := state.Stack.PopInt()
			if err != nil {
				return err
			}

			keyBytes, err := exportUnsignedBytes(keyInt, 32, "key must fit in an unsigned 256-bit integer")
			if err != nil {
				return err
			}
			tweakBytes, err := exportUnsignedBytes(tweakInt, 32, "tweak must fit in an unsigned 256-bit integer")
			if err != nil {
				return err
			}

			if err = state.ConsumeGas(vm.Secp256k1XonlyPubkeyTweakAddGasPrice); err != nil {
				return err
			}

			pub, ok := localec.XOnlyPubkeyTweakAddUncompressed(keyBytes, tweakBytes)
			if !ok {
				return state.Stack.PushBool(false)
			}

			x := new(big.Int).SetBytes(pub[1:33])
			y := new(big.Int).SetBytes(pub[33:65])

			if err = pushSmallInt(state, int64(pub[0])); err != nil {
				return err
			}
			if err = state.Stack.PushInt(x); err != nil {
				return err
			}
			if err = state.Stack.PushInt(y); err != nil {
				return err
			}
			return state.Stack.PushBool(true)
		},
		Name:       "SECP256K1_XONLY_PUBKEY_TWEAK_ADD",
		BitPrefix:  helpers.BytesPrefix(0xF9, 0x13),
		MinVersion: 9,
	}
}

func p256CheckSignOp(name string, prefix helpers.BitPrefix, fromSlice bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 3); err != nil {
				return err
			}

			keySlice, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			signatureSlice, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}

			var data []byte
			if fromSlice {
				msgSlice, popErr := state.Stack.PopSlice()
				if popErr != nil {
					return popErr
				}
				if msgSlice.BitsLeft()&7 != 0 {
					return vmerr.Error(vmerr.CodeCellUnderflow, "slice does not consist of an integer number of bytes")
				}
				data, err = msgSlice.PreloadSlice(msgSlice.BitsLeft())
				if err != nil {
					return vmerr.Error(vmerr.CodeCellUnderflow, "slice does not consist of an integer number of bytes")
				}
			} else {
				hashInt, popErr := state.Stack.PopInt()
				if popErr != nil {
					return popErr
				}
				data, err = exportUnsignedBytes(hashInt, 32, "data hash must fit in an unsigned 256-bit integer")
				if err != nil {
					return err
				}
			}

			signatureBytes, err := preloadFixedBytes(signatureSlice, 512, "p256 signature must contain at least 512 data bits")
			if err != nil {
				return err
			}
			keyBytes, err := preloadFixedBytes(keySlice, 264, "p256 public key must contain at least 33 data bytes")
			if err != nil {
				return err
			}

			if err = state.ConsumeGas(vm.P256SignatureCheckGasPrice); err != nil {
				return err
			}
			if state.SignatureCheckAlwaysSucceed {
				return state.Stack.PushBool(true)
			}

			x, y := elliptic.UnmarshalCompressed(elliptic.P256(), keyBytes)
			if x == nil || y == nil {
				return state.Stack.PushBool(false)
			}

			digest := sha256.Sum256(data)
			r := new(big.Int).SetBytes(signatureBytes[:32])
			s := new(big.Int).SetBytes(signatureBytes[32:64])
			ok := ecdsa.Verify(&ecdsa.PublicKey{
				Curve: elliptic.P256(),
				X:     x,
				Y:     y,
			}, digest[:], r, s)

			return state.Stack.PushBool(ok)
		},
		Name:       name,
		BitPrefix:  prefix,
		MinVersion: 4,
	}
}

func P256_CHKSIGNU() *helpers.SimpleOP {
	return p256CheckSignOp("P256_CHKSIGNU", helpers.BytesPrefix(0xF9, 0x14), false)
}

func P256_CHKSIGNS() *helpers.SimpleOP {
	return p256CheckSignOp("P256_CHKSIGNS", helpers.BytesPrefix(0xF9, 0x15), true)
}

func GETGLOBVAR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			idx, err := state.Stack.PopIntRangeInt64(0, 254)
			if err != nil {
				return err
			}
			v, err := state.GetGlobal(int(idx))
			if err != nil {
				return err
			}
			return pushHostValue(state, v)
		},
		Name:      "GETGLOBVAR",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x40),
	}
}

// constant GETGLOB/SETGLOB prefixes, computed once instead of on every decode
var (
	getGlobPrefix = helpers.UIntPrefix(0x7C2, 11)
	setGlobPrefix = helpers.UIntPrefix(0x7C3, 11)
)

// OpGETGLOB is a struct-based opcode: one allocation per executed instruction
// instead of an AdvancedOP carrying per-instance closures.
type OpGETGLOB struct {
	idx uint8
}

func GETGLOB(idx uint8) *OpGETGLOB {
	return &OpGETGLOB{idx: idx}
}

func (op *OpGETGLOB) GetPrefixes() []*cell.Slice {
	return helpers.PrefixSlices(getGlobPrefix)
}

func (op *OpGETGLOB) Deserialize(code *cell.Slice) error {
	if err := code.SkipBits(getGlobPrefix.Bits); err != nil {
		return err
	}

	v, err := code.LoadUInt(5)
	if err != nil {
		return err
	}
	op.idx = uint8(v)
	return nil
}

func (op *OpGETGLOB) Serialize() *cell.Builder {
	return cell.BeginCell().
		MustStoreSlice(getGlobPrefix.Data, getGlobPrefix.Bits).
		MustStoreUInt(uint64(op.idx&31), 5)
}

func (op *OpGETGLOB) SerializeText() string {
	return fmt.Sprintf("GETGLOB %d", op.idx&31)
}

func (op *OpGETGLOB) InstructionBits() int64 {
	return int64(getGlobPrefix.Bits) + 5
}

func (op *OpGETGLOB) Interpret(state *vm.State) error {
	v, err := state.GetGlobal(int(op.idx & 31))
	if err != nil {
		return err
	}
	return pushHostValue(state, v)
}

func SETGLOBVAR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if state.Stack.Len() < 2 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}
			idx, err := state.Stack.PopIntRangeInt64(0, 254)
			if err != nil {
				return err
			}
			val, err := state.Stack.PopAny()
			if err != nil {
				return err
			}
			return state.SetGlobal(int(idx), val)
		},
		Name:      "SETGLOBVAR",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x60),
	}
}

// OpSETGLOB is a struct-based opcode: one allocation per executed instruction
// instead of an AdvancedOP carrying per-instance closures.
type OpSETGLOB struct {
	idx uint8
}

func SETGLOB(idx uint8) *OpSETGLOB {
	return &OpSETGLOB{idx: idx}
}

func (op *OpSETGLOB) GetPrefixes() []*cell.Slice {
	return helpers.PrefixSlices(setGlobPrefix)
}

func (op *OpSETGLOB) Deserialize(code *cell.Slice) error {
	if err := code.SkipBits(setGlobPrefix.Bits); err != nil {
		return err
	}

	v, err := code.LoadUInt(5)
	if err != nil {
		return err
	}
	op.idx = uint8(v)
	return nil
}

func (op *OpSETGLOB) Serialize() *cell.Builder {
	return cell.BeginCell().
		MustStoreSlice(setGlobPrefix.Data, setGlobPrefix.Bits).
		MustStoreUInt(uint64(op.idx&31), 5)
}

func (op *OpSETGLOB) SerializeText() string {
	return fmt.Sprintf("SETGLOB %d", op.idx&31)
}

func (op *OpSETGLOB) InstructionBits() int64 {
	return int64(setGlobPrefix.Bits) + 5
}

func (op *OpSETGLOB) Interpret(state *vm.State) error {
	val, err := state.Stack.PopAny()
	if err != nil {
		return err
	}
	return state.SetGlobal(int(op.idx&31), val)
}
