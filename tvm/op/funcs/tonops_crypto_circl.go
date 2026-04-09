package funcs

import (
	"bytes"
	"math/big"

	ristretto "github.com/bwesterb/go-ristretto"
	circlbls "github.com/cloudflare/circl/ecc/bls12381"
	circlgroup "github.com/cloudflare/circl/group"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/internal/blsmap"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

const tonBLSSignatureDST = "BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_"

var (
	ristretto255L = func() *big.Int {
		n, ok := new(big.Int).SetString("7237005577332262213973186563042994240857116359379907606001950938285454250989", 10)
		if !ok {
			panic("failed to parse ristretto255 order")
		}
		return n
	}()
	blsOrder = func() *big.Int {
		return new(big.Int).SetBytes(circlbls.Order())
	}()
	blsG1ZeroCompressed = func() []byte {
		var p circlbls.G1
		p.SetIdentity()
		return p.BytesCompressed()
	}()
	blsG2ZeroCompressed = func() []byte {
		var p circlbls.G2
		p.SetIdentity()
		return p.BytesCompressed()
	}()
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return RIST255_FROMHASH() },
		func() vm.OP { return RIST255_VALIDATE() },
		func() vm.OP { return RIST255_ADD() },
		func() vm.OP { return RIST255_SUB() },
		func() vm.OP { return RIST255_MUL() },
		func() vm.OP { return RIST255_MULBASE() },
		func() vm.OP { return RIST255_PUSHL() },
		func() vm.OP { return RIST255_QVALIDATE() },
		func() vm.OP { return RIST255_QADD() },
		func() vm.OP { return RIST255_QSUB() },
		func() vm.OP { return RIST255_QMUL() },
		func() vm.OP { return RIST255_QMULBASE() },
		func() vm.OP { return BLS_VERIFY() },
		func() vm.OP { return BLS_AGGREGATE() },
		func() vm.OP { return BLS_FASTAGGREGATEVERIFY() },
		func() vm.OP { return BLS_AGGREGATEVERIFY() },
		func() vm.OP { return BLS_G1_ADD() },
		func() vm.OP { return BLS_G1_SUB() },
		func() vm.OP { return BLS_G1_NEG() },
		func() vm.OP { return BLS_G1_MUL() },
		func() vm.OP { return BLS_G1_MULTIEXP() },
		func() vm.OP { return BLS_G1_ZERO() },
		func() vm.OP { return BLS_MAP_TO_G1() },
		func() vm.OP { return BLS_G1_INGROUP() },
		func() vm.OP { return BLS_G1_ISZERO() },
		func() vm.OP { return BLS_G2_ADD() },
		func() vm.OP { return BLS_G2_SUB() },
		func() vm.OP { return BLS_G2_NEG() },
		func() vm.OP { return BLS_G2_MUL() },
		func() vm.OP { return BLS_G2_MULTIEXP() },
		func() vm.OP { return BLS_G2_ZERO() },
		func() vm.OP { return BLS_MAP_TO_G2() },
		func() vm.OP { return BLS_G2_INGROUP() },
		func() vm.OP { return BLS_G2_ISZERO() },
		func() vm.OP { return BLS_PAIRING() },
		func() vm.OP { return BLS_PUSHR() },
	)
}

func pushSliceBytes(state *vm.State, data []byte) error {
	return state.Stack.PushSlice(cell.BeginCell().MustStoreSlice(data, uint(len(data))*8).ToSlice())
}

func blsUnknown(msg string, err error) error {
	if err == nil {
		return vmerr.Error(vmerr.CodeUnknown, msg)
	}
	return vmerr.Error(vmerr.CodeUnknown, msg+": "+err.Error())
}

func blsScalarFromInt(x *big.Int) *circlbls.Scalar {
	var scalar circlbls.Scalar
	n := new(big.Int).Mod(new(big.Int).Set(x), blsOrder)
	scalar.SetBytes(n.FillBytes(make([]byte, circlbls.ScalarSize)))
	return &scalar
}

func blsScalarMod(x *big.Int) *big.Int {
	return new(big.Int).Mod(new(big.Int).Set(x), blsOrder)
}

func blsCalculateMultiexpGas(n int, base, coef1, coef2 int64) int64 {
	l := 4
	for (1 << (l + 1)) <= n {
		l++
	}
	return base + int64(n)*coef1 + int64(n)*coef2/int64(l)
}

