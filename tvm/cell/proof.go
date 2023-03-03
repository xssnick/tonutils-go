package cell

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

type cellHash = []byte

func (c *Cell) CreateProof(parts [][]byte) (*Cell, error) {
	proofBody := c.copy() // TODO: optimize
	hasParts, err := proofBody.toProof(parts)
	if err != nil {
		return nil, fmt.Errorf("failed to build proof for cell: %w", err)
	}

	if len(hasParts) != len(parts) {
		return nil, fmt.Errorf("given cell not contains all parts to proof")
	}

	// we unwrap level by 1 to correctly calc proofs on pruned cells
	calcHash, err := proofBody.proofHash(1)
	if err != nil {
		return nil, err
	}
	depth, err := proofBody.proofMaxDepth(0, 1)
	if err != nil {
		return nil, err
	}

	// build root Merkle Proof cell
	data := make([]byte, 1+32+2)
	data[0] = _MerkleProofType
	copy(data[1:], calcHash)
	binary.BigEndian.PutUint16(data[1+32:], depth)

	proof := &Cell{
		special: true,
		level:   0,
		bitsSz:  8 + 256 + 16,
		data:    data,
		refs:    []*Cell{proofBody},
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
			if ref.level >= 3 {
				return nil, fmt.Errorf("child level is to big to prune")
			}

			prunedData := make([]byte, 2+(ref.level+1)*(32+2))
			prunedData[0] = _PrunedType
			prunedData[1] = ref.level + 1

			for lvl := byte(0); lvl <= ref.level; lvl++ {
				hash, err := ref.proofHash(lvl)
				if err != nil {
					return nil, fmt.Errorf("failed to hash child proof: %w", err)
				}

				depth, err := ref.proofMaxDepth(0, lvl)
				if err != nil {
					return nil, fmt.Errorf("failed to hash child proof: %w", err)
				}

				copy(prunedData[2+(lvl*32):], hash)
				binary.BigEndian.PutUint16(prunedData[2+((lvl+1)*32)+2*lvl:], depth)
			}

			c.refs[toPruneIdx[i]] = &Cell{
				special: true,
				level:   ref.level + 1,
				bitsSz:  uint(len(prunedData) * 8),
				data:    prunedData,
			}
		}
	}

	typ := c.getType()
	for _, ref := range c.refs {
		if ref.level > c.level {
			if typ == _MerkleProofType {
				// proof should be 1 level less than child
				c.level = ref.level - 1
			} else {
				c.level = ref.level
			}
		}
	}

	return hasPartsRefs, nil
}

func CheckProof(proof *Cell, hash []byte) error {
	if !proof.special || proof.RefsNum() != 1 || proof.BitsSize() != 280 ||
		proof.data[0] != _MerkleProofType {
		return fmt.Errorf("not a merkle proof cell")
	}

	needLvl := proof.refs[0].level
	if needLvl > 0 {
		needLvl -= 1
	}

	if needLvl != proof.level {
		return fmt.Errorf("incorrect level of child")
	}
	if !bytes.Equal(hash, proof.data[1:33]) {
		return fmt.Errorf("incorrect proof hash")
	}

	// we unwrap level by 1 to correctly check proofs on pruned cells
	calcHash, err := proof.refs[0].proofHash(1)
	if err != nil {
		return err
	}

	if !bytes.Equal(hash, calcHash) {
		return fmt.Errorf("incorrect proof")
	}
	return nil
}

func (c *Cell) proofHash(unwrapLevel byte) ([]byte, error) {
	if unwrapLevel > 3 {
		return nil, fmt.Errorf("too big unwrap level")
	}

	switch c.getType() {
	case _PrunedType:
		if unwrapLevel > 0 {
			if unwrapLevel > c.level {
				return nil, fmt.Errorf("too big unwrap level for pruned cell")
			}
			return c.data[2 : 2+(32*unwrapLevel)], nil
		}
		// in case of 0 unwrap we calc hash in the same way as ordinary
		fallthrough
	default:
		body := c.BeginParse().MustLoadSlice(c.bitsSz)

		data := make([]byte, 2+len(body)+len(c.refs)*(2+32))
		data[0], data[1] = c.descriptors(unwrapLevel)
		copy(data[2:], body)
		offset := 2 + len(body)

		unusedBits := 8 - (c.bitsSz % 8)
		if unusedBits != 8 {
			// we need to set bit at the end if not whole byte was used
			data[offset-1] += 1 << (unusedBits - 1)
		}

		for i, ref := range c.refs {
			depth, err := ref.proofMaxDepth(0, unwrapLevel)
			if err != nil {
				return nil, err
			}
			hashProof, err := ref.proofHash(unwrapLevel)
			if err != nil {
				return nil, err
			}
			binary.BigEndian.PutUint16(data[offset+(2*i):], depth)
			copy(data[offset+(2*len(c.refs))+(32*i):], hashProof)
		}

		hash := sha256.New()
		hash.Write(data)
		return hash.Sum(nil), nil
	}
}

func (c *Cell) proofMaxDepth(start uint16, unwrapLevel byte) (uint16, error) {
	d := start
	switch c.getType() {
	case _PrunedType:
		if unwrapLevel > 0 {
			if unwrapLevel > c.level {
				return 0, fmt.Errorf("too big unwrap level for pruned cell")
			}

			depthOffset := 2 + (32 * (unwrapLevel)) + 2*(unwrapLevel-1)
			x := start + binary.BigEndian.Uint16(c.data[depthOffset:])
			if x > d {
				d = x
			}
			return d, nil
		}
		fallthrough
	default:
		for _, cc := range c.refs {
			x, err := cc.proofMaxDepth(start+1, unwrapLevel)
			if err != nil {
				return 0, err
			}

			if x > d {
				d = x
			}
		}
	}
	return d, nil
}
