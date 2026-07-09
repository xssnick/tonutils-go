package funcs

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/sha512"
	"math/big"
	"testing"

	circlbls "github.com/cloudflare/circl/ecc/bls12381"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	localec "github.com/xssnick/tonutils-go/tvm/internal/secp256k1"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func fuzzFuncsVersion(raw int64) int {
	version := int(raw % int64(vm.MaxSupportedGlobalVersion+1))
	if version < 0 {
		version = -version
	}
	return version
}

func TestFuzzFuncsVersionCoversDefaultRange(t *testing.T) {
	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		if got := fuzzFuncsVersion(int64(version)); got != version {
			t.Fatalf("seed version %d mapped to %d", version, got)
		}
	}
	if got := fuzzFuncsVersion(-int64(vm.MaxSupportedGlobalVersion)); got != vm.MaxSupportedGlobalVersion {
		t.Fatalf("negative default version mapped to %d, want %d", got, vm.MaxSupportedGlobalVersion)
	}
	for _, raw := range []int64{
		-1,
		-int64(vm.MaxSupportedGlobalVersion) - 1,
		-int64(vm.MaxSupportedGlobalVersion) - 2,
		-123456789,
		123456789,
	} {
		got := fuzzFuncsVersion(raw)
		if got < 0 || got > vm.MaxSupportedGlobalVersion {
			t.Fatalf("raw version %d mapped to %d, want within [0, %d]", raw, got, vm.MaxSupportedGlobalVersion)
		}
	}
}

func fuzzFuncsAnycastPrefix(depth uint8, seed uint32) []byte {
	prefix := make([]byte, (int(depth)+7)/8)
	for i := range prefix {
		prefix[i] = byte(seed >> (uint(i%4) * 8))
	}
	return prefix
}

func fuzzFuncsAddrData(bits uint16, seed uint32) []byte {
	data := make([]byte, (int(bits)+7)/8)
	for i := range data {
		data[i] = byte(seed >> (uint(i%4) * 8))
	}
	return data
}

func fuzzFuncsMessageAddr(kind uint8, anycast bool, depth uint8, seed uint32, bits uint16) *cell.Slice {
	depth = depth%30 + 1
	builder := cell.BeginCell()
	switch kind {
	case 0:
		builder.MustStoreUInt(0b10, 2)
		if anycast {
			builder.MustStoreUInt(1, 1)
			builder.MustStoreUInt(uint64(depth), 5)
			builder.MustStoreSlice(fuzzFuncsAnycastPrefix(depth, seed), uint(depth))
		} else {
			builder.MustStoreUInt(0, 1)
		}
		builder.MustStoreUInt(0, 8)
		builder.MustStoreSlice(fuzzFuncsAddrData(256, seed), 256)
	default:
		bits %= 512
		builder.MustStoreUInt(0b11, 2)
		if anycast {
			builder.MustStoreUInt(1, 1)
			builder.MustStoreUInt(uint64(depth), 5)
			builder.MustStoreSlice(fuzzFuncsAnycastPrefix(depth, seed), uint(depth))
		} else {
			builder.MustStoreUInt(0, 1)
		}
		builder.MustStoreUInt(uint64(bits), 9)
		builder.MustStoreInt(0, 32)
		builder.MustStoreSlice(fuzzFuncsAddrData(bits, seed), uint(bits))
	}
	return builder.ToSlice()
}

func FuzzTVMVersionedMessageAddressParsing(f *testing.F) {
	f.Add(int64(9), uint8(0), false, uint8(1), uint32(0), uint16(256))
	f.Add(int64(9), uint8(0), true, uint8(3), uint32(0xa5), uint16(256))
	f.Add(int64(10), uint8(0), true, uint8(3), uint32(0xa5), uint16(256))
	f.Add(int64(9), uint8(1), false, uint8(1), uint32(0x1234), uint16(257))
	f.Add(int64(10), uint8(1), false, uint8(1), uint32(0x1234), uint16(257))
	f.Add(int64(10), uint8(1), true, uint8(7), uint32(0xfeed), uint16(64))
	f.Add(int64(vm.MaxSupportedGlobalVersion), uint8(0), false, uint8(1), uint32(0x1234), uint16(256))
	f.Add(int64(vm.MaxSupportedGlobalVersion), uint8(0), true, uint8(3), uint32(0xa5), uint16(256))
	f.Add(int64(vm.MaxSupportedGlobalVersion), uint8(1), false, uint8(1), uint32(0x1234), uint16(257))
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for kind := uint8(0); kind < 2; kind++ {
			f.Add(version, kind, false, uint8(1), uint32(0x1234), uint16(256))
			f.Add(version, kind, true, uint8(3), uint32(0xa5), uint16(257))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawKind uint8, anycast bool, rawDepth uint8, seed uint32, rawBits uint16) {
		version := fuzzFuncsVersion(rawVersion)
		kind := rawKind % 2
		addr := fuzzFuncsMessageAddr(kind, anycast, rawDepth, seed, rawBits)
		parsed, rest, ok := parseMessageAddress(addr, version)

		shouldReject := version >= 10 && (anycast || kind == 1)
		if shouldReject {
			if ok || parsed != nil {
				t.Fatalf("version=%d kind=%d anycast=%v parsed addr, want reject", version, kind, anycast)
			}
			return
		}
		if !ok || parsed == nil {
			t.Fatalf("version=%d kind=%d anycast=%v rejected addr", version, kind, anycast)
		}
		if rest.BitsLeft() != 0 {
			t.Fatalf("version=%d kind=%d anycast=%v left %d bits", version, kind, anycast, rest.BitsLeft())
		}
	})
}

func fuzzFuncsExpectedStdRewrite(t *testing.T, anycast bool, rawDepth uint8, seed uint32) *cell.Slice {
	t.Helper()

	addrData := fuzzFuncsAddrData(256, seed)
	if !anycast {
		return cell.BeginCell().MustStoreSlice(addrData, 256).ToSlice()
	}

	depth := rawDepth%30 + 1
	rest := cell.BeginCell().MustStoreSlice(addrData, 256).ToSlice()
	if !rest.SkipFirst(uint(depth), 0) {
		t.Fatalf("failed to skip %d address bits", depth)
	}
	restData, err := rest.PreloadSlice(rest.BitsLeft())
	if err != nil {
		t.Fatalf("failed to read address tail: %v", err)
	}

	return cell.BeginCell().
		MustStoreSlice(fuzzFuncsAnycastPrefix(depth, seed), uint(depth)).
		MustStoreSlice(restData, rest.BitsLeft()).
		ToSlice()
}

func FuzzTVMVersionedRewriteAddressAnycastBoundaries(f *testing.F) {
	f.Add(int64(9), uint8(0), true, uint8(3), uint32(0xa5))
	f.Add(int64(9), uint8(1), true, uint8(3), uint32(0xa5))
	f.Add(int64(10), uint8(0), true, uint8(3), uint32(0xa5))
	f.Add(int64(10), uint8(2), true, uint8(3), uint32(0xa5))
	f.Add(int64(vm.MaxSupportedGlobalVersion), uint8(3), false, uint8(1), uint32(0x1234))
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for opKind := uint8(0); opKind < 4; opKind++ {
			f.Add(version, opKind, false, uint8(1), uint32(0x1000)+uint32(opKind))
			f.Add(version, opKind, true, uint8(3), uint32(0x2000)+uint32(opKind))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawOp uint8, anycast bool, rawDepth uint8, seed uint32) {
		version := fuzzFuncsVersion(rawVersion)
		opKind := rawOp % 4
		quiet := opKind == 0 || opKind == 1
		allowVar := opKind == 1 || opKind == 3

		addr := fuzzFuncsMessageAddr(0, anycast, rawDepth, seed, 256)
		st := newFuncTestState(t, nil)
		st.GlobalVersion = version
		if err := st.Stack.PushSlice(addr); err != nil {
			t.Fatalf("push std addr: %v", err)
		}

		var err error
		switch opKind {
		case 0:
			err = REWRITESTDADDRQ().Interpret(st)
		case 1:
			err = REWRITEVARADDRQ().Interpret(st)
		case 2:
			err = REWRITESTDADDR().Interpret(st)
		default:
			err = REWRITEVARADDR().Interpret(st)
		}

		wantReject := anycast && version >= 10
		if wantReject {
			if quiet {
				if err != nil {
					t.Fatalf("quiet rewrite version=%d op=%d failed: %v", version, opKind, err)
				}
				ok, popErr := st.Stack.PopBool()
				if popErr != nil || ok {
					t.Fatalf("quiet rewrite version=%d op=%d ok=(%v, %v), want false", version, opKind, ok, popErr)
				}
			} else if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeCellUnderflow {
				t.Fatalf("rewrite version=%d op=%d error=%v, want cell underflow", version, opKind, err)
			}
			if st.Stack.Len() != 0 {
				t.Fatalf("rewrite version=%d op=%d left %d stack values", version, opKind, st.Stack.Len())
			}
			return
		}

		if err != nil {
			t.Fatalf("rewrite version=%d op=%d anycast=%v failed: %v", version, opKind, anycast, err)
		}
		if quiet {
			ok, popErr := st.Stack.PopBool()
			if popErr != nil || !ok {
				t.Fatalf("quiet rewrite version=%d op=%d ok=(%v, %v), want true", version, opKind, ok, popErr)
			}
		}

		expected := fuzzFuncsExpectedStdRewrite(t, anycast, rawDepth, seed)
		if allowVar {
			got, popErr := st.Stack.PopSlice()
			if popErr != nil {
				t.Fatalf("pop rewrite slice: %v", popErr)
			}
			if got.BitsLeft() != expected.BitsLeft() || !bytes.Equal(mustSliceData(t, got), mustSliceData(t, expected)) {
				t.Fatalf("rewrite slice version=%d op=%d bits/data mismatch", version, opKind)
			}
			if got, want := st.Gas.Used(), int64(0); anycast {
				want = int64(vm.CellCreateGasPrice + vm.CellLoadGasPrice)
				if got != want {
					t.Fatalf("rewrite var anycast gas=%d want %d", got, want)
				}
			} else if got != want {
				t.Fatalf("rewrite var plain gas=%d want %d", got, want)
			}
		} else {
			got, popErr := st.Stack.PopIntFinite()
			if popErr != nil {
				t.Fatalf("pop rewrite int: %v", popErr)
			}
			if want := new(big.Int).SetBytes(mustSliceData(t, expected)); got.Cmp(want) != 0 {
				t.Fatalf("rewrite int version=%d op=%d got=%x want=%x", version, opKind, got, want)
			}
			if got := st.Gas.Used(); got != 0 {
				t.Fatalf("rewrite std gas=%d want 0", got)
			}
		}

		wc, popErr := st.Stack.PopIntFinite()
		if popErr != nil || wc.Sign() != 0 {
			t.Fatalf("rewrite wc=(%v, %v), want 0", wc, popErr)
		}
		if st.Stack.Len() != 0 {
			t.Fatalf("rewrite version=%d op=%d left %d stack values", version, opKind, st.Stack.Len())
		}
	})
}

