package tlb

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
	"testing"
)

func TestStateInit_CalcAddress(t *testing.T) {
	tests := []struct {
		name      string
		stateInit StateInit
		workchain int
		want      string
	}{
		{
			name: "Base",
			stateInit: StateInit{
				Code: cell.BeginCell().MustStoreUInt(0, 8).EndCell(),
				Data: cell.BeginCell().MustStoreUInt(0, 8).EndCell(),
			},
			workchain: 0,
			want:      "EQBPQF6r6-pUObVWu6RO05YwoHQRnjM95tRLAL_s2A6n0pvq",
		},
		{
			name:      "Empty",
			stateInit: StateInit{},
			workchain: 0,
			want:      "EQA_B407fiLIlE5VYZCaI2rki0in6kLyjdhhwitvZNfpe7eY",
		},
		{
			name: "Master",
			stateInit: StateInit{
				Code: cell.BeginCell().MustStoreUInt(123, 8).EndCell(),
				Data: cell.BeginCell().MustStoreUInt(456, 16).EndCell(),
			},
			workchain: -1,
			want:      "Ef_jHHi5wLtyTaS56iIEPUc9mJuoD2keQPxZX87rl2FcVDZ1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.stateInit.CalcAddress(tt.workchain)
			if got.String() != tt.want {
				t.Errorf("StateInit.CalcAddress() = %v, want %v", got, tt.want)
			}
		})
	}
}
