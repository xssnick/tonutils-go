package adnl

import (
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