func FuzzTVMVersionedStdAddrQuietOps(f *testing.F) {
	f.Add(int64(9), uint8(0), false, uint8(1), uint32(0x1234), uint16(256))
	f.Add(int64(9), uint8(0), true, uint8(3), uint32(0xa5), uint16(256))
	f.Add(int64(10), uint8(0), true, uint8(3), uint32(0xa5), uint16(256))
	f.Add(int64(12), uint8(1), false, uint8(1), uint32(0x5678), uint16(257))
	f.Add(int64(14), uint8(5), false, uint8(1), uint32(0), uint16(0))
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for kind := uint8(0); kind < 6; kind++ {
			f.Add(version, kind, false, uint8(1), uint32(0x1000)+uint32(kind), uint16(256))
			f.Add(version, kind, true, uint8(3), uint32(0x2000)+uint32(kind), uint16(257))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawKind uint8, anycast bool, rawDepth uint8, seed uint32, rawBits uint16) {
		version := fuzzFuncsVersion(rawVersion)
		kind := rawKind % 6
		stdAddr := fuzzFuncsMessageAddr(0, anycast, rawDepth, seed, rawBits)
		nonStdAddr := fuzzFuncsMessageAddr(1, anycast, rawDepth, seed, rawBits)
		st := newFuncTestState(t, nil)
		st.GlobalVersion = version

		switch kind {
		case 0:
			if err := st.Stack.PushSlice(stdAddr); err != nil {
				t.Fatalf("push std addr: %v", err)
			}
			if err := LDSTDADDRQ().Interpret(st); err != nil {
				t.Fatalf("LDSTDADDRQ version=%d failed: %v", version, err)
			}
			ok, err := st.Stack.PopBool()
			if err != nil {
				t.Fatalf("pop LDSTDADDRQ status: %v", err)
			}
			wantOK := !anycast || version < 10
			if ok != wantOK {
				t.Fatalf("LDSTDADDRQ version=%d anycast=%v ok=%v want %v", version, anycast, ok, wantOK)
			}
			if ok {
				if rest, err := st.Stack.PopSlice(); err != nil || rest.BitsLeft() != 0 {
					t.Fatalf("LDSTDADDRQ rest = (%v, %v)", rest, err)
				}
				if addr, err := st.Stack.PopSlice(); err != nil || !isValidStdMsgAddr(addr, version) {
					t.Fatalf("LDSTDADDRQ addr = (%v, %v)", addr, err)
				}
				return
			}
			if restored, err := st.Stack.PopSlice(); err != nil || restored.BitsLeft() != stdAddr.BitsLeft() {
				t.Fatalf("LDSTDADDRQ restored = (%v, %v)", restored, err)
			}
		case 1:
			if err := st.Stack.PushSlice(nonStdAddr); err != nil {
				t.Fatalf("push non-std addr: %v", err)
			}
			if err := LDSTDADDRQ().Interpret(st); err != nil {
				t.Fatalf("LDSTDADDRQ non-std version=%d failed: %v", version, err)
			}
			if ok, err := st.Stack.PopBool(); err != nil || ok {
				t.Fatalf("LDSTDADDRQ non-std version=%d ok=(%v, %v), want false", version, ok, err)
			}
			if restored, err := st.Stack.PopSlice(); err != nil || restored.BitsLeft() != nonStdAddr.BitsLeft() {
				t.Fatalf("LDSTDADDRQ non-std restored = (%v, %v)", restored, err)
			}
		case 2:
			noneWithTail := cell.BeginCell().MustStoreUInt(0, 2).MustStoreUInt(uint64(seed&0xff), 8).ToSlice()
			if err := st.Stack.PushSlice(noneWithTail); err != nil {
				t.Fatalf("push addr_none: %v", err)
			}
			if err := LDOPTSTDADDRQ().Interpret(st); err != nil {
				t.Fatalf("LDOPTSTDADDRQ none version=%d failed: %v", version, err)
			}
			if ok, err := st.Stack.PopBool(); err != nil || !ok {
				t.Fatalf("LDOPTSTDADDRQ none version=%d ok=(%v, %v), want true", version, ok, err)
			}
			if rest, err := st.Stack.PopSlice(); err != nil || rest.BitsLeft() != 8 {
				t.Fatalf("LDOPTSTDADDRQ none rest = (%v, %v)", rest, err)
			}
			if raw, err := st.Stack.PopAny(); err != nil || raw != nil {
				t.Fatalf("LDOPTSTDADDRQ none value = (%T %v, %v), want nil", raw, raw, err)
			}
		case 3:
			if err := st.Stack.PushSlice(stdAddr); err != nil {
				t.Fatalf("push std addr: %v", err)
			}
			if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
				t.Fatalf("push builder: %v", err)
			}
			if err := STSTDADDRQ().Interpret(st); err != nil {
				t.Fatalf("STSTDADDRQ version=%d failed: %v", version, err)
			}
			failed, err := st.Stack.PopBool()
			if err != nil {
				t.Fatalf("pop STSTDADDRQ status: %v", err)
			}
			wantFailed := anycast && version >= 10
			if failed != wantFailed {
				t.Fatalf("STSTDADDRQ version=%d anycast=%v failed=%v want %v", version, anycast, failed, wantFailed)
			}
			if _, err := st.Stack.PopBuilder(); err != nil {
				t.Fatalf("pop STSTDADDRQ builder: %v", err)
			}
			if failed {
				if restored, err := st.Stack.PopSlice(); err != nil || restored.BitsLeft() != stdAddr.BitsLeft() {
					t.Fatalf("STSTDADDRQ restored = (%v, %v)", restored, err)
				}
			}
		case 4:
			if err := st.Stack.PushSlice(nonStdAddr); err != nil {
				t.Fatalf("push non-std addr: %v", err)
			}
			if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
				t.Fatalf("push builder: %v", err)
			}
			if err := STSTDADDRQ().Interpret(st); err != nil {
				t.Fatalf("STSTDADDRQ non-std version=%d failed: %v", version, err)
			}
			if failed, err := st.Stack.PopBool(); err != nil || !failed {
				t.Fatalf("STSTDADDRQ non-std version=%d failed=(%v, %v), want true", version, failed, err)
			}
			if _, err := st.Stack.PopBuilder(); err != nil {
				t.Fatalf("pop STSTDADDRQ non-std builder: %v", err)
			}
			if restored, err := st.Stack.PopSlice(); err != nil || restored.BitsLeft() != nonStdAddr.BitsLeft() {
				t.Fatalf("STSTDADDRQ non-std restored = (%v, %v)", restored, err)
			}
		default:
			raw := new(big.Int).SetUint64(uint64(seed) + 1)
			if err := st.Stack.PushAny(raw); err != nil {
				t.Fatalf("push non-slice optional addr: %v", err)
			}
			if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
				t.Fatalf("push builder: %v", err)
			}
			if err := STOPTSTDADDRQ().Interpret(st); err != nil {
				t.Fatalf("STOPTSTDADDRQ non-slice version=%d failed: %v", version, err)
			}
			if failed, err := st.Stack.PopBool(); err != nil || !failed {
				t.Fatalf("STOPTSTDADDRQ non-slice version=%d failed=(%v, %v), want true", version, failed, err)
			}
			if _, err := st.Stack.PopBuilder(); err != nil {
				t.Fatalf("pop STOPTSTDADDRQ non-slice builder: %v", err)
			}
			restored, err := st.Stack.PopAny()
			if err != nil {
				t.Fatalf("pop STOPTSTDADDRQ restored value: %v", err)
			}
			got, ok := restored.(*big.Int)
			if !ok || got.Cmp(raw) != 0 {
				t.Fatalf("STOPTSTDADDRQ non-slice version=%d restored %T %v, want %v", version, restored, restored, raw)
			}
		}
	})
}

func fuzzFuncsInvalidRistrettoPoint(seed uint64) *big.Int {
	data := make([]byte, 32)
	for i := range data {
		data[i] = 0xFF
	}
	data[0] = byte(seed)
	return new(big.Int).SetBytes(data)
}

func FuzzTVMVersionedCryptoV14Edges(f *testing.F) {
	f.Add(int64(13), uint8(0), uint64(0))
	f.Add(int64(14), uint8(0), uint64(0))
	f.Add(int64(13), uint8(1), uint64(1))
	f.Add(int64(14), uint8(1), uint64(1))
	f.Add(int64(14), uint8(2), uint64(0))
	f.Add(int64(14), uint8(3), uint64(1))
	f.Add(int64(9), uint8(4), uint64(0xff))
	f.Add(int64(4), uint8(5), uint64(0x05))
	f.Add(int64(12), uint8(6), uint64(0xca))
	f.Add(int64(13), uint8(7), uint64(0))
	f.Add(int64(14), uint8(7), uint64(0))
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for kind := uint8(0); kind < 8; kind++ {
			f.Add(version, kind, uint64(kind))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawKind uint8, seed uint64) {
		version := fuzzFuncsVersion(rawVersion)

		switch rawKind % 8 {
		case 0:
			st := newFuncTestState(t, nil)
			st.GlobalVersion = version
			if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
				t.Fatalf("push scalar: %v", err)
			}
			err := RIST255_MULBASE().Interpret(st)
			if err != nil {
				t.Fatalf("RIST255_MULBASE zero scalar version=%d failed: %v", version, err)
			}
			if got, err := st.Stack.PopIntFinite(); err != nil || got.Sign() != 0 {
				t.Fatalf("RIST255_MULBASE zero scalar version=%d = (%v, %v), want 0", version, got, err)
			}
		case 1:
			st := newFuncTestState(t, nil)
			st.GlobalVersion = version
			if err := st.Stack.PushInt(fuzzFuncsInvalidRistrettoPoint(seed)); err != nil {
				t.Fatalf("push point: %v", err)
			}
			if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
				t.Fatalf("push scalar: %v", err)
			}
			if err := RIST255_QMUL().Interpret(st); err != nil {
				t.Fatalf("RIST255_QMUL invalid point zero scalar version=%d failed: %v", version, err)
			}
			ok, err := st.Stack.PopBool()
			if err != nil {
				t.Fatalf("pop RIST255_QMUL invalid point zero scalar status: %v", err)
			}
			if version >= 14 {
				if ok {
					t.Fatalf("RIST255_QMUL invalid point zero scalar version=%d status=true, want false", version)
				}
				if st.Stack.Len() != 0 {
					t.Fatalf("RIST255_QMUL invalid point zero scalar version=%d left %d stack values, want 0", version, st.Stack.Len())
				}
				return
			}
			if !ok {
				t.Fatalf("RIST255_QMUL invalid point zero scalar version=%d status=false, want true", version)
			}
			if got, err := st.Stack.PopIntFinite(); err != nil || got.Sign() != 0 {
				t.Fatalf("RIST255_QMUL invalid point zero scalar version=%d = (%v, %v), want 0", version, got, err)
			}
		case 2:
			st := newFuncTestState(t, nil)
			st.GlobalVersion = version
			st.SignatureCheckAlwaysSucceed = seed&1 != 0
			sig := make([]byte, ed25519.SignatureSize)
			sig[0] = byte(seed)
			identityKey := new(big.Int).Lsh(big.NewInt(1), 248)
			if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
				t.Fatalf("push hash: %v", err)
			}
			if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig, 512).ToSlice()); err != nil {
				t.Fatalf("push signature: %v", err)
			}
			if err := st.Stack.PushInt(identityKey); err != nil {
				t.Fatalf("push key: %v", err)
			}
			if err := CHKSIGNU().Interpret(st); err != nil {
				t.Fatalf("CHKSIGNU identity version=%d failed: %v", version, err)
			}
			got, err := st.Stack.PopBool()
			if err != nil {
				t.Fatalf("pop CHKSIGNU identity: %v", err)
			}
			keyBytes := identityKey.FillBytes(make([]byte, ed25519.PublicKeySize))
			want := tvmEd25519Verify(keyBytes, make([]byte, 32), sig)
			if version >= 14 && tvmEd25519RejectedPublicKeyV14(keyBytes) {
				want = false
			}
			want = want || st.SignatureCheckAlwaysSucceed
			if got != want {
				t.Fatalf("CHKSIGNU identity version=%d always=%v got=%v want=%v", version, st.SignatureCheckAlwaysSucceed, got, want)
			}
			wantCounter := uint32(0)
			wantFreeGas := int64(0)
			if version >= 4 {
				wantCounter = 1
				wantFreeGas = vm.SignatureCheckGasPrice
			}
			if st.SignatureCheckCounter != wantCounter || st.Gas.FreeConsumed != wantFreeGas {
				t.Fatalf("CHKSIGNU identity version=%d signature check counter/free = %d/%d, want %d/%d", version, st.SignatureCheckCounter, st.Gas.FreeConsumed, wantCounter, wantFreeGas)
			}
		case 3:
			st := newFuncTestState(t, nil)
			st.GlobalVersion = version
			st.SignatureCheckAlwaysSucceed = true
			sig := make([]byte, ed25519.SignatureSize)
			sig[0] = byte(seed)
			if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice([]byte{byte(seed), byte(seed >> 8)}, 16).ToSlice()); err != nil {
				t.Fatalf("push data: %v", err)
			}
			if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig, 512).ToSlice()); err != nil {
				t.Fatalf("push signature: %v", err)
			}
			if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
				t.Fatalf("push key: %v", err)
			}
			if err := CHKSIGNS().Interpret(st); err != nil {
				t.Fatalf("CHKSIGNS always version=%d failed: %v", version, err)
			}
			if got, err := st.Stack.PopBool(); err != nil || !got {
				t.Fatalf("CHKSIGNS always version=%d = (%v, %v), want true", version, got, err)
			}
			wantCounter := uint32(0)
			wantFreeGas := int64(0)
			if version >= 4 {
				wantCounter = 1
				wantFreeGas = vm.SignatureCheckGasPrice
			}
			if st.SignatureCheckCounter != wantCounter || st.Gas.FreeConsumed != wantFreeGas {
				t.Fatalf("CHKSIGNS always version=%d signature check counter/free = %d/%d, want %d/%d", version, st.SignatureCheckCounter, st.Gas.FreeConsumed, wantCounter, wantFreeGas)
			}
		case 4:
			st := newFuncTestState(t, nil)
			st.GlobalVersion = version
			key := bytes.Repeat([]byte{0xFF}, 32)
			key[31] = byte(seed)
			if err := st.Stack.PushInt(new(big.Int).SetBytes(key)); err != nil {
				t.Fatalf("push secp256k1 key: %v", err)
			}
			if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
				t.Fatalf("push secp256k1 tweak: %v", err)
			}
			if err := SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Interpret(st); err != nil {
				t.Fatalf("SECP256K1_XONLY_PUBKEY_TWEAK_ADD invalid key version=%d failed: %v", version, err)
			}
			if ok, err := st.Stack.PopBool(); err != nil || ok {
				t.Fatalf("SECP256K1_XONLY_PUBKEY_TWEAK_ADD invalid key version=%d = (%v, %v), want false", version, ok, err)
			}
		case 5:
			st := newFuncTestState(t, nil)
			st.GlobalVersion = version
			badKey := append([]byte{0x05}, bytes.Repeat([]byte{byte(seed)}, 32)...)
			sig := bytes.Repeat([]byte{byte(seed >> 8)}, 64)
			if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
				t.Fatalf("push p256 hash: %v", err)
			}
			if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig, 512).ToSlice()); err != nil {
				t.Fatalf("push p256 signature: %v", err)
			}
			if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(badKey, 264).ToSlice()); err != nil {
				t.Fatalf("push p256 key: %v", err)
			}
			if err := P256_CHKSIGNU().Interpret(st); err != nil {
				t.Fatalf("P256_CHKSIGNU invalid key version=%d failed: %v", version, err)
			}
			if ok, err := st.Stack.PopBool(); err != nil || ok {
				t.Fatalf("P256_CHKSIGNU invalid key version=%d = (%v, %v), want false", version, ok, err)
			}
		case 6:
			st := newFuncTestState(t, nil)
			st.GlobalVersion = version
			builder := cell.BeginCell().
				MustStoreUInt(seed&0xff, 8).
				MustStoreRef(cell.BeginCell().MustStoreUInt((seed>>8)&0xff, 8).EndCell())
			wantHash := builder.Copy().EndCell().Hash()
			if err := st.Stack.PushBuilder(builder); err != nil {
				t.Fatalf("push hashbu builder: %v", err)
			}
			if err := HASHBU().Interpret(st); err != nil {
				t.Fatalf("HASHBU version=%d failed: %v", version, err)
			}
			got, err := st.Stack.PopIntFinite()
			if err != nil {
				t.Fatalf("pop HASHBU result: %v", err)
			}
			if !bytes.Equal(got.FillBytes(make([]byte, 32)), wantHash) {
				t.Fatalf("HASHBU version=%d hash=%x want=%x", version, got.FillBytes(make([]byte, 32)), wantHash)
			}
		default:
			hash := sha256.Sum256([]byte("secp256k1-v14-fuzz"))
			privBytes := make([]byte, 32)
			privBytes[31] = 11 + byte(seed%17)
			nonceBytes := make([]byte, 32)
			nonceBytes[31] = 13 + byte(seed%19)
			v, r, s, _, ok := localec.SignRecoverable(privBytes, nonceBytes, hash[:])
			if !ok {
				t.Fatal("SignRecoverable failed")
			}

			st := newFuncTestState(t, nil)
			st.GlobalVersion = version
			if err := st.Stack.PushInt(new(big.Int).SetBytes(hash[:])); err != nil {
				t.Fatalf("push ECRECOVER hash: %v", err)
			}
			if err := st.Stack.PushInt(big.NewInt(int64(v + 27))); err != nil {
				t.Fatalf("push ECRECOVER ethereum v: %v", err)
			}
			if err := st.Stack.PushInt(r); err != nil {
				t.Fatalf("push ECRECOVER r: %v", err)
			}
			if err := st.Stack.PushInt(s); err != nil {
				t.Fatalf("push ECRECOVER s: %v", err)
			}
			if err := ECRECOVER().Interpret(st); err != nil {
				t.Fatalf("ECRECOVER ethereum v version=%d failed: %v", version, err)
			}
			got, err := st.Stack.PopBool()
			if err != nil {
				t.Fatalf("pop ECRECOVER ethereum v result: %v", err)
			}
			if got != (version >= 14) {
				t.Fatalf("ECRECOVER ethereum v version=%d result=%v, want %v", version, got, version >= 14)
			}
		}
	})
}

