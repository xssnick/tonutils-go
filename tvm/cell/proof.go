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
	body, err := buildProofBody(c, skeleton, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to build proof for cell: %w", err)
	}
	return createMerkleProofCell(body)
}

func buildProofBody(c *Cell, skeleton *ProofSkeleton, merkleDepth int) (*Cell, error) {
	if c == nil {
		return nil, fmt.Errorf("cell is nil")
	}
	if skeleton == nil {
		skeleton = CreateProofSkeleton()
	}
	if skeleton.recursive && !c.IsVirtualized() {
		return c, nil
	}

	refCnt := c.refsCount()
	if !skeleton.recursive {
		for i := refCnt; i < len(skeleton.branches); i++ {
			if skeleton.branches[i] == nil {
				continue
			}
			return nil, fmt.Errorf("failed to peek %d ref: %w", i, ErrNoMoreRefs)
		}
	}
	if refCnt == 0 {
		return c, nil
	}

	var refsBuf [4]*Cell
	refs := refsBuf[:refCnt]
	childDepth := merkleChildDepth(c, merkleDepth)
	for i := 0; i < refCnt; i++ {
		ref := c.visibleRef(i)

		var (
			next *Cell
			err  error
		)
		if skeleton.recursive {
			next, err = buildProofBody(ref, skeleton, childDepth)
			if err != nil {
				return nil, fmt.Errorf("failed to proof %d ref: %w", i, err)
			}
		} else if skeleton.branches[i] != nil {
			next, err = buildProofBody(ref, skeleton.branches[i], childDepth)
			if err != nil {
				return nil, fmt.Errorf("failed to proof %d ref: %w", i, err)
			}
		} else {
			if !ref.IsVirtualized() && ref.refsCount() == 0 {
				next = ref
			} else {
				next, err = createPrunedBranchFromCell(ref, childDepth+1)
				if err != nil {
					return nil, fmt.Errorf("failed to proof %d ref: %w", i, err)
				}
			}
		}
		refs[i] = next
	}

	rebuilt, err := rebuildCellWithRefs(c, refs)
	if err != nil {
		return nil, err
	}
	return rebuilt, nil
}

func createMerkleProofCell(body *Cell) (*Cell, error) {
	builder := BeginCell()
	if err := builder.StoreUInt(uint64(MerkleProofCellType), 8); err != nil {
		return nil, fmt.Errorf("failed to store merkle proof type: %w", err)
	}
	if err := builder.StoreSlice(body.getHash(0), hashSize*8); err != nil {
		return nil, fmt.Errorf("failed to store merkle proof hash: %w", err)
	}
	if err := builder.StoreUInt(uint64(body.getDepth(0)), depthSize*8); err != nil {
		return nil, fmt.Errorf("failed to store merkle proof depth: %w", err)
	}
	if err := builder.StoreRef(body); err != nil {
		return nil, fmt.Errorf("failed to store merkle proof ref: %w", err)
	}
	return finalizeCellFromBuilder(builder, true)
}

func CheckProof(proof *Cell, hash []byte) error {
	_, err := UnwrapProof(proof, hash)
	return err
}

func CheckProofVirtualized(proof *Cell, hash []byte) error {
	_, err := UnwrapProofVirtualized(proof, hash)
	return err
}

func UnwrapProof(proof *Cell, hash []byte) (*Cell, error) {
	if !proof.isSpecial() || proof.RefsNum() != 1 || proof.BitsSize() != 280 ||
		Type(proof.data[0]) != MerkleProofCellType {
		return nil, fmt.Errorf("not a merkle proof cell")
	}

	if !bytes.Equal(hash, proof.data[1:33]) {
		return nil, fmt.Errorf("incorrect proof hash")
	}

	body := proof.ref(0)
	calcDepth := body.getDepth(0)
	if calcDepth != binary.BigEndian.Uint16(proof.data[33:]) {
		return nil, fmt.Errorf("incorrect proof depth")
	}

	// we unwrap level by 1 to correctly check proofs on pruned cells
	calcHash := body.getHash(0)
	if !bytes.Equal(hash, calcHash) {
		return nil, fmt.Errorf("incorrect proof")
	}
	return body, nil
}

func UnwrapProofVirtualized(proof *Cell, hash []byte) (*Cell, error) {
	body, err := UnwrapProof(proof, hash)
	if err != nil {
		return nil, err
	}
	return body.Virtualize(0), nil
}

