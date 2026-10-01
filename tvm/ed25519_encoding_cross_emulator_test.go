//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"crypto/ed25519"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestTVMCrossEmulatorEd25519CanonicalREncoding(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library unavailable: %v", err)
	}

	message := make([]byte, 32)
	identity := make([]byte, ed25519.PublicKeySize)
	identity[0] = 1
	negativeZeroIdentity := bytes.Clone(identity)
	negativeZeroIdentity[31] = 0x80
	canonical := make([]byte, ed25519.SignatureSize)
	copy(canonical, identity)
	negativeZeroR := bytes.Clone(canonical)
	negativeZeroR[31] = 0x80
	nonCanonicalY := bytes.Clone(canonical)
	copy(nonCanonicalY, bytes.Repeat([]byte{0xff}, 32))
	nonCanonicalY[0], nonCanonicalY[31] = 0xee, 0x7f
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x4b}, ed25519.SeedSize))
	public := private.Public().(ed25519.PublicKey)
	valid := ed25519.Sign(private, message)
	invalid := bytes.Clone(valid)
	invalid[32] ^= 1

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		config, err := crossRunPreparedBlockchainConfig(version)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			name      string
			public    []byte
			signature []byte
			want      bool
		}{
			{name: "canonical_R_noncanonical_key", public: negativeZeroIdentity, signature: canonical, want: true},
			{name: "negative_zero_R", public: negativeZeroIdentity, signature: negativeZeroR},
			{name: "noncanonical_y_R", public: negativeZeroIdentity, signature: nonCanonicalY},
			{name: "canonical_identity_key", public: identity, signature: canonical, want: version < 14},
			{name: "ordinary_valid", public: public, signature: valid, want: true},
			{name: "ordinary_invalid", public: public, signature: invalid},
		} {
			for _, op := range []vm.OP{funcsop.CHKSIGNU(), funcsop.CHKSIGNS()} {
				t.Run(fmt.Sprintf("v%d/%s/%s", version, op.SerializeText(), tc.name), func(t *testing.T) {
					code := prependRawMethodDrop(codeFromBuilders(t, op.Serialize()))
					c7 := prepareCrossTestC7WithConfigRoot(config.Root(), code)
					var data any = new(big.Int).SetBytes(message)
					if op.SerializeText() == "CHKSIGNS" {
						data = testSliceFromBytes(message)
					}
					args := []any{data, testSliceFromBytes(tc.signature), new(big.Int).SetBytes(tc.public)}
					goStack, err := buildCrossStack(args...)
					if err != nil {
						t.Fatal(err)
					}
					refStack, err := buildCrossStack(args...)
					if err != nil {
						t.Fatal(err)
					}
					goResult, err := runGoCrossCodeWithVersion(code, testEmptyCell(), c7, goStack, version)
					if err != nil {
						t.Fatal(err)
					}
					refResult, err := runReferenceCrossCode(code, testEmptyCell(), c7, refStack)
					if err != nil {
						t.Fatal(err)
					}

					for _, result := range []struct {
						name string
						run  *crossRunResult
					}{
						{name: "Go", run: goResult},
						{name: "C++", run: refResult},
					} {
						if result.run.exitCode != 0 || result.run.gasUsed != 49 {
							t.Fatalf("%s exit/gas = %d/%d, want 0/49", result.name, result.run.exitCode, result.run.gasUsed)
						}
						want := int64(0)
						if tc.want {
							want = -1
						}
						var stack tlb.Stack
						if err := tlb.Parse(&stack, result.run.stack); err != nil {
							t.Fatal(err)
						}
						if stack.Depth() != 1 {
							t.Fatalf("%s stack depth = %d, want 1", result.name, stack.Depth())
						}
						value, err := stack.Pop()
						if err != nil {
							t.Fatal(err)
						}
						boolean, ok := value.(*big.Int)
						if !ok || boolean.Cmp(big.NewInt(want)) != 0 {
							t.Fatalf("%s signature result = %v, want %d", result.name, value, want)
						}
					}
				})
			}
		}
	}
}