func fuzzFuncsRistrettoZeroScalar(seed uint64) *big.Int {
	switch seed % 4 {
	case 0:
		return big.NewInt(0)
	case 1:
		return new(big.Int).Set(ristretto255L)
	case 2:
		return new(big.Int).Mul(ristretto255L, big.NewInt(2))
	default:
		return new(big.Int).Neg(new(big.Int).Set(ristretto255L))
	}
}

func fuzzFuncsRistrettoNonZeroScalar(seed uint64) *big.Int {
	n := big.NewInt(1 + int64(seed%31))
	if seed&1 != 0 {
		n.Add(n, ristretto255L)
	}
	if seed&2 != 0 {
		n.Neg(n)
	}
	return n
}

func fuzzFuncsKnownInvalidRistrettoPoint(t *testing.T, seed uint64) *big.Int {
	t.Helper()

	for i := uint64(0); i < 256; i++ {
		point := fuzzFuncsInvalidRistrettoPoint(seed + i)
		if _, err := parseRistrettoPoint(point); err != nil {
			return point
		}
	}
	t.Fatal("failed to build invalid ristretto point")
	return nil
}

func fuzzFuncsAssertRistrettoZeroResult(t *testing.T, st *vm.State, context string) {
	t.Helper()

	got, err := st.Stack.PopIntFinite()
	if err != nil || got.Sign() != 0 {
		t.Fatalf("%s result = (%v, %v), want 0", context, got, err)
	}
	if st.Stack.Len() != 0 {
		t.Fatalf("%s left %d stack values, want 0", context, st.Stack.Len())
	}
}

func fuzzFuncsAssertRistrettoQuietZeroResult(t *testing.T, st *vm.State, context string) {
	t.Helper()

	ok, err := st.Stack.PopBool()
	if err != nil || !ok {
		t.Fatalf("%s status = (%v, %v), want true", context, ok, err)
	}
	fuzzFuncsAssertRistrettoZeroResult(t, st, context)
}

func fuzzFuncsAssertRistrettoQuietFailure(t *testing.T, st *vm.State, context string) {
	t.Helper()

	ok, err := st.Stack.PopBool()
	if err != nil || ok {
		t.Fatalf("%s status = (%v, %v), want false", context, ok, err)
	}
	if st.Stack.Len() != 0 {
		t.Fatalf("%s left %d stack values, want 0", context, st.Stack.Len())
	}
}

func FuzzTVMRistrettoIdentityAndZeroScalarV14Boundary(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for opKind := uint8(0); opKind < 6; opKind++ {
			f.Add(version, opKind, uint64(version)<<8|uint64(opKind))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawOp uint8, seed uint64) {
		version := fuzzFuncsVersion(rawVersion)
		st := newFuncTestState(t, nil)
		st.GlobalVersion = version
		context := "version=" + big.NewInt(int64(version)).String()

		switch rawOp % 6 {
		case 0:
			if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
				t.Fatalf("push identity point: %v", err)
			}
			if err := st.Stack.PushInt(fuzzFuncsRistrettoNonZeroScalar(seed)); err != nil {
				t.Fatalf("push scalar: %v", err)
			}
			err := RIST255_MUL().Interpret(st)
			if version < 14 {
				if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeRangeCheck {
					t.Fatalf("%s RIST255_MUL identity err=%v, want range check", context, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s RIST255_MUL identity failed: %v", context, err)
			}
			fuzzFuncsAssertRistrettoZeroResult(t, st, context+" RIST255_MUL identity")
		case 1:
			if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
				t.Fatalf("push identity point: %v", err)
			}
			if err := st.Stack.PushInt(fuzzFuncsRistrettoNonZeroScalar(seed)); err != nil {
				t.Fatalf("push scalar: %v", err)
			}
			if err := RIST255_QMUL().Interpret(st); err != nil {
				t.Fatalf("%s RIST255_QMUL identity failed: %v", context, err)
			}
			if version < 14 {
				fuzzFuncsAssertRistrettoQuietFailure(t, st, context+" RIST255_QMUL identity")
				return
			}
			fuzzFuncsAssertRistrettoQuietZeroResult(t, st, context+" RIST255_QMUL identity")
		case 2:
			if err := st.Stack.PushInt(fuzzFuncsKnownInvalidRistrettoPoint(t, seed)); err != nil {
				t.Fatalf("push invalid point: %v", err)
			}
			if err := st.Stack.PushInt(fuzzFuncsRistrettoZeroScalar(seed)); err != nil {
				t.Fatalf("push zero scalar: %v", err)
			}
			err := RIST255_MUL().Interpret(st)
			if version >= 14 {
				if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeRangeCheck {
					t.Fatalf("%s RIST255_MUL invalid zero err=%v, want range check", context, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s RIST255_MUL invalid zero failed: %v", context, err)
			}
			fuzzFuncsAssertRistrettoZeroResult(t, st, context+" RIST255_MUL invalid zero")
		case 3:
			if err := st.Stack.PushInt(fuzzFuncsKnownInvalidRistrettoPoint(t, seed)); err != nil {
				t.Fatalf("push invalid point: %v", err)
			}
			if err := st.Stack.PushInt(fuzzFuncsRistrettoZeroScalar(seed)); err != nil {
				t.Fatalf("push zero scalar: %v", err)
			}
			if err := RIST255_QMUL().Interpret(st); err != nil {
				t.Fatalf("%s RIST255_QMUL invalid zero failed: %v", context, err)
			}
			if version >= 14 {
				fuzzFuncsAssertRistrettoQuietFailure(t, st, context+" RIST255_QMUL invalid zero")
				return
			}
			fuzzFuncsAssertRistrettoQuietZeroResult(t, st, context+" RIST255_QMUL invalid zero")
		case 4:
			if err := st.Stack.PushInt(fuzzFuncsRistrettoZeroScalar(seed)); err != nil {
				t.Fatalf("push zero scalar: %v", err)
			}
			if err := RIST255_MULBASE().Interpret(st); err != nil {
				t.Fatalf("%s RIST255_MULBASE zero failed: %v", context, err)
			}
			fuzzFuncsAssertRistrettoZeroResult(t, st, context+" RIST255_MULBASE zero")
		default:
			if err := st.Stack.PushInt(fuzzFuncsRistrettoZeroScalar(seed)); err != nil {
				t.Fatalf("push zero scalar: %v", err)
			}
			if err := RIST255_QMULBASE().Interpret(st); err != nil {
				t.Fatalf("%s RIST255_QMULBASE zero failed: %v", context, err)
			}
			fuzzFuncsAssertRistrettoQuietZeroResult(t, st, context+" RIST255_QMULBASE zero")
		}
	})
}

type fuzzFuncsEcrecoverCase struct {
	hash [32]byte
	v    byte
	r    *big.Int
	s    *big.Int
	pub  []byte
}

func fuzzFuncsEcrecoverEthereumCase(t *testing.T, recoveryID byte, seed uint64) fuzzFuncsEcrecoverCase {
	t.Helper()

	recoveryID &= 1
	payload := []byte("secp256k1-v14-eth")
	payload = append(payload, recoveryID)
	for shift := uint(0); shift < 64; shift += 8 {
		payload = append(payload, byte(seed>>shift))
	}
	hash := sha256.Sum256(payload)

	privBytes := make([]byte, 32)
	privBytes[31] = 1 + byte(seed%251)
	for i := uint64(0); i < 512; i++ {
		nonceBytes := make([]byte, 32)
		nonceBytes[30] = 1 + byte((seed+i*29)%251)
		nonceBytes[31] = 1 + byte((seed*17+i*31)%251)

		v, r, s, pub, ok := localec.SignRecoverable(privBytes, nonceBytes, hash[:])
		if ok && v == recoveryID {
			return fuzzFuncsEcrecoverCase{
				hash: hash,
				v:    v + 27,
				r:    new(big.Int).Set(r),
				s:    new(big.Int).Set(s),
				pub:  append([]byte(nil), pub...),
			}
		}
	}

	t.Fatalf("failed to find ECRECOVER case for recovery id %d", recoveryID)
	return fuzzFuncsEcrecoverCase{}
}

func fuzzFuncsRunEcrecoverEthereumCase(t *testing.T, version int, tc fuzzFuncsEcrecoverCase) (*vm.State, bool) {
	t.Helper()

	st := newFuncTestState(t, nil)
	st.GlobalVersion = version
	if err := st.Stack.PushInt(new(big.Int).SetBytes(tc.hash[:])); err != nil {
		t.Fatalf("push ECRECOVER hash: %v", err)
	}
	if err := st.Stack.PushInt(big.NewInt(int64(tc.v))); err != nil {
		t.Fatalf("push ECRECOVER ethereum v: %v", err)
	}
	if err := st.Stack.PushInt(new(big.Int).Set(tc.r)); err != nil {
		t.Fatalf("push ECRECOVER r: %v", err)
	}
	if err := st.Stack.PushInt(new(big.Int).Set(tc.s)); err != nil {
		t.Fatalf("push ECRECOVER s: %v", err)
	}
	if err := ECRECOVER().Interpret(st); err != nil {
		t.Fatalf("ECRECOVER ethereum v version=%d failed: %v", version, err)
	}
	ok, err := st.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop ECRECOVER ethereum v result: %v", err)
	}
	return st, ok
}

