package cell

import "testing"

func FuzzFromBOCMultiRoot(f *testing.F) {
	valid := BeginCell().
		MustStoreUInt(0x99, 8).
		MustStoreRef(BeginCell().MustStoreUInt(0xABCD, 16).EndCell()).
		EndCell()

	seeds := [][]byte{
		nil,
		{},
		valid.ToBOC(),
		valid.ToBOCWithOptions(BOCOptions{WithIndex: true}),
		valid.ToBOCWithOptions(BOCOptions{
			WithCRC32C:    true,
			WithIndex:     true,
			WithCacheBits: true,
			WithTopHash:   true,
			WithIntHashes: true,
		}),
		{
			0xB5, 0xEE, 0x9C, 0x72,
			0x81, 0x01,
			0x02, 0x01, 0x00, 0x06,
			0x00,
			0x03, 0x06,
			0x01, 0x00, 0x01,
			0x01, 0x00, 0x00,
		},
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		roots, err := FromBOCMultiRoot(data)
		if err != nil {
			return
		}
		if len(roots) == 0 {
			t.Fatal("expected parsed boc to contain at least one root")
		}

		reboc := ToBOCWithOptions(roots, BOCOptions{
			WithCRC32C:    true,
			WithIndex:     true,
			WithCacheBits: true,
			WithTopHash:   true,
			WithIntHashes: true,
		})
		if len(reboc) == 0 {
			t.Fatal("expected parsed roots to serialize back into a boc")
		}

		reparsed, err := FromBOCMultiRoot(reboc)
		if err != nil {
			t.Fatalf("failed to parse reserialized boc: %v", err)
		}

		if len(reparsed) == 0 {
			t.Fatal("expected reparsed canonical boc to contain at least one root")
		}
	})
}