func popBLSMsg(state *vm.State) ([]byte, error) {
	msgSlice, err := state.Stack.PopSlice()
	if err != nil {
		return nil, err
	}
	if msgSlice.BitsLeft()%8 != 0 {
		return nil, vmerr.Error(vmerr.CodeCellUnderflow, "message does not consist of an integer number of bytes")
	}
	msg, err := msgSlice.PreloadSlice(msgSlice.BitsLeft())
	if err != nil {
		return nil, vmerr.Error(vmerr.CodeCellUnderflow, "message does not consist of an integer number of bytes")
	}
	return msg, nil
}

func preloadBLSPoint(sl *cell.Slice, bits uint, msg string) ([]byte, error) {
	data, err := sl.PreloadSlice(bits)
	if err != nil {
		return nil, vmerr.Error(vmerr.CodeCellUnderflow, msg)
	}
	return data, nil
}

func parseBLSG1(data []byte) (*circlbls.G1, error) {
	var p circlbls.G1
	if err := p.SetBytes(data); err != nil {
		return nil, err
	}
	return &p, nil
}

func parseBLSG2(data []byte) (*circlbls.G2, error) {
	var p circlbls.G2
	if err := p.SetBytes(data); err != nil {
		return nil, err
	}
	return &p, nil
}

func blsHashToG2(msg []byte) *circlbls.G2 {
	var h circlbls.G2
	h.Hash(msg, []byte(tonBLSSignatureDST))
	return &h
}

func blsVerify(pubBytes, msg, sigBytes []byte) bool {
	pub, err := parseBLSG1(pubBytes)
	if err != nil || pub.IsIdentity() {
		return false
	}
	sig, err := parseBLSG2(sigBytes)
	if err != nil {
		return false
	}
	hash := blsHashToG2(msg)
	res := circlbls.ProdPairFrac(
		[]*circlbls.G1{pub, circlbls.G1Generator()},
		[]*circlbls.G2{hash, sig},
		[]int{1, -1},
	)
	return res.IsIdentity()
}

func blsFastAggregateVerify(pubBytes [][]byte, msg, sigBytes []byte) bool {
	if len(pubBytes) == 0 {
		return false
	}

	var agg circlbls.G1
	for i, pubData := range pubBytes {
		pub, err := parseBLSG1(pubData)
		if err != nil || pub.IsIdentity() {
			return false
		}
		if i == 0 {
			agg = *pub
		} else {
			agg.Add(&agg, pub)
		}
	}
	return blsVerify(agg.BytesCompressed(), msg, sigBytes)
}

func blsAggregateVerify(pubs [][]byte, msgs [][]byte, sigBytes []byte) bool {
	if len(pubs) == 0 || len(pubs) != len(msgs) {
		return false
	}

	listG1 := make([]*circlbls.G1, 0, len(pubs)+1)
	listG2 := make([]*circlbls.G2, 0, len(pubs)+1)
	signs := make([]int, 0, len(pubs)+1)

	for i := range pubs {
		pub, err := parseBLSG1(pubs[i])
		if err != nil || pub.IsIdentity() {
			return false
		}
		listG1 = append(listG1, pub)
		listG2 = append(listG2, blsHashToG2(msgs[i]))
		signs = append(signs, 1)
	}

	sig, err := parseBLSG2(sigBytes)
	if err != nil {
		return false
	}

	listG1 = append(listG1, circlbls.G1Generator())
	listG2 = append(listG2, sig)
	signs = append(signs, -1)

	return circlbls.ProdPairFrac(listG1, listG2, signs).IsIdentity()
}

func parseRistrettoPoint(x *big.Int) (circlgroup.Element, error) {
	buf, err := exportUnsignedBytes(x, 32, "x must fit in an unsigned 256-bit integer")
	if err != nil {
		return nil, err
	}
	point := circlgroup.Ristretto255.NewElement()
	if err = point.UnmarshalBinary(buf); err != nil {
		return nil, vmerr.Error(vmerr.CodeRangeCheck, "x is not a valid encoded element")
	}
	return point, nil
}

func pushRistrettoPoint(state *vm.State, point circlgroup.Element) error {
	data, err := point.MarshalBinary()
	if err != nil {
		return blsUnknown("failed to encode ristretto255 element", err)
	}
	return state.Stack.PushInt(new(big.Int).SetBytes(data))
}

