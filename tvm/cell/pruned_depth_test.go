package cell

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/bits"
	"testing"
)

func TestPrunedBranchStoredDepthLimit(t *testing.T) {
	for mask := byte(1); mask <= 7; mask++ {
		count := bits.OnesCount8(mask)
		for index := 0; index < count; index++ {
			for _, depth := range []uint16{0, 1024, 1025, 65535} {
				t.Run(fmt.Sprintf("mask%03b/slot%d/depth%d", mask, index, depth), func(t *testing.T) {
					payload := make([]byte, 2+count*(32+2))
					payload[0], payload[1] = byte(PrunedCellType), mask
					binary.BigEndian.PutUint16(payload[2+count*32+index*2:], depth)
					built, err := BeginCell().MustStoreSlice(payload, uint(len(payload)*8)).EndCellSpecial(true)
					if depth > 1024 {
						if err == nil {
							t.Fatal("builder accepted a stored depth above 1024")
						}
					} else if err != nil {
						t.Fatalf("builder rejected a valid stored depth: %v", err)
					}

					// Encode the payload directly so malformed cells also reach the
					// BoC parser without going through builder validation first.
					boc := []byte{0xb5, 0xee, 0x9c, 0x72, 1, 1, 1, 1, 0, byte(len(payload) + 2), 0,
						8 | mask<<5, byte(len(payload) * 2)}
					boc = append(boc, payload...)
					metadataCell := built
					if depth > 1024 {
						validPayload := bytes.Clone(payload)
						binary.BigEndian.PutUint16(validPayload[2+count*32+index*2:], 0)
						metadataCell, err = BeginCell().MustStoreSlice(validPayload, uint(len(validPayload)*8)).EndCellSpecial(true)
						if err != nil {
							t.Fatal(err)
						}
					}
					withHashes, err := metadataCell.ToBOCWithOptionsErr(BOCSerializeOptions{WithTopHash: true})
					if err != nil {
						t.Fatal(err)
					}
					// The single cell's payload ends this CRC-free BoC. Retain valid
					// cached metadata while corrupting only the embedded depth.
					copy(withHashes[len(withHashes)-len(payload):], payload)
					for _, mode := range []struct {
						name string
						opts BOCParseOptions
					}{
						{name: "eager"},
						{name: "lazy", opts: BOCParseOptions{Lazy: true}},
						{name: "trusted", opts: BOCParseOptions{TrustedHashes: true}},
						{name: "lazy_trusted", opts: BOCParseOptions{Lazy: true, TrustedHashes: true}},
					} {
						t.Run(mode.name, func(t *testing.T) {
							mode.opts.AllowNonZeroLevelRoot = true
							input := boc
							if mode.opts.TrustedHashes {
								input = withHashes
							}
							parsed, err := FromBOCWithOptions(input, mode.opts)
							if depth > 1024 {
								if err == nil {
									t.Fatal("BoC parser accepted a stored depth above 1024")
								}
								return
							}
							if err != nil {
								t.Fatalf("parse valid pruned branch: %v", err)
							}
							for level := 0; level <= 3; level++ {
								if parsed.Depth(level) != built.Depth(level) || !bytes.Equal(parsed.Hash(level), built.Hash(level)) {
									t.Fatalf("level %d metadata differs from the built cell", level)
								}
							}
						})
					}
				})
			}
		}
	}
}
