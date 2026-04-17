package funcs

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"

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
	return state.Stack.PushInt(big.NewInt(v))
}

func pushHostValue(state *vm.State, v any) error {
	switch x := v.(type) {
	case int:
		return state.Stack.PushInt(big.NewInt(int64(x)))
	case int8:
		return state.Stack.PushInt(big.NewInt(int64(x)))
	case int16:
		return state.Stack.PushInt(big.NewInt(int64(x)))
	case int32:
		return state.Stack.PushInt(big.NewInt(int64(x)))
	case int64:
		return state.Stack.PushInt(big.NewInt(x))
	case uint8:
		return state.Stack.PushInt(new(big.Int).SetUint64(uint64(x)))
	case uint16:
		return state.Stack.PushInt(new(big.Int).SetUint64(uint64(x)))
	case uint32:
		return state.Stack.PushInt(new(big.Int).SetUint64(uint64(x)))
	case uint64:
		return state.Stack.PushInt(new(big.Int).SetUint64(x))
	case *cell.Cell:
		if x == nil {
			return state.Stack.PushAny(nil)
		}
	case *cell.Slice:
		if x == nil {
			return state.Stack.PushAny(nil)
		}
	case *cell.Builder:
		if x == nil {
			return state.Stack.PushAny(nil)
		}
	}
	return state.Stack.PushAny(v)
}

func exportUnsignedBytes(x *big.Int, size int, msg string) ([]byte, error) {
	if x.Sign() < 0 || x.BitLen() > size*8 {
		return nil, vmerr.Error(vmerr.CodeRangeCheck, msg)
	}
	buf := make([]byte, size)
	raw := x.Bytes()
	copy(buf[len(buf)-len(raw):], raw)
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
		Name:      "GASCONSUMED",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x07),
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
				return err
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

func GETPARAMLONG(idx uint8) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		NameSerializer: func() string {
			return fmt.Sprintf("GETPARAMLONG %d", idx)
		},
		BitPrefix:     helpers.BytesPrefix(0xF8, 0x81),
		FixedSizeBits: 8,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(idx), 8)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			v, err := code.LoadUInt(8)
			if err != nil {
				return err
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

func loadConfigValue(state *vm.State, idx *big.Int) (*cell.Cell, error) {
	if idx == nil || idx.Sign() < 0 || idx.BitLen() > 32 {
		return nil, nil
	}

	root, err := configRootFromC7(state)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, nil
	}

	key := cell.BeginCell().MustStoreBigUInt(idx, 32).EndCell()
	val, err := root.AsDict(32).SetObserver(&state.Cells).LoadValue(key)
	if err != nil {
		if errors.Is(err, cell.ErrNoSuchKeyInDict) {
			return nil, nil
		}
		return nil, err
	}
	if val.BitsLeft() != 0 || val.RefsNum() != 1 {
		return nil, errors.New("value is not a single ref")
	}
	return val.PeekRefCell()
}

func CONFIGPARAM() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			idx, err := state.Stack.PopIntFinite()
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
			idx, err := state.Stack.PopIntFinite()
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
			cfg, err := state.GetUnpackedConfigTuple()
			if err != nil {
				return err
			}
			v, err := cfg.Index(1)
			if err != nil {
				return err
			}
			cs, ok := v.(*cell.Slice)
			if !ok {
				return vmerr.Error(vmerr.CodeTypeCheck)
			}
			if cs.BitsLeft() < 32 {
				return vmerr.Error(vmerr.CodeCellUnderflow, "invalid global-id config")
			}
			return pushSmallInt(state, int64(int32(cs.MustPreloadUInt(32))))
		},
		Name:      "GLOBALID",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x35),
	}
}

func chksignOp(name string, prefix helpers.BitPrefix, fromSlice bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			keyInt, err := state.Stack.PopIntFinite()
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
				hashInt, popErr := state.Stack.PopIntFinite()
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

			if err = state.RegisterChksgnCall(); err != nil {
				return err
			}

			return state.Stack.PushBool(ed25519.Verify(ed25519.PublicKey(keyBytes), data, sigBytes))
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func CHKSIGNU() *helpers.SimpleOP {
	return chksignOp("CHKSIGNU", helpers.BytesPrefix(0xF9, 0x10), false)
}

func CHKSIGNS() *helpers.SimpleOP {
	return chksignOp("CHKSIGNS", helpers.BytesPrefix(0xF9, 0x11), true)
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
			sInt, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			rInt, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			vInt, err := state.Stack.PopIntRange(0, 255)
			if err != nil {
				return err
			}
			hashInt, err := state.Stack.PopIntFinite()
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

			v := vInt.Int64()
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
		Name:      "ECRECOVER",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x12),
	}
}

func SECP256K1_XONLY_PUBKEY_TWEAK_ADD() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			tweakInt, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			keyInt, err := state.Stack.PopIntFinite()
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
		Name:      "SECP256K1_XONLY_PUBKEY_TWEAK_ADD",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x13),
	}
}

func p256CheckSignOp(name string, prefix helpers.BitPrefix, fromSlice bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
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
				hashInt, popErr := state.Stack.PopIntFinite()
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

			if err = state.ConsumeGas(vm.P256ChksgnGasPrice); err != nil {
				return err
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
		Name:      name,
		BitPrefix: prefix,
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
			idx, err := state.Stack.PopIntRange(0, 254)
			if err != nil {
				return err
			}
			v, err := state.GetGlobal(int(idx.Int64()))
			if err != nil {
				return err
			}
			return pushHostValue(state, v)
		},
		Name:      "GETGLOBVAR",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x40),
	}
}

func GETGLOB(idx uint8) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		NameSerializer: func() string {
			return fmt.Sprintf("GETGLOB %d", idx&31)
		},
		BitPrefix:     helpers.UIntPrefix(0x7C2, 11),
		FixedSizeBits: 5,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(idx&31), 5)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			v, err := code.LoadUInt(5)
			if err != nil {
				return err
			}
			idx = uint8(v)
			return nil
		},
		Action: func(state *vm.State) error {
			v, err := state.GetGlobal(int(idx))
			if err != nil {
				return err
			}
			return pushHostValue(state, v)
		},
	}
}

func SETGLOBVAR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			idx, err := state.Stack.PopIntRange(0, 254)
			if err != nil {
				return err
			}
			val, err := state.Stack.PopAny()
			if err != nil {
				return err
			}
			return state.SetGlobal(int(idx.Int64()), val)
		},
		Name:      "SETGLOBVAR",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x60),
	}
}

func SETGLOB(idx uint8) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		NameSerializer: func() string {
			return fmt.Sprintf("SETGLOB %d", idx&31)
		},
		BitPrefix:     helpers.UIntPrefix(0x7C3, 11),
		FixedSizeBits: 5,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(idx&31), 5)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			v, err := code.LoadUInt(5)
			if err != nil {
				return err
			}
			idx = uint8(v)
			return nil
		},
		Action: func(state *vm.State) error {
			val, err := state.Stack.PopAny()
			if err != nil {
				return err
			}
			return state.SetGlobal(int(idx), val)
		},
	}
}
