package cell

func (b *Builder) CanExtendBy(bits uint, refs uint) bool {
	return b.bitsSz+bits < 1024 && len(b.refs)+int(refs) <= 4
}

func (b *Builder) Depth() uint16 {
	var depth uint16
	for _, ref := range b.refs {
		childDepth := ref.Depth() + 1
		if childDepth > depth {
			depth = childDepth
		}
	}
	return depth
}

func (b *Builder) EndCellSpecial(special bool) (*Cell, error) {
	cl, err := finalizeCellFromBuilder(b, special)
	if err != nil {
		return nil, err
	}
	if b.observer != nil {
		b.observer.OnCellCreate()
	}
	return cl, nil
}

func (b *Builder) StoreSameBit(bit bool, bits uint) error {
	if !b.CanExtendBy(bits, 0) {
		return ErrNotFit1023
	}

	for bits >= 64 {
		if bit {
			if err := b.StoreUInt(^uint64(0), 64); err != nil {
				return err
			}
		} else {
			if err := b.StoreUInt(0, 64); err != nil {
				return err
			}
		}
		bits -= 64
	}

	if bits == 0 {
		return nil
	}

	if !bit {
		return b.StoreUInt(0, bits)
	}
	if bits == 64 {
		return b.StoreUInt(^uint64(0), bits)
	}
	return b.StoreUInt((uint64(1)<<bits)-1, bits)
}
