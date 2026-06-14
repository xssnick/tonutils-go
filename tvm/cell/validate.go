package cell

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

func ordinaryLevelMask(refs []*Cell) LevelMask {
	mask := byte(0)
	for _, ref := range refs {
		mask |= ref.getLevelMask().Mask
	}
	return LevelMask{Mask: mask}
}

func validateLoadedCell(c *Cell) error {
	return validateCell(c, true)
}

func validateBoundaryCell(c *Cell) error {
	return validateCell(c, false)
}

func validateCell(c *Cell, loadRefs bool) error {
	if !c.IsSpecial() {
		return nil
	}

	if c.bitsSz < 8 {
		return fmt.Errorf("not enough data for a special cell")
	}

	switch typ := c.GetType(); typ {
	case PrunedCellType:
		if _, err := specialCellRefs(c, typ, loadRefs); err != nil {
			return err
		}
		if len(c.data) < 2 {
			return fmt.Errorf("not enough data for a pruned branch special cell")
		}
		levelMask := c.getLevelMask()
		if levelMask.Mask != c.data[1] {
			return fmt.Errorf("pruned branch level mask mismatch")
		}
		level := levelMask.GetLevel()
		if level == 0 && c.IsLazy() {
			expectedBits := (2 + hashSize + depthSize) * 8
			if int(c.bitsSz) != expectedBits {
				return fmt.Errorf("not enough data for a pruned branch special cell")
			}
			return nil
		}
		if level > _DataCellMaxLevel || level == 0 {
			return fmt.Errorf("pruned branch has an invalid level")
		}
		expectedBits := (2 + levelMask.Apply(level-1).getHashesCount()*(hashSize+depthSize)) * 8
		if int(c.bitsSz) != expectedBits {
			return fmt.Errorf("not enough data for a pruned branch special cell")
		}
	case LibraryCellType:
		if _, err := specialCellRefs(c, typ, loadRefs); err != nil {
			return err
		}
		if c.bitsSz != 8+256 {
			return fmt.Errorf("not enough data for a library special cell")
		}
	case MerkleProofCellType:
		if c.bitsSz != 8+(hashSize+depthSize)*8 {
			return fmt.Errorf("not enough data for a merkle proof special cell")
		}
		refs, err := specialCellRefs(c, typ, loadRefs)
		if err != nil {
			return err
		}
		ref := refs[0]
		if !bytes.Equal(c.data[1:1+hashSize], ref.getHash(0)) {
			return fmt.Errorf("hash mismatch in a merkle proof special cell")
		}
		if binary.BigEndian.Uint16(c.data[1+hashSize:1+hashSize+depthSize]) != ref.getDepth(0) {
			return fmt.Errorf("depth mismatch in a merkle proof special cell")
		}
		expectedMask := ref.getLevelMask().Mask >> 1
		if c.getLevelMask().Mask != expectedMask {
			return fmt.Errorf("merkle proof level mask mismatch")
		}
	case MerkleUpdateCellType:
		if c.bitsSz != 8+(hashSize+depthSize)*8*2 {
			return fmt.Errorf("not enough data for a merkle update special cell")
		}
		refs, err := specialCellRefs(c, typ, loadRefs)
		if err != nil {
			return err
		}
		left, right := refs[0], refs[1]
		if !bytes.Equal(c.data[1:1+hashSize], left.getHash(0)) {
			return fmt.Errorf("first hash mismatch in a merkle update special cell")
		}
		if !bytes.Equal(c.data[1+hashSize:1+hashSize*2], right.getHash(0)) {
			return fmt.Errorf("second hash mismatch in a merkle update special cell")
		}
		firstDepthOff := 1 + hashSize*2
		secondDepthOff := firstDepthOff + depthSize
		if binary.BigEndian.Uint16(c.data[firstDepthOff:firstDepthOff+depthSize]) != left.getDepth(0) {
			return fmt.Errorf("first depth mismatch in a merkle update special cell")
		}
		if binary.BigEndian.Uint16(c.data[secondDepthOff:secondDepthOff+depthSize]) != right.getDepth(0) {
			return fmt.Errorf("second depth mismatch in a merkle update special cell")
		}
		expectedMask := (left.getLevelMask().Mask | right.getLevelMask().Mask) >> 1
		if c.getLevelMask().Mask != expectedMask {
			return fmt.Errorf("merkle update level mask mismatch")
		}
	default:
		return fmt.Errorf("unknown special cell type")
	}

	return nil
}

func specialCellRefs(c *Cell, typ Type, loadRefs bool) ([2]*Cell, error) {
	var refs [2]*Cell
	refCnt := c.refsCount()

	switch typ {
	case PrunedCellType:
		if refCnt != 0 {
			return refs, fmt.Errorf("pruned branch special cell has a cell reference")
		}
	case LibraryCellType:
		if refCnt != 0 {
			return refs, fmt.Errorf("library special cell has a cell reference")
		}
	case MerkleProofCellType:
		if refCnt != 1 {
			return refs, fmt.Errorf("wrong references count for a merkle proof special cell")
		}
		refView := newCellRefView(c)
		ref, err := refView.boundaryRef(0)
		if err != nil {
			return refs, fmt.Errorf("failed to peek merkle proof ref: %w", err)
		}
		if loadRefs {
			ref, err = ref.load()
			if err != nil {
				return refs, fmt.Errorf("failed to load merkle proof ref: %w", err)
			}
		}
		refs[0] = ref
	case MerkleUpdateCellType:
		if refCnt != 2 {
			return refs, fmt.Errorf("wrong references count for a merkle update special cell")
		}
		refView := newCellRefView(c)
		left, err := refView.boundaryRef(0)
		if err != nil {
			return refs, fmt.Errorf("failed to peek merkle update first ref: %w", err)
		}
		right, err := refView.boundaryRef(1)
		if err != nil {
			return refs, fmt.Errorf("failed to peek merkle update second ref: %w", err)
		}
		if loadRefs {
			left, err = left.load()
			if err != nil {
				return refs, fmt.Errorf("failed to load merkle update first ref: %w", err)
			}
			right, err = right.load()
			if err != nil {
				return refs, fmt.Errorf("failed to load merkle update second ref: %w", err)
			}
		}
		refs[0], refs[1] = left, right
	default:
		return refs, fmt.Errorf("unknown special cell type")
	}
	return refs, nil
}