func fuzzFuncsAssertEcrecoverOutput(t *testing.T, st *vm.State, pub []byte) {
	t.Helper()

	y, err := st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop ECRECOVER y: %v", err)
	}
	x, err := st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop ECRECOVER x: %v", err)
	}
	prefix, err := st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop ECRECOVER prefix: %v", err)
	}
	if prefix.Int64() != int64(pub[0]) ||
		x.Cmp(new(big.Int).SetBytes(pub[1:33])) != 0 ||
		y.Cmp(new(big.Int).SetBytes(pub[33:65])) != 0 {
		t.Fatal("unexpected ECRECOVER output")
	}
	if st.Stack.Len() != 0 {
		t.Fatalf("ECRECOVER left %d stack values, want 0", st.Stack.Len())
	}
}

func TestTVMEcrecoverEthereumRecoveryIDsStartAtV14BothIDs(t *testing.T) {
	for _, recoveryID := range []byte{0, 1} {
		tc := fuzzFuncsEcrecoverEthereumCase(t, recoveryID, uint64(recoveryID))
		if tc.v != recoveryID+27 {
			t.Fatalf("recovery id %d produced ethereum id %d", recoveryID, tc.v)
		}

		st, ok := fuzzFuncsRunEcrecoverEthereumCase(t, 13, tc)
		if ok {
			t.Fatalf("ECRECOVER ethereum id %d succeeded at v13", tc.v)
		}
		if st.Stack.Len() != 0 {
			t.Fatalf("ECRECOVER ethereum id %d left %d v13 stack values, want 0", tc.v, st.Stack.Len())
		}

		st, ok = fuzzFuncsRunEcrecoverEthereumCase(t, 14, tc)
		if !ok {
			t.Fatalf("ECRECOVER ethereum id %d failed at v14", tc.v)
		}
		fuzzFuncsAssertEcrecoverOutput(t, st, tc.pub)
	}
}

func FuzzTVMEcrecoverEthereumRecoveryIDsV14Boundary(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		f.Add(version, uint8(0), uint64(version))
		f.Add(version, uint8(1), uint64(version+1))
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawRecoveryID uint8, seed uint64) {
		version := fuzzFuncsVersion(rawVersion)
		tc := fuzzFuncsEcrecoverEthereumCase(t, rawRecoveryID, seed)
		st, ok := fuzzFuncsRunEcrecoverEthereumCase(t, version, tc)

		if version < 14 {
			if ok {
				t.Fatalf("ECRECOVER ethereum id %d succeeded at version %d", tc.v, version)
			}
			if st.Stack.Len() != 0 {
				t.Fatalf("ECRECOVER ethereum id %d left %d version=%d stack values, want 0", tc.v, st.Stack.Len(), version)
			}
			return
		}

		if !ok {
			t.Fatalf("ECRECOVER ethereum id %d failed at version %d", tc.v, version)
		}
		fuzzFuncsAssertEcrecoverOutput(t, st, tc.pub)
	})
}

func fuzzFuncsWantActionModeRangeError(kind uint8, version int, mode int64) bool {
	if kind == 0 || kind == 3 {
		if version < 4 {
			return mode > 15
		}
		return mode > 31
	}
	if version < 4 {
		return mode > 2
	}
	return mode > 31 || mode&^16 > 2
}

func FuzzTVMVersionedSendMsgTupleAmountExtraSlot(f *testing.F) {
	f.Add(int64(9), uint8(0), int64(123), uint8(0))
	f.Add(int64(9), uint8(0), int64(123), uint8(1))
	f.Add(int64(9), uint8(0), int64(123), uint8(2))
	f.Add(int64(10), uint8(0), int64(123), uint8(0))
	f.Add(int64(10), uint8(0), int64(123), uint8(1))
	f.Add(int64(10), uint8(0), int64(123), uint8(2))
	f.Add(int64(9), uint8(1), int64(-456), uint8(1))
	f.Add(int64(10), uint8(1), int64(-456), uint8(1))
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for idxKind := uint8(0); idxKind < 2; idxKind++ {
			for form := uint8(0); form < 5; form++ {
				f.Add(version, idxKind, int64(version*17), form)
			}
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawIdx uint8, rawAmount int64, rawForm uint8) {
		version := fuzzFuncsVersion(rawVersion)
		idx := 7
		name := "BALANCE"
		if rawIdx%2 == 1 {
			idx = 11
			name = "INCOMINGVALUE"
		}
		amountValue := rawAmount % 1_000_000

		form := rawForm % 5
		var param tuple.Tuple
		switch form {
		case 0:
			param = tuple.NewTupleValue(big.NewInt(amountValue), nil)
		case 1:
			param = tuple.NewTupleValue(big.NewInt(amountValue), cell.BeginCell().MustStoreUInt(uint64(rawForm), 8).EndCell())
		case 2:
			param = tuple.NewTupleValue(big.NewInt(amountValue))
		case 3:
			param = tuple.NewTupleValue()
		default:
			param = tuple.NewTupleValue("bad-amount", nil)
		}

		st := newFuncTestState(t, map[int]any{idx: param})
		st.GlobalVersion = version

		amount, hasExtra, err := sendMsgTupleAmount(st, idx, name)
		switch {
		case form == 3:
			if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeRangeCheck {
				t.Fatalf("version=%d idx=%d missing amount err=%v, want range check", version, idx, err)
			}
			return
		case form == 4:
			if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeTypeCheck {
				t.Fatalf("version=%d idx=%d bad amount err=%v, want type check", version, idx, err)
			}
			return
		case version < 10 && form == 2:
			if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeRangeCheck {
				t.Fatalf("version=%d idx=%d missing legacy extra err=%v, want range check", version, idx, err)
			}
			return
		}

		if err != nil {
			t.Fatalf("version=%d idx=%d form=%d sendMsgTupleAmount failed: %v", version, idx, form, err)
		}
		if amount.Cmp(big.NewInt(amountValue)) != 0 {
			t.Fatalf("version=%d idx=%d amount=%s want %d", version, idx, amount, amountValue)
		}
		wantExtra := version < 10 && form == 1
		if hasExtra != wantExtra {
			t.Fatalf("version=%d idx=%d form=%d hasExtra=%v want %v", version, idx, form, hasExtra, wantExtra)
		}
	})
}

func FuzzTVMVersionedSendMsgSizeLimitConfigBoundary(f *testing.F) {
	f.Add(int64(5), uint8(0), uint32(100), uint32(200))
	f.Add(int64(6), uint8(0), uint32(100), uint32(200))
	f.Add(int64(5), uint8(1), uint32(101), uint32(201))
	f.Add(int64(6), uint8(1), uint32(101), uint32(201))
	f.Add(int64(5), uint8(3), uint32(0), uint32(0))
	f.Add(int64(6), uint8(3), uint32(0), uint32(0))
	f.Add(int64(6), uint8(4), uint32(0), uint32(0))
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for form := uint8(0); form < 5; form++ {
			f.Add(version, form, uint32(version+1), uint32((version+1)*17))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawForm uint8, rawMaxBits, rawMaxCells uint32) {
		version := fuzzFuncsVersion(rawVersion)
		form := rawForm % 5
		wantMaxCells := uint64(rawMaxCells)

		var limitSlice *cell.Slice
		switch form {
		case 1:
			limitSlice = cell.BeginCell().
				MustStoreUInt(0x01, 8).
				MustStoreUInt(uint64(rawMaxBits), 32).
				MustStoreUInt(wantMaxCells, 32).
				ToSlice()
		case 2:
			limitSlice = cell.BeginCell().
				MustStoreUInt(0x02, 8).
				MustStoreUInt(uint64(rawMaxBits), 32).
				MustStoreUInt(wantMaxCells, 32).
				ToSlice()
		case 3:
			limitSlice = cell.BeginCell().MustStoreUInt(0x03, 8).ToSlice()
		case 4:
			limitSlice = cell.BeginCell().
				MustStoreUInt(0x01, 8).
				MustStoreUInt(uint64(rawMaxBits), 32).
				ToSlice()
		}

		cfg := tuple.NewTupleSized(7)
		if limitSlice != nil {
			if err := cfg.Set(6, limitSlice); err != nil {
				t.Fatalf("set size limit config: %v", err)
			}
		}
		st := newFuncTestState(t, map[int]any{paramIdxUnpackedConfig: cfg})
		st.GlobalVersion = version

		got, err := getSizeLimitsMaxMsgCells(st)
		if version < 6 || form == 0 {
			if err != nil || got != 1<<13 {
				t.Fatalf("version=%d form=%d max cells = (%d, %v), want default", version, form, got, err)
			}
			return
		}
		if form >= 3 {
			if err == nil {
				t.Fatalf("version=%d form=%d max cells = %d, want error", version, form, got)
			}
			return
		}
		if err != nil || got != wantMaxCells {
			t.Fatalf("version=%d form=%d max cells = (%d, %v), want %d", version, form, got, err, wantMaxCells)
		}
	})
}

func FuzzTVMVersionedSendMsgPricesSourceBoundary(f *testing.F) {
	f.Add(int64(5), false, uint8(0), uint64(11), uint64(99))
	f.Add(int64(6), false, uint8(0), uint64(11), uint64(99))
	f.Add(int64(5), true, uint8(0), uint64(12), uint64(98))
	f.Add(int64(6), true, uint8(0), uint64(12), uint64(98))
	f.Add(int64(5), false, uint8(1), uint64(13), uint64(97))
	f.Add(int64(6), false, uint8(1), uint64(13), uint64(97))
	f.Add(int64(5), true, uint8(2), uint64(14), uint64(96))
	f.Add(int64(6), true, uint8(2), uint64(14), uint64(96))
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for sourceForm := uint8(0); sourceForm < 3; sourceForm++ {
			f.Add(version, false, sourceForm, uint64(version+1), uint64((version+1)*10))
			f.Add(version, true, sourceForm, uint64(version+2), uint64((version+2)*10))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, masterchain bool, rawSourceForm uint8, rawRootLump, rawUnpackedLump uint64) {
		version := fuzzFuncsVersion(rawVersion)
		sourceForm := rawSourceForm % 3
		rootLump := rawRootLump%1_000_000 + 1
		unpackedLump := rawUnpackedLump%1_000_000 + 1
		if rootLump == unpackedLump {
			unpackedLump++
		}

		rootParam := tlb.ConfigParamMsgForwardPricesBasechain
		unpackedIdx := 5
		if masterchain {
			rootParam = tlb.ConfigParamMsgForwardPricesMasterchain
			unpackedIdx = 4
		}

		rootEntries := map[uint32]*cell.Cell{}
		if sourceForm != 1 {
			rootEntries[rootParam] = makeMsgPricesSlice(rootLump, 0, 0).MustToCell()
		}
		unpacked := tuple.NewTupleSized(6)
		if sourceForm != 2 {
			if err := unpacked.Set(unpackedIdx, makeMsgPricesSlice(unpackedLump, 0, 0)); err != nil {
				t.Fatalf("set unpacked msg prices: %v", err)
			}
		}

		st := newFuncTestState(t, map[int]any{
			9:                      makeConfigRootRefDict(t, rootEntries),
			paramIdxUnpackedConfig: unpacked,
		})
		st.GlobalVersion = version

		prices, err := getSendMsgPrices(st, masterchain)
		switch {
		case version < 6 && sourceForm == 1:
			if err == nil {
				t.Fatalf("version=%d sourceForm=%d got root prices %+v, want missing-root error", version, sourceForm, prices)
			}
			return
		case version >= 6 && sourceForm == 2:
			if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeTypeCheck {
				t.Fatalf("version=%d sourceForm=%d unpacked missing err=%v, want type check", version, sourceForm, err)
			}
			return
		}

		if err != nil {
			t.Fatalf("version=%d sourceForm=%d getSendMsgPrices failed: %v", version, sourceForm, err)
		}
		want := rootLump
		if version >= 6 {
			want = unpackedLump
		}
		if prices.LumpPrice != want {
			t.Fatalf("version=%d masterchain=%t sourceForm=%d lump=%d want %d", version, masterchain, sourceForm, prices.LumpPrice, want)
		}
	})
}

