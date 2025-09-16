package keys

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"reflect"
	"testing"
)

func Test_sharedKey(t *testing.T) {
	type args struct {
		ourKey    ed25519.PrivateKey
		serverKey ed25519.PublicKey
	}
	tests := []struct {
		name    string
		args    args
		want    []byte
		wantErr bool
	}{
		{
			name: "basic",
			args: args{
				ourKey: ed25519.NewKeyFromSeed([]byte{
					175, 46, 138, 194, 124, 100, 226,
					85, 88, 44, 196, 159, 130, 167,
					223, 23, 125, 231, 145, 177, 104,
					171, 189, 252, 16, 143, 108, 237,
					99, 32, 104, 10}),
				serverKey: []byte{
					159, 133, 67, 157, 32, 148, 185, 42,
					99, 156, 44, 148, 147, 215, 183, 64,
					227, 157, 234, 141, 8, 181, 37, 152,
					109, 57, 214, 221, 105, 231, 243, 9,
				},
			},
			want: []byte{
				220, 183, 46, 193, 213, 106,
				149, 6, 197, 7, 75, 228, 108, 247,
				216, 126, 194, 59, 250, 51,
				191, 19, 17, 221, 189, 86,
				228, 159, 226, 223, 135, 119,
			},
			wantErr: false,
		},
		{
			name: "invalid server key",
			args: args{
				ourKey: ed25519.NewKeyFromSeed([]byte{
					175, 46, 138, 194, 124, 100, 226,
					85, 88, 44, 196, 159, 130, 167,
					223, 23, 125, 231, 145, 177, 104,
					171, 189, 252, 16, 143, 108, 237,
					99, 32, 104, 10}),
				serverKey: []byte{1, 2, 3},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SharedKey(tt.args.ourKey, tt.args.serverKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("sharedKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("sharedKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildSharedCipher(t *testing.T) {
	key := []byte{
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
	}
	checksum := []byte{
		0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27,
		0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f,
		0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37,
		0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f,
	}

	stream, err := BuildSharedCipher(key, checksum)
	if err != nil {
		t.Fatalf("BuildSharedCipher() error = %v", err)
	}

	plaintext := []byte{
		0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48,
		0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f, 0x50,
		0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58,
		0x59, 0x5a, 0x5b, 0x5c, 0x5d, 0x5e, 0x5f, 0x60,
	}
	got := make([]byte, len(plaintext))
	stream.XORKeyStream(got, plaintext)

	expectedKey := make([]byte, 32)
	copy(expectedKey, key[:16])
	copy(expectedKey[16:], checksum[16:])
	expectedIV := make([]byte, 16)
	copy(expectedIV, checksum[:4])
	copy(expectedIV[4:], key[20:])
	block, err := aes.NewCipher(expectedKey)
	if err != nil {
		t.Fatalf("failed to init AES cipher: %v", err)
	}
	expectedStream := cipher.NewCTR(block, expectedIV)
	want := make([]byte, len(plaintext))
	expectedStream.XORKeyStream(want, plaintext)

	if !bytes.Equal(got, want) {
		t.Errorf("BuildSharedCipher() produced %x, want %x", got, want)
	}
}

func TestNewCipherCtr(t *testing.T) {
	key := []byte{
		0x60, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67,
		0x68, 0x69, 0x6a, 0x6b, 0x6c, 0x6d, 0x6e, 0x6f,
	}
	iv := []byte{
		0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77,
		0x78, 0x79, 0x7a, 0x7b, 0x7c, 0x7d, 0x7e, 0x7f,
	}
	stream, err := NewCipherCtr(key, iv)
	if err != nil {
		t.Fatalf("NewCipherCtr() error = %v", err)
	}

	plaintext := []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}
	got := make([]byte, len(plaintext))
	stream.XORKeyStream(got, plaintext)

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("failed to init AES cipher: %v", err)
	}
	expectedStream := cipher.NewCTR(block, iv)
	want := make([]byte, len(plaintext))
	expectedStream.XORKeyStream(want, plaintext)

	if !bytes.Equal(got, want) {
		t.Errorf("NewCipherCtr() produced %x, want %x", got, want)
	}

	if _, err := NewCipherCtr(key[:15], iv); err == nil {
		t.Fatal("NewCipherCtr() expected error for invalid key length")
	}
}

func TestEd25519PrivateToX25519(t *testing.T) {
	seed := []byte{
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
	}
	priv := ed25519.NewKeyFromSeed(seed)
	got := Ed25519PrivateToX25519(priv)
	want := []byte{
		0x38, 0x94, 0xee, 0xa4, 0x9c, 0x58, 0x0a, 0xef,
		0x81, 0x69, 0x35, 0x76, 0x2b, 0xe0, 0x49, 0x55,
		0x9d, 0x6d, 0x14, 0x40, 0xde, 0xde, 0x12, 0xe6,
		0xa1, 0x25, 0xf1, 0x84, 0x1f, 0xff, 0x8e, 0x6f,
	}

	if !bytes.Equal(got, want) {
		t.Errorf("Ed25519PrivateToX25519() = %x, want %x", got, want)
	}
}

func TestEd25519PubToX25519(t *testing.T) {
	seed := []byte{
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
	}
	priv := ed25519.NewKeyFromSeed(seed)
	got, err := Ed25519PubToX25519(priv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatalf("Ed25519PubToX25519() error = %v", err)
	}
	want := []byte{
		0x47, 0x01, 0xd0, 0x84, 0x88, 0x45, 0x1f, 0x54,
		0x5a, 0x40, 0x9f, 0xb5, 0x8a, 0xe3, 0xe5, 0x85,
		0x81, 0xca, 0x40, 0xac, 0x3f, 0x7f, 0x11, 0x46,
		0x98, 0xcd, 0x71, 0xde, 0xac, 0x73, 0xca, 0x01,
	}

	if !bytes.Equal(got, want) {
		t.Errorf("Ed25519PubToX25519() = %x, want %x", got, want)
	}
}

func TestEd25519PubToX25519_InvalidKey(t *testing.T) {
	if _, err := Ed25519PubToX25519(ed25519.PublicKey(make([]byte, 31))); err == nil {
		t.Fatal("Ed25519PubToX25519() expected error for invalid key length")
	}
}
