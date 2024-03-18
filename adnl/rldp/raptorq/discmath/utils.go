package discmath

func InversePermutation(mut []uint32) []uint32 {
	res := make([]uint32, len(mut))
	for i, u := range mut {
		res[u] = uint32(i)
	}
	return res
}