func (c *Cell) getHash(level int) []byte {
	if base := c.baseCell(); base != nil {
		return base.getHash(min(level, int(c.effectiveLevelValue())))
	}

	hashIndex := c.getLevelMask().Apply(level).getHashIndex()

	if c.GetType() == PrunedCellType {
		prunedHashIndex := c.getLevelMask().getHashIndex()
		if hashIndex != prunedHashIndex {
			// return hash from data
			return c.data[2+(hashIndex*32) : 2+((hashIndex+1)*32)]
		}
		hashIndex = 0
	}

	return c.hashAt(hashIndex)
}

func (c *Cell) calculateHashesSafe() (err error) {
	defer func() {
		if r := recover(); r != nil {
			switch v := r.(type) {
			case string:
				if v == "depth is more than max depth" {
					err = ErrCellDepthLimit
					return
				}
			case error:
				if v.Error() == "depth is more than max depth" {
					err = ErrCellDepthLimit
					return
				}
			}
			panic(r)
		}
	}()

	c.calculateHashes()
	return nil
}

// calculateHashes - we are precalculating cell hashes during creation for safe read parallel access later
func (c *Cell) calculateHashes() {
	c.clearVirtualization()

	totalHashCount := c.getLevelMask().getHashIndex() + 1
	if totalHashCount <= 1 {
		c.clearExtraHashes()
	} else {
		meta := c.ensureMeta()
		if meta.extraHashes == nil {
			meta.extraHashes = new([3]Hash)
		}
		meta.extraDepths = [3]uint16{}
	}

	hashCount := totalHashCount
	typ := c.GetType()
	if typ == PrunedCellType {
		hashCount = 1
	}

	hashIndexOffset := totalHashCount - hashCount
	hashIndex := 0
	level := c.getLevelMask().GetLevel()
	isMerkle := typ == MerkleProofCellType || typ == MerkleUpdateCellType
	var bodyBuf [maxCellDataBytes]byte
	var hashBuf [2 + maxCellDataBytes + (4 * depthSize) + (4 * hashSize)]byte

	for levelIndex := 0; levelIndex <= level; levelIndex++ {
		if !c.getLevelMask().IsSignificant(levelIndex) {
			continue
		}

		if levelIndex < hashIndexOffset {
			hashIndex++
			continue
		}

		dsc1, dsc2 := c.descriptors(c.getLevelMask().Apply(levelIndex))
		hashBuf[0], hashBuf[1] = dsc1, dsc2
		bufPos := 2

		if hashIndex == hashIndexOffset {
			if levelIndex != 0 && typ != PrunedCellType {
				// should never happen
				panic("not pruned or 0")
			}

			if c.bitsSz%8 == 0 {
				bufPos += copy(hashBuf[bufPos:], c.data)
			} else {
				bodySize := c.serializeBOCBodyTo(bodyBuf[:])
				bufPos += copy(hashBuf[bufPos:], bodyBuf[:bodySize])
			}
		} else {
			if levelIndex == 0 || typ == PrunedCellType {
				// should never happen
				panic("pruned or 0")
			}
			prevHashOff := hashIndex - hashIndexOffset - 1
			bufPos += copy(hashBuf[bufPos:], c.hashAt(prevHashOff))
		}

		childLevelIndex := levelIndex
		if isMerkle {
			childLevelIndex++
		}

		var depth uint16
		refCnt := c.refsCount()
		for i := 0; i < refCnt; i++ {
			childDepth := c.refs[i].getDepth(childLevelIndex)
			binary.BigEndian.PutUint16(hashBuf[bufPos:bufPos+depthSize], childDepth)
			bufPos += depthSize

			if childDepth > depth {
				depth = childDepth
			}
		}
		if refCnt > 0 {
			depth++
			if depth >= maxDepth {
				panic("depth is more than max depth")
			}
		}

		for i := 0; i < refCnt; i++ {
			bufPos += copy(hashBuf[bufPos:], c.refs[i].getHash(childLevelIndex))
		}

		off := hashIndex - hashIndexOffset
		c.setDepthAt(off, depth)
		sum := sha256.Sum256(hashBuf[:bufPos])
		c.setHashAt(off, sum[:])
		hashIndex++
	}
}

func (c *Cell) getDepth(level int) uint16 {
	if base := c.baseCell(); base != nil {
		return base.getDepth(min(level, int(c.effectiveLevelValue())))
	}

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

	return c.depthAt(hashIndex)
}