func RIST255_FROMHASH() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			x2, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			x1, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			if err = state.ConsumeGas(vm.Rist255FromhashGasPrice); err != nil {
				return err
			}

			x1Bytes, err := exportUnsignedBytes(x1, 32, "x1 must fit in an unsigned 256-bit integer")
			if err != nil {
				return err
			}
			x2Bytes, err := exportUnsignedBytes(x2, 32, "x2 must fit in an unsigned 256-bit integer")
			if err != nil {
				return err
			}

			var u1, u2 [32]byte
			copy(u1[:], x1Bytes)
			copy(u2[:], x2Bytes)

			var p1, p2, out ristretto.Point
			p1.SetElligator(&u1)
			p2.SetElligator(&u2)
			out.Add(&p1, &p2)
			return state.Stack.PushInt(new(big.Int).SetBytes(out.Bytes()))
		},
		Name:      "RIST255_FROMHASH",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x20),
	}
}

func pushConstIntOp(name string, prefix helpers.BitPrefix, value *big.Int) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			return state.Stack.PushInt(new(big.Int).Set(value))
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func RIST255_PUSHL() *helpers.SimpleOP {
	return pushConstIntOp("RIST255_PUSHL", helpers.BytesPrefix(0xF9, 0x26), ristretto255L)
}

func rist255ValidateOp(name string, prefix helpers.BitPrefix, quiet bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			x, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			if err = state.ConsumeGas(vm.Rist255ValidateGasPrice); err != nil {
				return err
			}
			_, err = parseRistrettoPoint(x)
			if err != nil {
				if quiet {
					return state.Stack.PushBool(false)
				}
				return err
			}
			if quiet {
				return state.Stack.PushBool(true)
			}
			return nil
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func rist255BinaryOp(name string, prefix helpers.BitPrefix, quiet bool, op func(circlgroup.Element, circlgroup.Element) circlgroup.Element) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			y, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			x, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			if err = state.ConsumeGas(vm.Rist255AddGasPrice); err != nil {
				return err
			}

			px, err := parseRistrettoPoint(x)
			if err != nil {
				if quiet {
					return state.Stack.PushBool(false)
				}
				return err
			}
			py, err := parseRistrettoPoint(y)
			if err != nil {
				if quiet {
					return state.Stack.PushBool(false)
				}
				return err
			}

			if err = pushRistrettoPoint(state, op(px, py)); err != nil {
				return err
			}
			if quiet {
				return state.Stack.PushBool(true)
			}
			return nil
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func rist255MulOp(name string, prefix helpers.BitPrefix, quiet bool, base bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			n, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			if err = state.ConsumeGas(func() int64 {
				if base {
					return vm.Rist255MulbaseGasPrice
				}
				return vm.Rist255MulGasPrice
			}()); err != nil {
				return err
			}

			var point circlgroup.Element
			if !base {
				x, popErr := state.Stack.PopIntFinite()
				if popErr != nil {
					return popErr
				}
				point, err = parseRistrettoPoint(x)
				if err != nil {
					if quiet {
						return state.Stack.PushBool(false)
					}
					return err
				}
			}

			scalar := circlgroup.Ristretto255.NewScalar().SetBigInt(new(big.Int).Mod(new(big.Int).Set(n), ristretto255L))
			if scalar.IsZero() {
				if err = pushSmallInt(state, 0); err != nil {
					return err
				}
				if quiet {
					return state.Stack.PushBool(true)
				}
				return nil
			}

			result := circlgroup.Ristretto255.NewElement()
			if base {
				result.MulGen(scalar)
			} else {
				result.Mul(point, scalar)
			}
			if err = pushRistrettoPoint(state, result); err != nil {
				return err
			}
			if quiet {
				return state.Stack.PushBool(true)
			}
			return nil
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func RIST255_VALIDATE() *helpers.SimpleOP {
	return rist255ValidateOp("RIST255_VALIDATE", helpers.BytesPrefix(0xF9, 0x21), false)
}

func RIST255_QVALIDATE() *helpers.SimpleOP {
	return rist255ValidateOp("RIST255_QVALIDATE", helpers.BytesPrefix(0xB7, 0xF9, 0x21), true)
}

func RIST255_ADD() *helpers.SimpleOP {
	return rist255BinaryOp("RIST255_ADD", helpers.BytesPrefix(0xF9, 0x22), false, func(x, y circlgroup.Element) circlgroup.Element {
		return circlgroup.Ristretto255.NewElement().Add(x, y)
	})
}

func RIST255_QADD() *helpers.SimpleOP {
	return rist255BinaryOp("RIST255_QADD", helpers.BytesPrefix(0xB7, 0xF9, 0x22), true, func(x, y circlgroup.Element) circlgroup.Element {
		return circlgroup.Ristretto255.NewElement().Add(x, y)
	})
}

func RIST255_SUB() *helpers.SimpleOP {
	return rist255BinaryOp("RIST255_SUB", helpers.BytesPrefix(0xF9, 0x23), false, func(x, y circlgroup.Element) circlgroup.Element {
		negY := circlgroup.Ristretto255.NewElement().Neg(y)
		return circlgroup.Ristretto255.NewElement().Add(x, negY)
	})
}

func RIST255_QSUB() *helpers.SimpleOP {
	return rist255BinaryOp("RIST255_QSUB", helpers.BytesPrefix(0xB7, 0xF9, 0x23), true, func(x, y circlgroup.Element) circlgroup.Element {
		negY := circlgroup.Ristretto255.NewElement().Neg(y)
		return circlgroup.Ristretto255.NewElement().Add(x, negY)
	})
}

func RIST255_MUL() *helpers.SimpleOP {
	return rist255MulOp("RIST255_MUL", helpers.BytesPrefix(0xF9, 0x24), false, false)
}

func RIST255_QMUL() *helpers.SimpleOP {
	return rist255MulOp("RIST255_QMUL", helpers.BytesPrefix(0xB7, 0xF9, 0x24), true, false)
}

func RIST255_MULBASE() *helpers.SimpleOP {
	return rist255MulOp("RIST255_MULBASE", helpers.BytesPrefix(0xF9, 0x25), false, true)
}

func RIST255_QMULBASE() *helpers.SimpleOP {
	return rist255MulOp("RIST255_QMULBASE", helpers.BytesPrefix(0xB7, 0xF9, 0x25), true, true)
}

func BLS_VERIFY() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := state.ConsumeGas(vm.BlsVerifyGasPrice); err != nil {
				return err
			}

			sigSlice, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			msg, err := popBLSMsg(state)
			if err != nil {
				return err
			}
			pubSlice, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}

			sigBytes, err := preloadBLSPoint(sigSlice, 96*8, "slice must contain at least 96 bytes")
			if err != nil {
				return err
			}
			pubBytes, err := preloadBLSPoint(pubSlice, 48*8, "slice must contain at least 48 bytes")
			if err != nil {
				return err
			}
			return state.Stack.PushBool(blsVerify(pubBytes, msg, sigBytes))
		},
		Name:      "BLS_VERIFY",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x30, 0x00),
	}
}

