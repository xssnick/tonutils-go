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
	if !c.isSpecial() {
		// TODO: not allow later?
		// Keep ordinary cells permissive on the read path. The reference parser rejects
		// non-canonical ordinary level masks, but tonutils-go historically accepted
		// legacy BOCs such as the dict fixture in dict_test.go with a mismatched root
		// level mask. Special cells stay strict because their descriptors affect proof
		// and hash semantics.
		return nil
	}

	if c.bitsSz < 8 {
		return fmt.Errorf("not enough data for a special cell")
	}

	switch typ := c.GetType(); typ {
	case PrunedCellType:
		if c.refsCount() != 0 {
			return fmt.Errorf("pruned branch special cell has a cell reference")
		}
		if len(c.data) < 2 {
			return fmt.Errorf("not enough data for a pruned branch special cell")
		}
		if c.getLevelMask().Mask != c.data[1] {
			return fmt.Errorf("pruned branch level mask mismatch")
		}
		level := c.getLevelMask().GetLevel()
		if level > _DataCellMaxLevel || level == 0 {
			return fmt.Errorf("pruned branch has an invalid level")
		}
		expectedBits := (2 + c.getLevelMask().Apply(level-1).getHashesCount()*(hashSize+depthSize)) * 8
		if int(c.bitsSz) != expectedBits {
			return fmt.Errorf("not enough data for a pruned branch special cell")
		}
	case LibraryCellType:
		if c.bitsSz != 8+256 {
			return fmt.Errorf("not enough data for a library special cell")
		}
	case MerkleProofCellType:
		if c.bitsSz != 8+(hashSize+depthSize)*8 {
			return fmt.Errorf("not enough data for a merkle proof special cell")
		}
		if c.refsCount() != 1 {
			return fmt.Errorf("wrong references count for a merkle proof special cell")
		}
		if !bytes.Equal(c.data[1:1+hashSize], c.ref(0).getHash(0)) {
			return fmt.Errorf("hash mismatch in a merkle proof special cell")
		}
		if binary.BigEndian.Uint16(c.data[1+hashSize:1+hashSize+depthSize]) != c.ref(0).getDepth(0) {
			return fmt.Errorf("depth mismatch in a merkle proof special cell")
		}
		expectedMask := c.ref(0).getLevelMask().Mask >> 1
		if c.getLevelMask().Mask != expectedMask {
			return fmt.Errorf("merkle proof level mask mismatch")
		}
	case MerkleUpdateCellType:
		if c.bitsSz != 8+(hashSize+depthSize)*8*2 {
			return fmt.Errorf("not enough data for a merkle update special cell")
		}
		if c.refsCount() != 2 {
			return fmt.Errorf("wrong references count for a merkle update special cell")
		}
		if !bytes.Equal(c.data[1:1+hashSize], c.ref(0).getHash(0)) {
			return fmt.Errorf("first hash mismatch in a merkle update special cell")
		}
		if !bytes.Equal(c.data[1+hashSize:1+hashSize*2], c.ref(1).getHash(0)) {
			return fmt.Errorf("second hash mismatch in a merkle update special cell")
		}
		firstDepthOff := 1 + hashSize*2
		secondDepthOff := firstDepthOff + depthSize
		if binary.BigEndian.Uint16(c.data[firstDepthOff:firstDepthOff+depthSize]) != c.ref(0).getDepth(0) {
			return fmt.Errorf("first depth mismatch in a merkle update special cell")
		}
		if binary.BigEndian.Uint16(c.data[secondDepthOff:secondDepthOff+depthSize]) != c.ref(1).getDepth(0) {
			return fmt.Errorf("second depth mismatch in a merkle update special cell")
		}
		expectedMask := (c.ref(0).getLevelMask().Mask | c.ref(1).getLevelMask().Mask) >> 1
		if c.getLevelMask().Mask != expectedMask {
			return fmt.Errorf("merkle update level mask mismatch")
		}
	default:
		return fmt.Errorf("unknown special cell type")
	}

	return nil
}
