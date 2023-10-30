package discmath

import (
	"reflect"
	"testing"
)

func TestNewPlainMatrixGF2(t *testing.T) {
	type args struct {
		rows uint32
		cols uint32
	}

	tests := []struct {
		name string
		args args
		want *PlainMatrixGF2
	}{
		{
			name: "1x1",
			args: args{
				rows: 1,
				cols: 1,
			},
			want: &PlainMatrixGF2{
				rows: 1,
				cols: 1,
				data: []byte{0},
			},
		},
		{
			name: "size lower one element",
			args: args{
				rows: 2,
				cols: 3,
			},
			want: &PlainMatrixGF2{
				rows: 2,
				cols: 3,
				data: []byte{0},
			},
		},
		{
			name: "size greater one element",
			args: args{
				rows: 2,
				cols: 7,
			},
			want: &PlainMatrixGF2{
				rows: 2,
				cols: 7,
				data: []byte{0, 0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewPlainMatrixGF2(tt.args.rows, tt.args.cols)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewPlainMatrixGF2() = %v, want %v", got, tt.want)
			}

			gotRows := got.RowsNum()
			if gotRows != tt.want.rows {
				t.Errorf("RowsNum() = %v, want %v", gotRows, tt.want.rows)
			}

			gotCols := got.ColsNum()
			if gotCols != tt.want.cols {
				t.Errorf("ColsNum() = %v, want %v", gotCols, tt.want.cols)
			}
		})
	}
}