func BLS_AGGREGATE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			n, err := state.Stack.PopIntRange(1, int64(state.Stack.Len()))
			if err != nil {
				return err
			}
			count := int(n.Int64())
			if err = state.ConsumeGas(vm.BlsAggregateBaseGasPrice + int64(count)*vm.BlsAggregateElementGasPrice); err != nil {
				return err
			}

			var agg circlbls.G2
			for i := 0; i < count; i++ {
				sl, popErr := state.Stack.PopSlice()
				if popErr != nil {
					return popErr
				}
				data, preErr := preloadBLSPoint(sl, 96*8, "slice must contain at least 96 bytes")
				if preErr != nil {
					return preErr
				}
				p, parseErr := parseBLSG2(data)
				if parseErr != nil {
					return blsUnknown("invalid bls signature", parseErr)
				}
				if i == 0 {
					agg = *p
				} else {
					agg.Add(&agg, p)
				}
			}
			return pushSliceBytes(state, agg.BytesCompressed())
		},
		Name:      "BLS_AGGREGATE",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x30, 0x01),
	}
}

func BLS_FASTAGGREGATEVERIFY() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			sigSlice, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			msg, err := popBLSMsg(state)
			if err != nil {
				return err
			}
			n, err := state.Stack.PopIntRange(0, int64(state.Stack.Len()))
			if err != nil {
				return err
			}
			count := int(n.Int64())
			if err = state.ConsumeGas(vm.BlsFastAggregateVerifyBaseGasPrice + int64(count)*vm.BlsFastAggregateVerifyElementGasPrice); err != nil {
				return err
			}

			sigBytes, err := preloadBLSPoint(sigSlice, 96*8, "slice must contain at least 96 bytes")
			if err != nil {
				return err
			}

			pubs := make([][]byte, count)
			for i := count - 1; i >= 0; i-- {
				sl, popErr := state.Stack.PopSlice()
				if popErr != nil {
					return popErr
				}
				pubs[i], err = preloadBLSPoint(sl, 48*8, "slice must contain at least 48 bytes")
				if err != nil {
					return err
				}
			}

			return state.Stack.PushBool(blsFastAggregateVerify(pubs, msg, sigBytes))
		},
		Name:      "BLS_FASTAGGREGATEVERIFY",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x30, 0x02),
	}
}