func FuzzTVMVersionedActionModeBoundaries(f *testing.F) {
	f.Add(int64(3), uint8(0), uint8(15))
	f.Add(int64(3), uint8(0), uint8(16))
	f.Add(int64(4), uint8(0), uint8(16))
	f.Add(int64(3), uint8(3), uint8(15))
	f.Add(int64(3), uint8(3), uint8(16))
	f.Add(int64(4), uint8(3), uint8(16))
	f.Add(int64(3), uint8(1), uint8(2))
	f.Add(int64(3), uint8(1), uint8(16))
	f.Add(int64(4), uint8(1), uint8(16))
	f.Add(int64(3), uint8(2), uint8(2))
	f.Add(int64(3), uint8(2), uint8(16))
	f.Add(int64(4), uint8(2), uint8(18))
	f.Add(int64(4), uint8(2), uint8(19))
	f.Add(int64(vm.MaxSupportedGlobalVersion), uint8(0), uint8(32))
	f.Add(int64(vm.MaxSupportedGlobalVersion), uint8(3), uint8(32))
	f.Add(int64(vm.MaxSupportedGlobalVersion), uint8(1), uint8(31))
	f.Add(int64(vm.MaxSupportedGlobalVersion), uint8(2), uint8(19))
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for kind := uint8(0); kind < 4; kind++ {
			for _, mode := range []uint8{2, 15, 16, 18, 19, 31, 32} {
				f.Add(version, kind, mode)
			}
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawKind uint8, rawMode uint8) {
		version := fuzzFuncsVersion(rawVersion)
		kind := rawKind % 4
		mode := int64(rawMode % 40)

		st := newFuncTestState(t, nil)
		st.InitForExecution()
		st.GlobalVersion = version

		var err error
		switch kind {
		case 0:
			if err = st.Stack.PushInt(big.NewInt(10)); err != nil {
				t.Fatalf("push amount: %v", err)
			}
			if err = st.Stack.PushInt(big.NewInt(mode)); err != nil {
				t.Fatalf("push mode: %v", err)
			}
			err = RAWRESERVE().Interpret(st)
		case 3:
			if err = st.Stack.PushInt(big.NewInt(10)); err != nil {
				t.Fatalf("push amount: %v", err)
			}
			if err = st.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
				t.Fatalf("push extra currencies: %v", err)
			}
			if err = st.Stack.PushInt(big.NewInt(mode)); err != nil {
				t.Fatalf("push mode: %v", err)
			}
			err = RAWRESERVEX().Interpret(st)
		case 1:
			if err = st.Stack.PushCell(cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()); err != nil {
				t.Fatalf("push code: %v", err)
			}
			if err = st.Stack.PushInt(big.NewInt(mode)); err != nil {
				t.Fatalf("push mode: %v", err)
			}
			err = SETLIBCODE().Interpret(st)
		default:
			if err = st.Stack.PushInt(new(big.Int).SetBytes([]byte{0x22})); err != nil {
				t.Fatalf("push hash: %v", err)
			}
			if err = st.Stack.PushInt(big.NewInt(mode)); err != nil {
				t.Fatalf("push mode: %v", err)
			}
			err = CHANGELIB().Interpret(st)
		}

		wantRange := fuzzFuncsWantActionModeRangeError(kind, version, mode)
		if wantRange {
			if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeRangeCheck {
				t.Fatalf("kind=%d version=%d mode=%d err=%v, want range check", kind, version, mode, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("kind=%d version=%d mode=%d failed: %v", kind, version, mode, err)
		}
	})
}

func FuzzTVMVersionedDataSizeLowGasDeferral(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for kind := uint8(0); kind < 4; kind++ {
			f.Add(version, kind, uint8(1), uint8(version), uint8(kind+1))
			f.Add(version, kind, uint8(3), uint8(version+7), uint8(kind+5))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawKind, rawRefs, rawRootBits, rawChildBits uint8) {
		version := fuzzFuncsVersion(rawVersion)
		kind := rawKind % 4
		refs := int(rawRefs%4) + 1
		rootBits := uint(rawRootBits % 16)
		childBits := uint(rawChildBits%16) + 1

		root := cell.BeginCell()
		if rootBits > 0 {
			root.MustStoreUInt(uint64(rawRootBits)&((1<<rootBits)-1), rootBits)
		}
		uniqueChildBits := int64(0)
		seenChildren := map[cell.Hash]struct{}{}
		for i := 0; i < refs; i++ {
			child := cell.BeginCell().MustStoreUInt(uint64(i+1)&((1<<childBits)-1), childBits).EndCell()
			if _, ok := seenChildren[child.HashKey()]; !ok {
				seenChildren[child.HashKey()] = struct{}{}
				uniqueChildBits += int64(childBits)
			}
			root.MustStoreRef(child)
		}
		rootCell := root.EndCell()

		st := vm.NewExecutionState(version, vm.GasWithLimit(50), nil, tuple.Tuple{}, vm.NewStack())
		st.InitForExecution()
		if kind < 2 {
			if err := st.Stack.PushCell(rootCell); err != nil {
				t.Fatalf("push cell: %v", err)
			}
			if err := st.Stack.PushInt(big.NewInt(int64(refs + 1))); err != nil {
				t.Fatalf("push cell bound: %v", err)
			}
		} else {
			if err := st.Stack.PushSlice(rootCell.MustBeginParse()); err != nil {
				t.Fatalf("push slice: %v", err)
			}
			if err := st.Stack.PushInt(big.NewInt(int64(refs))); err != nil {
				t.Fatalf("push slice bound: %v", err)
			}
		}

		var err error
		switch kind {
		case 0:
			err = CDATASIZE().Interpret(st)
		case 1:
			err = CDATASIZEQ().Interpret(st)
		case 2:
			err = SDATASIZE().Interpret(st)
		default:
			err = SDATASIZEQ().Interpret(st)
		}

		if version >= 4 {
			if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeOutOfGas {
				t.Fatalf("kind=%d version=%d err=%v, want out of gas", kind, version, err)
			}
			if st.Stack.Len() != 0 {
				t.Fatalf("kind=%d version=%d stack len=%d, want 0", kind, version, st.Stack.Len())
			}
			return
		}

		if err != nil {
			t.Fatalf("kind=%d version=%d unexpected error: %v", kind, version, err)
		}
		if code, ok := vmerr.ErrorCode(st.CheckGas()); !ok || code != vmerr.CodeOutOfGas {
			t.Fatalf("kind=%d version=%d CheckGas=(%d,%t), want out of gas", kind, version, code, ok)
		}

		if kind == 1 || kind == 3 {
			ok, popErr := st.Stack.PopBool()
			if popErr != nil || !ok {
				t.Fatalf("kind=%d version=%d ok=(%v,%v), want true", kind, version, ok, popErr)
			}
		}
		uniqueChildren := int64(len(seenChildren))
		wantCells := uniqueChildren + 1
		if kind >= 2 {
			wantCells = uniqueChildren
		}
		wantBits := int64(rootBits) + uniqueChildBits
		wantRefs := int64(refs)
		gotRefs, popErr := st.Stack.PopIntFinite()
		if popErr != nil || gotRefs.Int64() != wantRefs {
			t.Fatalf("kind=%d version=%d refs=(%v,%v), want %d", kind, version, gotRefs, popErr, wantRefs)
		}
		gotBits, popErr := st.Stack.PopIntFinite()
		if popErr != nil || gotBits.Int64() != wantBits {
			t.Fatalf("kind=%d version=%d bits=(%v,%v), want %d", kind, version, gotBits, popErr, wantBits)
		}
		gotCells, popErr := st.Stack.PopIntFinite()
		if popErr != nil || gotCells.Int64() != wantCells {
			t.Fatalf("kind=%d version=%d cells=(%v,%v), want %d", kind, version, gotCells, popErr, wantCells)
		}
	})
}

func FuzzTVMVersionedFeeHashUnderflowPrecheck(f *testing.F) {
	f.Add(int64(8), uint8(0), false, uint8(0))
	f.Add(int64(9), uint8(0), false, uint8(0))
	f.Add(int64(8), uint8(5), false, uint8(0))
	f.Add(int64(9), uint8(5), false, uint8(0))
	f.Add(int64(8), uint8(6), false, uint8(0))
	f.Add(int64(9), uint8(6), false, uint8(0))
	f.Add(int64(14), uint8(4), true, uint8(0))
	f.Add(int64(14), uint8(6), false, uint8(254))
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for kind := uint8(0); kind < 7; kind++ {
			f.Add(version, kind, false, uint8(0))
			f.Add(version, kind, true, uint8(254))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawKind uint8, flag bool, rawHashID uint8) {
		version := fuzzFuncsVersion(rawVersion)
		kind := rawKind % 7
		st := &vm.State{
			GlobalVersion: version,

			Stack: vm.NewStack(),
		}

		var err error
		switch kind {
		case 0:
			if err = st.Stack.PushBool(flag); err != nil {
				t.Fatalf("push masterchain flag: %v", err)
			}
			err = GETGASFEE().Interpret(st)
		case 1:
			if err = st.Stack.PushBool(flag); err != nil {
				t.Fatalf("push masterchain flag: %v", err)
			}
			err = GETSTORAGEFEE().Interpret(st)
		case 2:
			if err = st.Stack.PushBool(flag); err != nil {
				t.Fatalf("push masterchain flag: %v", err)
			}
			err = GETFORWARDFEE().Interpret(st)
		case 3:
			if err = st.Stack.PushBool(flag); err != nil {
				t.Fatalf("push masterchain flag: %v", err)
			}
			err = GETORIGINALFWDFEE().Interpret(st)
		case 4:
			if err = st.Stack.PushBool(flag); err != nil {
				t.Fatalf("push masterchain flag: %v", err)
			}
			err = GETGASFEESIMPLE().Interpret(st)
		case 5:
			if err = st.Stack.PushBool(flag); err != nil {
				t.Fatalf("push masterchain flag: %v", err)
			}
			err = GETFORWARDFEESIMPLE().Interpret(st)
		default:
			if err = st.Stack.PushInt(big.NewInt(int64(rawHashID % 255))); err != nil {
				t.Fatalf("push hash id: %v", err)
			}
			err = HASHEXT(255).Interpret(st)
		}

		if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeStackUnderflow {
			t.Fatalf("kind=%d version=%d err=%v, want stack underflow", kind, version, err)
		}
		wantLen := 0
		if version >= 9 {
			wantLen = 1
		}
		if st.Stack.Len() != wantLen {
			t.Fatalf("kind=%d version=%d stack len=%d, want %d", kind, version, st.Stack.Len(), wantLen)
		}
	})
}

func fuzzFuncsStoragePricesSlice(seed uint64) *cell.Slice {
	return cell.BeginCell().
		MustStoreUInt(0xCC, 8).
		MustStoreUInt(1, 32).
		MustStoreUInt(seed%17+1, 64).
		MustStoreUInt(seed%19+2, 64).
		MustStoreUInt(seed%23+3, 64).
		MustStoreUInt(seed%29+4, 64).
		ToSlice()
}

func fuzzFuncsGasPricesSlice(seed uint64) *cell.Slice {
	return cell.BeginCell().
		MustStoreUInt(0xD1, 8).
		MustStoreUInt(seed%100+10, 64).
		MustStoreUInt(seed%200+20, 64).
		MustStoreUInt(0xDE, 8).
		MustStoreUInt(seed%31+1, 64).
		MustStoreUInt(1_000_000, 64).
		MustStoreUInt(2_000_000, 64).
		MustStoreUInt(10_000, 64).
		MustStoreUInt(3_000_000, 64).
		MustStoreUInt(4_000_000, 64).
		MustStoreUInt(5_000_000, 64).
		ToSlice()
}

func fuzzFuncsMsgPricesSlice(seed uint64) *cell.Slice {
	return cell.BeginCell().
		MustStoreUInt(0xEA, 8).
		MustStoreUInt(seed%97+1, 64).
		MustStoreUInt(seed%37+1, 64).
		MustStoreUInt(seed%41+1, 64).
		MustStoreUInt(123, 32).
		MustStoreUInt(1000, 16).
		MustStoreUInt(2000, 16).
		ToSlice()
}

