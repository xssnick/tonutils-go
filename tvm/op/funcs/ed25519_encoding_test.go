package funcs

import (
	"bytes"
	"crypto/ed25519"
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestSignatureCheckCanonicalREncoding(t *testing.T) {
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

	for _, version := range []int{0, 4, 13, 14, vm.MaxSupportedGlobalVersion} {
		for _, tc := range []struct {
			name      string
			public    []byte
			signature []byte
			always    bool
			want      bool
		}{
			{name: "canonical_R_noncanonical_key", public: negativeZeroIdentity, signature: canonical, want: true},
			{name: "negative_zero_R", public: negativeZeroIdentity, signature: negativeZeroR},
			{name: "noncanonical_y_R", public: negativeZeroIdentity, signature: nonCanonicalY},
			{name: "canonical_identity_key", public: identity, signature: canonical, want: version < 14},
			{name: "ordinary_valid", public: public, signature: valid, want: true},
			{name: "ordinary_invalid", public: public, signature: invalid},
			{name: "always_succeed_noncanonical_R", public: negativeZeroIdentity, signature: negativeZeroR, always: true, want: true},
		} {
			for _, op := range []vm.OP{CHKSIGNU(), CHKSIGNS()} {
				t.Run(fmt.Sprintf("v%d/%s/%s", version, op.SerializeText(), tc.name), func(t *testing.T) {
					state := newFuncTestState(t, nil)
					state.GlobalVersion = version
					state.SignatureCheckAlwaysSucceed = tc.always
					var data any = new(big.Int).SetBytes(message)
					if op.SerializeText() == "CHKSIGNS" {
						data = cell.BeginCell().MustStoreSlice(message, 256).ToSlice()
					}
					pushFuncTestStack(t, state, data,
						cell.BeginCell().MustStoreSlice(tc.signature, 512).ToSlice(),
						new(big.Int).SetBytes(tc.public))

					if err := op.Interpret(state); err != nil {
						t.Fatal(err)
					}
					if got, err := state.Stack.PopBool(); err != nil || got != tc.want {
						t.Fatalf("signature result = %t, error = %v, want %t", got, err, tc.want)
					}
					if state.Stack.Len() != 0 {
						t.Fatal("signature check left extra operands")
					}
				})
			}
		}
	}
}