func BLS_AGGREGATEVERIFY() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			sigSlice, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			n, err := state.Stack.PopIntRange(0, int64(state.Stack.Len()/2))
			if err != nil {
				return err
			}
			count := int(n.Int64())
			if err = state.ConsumeGas(vm.BlsAggregateVerifyBaseGasPrice + int64(count)*vm.BlsAggregateVerifyElementGasPrice); err != nil {
				return err
			}

			sigBytes, err := preloadBLSPoint(sigSlice, 96*8, "slice must contain at least 96 bytes")
			if err != nil {
				return err
			}

			pubs := make([][]byte, count)
			msgs := make([][]byte, count)
			for i := count - 1; i >= 0; i-- {
				msgs[i], err = popBLSMsg(state)
				if err != nil {
					return err
				}
				pubSlice, popErr := state.Stack.PopSlice()
				if popErr != nil {
					return popErr
				}
				pubs[i], err = preloadBLSPoint(pubSlice, 48*8, "slice must contain at least 48 bytes")
				if err != nil {
					return err
				}
			}

			return state.Stack.PushBool(blsAggregateVerify(pubs, msgs, sigBytes))
		},
		Name:      "BLS_AGGREGATEVERIFY",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x30, 0x03),
	}
}

func blsG1BinaryOp(name string, prefix helpers.BitPrefix, op func(a, b *circlbls.G1) []byte) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := state.ConsumeGas(vm.BlsG1AddSubGasPrice); err != nil {
				return err
			}
			bSlice, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			aSlice, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			bData, err := preloadBLSPoint(bSlice, 48*8, "slice must contain at least 48 bytes")
			if err != nil {
				return err
			}
			aData, err := preloadBLSPoint(aSlice, 48*8, "slice must contain at least 48 bytes")
			if err != nil {
				return err
			}
			a, err := parseBLSG1(aData)
			if err != nil {
				return blsUnknown("invalid bls g1 point", err)
			}
			b, err := parseBLSG1(bData)
			if err != nil {
				return blsUnknown("invalid bls g1 point", err)
			}
			return pushSliceBytes(state, op(a, b))
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func BLS_G1_ADD() *helpers.SimpleOP {
	return blsG1BinaryOp("BLS_G1_ADD", helpers.BytesPrefix(0xF9, 0x30, 0x10), func(a, b *circlbls.G1) []byte {
		var out circlbls.G1
		out.Add(a, b)
		return out.BytesCompressed()
	})
}

func BLS_G1_SUB() *helpers.SimpleOP {
	return blsG1BinaryOp("BLS_G1_SUB", helpers.BytesPrefix(0xF9, 0x30, 0x11), func(a, b *circlbls.G1) []byte {
		var negB circlbls.G1
		negB = *b
		negB.Neg()
		var out circlbls.G1
		out.Add(a, &negB)
		return out.BytesCompressed()
	})
}

func BLS_G1_NEG() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := state.ConsumeGas(vm.BlsG1NegGasPrice); err != nil {
				return err
			}
			aSlice, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			aData, err := preloadBLSPoint(aSlice, 48*8, "slice must contain at least 48 bytes")
			if err != nil {
				return err
			}
			a, err := parseBLSG1(aData)
			if err != nil {
				return blsUnknown("invalid bls g1 point", err)
			}
			a.Neg()
			return pushSliceBytes(state, a.BytesCompressed())
		},
		Name:      "BLS_G1_NEG",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x30, 0x12),
	}
}

func BLS_G1_MUL() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			x, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			if err = state.ConsumeGas(vm.BlsG1MulGasPrice); err != nil {
				return err
			}

			pSlice, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			pData, err := preloadBLSPoint(pSlice, 48*8, "slice must contain at least 48 bytes")
			if err != nil {
				return err
			}

			n := new(big.Int).Mod(new(big.Int).Set(x), blsOrder)
			if n.Sign() == 0 {
				return pushSliceBytes(state, blsG1ZeroCompressed)
			}

			p, err := parseBLSG1(pData)
			if err != nil {
				return blsUnknown("invalid bls g1 point", err)
			}

			var out circlbls.G1
			out.ScalarMult(blsScalarFromInt(n), p)
			return pushSliceBytes(state, out.BytesCompressed())
		},
		Name:      "BLS_G1_MUL",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x30, 0x13),
	}
}