func fuzzFuncsFeeState(t *testing.T, version int, seed uint64) (*vm.State, uint64) {
	t.Helper()

	extraAmount := seed%100_000 + 1
	extra := cell.NewDict(32)
	if _, err := extra.SetBuilderWithMode(
		cell.BeginCell().MustStoreUInt(7, 32).EndCell(),
		cell.BeginCell().MustStoreVarUInt(extraAmount, 32),
		cell.DictSetModeSet,
	); err != nil {
		t.Fatalf("seed extra balance dict: %v", err)
	}

	unpacked := tuple.NewTupleSized(7)
	if err := unpacked.Set(0, fuzzFuncsStoragePricesSlice(seed)); err != nil {
		t.Fatalf("set storage prices: %v", err)
	}
	if err := unpacked.Set(2, fuzzFuncsGasPricesSlice(seed)); err != nil {
		t.Fatalf("set masterchain gas prices: %v", err)
	}
	if err := unpacked.Set(3, fuzzFuncsGasPricesSlice(seed+1)); err != nil {
		t.Fatalf("set basechain gas prices: %v", err)
	}
	if err := unpacked.Set(4, fuzzFuncsMsgPricesSlice(seed)); err != nil {
		t.Fatalf("set masterchain msg prices: %v", err)
	}
	if err := unpacked.Set(5, fuzzFuncsMsgPricesSlice(seed+1)); err != nil {
		t.Fatalf("set basechain msg prices: %v", err)
	}

	st := newFuncTestState(t, map[int]any{
		7:  tuple.NewTupleValue(big.NewInt(10_000_000), extra.AsCell()),
		14: unpacked,
	})
	st.GlobalVersion = version
	return st, extraAmount
}

func FuzzTVMVersionedFeeHashRuntimeEdges(f *testing.F) {
	f.Add(int64(6), uint8(0), uint64(0))
	f.Add(int64(9), uint8(1), uint64(1))
	f.Add(int64(10), uint8(2), uint64(2))
	f.Add(int64(14), uint8(3), uint64(3))
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for kind := uint8(0); kind < 6; kind++ {
			f.Add(version, kind, uint64(version)<<8|uint64(kind))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawKind uint8, seed uint64) {
		version := fuzzFuncsVersion(rawVersion)
		kind := rawKind % 6
		st, extraAmount := fuzzFuncsFeeState(t, version, seed)

		switch kind {
		case 0:
			gas := int64(seed%10_000 + 1)
			if err := st.Stack.PushInt(big.NewInt(gas)); err != nil {
				t.Fatalf("push gas: %v", err)
			}
			if err := st.Stack.PushBool(seed&1 != 0); err != nil {
				t.Fatalf("push masterchain: %v", err)
			}
			if err := GETGASFEE().Interpret(st); err != nil {
				t.Fatalf("GETGASFEE version=%d failed: %v", version, err)
			}
			full, err := st.Stack.PopIntFinite()
			if err != nil {
				t.Fatalf("pop GETGASFEE: %v", err)
			}

			st, _ = fuzzFuncsFeeState(t, version, seed)
			if err := st.Stack.PushInt(big.NewInt(gas)); err != nil {
				t.Fatalf("push simple gas: %v", err)
			}
			if err := st.Stack.PushBool(seed&1 != 0); err != nil {
				t.Fatalf("push simple masterchain: %v", err)
			}
			if err := GETGASFEESIMPLE().Interpret(st); err != nil {
				t.Fatalf("GETGASFEESIMPLE version=%d failed: %v", version, err)
			}
			simple, err := st.Stack.PopIntFinite()
			if err != nil {
				t.Fatalf("pop GETGASFEESIMPLE: %v", err)
			}
			if full.Cmp(simple) < 0 {
				t.Fatalf("gas fee version=%d full=%v simple=%v", version, full, simple)
			}
		case 1:
			cellsCnt := int64(seed%7 + 1)
			bits := int64(seed%512 + 1)
			if err := st.Stack.PushInt(big.NewInt(cellsCnt)); err != nil {
				t.Fatalf("push cells: %v", err)
			}
			if err := st.Stack.PushInt(big.NewInt(bits)); err != nil {
				t.Fatalf("push bits: %v", err)
			}
			if err := st.Stack.PushBool(seed&1 != 0); err != nil {
				t.Fatalf("push masterchain: %v", err)
			}
			if err := GETFORWARDFEE().Interpret(st); err != nil {
				t.Fatalf("GETFORWARDFEE version=%d failed: %v", version, err)
			}
			full, err := st.Stack.PopIntFinite()
			if err != nil {
				t.Fatalf("pop GETFORWARDFEE: %v", err)
			}

			st, _ = fuzzFuncsFeeState(t, version, seed)
			if err := st.Stack.PushInt(big.NewInt(cellsCnt)); err != nil {
				t.Fatalf("push simple cells: %v", err)
			}
			if err := st.Stack.PushInt(big.NewInt(bits)); err != nil {
				t.Fatalf("push simple bits: %v", err)
			}
			if err := st.Stack.PushBool(seed&1 != 0); err != nil {
				t.Fatalf("push simple masterchain: %v", err)
			}
			if err := GETFORWARDFEESIMPLE().Interpret(st); err != nil {
				t.Fatalf("GETFORWARDFEESIMPLE version=%d failed: %v", version, err)
			}
			simple, err := st.Stack.PopIntFinite()
			if err != nil {
				t.Fatalf("pop GETFORWARDFEESIMPLE: %v", err)
			}
			if full.Cmp(simple) < 0 {
				t.Fatalf("forward fee version=%d full=%v simple=%v", version, full, simple)
			}
		case 2:
			if err := st.Stack.PushInt(big.NewInt(7)); err != nil {
				t.Fatalf("push hit id: %v", err)
			}
			if err := GETEXTRABALANCE().Interpret(st); err != nil {
				t.Fatalf("GETEXTRABALANCE hit version=%d failed: %v", version, err)
			}
			got, err := st.Stack.PopIntFinite()
			if err != nil || got.Uint64() != extraAmount {
				t.Fatalf("GETEXTRABALANCE hit version=%d = (%v, %v), want %d", version, got, err, extraAmount)
			}

			st, _ = fuzzFuncsFeeState(t, version, seed)
			if err := st.Stack.PushInt(big.NewInt(8)); err != nil {
				t.Fatalf("push miss id: %v", err)
			}
			if err := GETEXTRABALANCE().Interpret(st); err != nil {
				t.Fatalf("GETEXTRABALANCE miss version=%d failed: %v", version, err)
			}
			got, err = st.Stack.PopIntFinite()
			if err != nil || got.Sign() != 0 {
				t.Fatalf("GETEXTRABALANCE miss version=%d = (%v, %v), want 0", version, got, err)
			}
		case 3:
			data := []byte{byte(seed), byte(seed >> 8), byte(seed >> 16)}
			if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(data, uint(len(data))*8).ToSlice()); err != nil {
				t.Fatalf("push hash input: %v", err)
			}
			if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
				t.Fatalf("push count: %v", err)
			}
			if err := HASHEXT(0).Interpret(st); err != nil {
				t.Fatalf("HASHEXT version=%d failed: %v", version, err)
			}
			got, err := st.Stack.PopIntFinite()
			if err != nil {
				t.Fatalf("pop HASHEXT: %v", err)
			}
			want := sha256.Sum256(data)
			if got.Cmp(new(big.Int).SetBytes(want[:])) != 0 {
				t.Fatalf("HASHEXT version=%d = %x want %x", version, got.Bytes(), want[:])
			}
		case 4:
			prefix := byte(seed >> 24)
			data := []byte{byte(seed), byte(seed >> 8)}
			if err := st.Stack.PushBuilder(cell.BeginCell().MustStoreUInt(uint64(prefix), 8)); err != nil {
				t.Fatalf("push output builder: %v", err)
			}
			if err := st.Stack.PushBuilder(cell.BeginCell().MustStoreSlice(data, uint(len(data))*8)); err != nil {
				t.Fatalf("push input builder: %v", err)
			}
			if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
				t.Fatalf("push count: %v", err)
			}
			if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
				t.Fatalf("push hash id: %v", err)
			}
			if err := HASHEXT(1<<9 | 255).Interpret(st); err != nil {
				t.Fatalf("HASHEXTA version=%d failed: %v", version, err)
			}
			got, err := st.Stack.PopBuilder()
			if err != nil {
				t.Fatalf("pop HASHEXTA builder: %v", err)
			}
			wantHash := sha256.Sum256(data)
			want := append([]byte{prefix}, wantHash[:]...)
			if raw := mustSliceData(t, got.ToSlice()); !bytes.Equal(raw, want) {
				t.Fatalf("HASHEXTA version=%d = %x want %x", version, raw, want)
			}
		default:
			if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(seed&0x7f, 7).ToSlice()); err != nil {
				t.Fatalf("push unaligned input: %v", err)
			}
			if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
				t.Fatalf("push count: %v", err)
			}
			err := HASHEXT(0).Interpret(st)
			if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeCellUnderflow {
				t.Fatalf("HASHEXT unaligned version=%d err=%v, want cell underflow", version, err)
			}
		}
	})
}

func fuzzFuncsBLSSlice(data []byte) *cell.Slice {
	return cell.BeginCell().MustStoreSlice(data, uint(len(data))*8).ToSlice()
}

func fuzzFuncsBLSScalar(seed uint64) *big.Int {
	return new(big.Int).SetUint64(seed%97 + 1)
}

func fuzzFuncsBLSMessage(seed uint64) []byte {
	return []byte{
		'f', 'u', 'z', 'z',
		byte(seed),
		byte(seed >> 8),
		byte(seed >> 16),
		byte(seed >> 24),
	}
}

func fuzzFuncsBLSPubBytes(seed uint64) []byte {
	var pub circlbls.G1
	pub.ScalarMult(blsScalarFromInt(fuzzFuncsBLSScalar(seed)), circlbls.G1Generator())
	return pub.BytesCompressed()
}

func fuzzFuncsBLSSigBytes(t *testing.T, seed uint64, msg []byte) []byte {
	t.Helper()

	hash := blsHashToG2(msg)
	var sig circlbls.G2
	sig.ScalarMult(blsScalarFromInt(fuzzFuncsBLSScalar(seed)), hash)
	return sig.BytesCompressed()
}

func fuzzFuncsBLSAggregateSig(t *testing.T, sigs ...[]byte) []byte {
	t.Helper()

	var agg circlbls.G2
	for i, data := range sigs {
		p, err := parseBLSG2(data)
		if err != nil {
			t.Fatalf("parse BLS sig %d: %v", i, err)
		}
		if i == 0 {
			agg = *p
		} else {
			agg.Add(&agg, p)
		}
	}
	return agg.BytesCompressed()
}

func fuzzFuncsBLSG2Bytes(seed uint64) []byte {
	var p circlbls.G2
	p.ScalarMult(blsScalarFromInt(fuzzFuncsBLSScalar(seed)), circlbls.G2Generator())
	return p.BytesCompressed()
}

func fuzzFuncsWantBLSG2MultiExp(t *testing.T, g2a []byte, scalarA *big.Int, g2b []byte, scalarB *big.Int) []byte {
	t.Helper()

	p1, err := parseBLSG2(g2a)
	if err != nil {
		t.Fatalf("parse first G2: %v", err)
	}
	p2, err := parseBLSG2(g2b)
	if err != nil {
		t.Fatalf("parse second G2: %v", err)
	}
	var term1, term2, out circlbls.G2
	term1.ScalarMult(blsScalarFromInt(scalarA), p1)
	term2.ScalarMult(blsScalarFromInt(scalarB), p2)
	out.Add(&term1, &term2)
	return out.BytesCompressed()
}

