package cell

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

type cellHash = []byte

func (c *Cell) CreateProof(forHashes [][]byte) (*Cell, error) {
	proofBody := c.copy()
	hasParts, err := proofBody.toProof(forHashes)
	if err != nil {
		return nil, fmt.Errorf("failed to build proof for cell: %w", err)
	}

	if len(hasParts) != len(forHashes) {
		return nil, fmt.Errorf("given cell not contains all parts to proof")
	}

	// build root Merkle Proof cell
	data := make([]byte, 1+32+2)
	data[0] = byte(MerkleProofCellType)
	copy(data[1:], c.getHash(c.levelMask.GetLevel()))
	binary.BigEndian.PutUint16(data[1+32:], c.getDepth(c.levelMask.GetLevel()))

	proof := &Cell{
		special:   true,
		levelMask: c.levelMask,
		bitsSz:    8 + 256 + 16,
		data:      data,
		refs:      []*Cell{proofBody},
	}

	return proof, nil
}

func (c *Cell) toProof(parts []cellHash) ([]cellHash, error) {
	for _, part := range parts {
		if bytes.Equal(c.Hash(), part) {
			// for this cell we need a proof
			return []cellHash{part}, nil
		}
	}
	if len(c.refs) == 0 {
		return nil, nil
	}

	var toPruneIdx [4]byte
	var toPruneRefs = make([]*Cell, 0, len(c.refs))
	var hasPartsRefs []cellHash
	for i, ref := range c.refs {
		hasParts, err := ref.toProof(parts)
		if err != nil {
			return nil, err
		}

		if len(hasParts) > 0 {
			// add hash to final list if it is not there yet
		partsIter:
			for _, part := range hasParts {
				for _, hPart := range hasPartsRefs {
					if bytes.Equal(part, hPart) {
						continue partsIter
					}
				}
				hasPartsRefs = append(hasPartsRefs, part)
			}
		} else if len(ref.refs) > 0 { // we prune only if cell has refs
			toPruneIdx[len(toPruneRefs)] = byte(i)
			toPruneRefs = append(toPruneRefs, ref)
		}
	}

	if len(hasPartsRefs) > 0 && len(toPruneRefs) > 0 {
		// contains some useful and unuseful refs, pune unuseful
		for i, ref := range toPruneRefs {
			if ref.levelMask.GetLevel() >= 3 {
				return nil, fmt.Errorf("child level is to big to prune")
			}

			ourLvl := ref.levelMask.GetLevel()

			prunedData := make([]byte, 2+(ourLvl+1)*(32+2))
			prunedData[0] = byte(PrunedCellType)
			prunedData[1] = byte(ref.levelMask.GetLevel()) + 1

			for lvl := 0; lvl <= ourLvl; lvl++ {
				copy(prunedData[2+(lvl*32):], ref.getHash(lvl))
				binary.BigEndian.PutUint16(prunedData[2+((lvl+1)*32)+2*lvl:], ref.getDepth(lvl))
			}

			c.refs[toPruneIdx[i]] = &Cell{
				special:   true,
				levelMask: LevelMask{ref.levelMask.Mask + 1},
				bitsSz:    uint(len(prunedData) * 8),
				data:      prunedData,
			}
		}
	}

	typ := c.GetType()
	for _, ref := range c.refs {
		if ref.levelMask.GetLevel() > c.levelMask.GetLevel() {
			if typ == MerkleProofCellType {
				// proof should be 1 level less than child
				c.levelMask = LevelMask{ref.levelMask.Mask - 1}
			} else {
				c.levelMask = ref.levelMask
			}
		}
	}

	return hasPartsRefs, nil
}

func CheckProof(proof *Cell, hash []byte) error {
	_, err := UnwrapProof(proof, hash)
	return err
}

func UnwrapProof(proof *Cell, hash []byte) (*Cell, error) {
	if !proof.special || proof.RefsNum() != 1 || proof.BitsSize() != 280 ||
		Type(proof.data[0]) != MerkleProofCellType {
		return nil, fmt.Errorf("not a merkle proof cell")
	}

	needLvl := proof.refs[0].levelMask.GetLevel()
	if needLvl > 0 {
		needLvl -= 1
	}

	if needLvl != proof.levelMask.GetLevel() {
		return nil, fmt.Errorf("incorrect level of proof")
	}
	if !bytes.Equal(hash, proof.data[1:33]) {
		return nil, fmt.Errorf("incorrect proof hash")
	}

	// we unwrap level by 1 to correctly check proofs on pruned cells
	calcHash := proof.refs[0].getHash(needLvl)
	if !bytes.Equal(hash, calcHash) {
		return nil, fmt.Errorf("incorrect proof")
	}
	return proof.refs[0], nil
}