func BLS_G1_ZERO() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			return pushSliceBytes(state, blsG1ZeroCompressed)
		},
		Name:      "BLS_G1_ZERO",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x30, 0x15),
	}
}

func BLS_G1_MULTIEXP() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			n, err := state.Stack.PopIntRange(0, int64(state.Stack.Len()/2))
			if err != nil {
				return err
			}
			count := int(n.Int64())
			if err = state.ConsumeGas(blsCalculateMultiexpGas(
				count,
				vm.BlsG1MultiexpBaseGasPrice,
				vm.BlsG1MultiexpCoef1GasPrice,
				vm.BlsG1MultiexpCoef2GasPrice,
			)); err != nil {
				return err
			}

			if count == 0 {
				return pushSliceBytes(state, blsG1ZeroCompressed)
			}

			if count == 1 {
				x, err := state.Stack.PopIntFinite()
				if err != nil {
					return err
				}
				pSlice, err := state.Stack.PopSlice()
				if err != nil {
					return err
				}
				pData, err := preloadBLSPoint(pSlice, 48*8, "slice must contain at least 48 bytes")
				if err != nil {
					return err
				}
				scalar := blsScalarMod(x)
				if scalar.Sign() == 0 {
					return pushSliceBytes(state, blsG1ZeroCompressed)
				}
				p, err := parseBLSG1(pData)
				if err != nil {
					return blsUnknown("invalid bls g1 point", err)
				}
				var out circlbls.G1
				out.ScalarMult(blsScalarFromInt(scalar), p)
				return pushSliceBytes(state, out.BytesCompressed())
			}

			var acc circlbls.G1
			acc.SetIdentity()
			for i := count - 1; i >= 0; i-- {
				x, err := state.Stack.PopIntFinite()
				if err != nil {
					return err
				}
				pSlice, err := state.Stack.PopSlice()
				if err != nil {
					return err
				}
				pData, err := preloadBLSPoint(pSlice, 48*8, "slice must contain at least 48 bytes")
				if err != nil {
					return err
				}
				p, err := parseBLSG1(pData)
				if err != nil {
					return blsUnknown("invalid bls g1 point", err)
				}
				scalar := blsScalarMod(x)
				if scalar.Sign() == 0 {
					continue
				}
				var term circlbls.G1
				term.ScalarMult(blsScalarFromInt(scalar), p)
				acc.Add(&acc, &term)
			}
			return pushSliceBytes(state, acc.BytesCompressed())
		},
		Name:      "BLS_G1_MULTIEXP",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x30, 0x14),
	}
}

func BLS_MAP_TO_G1() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := state.ConsumeGas(vm.BlsMapToG1GasPrice); err != nil {
				return err
			}
			sl, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			data, err := preloadBLSPoint(sl, 48*8, "slice must contain at least 48 bytes")
			if err != nil {
				return err
			}
			out, err := blsmap.MapToG1(data)
			if err != nil {
				return blsUnknown("failed to map raw bls fp to g1", err)
			}
			return pushSliceBytes(state, out)
		},
		Name:      "BLS_MAP_TO_G1",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x30, 0x16),
	}
}

func BLS_G1_INGROUP() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := state.ConsumeGas(vm.BlsG1InGroupGasPrice); err != nil {
				return err
			}
			aSlice, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			aData, err := preloadBLSPoint(aSlice, 48*8, "slice must contain at least 48 bytes")
			if err != nil {
				return err
			}
			_, err = parseBLSG1(aData)
			return state.Stack.PushBool(err == nil)
		},
		Name:      "BLS_G1_INGROUP",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x30, 0x17),
	}
}

func BLS_G1_ISZERO() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			aSlice, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			aData, err := preloadBLSPoint(aSlice, 48*8, "slice must contain at least 48 bytes")
			if err != nil {
				return err
			}
			return state.Stack.PushBool(bytes.Equal(aData, blsG1ZeroCompressed))
		},
		Name:      "BLS_G1_ISZERO",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x30, 0x18),
	}
}

