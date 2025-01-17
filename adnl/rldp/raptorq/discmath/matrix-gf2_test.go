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
				rows:    1,
				cols:    1,
				rowSize: 1,
				data:    []byte{0},
			},
		},
		{
			name: "column size lower one element",
			args: args{
				rows: 3,
				cols: 5,
			},
			want: &PlainMatrixGF2{
				rows:    3,
				cols:    5,
				rowSize: 1,
				data:    []byte{0, 0, 0},
			},
		},
		{
			name: "column size greater one element",
			args: args{
				rows: 3,
				cols: 10,
			},
			want: &PlainMatrixGF2{
				rows:    3,
				cols:    10,
				rowSize: 2,
				data:    []byte{0, 0, 0, 0, 0, 0},
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
				rows:    2,
				cols:    5,
				rowSize: 1,
				data:    []byte{19, 0}, // 000[10011]
			},
			args: args{
				row: 0,
				col: 2,
			},
			want: 0,
		},
		{
			name: "get 1",
			m: &PlainMatrixGF2{
				rows:    2,
				cols:    5,
				rowSize: 1,
				data:    []byte{19, 0}, // 000[10011]
			},
			args: args{
				row: 0,
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
				rows:    3,
				cols:    3,
				rowSize: 1,
				data:    []byte{0, 0, 0},
			},
			args: args{
				row: 0,
				col: 1,
			},
			want: []byte{2, 0, 0},
		},
		{
			name: "3x3 set [2,2]",
			m: &PlainMatrixGF2{
				rows:    3,
				cols:    3,
				rowSize: 1,
				data:    []byte{0, 0, 0},
			},
			args: args{
				row: 2,
				col: 2,
			},
			want: []byte{0, 0, 4},
		},
		{
			name: "2x10, set [1,9]",
			m: &PlainMatrixGF2{
				rows:    2,
				cols:    10,
				rowSize: 2,
				data:    []byte{0, 0, 0, 0},
			},
			args: args{
				row: 1,
				col: 9,
			},
			want: []byte{0, 0, 0, 2},
		},
		{
			name: "set bit which is already set",
			m: &PlainMatrixGF2{
				rows:    3,
				cols:    3,
				rowSize: 1,
				data:    []byte{0, 2, 0},
			},
			args: args{
				row: 1,
				col: 1,
			},
			want: []byte{0, 2, 0},
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
				rows:    3,
				cols:    3,
				rowSize: 1,
				data:    []byte{2, 0, 0},
			},
			args: args{
				row: 0,
				col: 1,
			},
			want: []byte{0, 0, 0},
		},
		{
			name: "3x3 unset [1,2]",
			m: &PlainMatrixGF2{
				rows:    3,
				cols:    3,
				rowSize: 1,
				data:    []byte{0, 4, 0},
			},
			args: args{
				row: 1,
				col: 2,
			},
			want: []byte{0, 0, 0},
		},
		{
			name: "unset bit which is already unset",
			m: &PlainMatrixGF2{
				rows:    3,
				cols:    3,
				rowSize: 1,
				data:    []byte{0, 0, 0},
			},
			args: args{
				row: 1,
				col: 1,
			},
			want: []byte{0, 0, 0},
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
		name string
		m    *PlainMatrixGF2
		row  uint32
		want []byte
	}{
		{
			name: "all row in one element",
			m: &PlainMatrixGF2{
				rows:    3,
				cols:    2,
				rowSize: 1,
				data:    []byte{0, 8, 0},
			},
			row:  1,
			want: []byte{8},
		},
		{
			name: "row in few elements",
			m: &PlainMatrixGF2{
				rows:    2,
				cols:    10,
				rowSize: 2,
				data:    []byte{0, 0, 17, 3}, // 1,0: 00010001] 1,1: 000000[11
			},
			row:  1,
			want: []byte{17, 3},
		},
		{
			name: "last row",
			m: &PlainMatrixGF2{
				rows:    3,
				cols:    8,
				rowSize: 1,
				data:    []byte{0, 0, 0},
			},
			row:  2,
			want: []byte{0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.m.GetRow(tt.row)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetRow() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlainMatrixGF2_RowAdd(t *testing.T) {
	type args struct {
		row  uint32
		what []byte
	}

	tests := []struct {
		name string
		m    *PlainMatrixGF2
		args args
		want []byte
	}{
		{
			name: "row in one element",
			m: &PlainMatrixGF2{
				rows:    3,
				cols:    3,
				rowSize: 1,
				data:    []byte{0, 3, 0},
			},
			args: args{
				row:  1,
				what: []byte{6},
			},
			want: []byte{0, 5, 0},
		},
		{
			name: "row in few elements",
			m: &PlainMatrixGF2{
				rows:    2,
				cols:    10,
				rowSize: 2,
				data:    []byte{0, 0, 129, 2}, // 1,0: 10000001] | 1,1: 000000[10
			},
			args: args{
				row:  1,
				what: []byte{1, 3},
			},
			want: []byte{0, 0, 128, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.m.RowAdd(tt.args.row, tt.args.what)
			if !reflect.DeepEqual(tt.m.data, tt.want) {
				t.Errorf("RowAdd() = %v, want %v", tt.m.data, tt.want)
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
			name: "2x3",
			m: &PlainMatrixGF2{
				rows:    2,
				cols:    3,
				rowSize: 1,
				data:    []byte{2, 5},
			},
			want: &MatrixGF256{
				Rows: 2,
				Cols: 3,
				Data: []uint8{0, 1, 0, 1, 0, 1},
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
				rows:    2,
				cols:    3,
				rowSize: 1,
				data:    []byte{2, 5},
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

// --- Benchmarks ---

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
// Data size: 1300 bytes
// BenchmarkPlainMatrixGF2-12: GetRow+RowAdd    	 1000000	      1096 ns/op	       0 B/op	       0 allocs/op
// BenchmarkPlainMatrixGF2-12: GetRow    	         10408053	      109.8 ns/op	       0 B/op	       0 allocs/op
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
			what := m.GetRow(j + 1)
			m.RowAdd(j, what)
		}
	}
}