func FuzzTVMVersionedBLSRuntimeEdges(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for kind := uint8(0); kind < 8; kind++ {
			f.Add(version, kind, uint64(version)<<8|uint64(kind))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawKind uint8, seed uint64) {
		version := fuzzFuncsVersion(rawVersion)
		kind := rawKind % 8
		st := newFuncTestState(t, nil)
		st.GlobalVersion = version

		msg := fuzzFuncsBLSMessage(seed)
		msg2 := fuzzFuncsBLSMessage(seed ^ 0xA5A5)
		pub1 := fuzzFuncsBLSPubBytes(seed + 1)
		pub2 := fuzzFuncsBLSPubBytes(seed + 2)
		sig1 := fuzzFuncsBLSSigBytes(t, seed+1, msg)
		sig2 := fuzzFuncsBLSSigBytes(t, seed+2, msg)
		sig2Msg2 := fuzzFuncsBLSSigBytes(t, seed+2, msg2)
		aggSig := fuzzFuncsBLSAggregateSig(t, sig1, sig2)
		aggDistinctSig := fuzzFuncsBLSAggregateSig(t, sig1, sig2Msg2)

		switch kind {
		case 0:
			if err := st.Stack.PushSlice(fuzzFuncsBLSSlice(pub1)); err != nil {
				t.Fatalf("push pub: %v", err)
			}
			if err := st.Stack.PushSlice(fuzzFuncsBLSSlice(msg)); err != nil {
				t.Fatalf("push msg: %v", err)
			}
			if err := st.Stack.PushSlice(fuzzFuncsBLSSlice(sig1)); err != nil {
				t.Fatalf("push sig: %v", err)
			}
			if err := BLS_VERIFY().Interpret(st); err != nil {
				t.Fatalf("BLS_VERIFY version=%d failed: %v", version, err)
			}
			if ok, err := st.Stack.PopBool(); err != nil || !ok {
				t.Fatalf("BLS_VERIFY version=%d = (%v, %v), want true", version, ok, err)
			}
		case 1:
			if err := st.Stack.PushSlice(fuzzFuncsBLSSlice(sig1)); err != nil {
				t.Fatalf("push sig1: %v", err)
			}
			if err := st.Stack.PushSlice(fuzzFuncsBLSSlice(sig2)); err != nil {
				t.Fatalf("push sig2: %v", err)
			}
			if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
				t.Fatalf("push count: %v", err)
			}
			if err := BLS_AGGREGATE().Interpret(st); err != nil {
				t.Fatalf("BLS_AGGREGATE version=%d failed: %v", version, err)
			}
			got, err := st.Stack.PopSlice()
			if err != nil || !bytes.Equal(mustSliceData(t, got), aggSig) {
				t.Fatalf("BLS_AGGREGATE version=%d mismatch: %v", version, err)
			}
		case 2:
			if err := st.Stack.PushSlice(fuzzFuncsBLSSlice(pub1)); err != nil {
				t.Fatalf("push pub1: %v", err)
			}
			if err := st.Stack.PushSlice(fuzzFuncsBLSSlice(pub2)); err != nil {
				t.Fatalf("push pub2: %v", err)
			}
			if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
				t.Fatalf("push count: %v", err)
			}
			if err := st.Stack.PushSlice(fuzzFuncsBLSSlice(msg)); err != nil {
				t.Fatalf("push msg: %v", err)
			}
			if err := st.Stack.PushSlice(fuzzFuncsBLSSlice(aggSig)); err != nil {
				t.Fatalf("push sig: %v", err)
			}
			if err := BLS_FASTAGGREGATEVERIFY().Interpret(st); err != nil {
				t.Fatalf("BLS_FASTAGGREGATEVERIFY version=%d failed: %v", version, err)
			}
			if ok, err := st.Stack.PopBool(); err != nil || !ok {
				t.Fatalf("BLS_FASTAGGREGATEVERIFY version=%d = (%v, %v), want true", version, ok, err)
			}
		case 3:
			if err := st.Stack.PushSlice(fuzzFuncsBLSSlice(pub1)); err != nil {
				t.Fatalf("push pub1: %v", err)
			}
			if err := st.Stack.PushSlice(fuzzFuncsBLSSlice(msg)); err != nil {
				t.Fatalf("push msg1: %v", err)
			}
			if err := st.Stack.PushSlice(fuzzFuncsBLSSlice(pub2)); err != nil {
				t.Fatalf("push pub2: %v", err)
			}
			if err := st.Stack.PushSlice(fuzzFuncsBLSSlice(msg2)); err != nil {
				t.Fatalf("push msg2: %v", err)
			}
			if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
				t.Fatalf("push count: %v", err)
			}
			if err := st.Stack.PushSlice(fuzzFuncsBLSSlice(aggDistinctSig)); err != nil {
				t.Fatalf("push sig: %v", err)
			}
			if err := BLS_AGGREGATEVERIFY().Interpret(st); err != nil {
				t.Fatalf("BLS_AGGREGATEVERIFY version=%d failed: %v", version, err)
			}
			if ok, err := st.Stack.PopBool(); err != nil || !ok {
				t.Fatalf("BLS_AGGREGATEVERIFY version=%d = (%v, %v), want true", version, ok, err)
			}
		case 4:
			g2 := fuzzFuncsBLSG2Bytes(seed + 3)
			if err := st.Stack.PushSlice(fuzzFuncsBLSSlice(g2)); err != nil {
				t.Fatalf("push g2: %v", err)
			}
			if err := st.Stack.PushInt(new(big.Int).Set(blsOrder)); err != nil {
				t.Fatalf("push order: %v", err)
			}
			if err := BLS_G2_MUL().Interpret(st); err != nil {
				t.Fatalf("BLS_G2_MUL version=%d failed: %v", version, err)
			}
			got, err := st.Stack.PopSlice()
			if err != nil || !bytes.Equal(mustSliceData(t, got), blsG2ZeroCompressed) {
				t.Fatalf("BLS_G2_MUL(order) version=%d mismatch: %v", version, err)
			}
		case 5:
			g2a := fuzzFuncsBLSG2Bytes(seed + 4)
			g2b := fuzzFuncsBLSG2Bytes(seed + 5)
			scalarA := new(big.Int).SetUint64(seed%11 + 1)
			scalarB := new(big.Int).SetUint64(seed%13 + 2)
			if err := st.Stack.PushSlice(fuzzFuncsBLSSlice(g2a)); err != nil {
				t.Fatalf("push g2a: %v", err)
			}
			if err := st.Stack.PushInt(scalarA); err != nil {
				t.Fatalf("push scalarA: %v", err)
			}
			if err := st.Stack.PushSlice(fuzzFuncsBLSSlice(g2b)); err != nil {
				t.Fatalf("push g2b: %v", err)
			}
			if err := st.Stack.PushInt(scalarB); err != nil {
				t.Fatalf("push scalarB: %v", err)
			}
			if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
				t.Fatalf("push count: %v", err)
			}
			if err := BLS_G2_MULTIEXP().Interpret(st); err != nil {
				t.Fatalf("BLS_G2_MULTIEXP version=%d failed: %v", version, err)
			}
			got, err := st.Stack.PopSlice()
			want := fuzzFuncsWantBLSG2MultiExp(t, g2a, scalarA, g2b, scalarB)
			if err != nil || !bytes.Equal(mustSliceData(t, got), want) {
				t.Fatalf("BLS_G2_MULTIEXP version=%d mismatch: %v", version, err)
			}
		case 6:
			if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0x42}, 95), 95*8).ToSlice()); err != nil {
				t.Fatalf("push short fp2: %v", err)
			}
			err := BLS_MAP_TO_G2().Interpret(st)
			if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeCellUnderflow {
				t.Fatalf("BLS_MAP_TO_G2 short version=%d err=%v, want cell underflow", version, err)
			}
		default:
			g1 := fuzzFuncsBLSPubBytes(seed + 6)
			g2 := fuzzFuncsBLSG2Bytes(seed + 7)
			if err := st.Stack.PushSlice(fuzzFuncsBLSSlice(g1)); err != nil {
				t.Fatalf("push g1: %v", err)
			}
			if err := st.Stack.PushSlice(fuzzFuncsBLSSlice(g2)); err != nil {
				t.Fatalf("push g2: %v", err)
			}
			if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
				t.Fatalf("push count: %v", err)
			}
			if err := BLS_PAIRING().Interpret(st); err != nil {
				t.Fatalf("BLS_PAIRING version=%d failed: %v", version, err)
			}
			if ok, err := st.Stack.PopBool(); err != nil || ok {
				t.Fatalf("BLS_PAIRING version=%d = (%v, %v), want false", version, ok, err)
			}
		}
	})
}

func fuzzFuncsSeedBytes(seed uint64) []byte {
	data := make([]byte, 32)
	for i := range data {
		data[i] = byte(seed >> ((uint(i) % 8) * 8))
	}
	return data
}

func fuzzFuncsC7Params(t *testing.T, seed uint64) tuple.Tuple {
	t.Helper()

	seedBytes := fuzzFuncsSeedBytes(seed)
	src := cell.BeginCell().MustStoreUInt(seed&0xff, 8).ToSlice()
	params := tuple.NewTupleSized(201)
	if err := params.Set(paramIdxRandomSeed, new(big.Int).SetBytes(seedBytes)); err != nil {
		t.Fatalf("set random seed param: %v", err)
	}
	if err := params.Set(paramIdxPrevBlocksInfo, tuple.NewTupleValue(
		new(big.Int).SetUint64(seed&0xffff),
		new(big.Int).SetUint64((seed>>16)&0xffff),
		new(big.Int).SetUint64((seed>>32)&0xffff),
	)); err != nil {
		t.Fatalf("set prev blocks param: %v", err)
	}
	if err := params.Set(paramIdxPrecompiledGas, new(big.Int).SetUint64(seed&0xffff)); err != nil {
		t.Fatalf("set precompiled gas param: %v", err)
	}
	if err := params.Set(paramIdxInMsgParams, tuple.NewTupleValue(
		new(big.Int).SetUint64(seed&1),
		new(big.Int).SetUint64((seed>>1)&1),
		src,
		new(big.Int).SetUint64((seed>>8)&0xffff),
		new(big.Int).SetUint64((seed>>24)&0xffff),
		new(big.Int).SetUint64((seed>>40)&0xffff),
		tuple.NewTupleValue(new(big.Int).SetUint64(seed&0xff), nil),
		tuple.NewTupleValue(new(big.Int).SetUint64((seed>>8)&0xff), nil),
		nil,
		nil,
	)); err != nil {
		t.Fatalf("set in-msg params: %v", err)
	}
	if err := params.Set(200, new(big.Int).SetUint64(seed^0xA5A5)); err != nil {
		t.Fatalf("set high param: %v", err)
	}

	c7 := tuple.NewTupleSized(1)
	if err := c7.Set(0, params); err != nil {
		t.Fatalf("set c7 params: %v", err)
	}
	return c7
}

func newFuzzFuncsVersionedState(t *testing.T, version int, c7 tuple.Tuple) *vm.State {
	t.Helper()

	st := vm.NewExecutionState(version, vm.NewGas(), nil, c7, vm.NewStack())
	st.InitForExecution()
	return st
}

func fuzzFuncsGlobalValue(kind uint8, seed uint64) any {
	switch kind % 6 {
	case 0:
		return nil
	case 1:
		return big.NewInt(int64(seed&0xffff) - 0x8000)
	case 2:
		return cell.BeginCell().MustStoreUInt(seed&0xff, 8).EndCell()
	case 3:
		return cell.BeginCell().MustStoreUInt(seed&0xffff, 16).ToSlice()
	case 4:
		return cell.BeginCell().MustStoreUInt(seed&0xff, 8)
	default:
		return tuple.NewTupleValue(new(big.Int).SetUint64(seed&0xffff), nil)
	}
}

func assertFuzzFuncsGlobalValue(t *testing.T, got any, want any) {
	t.Helper()

	switch w := want.(type) {
	case nil:
		if got != nil {
			t.Fatalf("global value = %T, want nil", got)
		}
	case *big.Int:
		g, ok := got.(*big.Int)
		if !ok || g.Cmp(w) != 0 {
			t.Fatalf("global int = %v (%T), want %v", got, got, w)
		}
	case *cell.Cell:
		g, ok := got.(*cell.Cell)
		if !ok || !bytes.Equal(g.Hash(), w.Hash()) {
			t.Fatalf("global cell = %T, want matching cell", got)
		}
	case *cell.Slice:
		g, ok := got.(*cell.Slice)
		if !ok || !bytes.Equal(mustSliceData(t, g), mustSliceData(t, w)) {
			t.Fatalf("global slice = %T, want matching slice", got)
		}
	case *cell.Builder:
		g, ok := got.(*cell.Builder)
		if !ok || !bytes.Equal(mustSliceData(t, g.ToSlice()), mustSliceData(t, w.ToSlice())) {
			t.Fatalf("global builder = %T, want matching builder", got)
		}
	case tuple.Tuple:
		g, ok := got.(tuple.Tuple)
		if !ok || g.Len() != w.Len() {
			t.Fatalf("global tuple = %T len mismatch, want len %d", got, w.Len())
		}
		if mustTupleInt(t, g, 0).Cmp(mustTupleInt(t, w, 0)) != 0 {
			t.Fatalf("global tuple index 0 = %v, want %v", mustTupleInt(t, g, 0), mustTupleInt(t, w, 0))
		}
		second, err := g.Index(1)
		if err != nil || second != nil {
			t.Fatalf("global tuple index 1 = (%T, %v), want nil", second, err)
		}
	default:
		t.Fatalf("unsupported fuzz global value type %T", want)
	}
}

func assertFuzzFuncsRangeCheck(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("expected range check")
	}
	if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeRangeCheck {
		t.Fatalf("error = %v, want range check", err)
	}
}