func TestPlainMatrixGF2_Get(t *testing.T) {
	type args struct {
		row uint32
		col uint32
	}

	tests := []struct {
		name string
		m    *PlainMatrixGF2
		args args
		want byte
	}{
		{
			name: "get 0",
			m: &PlainMatrixGF2{
				rows: 2,
				cols: 5,
				data: []byte{131, 0}, // 1000 0011
			},
			args: args{
				row: 1,
				col: 4,
			},
			want: 0,
		},
		{
			name: "get 1",
			m: &PlainMatrixGF2{
				rows: 2,
				cols: 5,
				data: []byte{0, 2}, // 0000 0010
			},
			args: args{
				row: 1,
				col: 4,
			},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.m.Get(tt.args.row, tt.args.col)
			if got != tt.want {
				t.Errorf("Get() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlainMatrixGF2_Set(t *testing.T) {
	type args struct {
		row uint32
		col uint32
	}

	tests := []struct {
		name string
		m    *PlainMatrixGF2
		args args
		want []byte
	}{
		{
			name: "3x3 set [0,1]",
			m: &PlainMatrixGF2{
				rows: 3,
				cols: 3,
				data: []byte{0, 0},
			},
			args: args{
				row: 0,
				col: 1,
			},
			want: []byte{2, 0},
		},
		{
			name: "3x3 set [2,2]",
			m: &PlainMatrixGF2{
				rows: 3,
				cols: 3,
				data: []byte{0, 0},
			},
			args: args{
				row: 2,
				col: 2,
			},
			want: []byte{0, 1},
		},
		{
			name: "2x10, set [1,9]",
			m: &PlainMatrixGF2{
				rows: 2,
				cols: 10,
				data: []byte{0, 0, 0},
			},
			args: args{
				row: 1,
				col: 9,
			},
			want: []byte{0, 0, 8},
		},
		{
			name: "set bit which is already set",
			m: &PlainMatrixGF2{
				rows: 3,
				cols: 3,
				data: []byte{24, 0}, // 1: 1 1 0
			},
			args: args{
				row: 1,
				col: 1,
			},
			want: []byte{24, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.m.Set(tt.args.row, tt.args.col)
			if !reflect.DeepEqual(tt.m.data, tt.want) {
				t.Errorf("Set() = %v, want %v", tt.m.data, tt.want)
			}
		})
	}
}

func TestPlainMatrixGF2_Unset(t *testing.T) {
	type args struct {
		row uint32
		col uint32
	}

	tests := []struct {
		name string
		m    *PlainMatrixGF2
		args args
		want []byte
	}{
		{
			name: "3x3 unset [0,1]",
			m: &PlainMatrixGF2{
				rows: 3,
				cols: 3,
				data: []byte{2, 0},
			},
			args: args{
				row: 0,
				col: 1,
			},
			want: []byte{0, 0},
		},
		{
			name: "3x3 unset [1,2]",
			m: &PlainMatrixGF2{
				rows: 3,
				cols: 3,
				data: []byte{32, 0},
			},
			args: args{
				row: 1,
				col: 2,
			},
			want: []byte{0, 0},
		},
		{
			name: "unset bit which is already unset",
			m: &PlainMatrixGF2{
				rows: 3,
				cols: 3,
				data: []byte{0, 0},
			},
			args: args{
				row: 1,
				col: 1,
			},
			want: []byte{0, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.m.Unset(tt.args.row, tt.args.col)
			if !reflect.DeepEqual(tt.m.data, tt.want) {
				t.Errorf("Unset() = %v, want %v", tt.m.data, tt.want)
			}
		})
	}
}

func TestPlainMatrixGF2_GetRow(t *testing.T) {
	tests := []struct {
		name             string
		m                *PlainMatrixGF2
		row              uint32
		want             []byte
		wantFirstBitmask byte
		wantLastBitmask  byte
	}{
		{
			name: "all row in one element",
			m: &PlainMatrixGF2{
				rows: 3,
				cols: 2,
				data: []byte{8}, // 00 00[10]00
			},
			row:              1,
			want:             []byte{8},
			wantFirstBitmask: 12, // 00 00[11]00
			wantLastBitmask:  12, // 00 00[11]00
		},
		{
			name: "row in few elements",
			m: &PlainMatrixGF2{
				rows: 2,
				cols: 10,
				data: []byte{0, 248, 12}, // 0: 0000 0000 | 1: [1111 10]00 | 2: 0000 [1100]
			},
			row:              1,
			want:             []byte{248, 12},
			wantFirstBitmask: 252,
			wantLastBitmask:  15,
		},
		{
			name: "last row",
			m: &PlainMatrixGF2{
				rows: 3,
				cols: 8,
				data: []byte{0, 0, 0},
			},
			row:              2,
			want:             []byte{0},
			wantFirstBitmask: 255,
			wantLastBitmask:  255,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotFirstBitmask, gotLastBitmask := tt.m.GetRow(tt.row)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetRow() got = %v, want %v", got, tt.want)
			}

			if gotFirstBitmask != tt.wantFirstBitmask {
				t.Errorf("GetRow() gotFirstBitmask = %v, want %v", gotFirstBitmask, tt.wantFirstBitmask)
			}

			if gotLastBitmask != tt.wantLastBitmask {
				t.Errorf("GetRow() gotLastBitmask = %v, want %v", gotLastBitmask, tt.wantLastBitmask)
			}
		})
	}
}

func TestPlainMatrixGF2_RowAdd(t *testing.T) {
	type args struct {
		row          uint32
		what         []byte
		firstBitmask byte
		lastBitmask  byte
	}

	tests := []struct {
		name string
		m    *PlainMatrixGF2
		args args
		want []byte
	}{
		{
			name: "same rows",
			m: &PlainMatrixGF2{
				rows: 3,
				cols: 3,
				data: []byte{146, 0}, // 0: 10[010]010
			},
			args: args{
				row:          1,
				what:         []byte{24}, // 00[011]000
				firstBitmask: 56,         // 00[111]000
				lastBitmask:  56,         // 00[111]000
			},
			want: []byte{138, 0}, // 10[001]010
		},
		{
			name: "different rows: #1 + #2",
			m: &PlainMatrixGF2{
				rows: 3,
				cols: 3,
				data: []byte{146, 0}, // 0: 10[010]010 | 1: 0000 0000
			},
			args: args{
				row:          1,
				what:         []byte{128, 1}, // 0: 10]00 0000 | 1: 0000 000[1
				firstBitmask: 192,
				lastBitmask:  1,
			},
			want: []byte{162, 0}, // 0: 10[100]010 | 1: 0000 0000
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.m.RowAdd(tt.args.row, tt.args.what, tt.args.firstBitmask, tt.args.lastBitmask)
			if !reflect.DeepEqual(tt.m.data, tt.want) {
				t.Errorf("RowAdd() = %v, want %v", tt.m.data, tt.want)
			}
		})
	}
}

func TestPlainMatrixGF2_Mul(t *testing.T) {
	tests := []struct {
		name string
		m    *PlainMatrixGF2
		s    *MatrixGF256
		want *PlainMatrixGF2
	}{
		// TODO: Add test cases.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.m.Mul(tt.s)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Mul() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlainMatrixGF2_ToGF256(t *testing.T) {
	tests := []struct {
		name string
		m    *PlainMatrixGF2
		want *MatrixGF256
	}{
		{
			name: "5x3",
			m: &PlainMatrixGF2{
				rows: 5,
				cols: 3,
				data: []byte{234, 49}, // 0: 11][101][010] | 1: 0[011][000][1
			},
			want: &MatrixGF256{
				rows: []GF256{
					{data: []uint8{0, 1, 0}},
					{data: []uint8{1, 0, 1}},
					{data: []uint8{1, 1, 1}},
					{data: []uint8{0, 0, 0}},
					{data: []uint8{1, 1, 0}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.m.ToGF256()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ToGF256() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlainMatrixGF2_String(t *testing.T) {
	tests := []struct {
		name string
		m    *PlainMatrixGF2
		want string
	}{
		{
			name: "2x3",
			m: &PlainMatrixGF2{
				rows: 2,
				cols: 3,
				data: []byte{42}, // 00 [101][010]
			},
			want: "00 01 00\n" + "01 00 01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.m.String()
			if got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Size: 100x100
// Data size: 10 000 bytes
// BenchmarkMatrixGF2-12: GetRow+RowAdd    	   114345	     10073 ns/op	       0 B/op	       0 allocs/op
// BenchmarkMatrixGF2-12: GetRow               25560945	     43.34 ns/op	       0 B/op	       0 allocs/op
func BenchmarkMatrixGF2(b *testing.B) {
	const dimension = 100

	m := NewMatrixGF2(dimension, dimension)
	for i := uint32(0); i < dimension; i++ {
		m.Set(i, i)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for j := uint32(0); j < m.RowsNum()-1; j++ {
			what := m.GetRow(j + 1)
			m.RowAdd(j, what)
		}
	}
}

// Size: 100x100
// Data size: 1250 bytes
// BenchmarkPlainMatrixGF2-12  GetRow+RowAdd  	       30560	     39130 ns/op	       0 B/op	       0 allocs/op
// BenchmarkPlainMatrixGF2-12: GetRow    	           2639367	     435.2 ns/op	       0 B/op	       0 allocs/op
func BenchmarkPlainMatrixGF2(b *testing.B) {
	const dimension = 100

	m := NewPlainMatrixGF2(dimension, dimension)
	for i := uint32(0); i < dimension; i++ {
		m.Set(i, i)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for j := uint32(0); j < m.RowsNum()-1; j++ {
			what, fBitmask, lBitmask := m.GetRow(j + 1)
			m.RowAdd(j, what, fBitmask, lBitmask)
		}
	}
}