func blsG2BinaryOp(name string, prefix helpers.BitPrefix, op func(a, b *circlbls.G2) []byte) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := state.ConsumeGas(vm.BlsG2AddSubGasPrice); err != nil {
				return err
			}
			bSlice, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			aSlice, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			bData, err := preloadBLSPoint(bSlice, 96*8, "slice must contain at least 96 bytes")
			if err != nil {
				return err
			}
			aData, err := preloadBLSPoint(aSlice, 96*8, "slice must contain at least 96 bytes")
			if err != nil {
				return err
			}
			a, err := parseBLSG2(aData)
			if err != nil {
				return blsUnknown("invalid bls g2 point", err)
			}
			b, err := parseBLSG2(bData)
			if err != nil {
				return blsUnknown("invalid bls g2 point", err)
			}
			return pushSliceBytes(state, op(a, b))
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func BLS_G2_ADD() *helpers.SimpleOP {
	return blsG2BinaryOp("BLS_G2_ADD", helpers.BytesPrefix(0xF9, 0x30, 0x20), func(a, b *circlbls.G2) []byte {
		var out circlbls.G2
		out.Add(a, b)
		return out.BytesCompressed()
	})
}

func BLS_G2_SUB() *helpers.SimpleOP {
	return blsG2BinaryOp("BLS_G2_SUB", helpers.BytesPrefix(0xF9, 0x30, 0x21), func(a, b *circlbls.G2) []byte {
		var negB circlbls.G2
		negB = *b
		negB.Neg()
		var out circlbls.G2
		out.Add(a, &negB)
		return out.BytesCompressed()
	})
}

func BLS_G2_NEG() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := state.ConsumeGas(vm.BlsG2NegGasPrice); err != nil {
				return err
			}
			aSlice, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			aData, err := preloadBLSPoint(aSlice, 96*8, "slice must contain at least 96 bytes")
			if err != nil {
				return err
			}
			a, err := parseBLSG2(aData)
			if err != nil {
				return blsUnknown("invalid bls g2 point", err)
			}
			a.Neg()
			return pushSliceBytes(state, a.BytesCompressed())
		},
		Name:      "BLS_G2_NEG",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x30, 0x22),
	}
}

func BLS_G2_MUL() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			x, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			if err = state.ConsumeGas(vm.BlsG2MulGasPrice); err != nil {
				return err
			}

			pSlice, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			pData, err := preloadBLSPoint(pSlice, 96*8, "slice must contain at least 96 bytes")
			if err != nil {
				return err
			}

			n := new(big.Int).Mod(new(big.Int).Set(x), blsOrder)
			if n.Sign() == 0 {
				return pushSliceBytes(state, blsG2ZeroCompressed)
			}

			p, err := parseBLSG2(pData)
			if err != nil {
				return blsUnknown("invalid bls g2 point", err)
			}

			var out circlbls.G2
			out.ScalarMult(blsScalarFromInt(n), p)
			return pushSliceBytes(state, out.BytesCompressed())
		},
		Name:      "BLS_G2_MUL",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x30, 0x23),
	}
}

func BLS_G2_ZERO() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			return pushSliceBytes(state, blsG2ZeroCompressed)
		},
		Name:      "BLS_G2_ZERO",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x30, 0x25),
	}
}