func (c *Cell) getLevelMask() LevelMask {
	return c.levelMask
}

func (c *Cell) getHash(level int) []byte {
	hashIndex := c.getLevelMask().Apply(level).getHashIndex()

	if c.GetType() == PrunedCellType {
		prunedHashIndex := c.getLevelMask().getHashIndex()
		if hashIndex != prunedHashIndex {
			// return hash from data
			return c.data[2+(hashIndex*32) : 2+((hashIndex+1)*32)]
		}
		hashIndex = 0
	}

	// lazy hash calc
	if len(c.hashes) <= hashIndex*32 {
		c.calculateHashes()
	}

	return c.hashes[hashIndex*32 : (hashIndex+1)*32]
}

func (c *Cell) calculateHashes() {
	totalHashCount := c.levelMask.getHashIndex() + 1
	c.hashes = make([]byte, 32*totalHashCount)
	c.depthLevels = make([]uint16, totalHashCount)

	hashCount := totalHashCount
	typ := c.GetType()
	if typ == PrunedCellType {
		hashCount = 1
	}

	hashIndexOffset := totalHashCount - hashCount
	hashIndex := 0
	level := c.levelMask.GetLevel()
	for levelIndex := 0; levelIndex <= level; levelIndex++ {
		if !c.levelMask.IsSignificant(levelIndex) {
			continue
		}

		func() {
			defer func() {
				hashIndex++
			}()

			if levelIndex < hashIndexOffset {
				return
			}

			dsc := make([]byte, 2)
			dsc[0], dsc[1] = c.descriptors(c.levelMask.Apply(levelIndex))

			hash := sha256.New()
			hash.Write(dsc)

			if hashIndex == hashIndexOffset {
				if levelIndex != 0 && typ != PrunedCellType {
					// should never happen
					panic("not pruned or 0")
				}

				data := c.BeginParse().MustLoadSlice(c.bitsSz)
				unusedBits := 8 - (c.bitsSz % 8)
				if unusedBits != 8 {
					// we need to set bit at the end if not whole byte was used
					data[len(data)-1] += 1 << (unusedBits - 1)
				}
				hash.Write(data)
			} else {
				if levelIndex == 0 || typ == PrunedCellType {
					// should never happen
					panic("pruned or 0")
				}
				off := hashIndex - hashIndexOffset - 1
				hash.Write(c.hashes[off*32 : (off+1)*32])
			}

			var depth uint16
			for i := 0; i < len(c.refs); i++ {
				var childDepth uint16
				if typ == MerkleProofCellType || typ == MerkleUpdateCellType {
					childDepth = c.refs[i].getDepth(levelIndex + 1)
				} else {
					childDepth = c.refs[i].getDepth(levelIndex)
				}

				depthBytes := make([]byte, 2)
				binary.BigEndian.PutUint16(depthBytes, childDepth)
				hash.Write(depthBytes)

				if childDepth > depth {
					depth = childDepth
				}
			}
			if len(c.refs) > 0 {
				depth++
				if depth >= maxDepth {
					panic("depth is more than max depth")
				}
			}

			for i := 0; i < len(c.refs); i++ {
				if typ == MerkleProofCellType || typ == MerkleUpdateCellType {
					hash.Write(c.refs[i].getHash(levelIndex + 1))
				} else {
					hash.Write(c.refs[i].getHash(levelIndex))
				}
			}
			off := hashIndex - hashIndexOffset
			c.depthLevels[off] = depth
			copy(c.hashes[off*32:(off+1)*32], hash.Sum(nil))
		}()
	}
}

func (c *Cell) getDepth(level int) uint16 {
	hashIndex := c.getLevelMask().Apply(level).getHashIndex()
	if c.GetType() == PrunedCellType {
		prunedHashIndex := c.getLevelMask().getHashIndex()
		if hashIndex != prunedHashIndex {
			// return depth from data
			off := 2 + 32*prunedHashIndex + hashIndex*2
			return binary.BigEndian.Uint16(c.data[off : off+2])
		}
		hashIndex = 0
	}

	// lazy hash calc
	if len(c.depthLevels) <= hashIndex {
		c.calculateHashes()
	}

	return c.depthLevels[hashIndex]
}
