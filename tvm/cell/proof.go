package cell

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

type cellHash = []byte

type ProofSkeleton struct {
	recursive bool
	branches  [4]*ProofSkeleton
}

func CreateProofSkeleton() *ProofSkeleton {
	return &ProofSkeleton{}
}

// ProofRef - include ref with index i to proof
func (s *ProofSkeleton) ProofRef(i int) *ProofSkeleton {
	if s.branches[i] == nil {
		s.branches[i] = &ProofSkeleton{}
	}
	return s.branches[i]
}

// SetRecursive - include all underlying refs recursively in ordinary form to proof
func (s *ProofSkeleton) SetRecursive() {
	s.recursive = true
}

// AttachAt - attach skeleton chain at specific ref slot
func (s *ProofSkeleton) AttachAt(i int, sk *ProofSkeleton) {
	s.branches[i] = sk
}

// Merge - merge 2 proof chains in a single proof tree
func (s *ProofSkeleton) Merge(sk *ProofSkeleton) {
	for i, v := range sk.branches {
		if v == nil {
			continue
		}

		if s.branches[i] == nil {
			s.branches[i] = v
			continue
		}

		if v.recursive {
			s.branches[i].SetRecursive()
			continue
		}
		s.branches[i].Merge(v)
	}
}

func (s *ProofSkeleton) Copy() *ProofSkeleton {
	return &ProofSkeleton{
		recursive: s.recursive,
		branches:  s.branches,
	}
}

func (c *Cell) CreateProof(skeleton *ProofSkeleton) (*Cell, error) {
	body, err := toProof(c, skeleton)
	if err != nil {
		return nil, fmt.Errorf("failed to build proof for cell: %w", err)
	}

	// build root Merkle Proof cell
	data := make([]byte, 1+32+2)
	data[0] = byte(MerkleProofCellType)
	copy(data[1:], body.getHash(0))
	binary.BigEndian.PutUint16(data[1+32:], body.getDepth(0))

	proof := &Cell{
		special:   true,
		levelMask: LevelMask{body.levelMask.Mask},
		bitsSz:    8 + 256 + 16,
		data:      data,
		refs:      []*Cell{body},
	}
	if proof.levelMask.Mask > 0 {
		proof.levelMask.Mask -= 1
	}
	proof.calculateHashes()

	return proof, nil
}

func toProof(c *Cell, skeleton *ProofSkeleton) (*Cell, error) {
	if skeleton.recursive {
		return c, nil
	}

	cLvl := c.levelMask.Mask
	c = c.copy()
	for i := 0; i < len(skeleton.branches); i++ {
		if skeleton.branches[i] != nil { // dive into branch
			r, err := c.PeekRef(i)
			if err != nil {
				return nil, fmt.Errorf("failed to peek %d ref: %w", i, err)
			}

			r, err = toProof(r, skeleton.branches[i])
			if err != nil {
				return nil, fmt.Errorf("failed to proof %d ref: %w", i, err)
			}
			c.refs[i] = r

			cLvl |= r.levelMask.Mask
		} else if len(c.refs) > i && len(c.refs[i].refs) > 0 { // prune branch
			r, err := c.PeekRef(i)
			if err != nil {
				return nil, fmt.Errorf("failed to peek %d ref: %w", i, err)
			}

			parentLvl := c.levelMask.GetLevel()
			ourLvl := r.levelMask.GetLevel()
			if parentLvl >= 3 || ourLvl >= 3 {
				return nil, fmt.Errorf("level is to big to prune")
			}

			prunedData := make([]byte, 2+(ourLvl+1)*(32+2))
			prunedData[0] = byte(PrunedCellType)
			prunedData[1] = r.levelMask.Mask | (1 << parentLvl)

			for lvl := 0; lvl <= ourLvl; lvl++ {
				copy(prunedData[2+(lvl*32):], r.getHash(lvl))
				binary.BigEndian.PutUint16(prunedData[2+((ourLvl+1)*32)+2*lvl:], r.getDepth(lvl))
			}

			r = &Cell{
				special:   true,
				levelMask: LevelMask{prunedData[1]},
				bitsSz:    uint(len(prunedData) * 8),
				data:      prunedData,
			}
			r.calculateHashes()
			c.refs[i] = r

			cLvl |= r.levelMask.Mask
		}
	}

	if c.special && c.GetType() == MerkleProofCellType {
		// unset merkle level bit
		m := LevelMask{cLvl}
		mask := byte(^(1 << m.GetLevel()))
		c.levelMask = LevelMask{m.Mask & mask}
	} else {
		c.levelMask = LevelMask{cLvl}
	}
	c.calculateHashes()

	return c, nil
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

	if !bytes.Equal(hash, proof.data[1:33]) {
		return nil, fmt.Errorf("incorrect proof hash")
	}

	calcDepth := proof.refs[0].getDepth(0)
	if calcDepth != binary.BigEndian.Uint16(proof.data[33:]) {
		return nil, fmt.Errorf("incorrect proof depth")
	}

	// we unwrap level by 1 to correctly check proofs on pruned cells
	calcHash := proof.refs[0].getHash(0)
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

	return c.hashes[hashIndex*32 : (hashIndex+1)*32]
}

// calculateHashes - we are precalculating cell hashes during creation for safe read parallel access later
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

	return c.depthLevels[hashIndex]
}