func BLS_G2_MULTIEXP() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			n, err := state.Stack.PopIntRange(0, int64(state.Stack.Len()/2))
			if err != nil {
				return err
			}
			count := int(n.Int64())
			if err = state.ConsumeGas(blsCalculateMultiexpGas(
				count,
				vm.BlsG2MultiexpBaseGasPrice,
				vm.BlsG2MultiexpCoef1GasPrice,
				vm.BlsG2MultiexpCoef2GasPrice,
			)); err != nil {
				return err
			}

			if count == 0 {
				return pushSliceBytes(state, blsG2ZeroCompressed)
			}

			if count == 1 {
				x, err := state.Stack.PopIntFinite()
				if err != nil {
					return err
				}
				pSlice, err := state.Stack.PopSlice()
				if err != nil {
					return err
				}
				pData, err := preloadBLSPoint(pSlice, 96*8, "slice must contain at least 96 bytes")
				if err != nil {
					return err
				}
				scalar := blsScalarMod(x)
				if scalar.Sign() == 0 {
					return pushSliceBytes(state, blsG2ZeroCompressed)
				}
				p, err := parseBLSG2(pData)
				if err != nil {
					return blsUnknown("invalid bls g2 point", err)
				}
				var out circlbls.G2
				out.ScalarMult(blsScalarFromInt(scalar), p)
				return pushSliceBytes(state, out.BytesCompressed())
			}

			var acc circlbls.G2
			acc.SetIdentity()
			for i := count - 1; i >= 0; i-- {
				x, err := state.Stack.PopIntFinite()
				if err != nil {
					return err
				}
				pSlice, err := state.Stack.PopSlice()
				if err != nil {
					return err
				}
				pData, err := preloadBLSPoint(pSlice, 96*8, "slice must contain at least 96 bytes")
				if err != nil {
					return err
				}
				p, err := parseBLSG2(pData)
				if err != nil {
					return blsUnknown("invalid bls g2 point", err)
				}
				scalar := blsScalarMod(x)
				if scalar.Sign() == 0 {
					continue
				}
				var term circlbls.G2
				term.ScalarMult(blsScalarFromInt(scalar), p)
				acc.Add(&acc, &term)
			}
			return pushSliceBytes(state, acc.BytesCompressed())
		},
		Name:      "BLS_G2_MULTIEXP",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x30, 0x24),
	}
}

func BLS_MAP_TO_G2() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := state.ConsumeGas(vm.BlsMapToG2GasPrice); err != nil {
				return err
			}
			sl, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			data, err := preloadBLSPoint(sl, 96*8, "slice must contain at least 96 bytes")
			if err != nil {
				return err
			}
			out, err := blsmap.MapToG2(data)
			if err != nil {
				return blsUnknown("failed to map raw bls fp2 to g2", err)
			}
			return pushSliceBytes(state, out)
		},
		Name:      "BLS_MAP_TO_G2",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x30, 0x26),
	}
}

func BLS_G2_INGROUP() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := state.ConsumeGas(vm.BlsG2InGroupGasPrice); err != nil {
				return err
			}
			aSlice, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			aData, err := preloadBLSPoint(aSlice, 96*8, "slice must contain at least 96 bytes")
			if err != nil {
				return err
			}
			_, err = parseBLSG2(aData)
			return state.Stack.PushBool(err == nil)
		},
		Name:      "BLS_G2_INGROUP",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x30, 0x27),
	}
}

func BLS_G2_ISZERO() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			aSlice, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			aData, err := preloadBLSPoint(aSlice, 96*8, "slice must contain at least 96 bytes")
			if err != nil {
				return err
			}
			return state.Stack.PushBool(bytes.Equal(aData, blsG2ZeroCompressed))
		},
		Name:      "BLS_G2_ISZERO",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x30, 0x28),
	}
}

func BLS_PAIRING() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			n, err := state.Stack.PopIntRange(0, int64(state.Stack.Len()/2))
			if err != nil {
				return err
			}
			count := int(n.Int64())
			if err = state.ConsumeGas(vm.BlsPairingBaseGasPrice + int64(count)*vm.BlsPairingElementGasPrice); err != nil {
				return err
			}

			var acc circlbls.Gt
			acc.SetIdentity()
			for i := 0; i < count; i++ {
				p2Slice, popErr := state.Stack.PopSlice()
				if popErr != nil {
					return popErr
				}
				p1Slice, popErr := state.Stack.PopSlice()
				if popErr != nil {
					return popErr
				}
				p2Data, err := preloadBLSPoint(p2Slice, 96*8, "slice must contain at least 96 bytes")
				if err != nil {
					return err
				}
				p1Data, err := preloadBLSPoint(p1Slice, 48*8, "slice must contain at least 48 bytes")
				if err != nil {
					return err
				}
				p1, err := parseBLSG1(p1Data)
				if err != nil {
					return blsUnknown("invalid bls pairing input", err)
				}
				p2, err := parseBLSG2(p2Data)
				if err != nil {
					return blsUnknown("invalid bls pairing input", err)
				}
				pair := circlbls.Pair(p1, p2)
				var next circlbls.Gt
				next.Mul(&acc, pair)
				acc = next
			}
			return state.Stack.PushBool(acc.IsIdentity())
		},
		Name:      "BLS_PAIRING",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x30, 0x30),
	}
}

func BLS_PUSHR() *helpers.SimpleOP {
	return pushConstIntOp("BLS_PUSHR", helpers.BytesPrefix(0xF9, 0x30, 0x31), blsOrder)
}
