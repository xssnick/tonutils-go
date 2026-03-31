package cell

// All is kept for backwards compatibility with older examples.
func (d *Dictionary) All(skipPruned ...bool) []HashmapKV {
	items, err := d.LoadAll(skipPruned...)
	if err != nil {
		panic(err)
	}

	res := make([]HashmapKV, 0, len(items))
	for _, item := range items {
		res = append(res, HashmapKV{
			Key:   item.Key.MustToCell(),
			Value: item.Value.MustToCell(),
		})
	}
	return res
}