func FuzzTVMVersionedGlobalOpsEdges(f *testing.F) {
	f.Add(int64(0), uint8(0), int16(0), uint8(1), uint64(0x11))
	f.Add(int64(0), uint8(0), int16(254), uint8(2), uint64(0x22))
	f.Add(int64(4), uint8(1), int16(200), uint8(0), uint64(0x33))
	f.Add(int64(4), uint8(2), int16(-1), uint8(1), uint64(0x44))
	f.Add(int64(6), uint8(3), int16(255), uint8(3), uint64(0x55))
	f.Add(int64(6), uint8(4), int16(37), uint8(4), uint64(0x66))
	f.Add(int64(vm.MaxSupportedGlobalVersion), uint8(4), int16(-1), uint8(5), uint64(0x77))
	f.Add(int64(vm.MaxSupportedGlobalVersion), uint8(5), int16(31), uint8(0), uint64(0x88))
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for kind := uint8(0); kind < 6; kind++ {
			f.Add(version, kind, int16(kind)-1, kind, uint64(version)<<16|uint64(kind))
			f.Add(version, kind, int16(254+kind), kind+1, uint64(version)<<24|uint64(kind)<<8)
			f.Add(version, kind, int16(37+kind), kind+2, uint64(version)<<32|uint64(kind))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawKind uint8, rawIdx int16, rawValueKind uint8, seed uint64) {
		version := fuzzFuncsVersion(rawVersion)
		kind := rawKind % 6
		value := fuzzFuncsGlobalValue(rawValueKind, seed)
		st := newFuzzFuncsVersionedState(t, version, tuple.NewTupleSized(255))

		validIdx := int(rawIdx)
		if validIdx < 0 {
			validIdx = -validIdx
		}
		validIdx %= 255

		switch kind {
		case 0:
			if err := st.Stack.PushHostValue(value); err != nil {
				t.Fatalf("push global value: %v", err)
			}
			if err := st.Stack.PushInt(big.NewInt(int64(validIdx))); err != nil {
				t.Fatalf("push global index: %v", err)
			}
			if err := SETGLOBVAR().Interpret(st); err != nil {
				t.Fatalf("SETGLOBVAR version=%d idx=%d failed: %v", version, validIdx, err)
			}
			if err := st.Stack.PushInt(big.NewInt(int64(validIdx))); err != nil {
				t.Fatalf("push get global index: %v", err)
			}
			if err := GETGLOBVAR().Interpret(st); err != nil {
				t.Fatalf("GETGLOBVAR version=%d idx=%d failed: %v", version, validIdx, err)
			}
			got, err := st.Stack.PopAny()
			if err != nil {
				t.Fatalf("pop global value: %v", err)
			}
			assertFuzzFuncsGlobalValue(t, got, value)
		case 1:
			if err := st.Stack.PushInt(big.NewInt(int64(validIdx))); err != nil {
				t.Fatalf("push missing global index: %v", err)
			}
			if err := GETGLOBVAR().Interpret(st); err != nil {
				t.Fatalf("GETGLOBVAR missing version=%d idx=%d failed: %v", version, validIdx, err)
			}
			got, err := st.Stack.PopAny()
			if err != nil {
				t.Fatalf("pop missing global value: %v", err)
			}
			assertFuzzFuncsGlobalValue(t, got, nil)
		case 2:
			if validIdx < 255 {
				validIdx = 255
			}
			if rawIdx < 0 {
				validIdx = -1
			}
			if err := st.Stack.PushInt(big.NewInt(int64(validIdx))); err != nil {
				t.Fatalf("push invalid get index: %v", err)
			}
			assertFuzzFuncsRangeCheck(t, GETGLOBVAR().Interpret(st))
		case 3:
			if validIdx < 255 {
				validIdx = 255
			}
			if rawIdx < 0 {
				validIdx = -1
			}
			if err := st.Stack.PushHostValue(value); err != nil {
				t.Fatalf("push invalid set value: %v", err)
			}
			if err := st.Stack.PushInt(big.NewInt(int64(validIdx))); err != nil {
				t.Fatalf("push invalid set index: %v", err)
			}
			assertFuzzFuncsRangeCheck(t, SETGLOBVAR().Interpret(st))
		case 4:
			rawImmediateIdx := uint8(rawIdx)
			idx := int(rawImmediateIdx & 31)
			if err := st.Stack.PushHostValue(value); err != nil {
				t.Fatalf("push immediate global value: %v", err)
			}
			if err := SETGLOB(rawImmediateIdx).Interpret(st); err != nil {
				t.Fatalf("SETGLOB version=%d idx=%d failed: %v", version, idx, err)
			}
			if err := GETGLOB(rawImmediateIdx).Interpret(st); err != nil {
				t.Fatalf("GETGLOB version=%d idx=%d failed: %v", version, idx, err)
			}
			got, err := st.Stack.PopAny()
			if err != nil {
				t.Fatalf("pop immediate global value: %v", err)
			}
			assertFuzzFuncsGlobalValue(t, got, value)
		default:
			rawImmediateIdx := uint8(rawIdx)
			idx := int(rawImmediateIdx & 31)
			if err := GETGLOB(rawImmediateIdx).Interpret(st); err != nil {
				t.Fatalf("GETGLOB missing version=%d idx=%d failed: %v", version, idx, err)
			}
			got, err := st.Stack.PopAny()
			if err != nil {
				t.Fatalf("pop missing immediate global value: %v", err)
			}
			assertFuzzFuncsGlobalValue(t, got, nil)
		}
	})
}

func FuzzTVMVersionedC7ParamsPRNGEdges(f *testing.F) {
	f.Add(int64(0), uint8(0), uint64(0))
	f.Add(int64(4), uint8(1), uint64(0x1111))
	f.Add(int64(6), uint8(2), uint64(0x2222))
	f.Add(int64(9), uint8(3), uint64(0x3333))
	f.Add(int64(11), uint8(4), uint64(0x4444))
	f.Add(int64(vm.MaxSupportedGlobalVersion), uint8(5), uint64(0x5555))
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for kind := uint8(0); kind < 8; kind++ {
			f.Add(version, kind, uint64(version)<<8|uint64(kind))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawKind uint8, seed uint64) {
		version := fuzzFuncsVersion(rawVersion)
		kind := rawKind % 8
		c7 := fuzzFuncsC7Params(t, seed)
		st := newFuzzFuncsVersionedState(t, version, c7)

		switch kind {
		case 0:
			if err := GETPARAMLONG(200).Interpret(st); err != nil {
				t.Fatalf("GETPARAMLONG high index version=%d failed: %v", version, err)
			}
			got, err := st.Stack.PopIntFinite()
			if err != nil || got.Uint64() != seed^0xA5A5 {
				t.Fatalf("GETPARAMLONG high index version=%d = (%v, %v)", version, got, err)
			}
		case 1:
			if err := PREVMCBLOCKS_100().Interpret(st); err != nil {
				t.Fatalf("PREVMCBLOCKS_100 version=%d failed: %v", version, err)
			}
			got, err := st.Stack.PopIntFinite()
			if err != nil || got.Uint64() != (seed>>32)&0xffff {
				t.Fatalf("PREVMCBLOCKS_100 version=%d = (%v, %v)", version, got, err)
			}
		case 2:
			if err := INMSGPARAM(2).Interpret(st); err != nil {
				t.Fatalf("INMSGPARAM src version=%d failed: %v", version, err)
			}
			got, err := st.Stack.PopSlice()
			if err != nil {
				t.Fatalf("pop INMSGPARAM src: %v", err)
			}
			raw, err := got.PreloadSlice(got.BitsLeft())
			if err != nil || len(raw) != 1 || raw[0] != byte(seed) {
				t.Fatalf("INMSGPARAM src version=%d = (%x, %v)", version, raw, err)
			}
		case 3:
			if err := GETPRECOMPILEDGAS().Interpret(st); err != nil {
				t.Fatalf("GETPRECOMPILEDGAS version=%d failed: %v", version, err)
			}
			got, err := st.Stack.PopIntFinite()
			if err != nil || got.Uint64() != seed&0xffff {
				t.Fatalf("GETPRECOMPILEDGAS version=%d = (%v, %v)", version, got, err)
			}
		case 4:
			seedBytes := fuzzFuncsSeedBytes(seed)
			sum := sha512.Sum512(seedBytes)
			if err := RANDU256().Interpret(st); err != nil {
				t.Fatalf("RANDU256 version=%d failed: %v", version, err)
			}
			got, err := st.Stack.PopIntFinite()
			if err != nil || got.Cmp(new(big.Int).SetBytes(sum[32:])) != 0 {
				t.Fatalf("RANDU256 version=%d = (%v, %v)", version, got, err)
			}
			updated, err := st.GetParam(paramIdxRandomSeed)
			if err != nil {
				t.Fatalf("read updated seed: %v", err)
			}
			if gotSeed, ok := updated.(*big.Int); !ok || gotSeed.Cmp(new(big.Int).SetBytes(sum[:32])) != 0 {
				t.Fatalf("RANDU256 updated seed version=%d = %T %v", version, updated, updated)
			}
		case 5:
			bound := new(big.Int).SetUint64(seed & 0xffff)
			if err := st.Stack.PushInt(bound); err != nil {
				t.Fatalf("push RAND bound: %v", err)
			}
			seedBytes := fuzzFuncsSeedBytes(seed)
			sum := sha512.Sum512(seedBytes)
			want := new(big.Int).Mul(bound, new(big.Int).SetBytes(sum[32:]))
			want.Rsh(want, 256)
			if err := RAND().Interpret(st); err != nil {
				t.Fatalf("RAND version=%d failed: %v", version, err)
			}
			got, err := st.Stack.PopIntFinite()
			if err != nil || got.Cmp(want) != 0 {
				t.Fatalf("RAND version=%d = (%v, %v), want %v", version, got, err, want)
			}
		case 6:
			next := new(big.Int).SetUint64(seed ^ 0x5A5A)
			if err := st.Stack.PushInt(next); err != nil {
				t.Fatalf("push SETRAND seed: %v", err)
			}
			if err := SETRAND().Interpret(st); err != nil {
				t.Fatalf("SETRAND version=%d failed: %v", version, err)
			}
			updated, err := st.GetParam(paramIdxRandomSeed)
			if err != nil {
				t.Fatalf("read SETRAND seed: %v", err)
			}
			if gotSeed, ok := updated.(*big.Int); !ok || gotSeed.Cmp(next) != 0 {
				t.Fatalf("SETRAND seed version=%d = %T %v, want %v", version, updated, updated, next)
			}
		default:
			mix := new(big.Int).SetUint64(seed ^ 0xC3C3)
			if err := st.Stack.PushInt(mix); err != nil {
				t.Fatalf("push ADDRAND seed: %v", err)
			}
			seedBytes := fuzzFuncsSeedBytes(seed)
			mixBytes := mix.FillBytes(make([]byte, 32))
			sum := sha256.Sum256(append(seedBytes, mixBytes...))
			if err := ADDRAND().Interpret(st); err != nil {
				t.Fatalf("ADDRAND version=%d failed: %v", version, err)
			}
			updated, err := st.GetParam(paramIdxRandomSeed)
			if err != nil {
				t.Fatalf("read ADDRAND seed: %v", err)
			}
			if gotSeed, ok := updated.(*big.Int); !ok || gotSeed.Cmp(new(big.Int).SetBytes(sum[:])) != 0 {
				t.Fatalf("ADDRAND seed version=%d = %T %v", version, updated, updated)
			}
		}
	})
}

func FuzzTVMVersionedInMsgParamDirectConstructorMask(f *testing.F) {
	f.Add(int64(0), uint8(16), uint64(0))
	f.Add(int64(11), uint8(18), uint64(0x1111))
	f.Add(int64(vm.MaxSupportedGlobalVersion), uint8(31), uint64(0x2222))
	f.Add(int64(vm.MaxSupportedGlobalVersion), uint8(255), uint64(0x3333))
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for _, idx := range []uint8{0, 1, 2, 15, 16, 31, 255} {
			f.Add(version, idx, uint64(version)<<16|uint64(idx))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawIdx uint8, seed uint64) {
		version := fuzzFuncsVersion(rawVersion)
		maskedIdx := int(rawIdx & 15)

		inMsgParams := tuple.NewTupleSized(16)
		base := int64(seed & 0xffff)
		for i := 0; i < 16; i++ {
			if err := inMsgParams.Set(i, big.NewInt(base+int64(i))); err != nil {
				t.Fatalf("set in-msg param %d: %v", i, err)
			}
		}

		params := tuple.NewTupleSized(paramIdxInMsgParams + 1)
		if err := params.Set(paramIdxInMsgParams, inMsgParams); err != nil {
			t.Fatalf("set in-msg params: %v", err)
		}

		c7 := tuple.NewTupleSized(1)
		if err := c7.Set(0, params); err != nil {
			t.Fatalf("set c7 params: %v", err)
		}

		st := newFuzzFuncsVersionedState(t, version, c7)
		if err := INMSGPARAM(rawIdx).Interpret(st); err != nil {
			t.Fatalf("INMSGPARAM direct version=%d idx=%d failed: %v", version, rawIdx, err)
		}
		got, err := st.Stack.PopIntFinite()
		if err != nil || got.Int64() != base+int64(maskedIdx) {
			t.Fatalf("INMSGPARAM direct version=%d idx=%d masked=%d = (%v, %v)", version, rawIdx, maskedIdx, got, err)
		}
	})
}
