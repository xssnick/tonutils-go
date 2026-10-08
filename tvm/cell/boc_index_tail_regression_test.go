package cell

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"testing"
)

func indexedBOCWithDeclaredTail(payloadSize int, withCRC bool) []byte {
	if payloadSize < 2 || payloadSize > 0xffff {
		panic("invalid test payload size")
	}

	offsetBytes := 1
	if payloadSize > 0xff {
		offsetBytes = 2
	}
	boc := append([]byte(nil), bocMagic...)
	boc = append(boc,
		0x81, byte(offsetBytes), // indexed, one-byte cell indexes
		0x01, 0x01, 0x00, // one cell, one root, no absent cells
	)
	if offsetBytes == 1 {
		boc = append(boc, byte(payloadSize), 0x00, 0x02)
	} else {
		boc = binary.BigEndian.AppendUint16(boc, uint16(payloadSize))
		boc = append(boc, 0x00)
		boc = binary.BigEndian.AppendUint16(boc, 2)
	}
	boc = append(boc, 0x00, 0x00) // the indexed empty cell
	boc = append(boc, bytes.Repeat([]byte{0xff}, payloadSize-2)...)
	if !withCRC {
		return boc
	}

	boc[4] |= 1 << 6
	crc := crc32.Checksum(boc, castTable)
	var trailer [4]byte
	binary.LittleEndian.PutUint32(trailer[:], crc)
	return append(boc, trailer[:]...)
}

func indexedDeclaredTailParsers() []struct {
	name  string
	opts  BOCParseOptions
	parse func([]byte, BOCParseOptions) (*Cell, error)
} {
	return []struct {
		name  string
		opts  BOCParseOptions
		parse func([]byte, BOCParseOptions) (*Cell, error)
	}{
		{
			name: "eager",
			opts: BOCParseOptions{MaxCells: 1},
			parse: func(boc []byte, opts BOCParseOptions) (*Cell, error) {
				return FromBOCWithOptions(boc, opts)
			},
		},
		{
			name: "eager-stream",
			opts: BOCParseOptions{MaxCells: 1},
			parse: func(boc []byte, opts BOCParseOptions) (*Cell, error) {
				roots, _, err := FromBOCMultiRootReader(bytes.NewReader(boc), opts)
				if err != nil {
					return nil, err
				}
				return roots[0], nil
			},
		},
		{
			name: "no-copy",
			opts: BOCParseOptions{NoCopyPayload: true, MaxCells: 1},
			parse: func(boc []byte, opts BOCParseOptions) (*Cell, error) {
				return FromBOCWithOptions(boc, opts)
			},
		},
		{
			name: "lazy",
			opts: BOCParseOptions{Lazy: true, MaxCells: 1},
			parse: func(boc []byte, opts BOCParseOptions) (*Cell, error) {
				return FromBOCWithOptions(boc, opts)
			},
		},
		{
			name: "lazy-no-copy",
			opts: BOCParseOptions{Lazy: true, NoCopyPayload: true, MaxCells: 1},
			parse: func(boc []byte, opts BOCParseOptions) (*Cell, error) {
				return FromBOCWithOptions(boc, opts)
			},
		},
		{
			name: "lazy-stream",
			opts: BOCParseOptions{Lazy: true, MaxCells: 1},
			parse: func(boc []byte, opts BOCParseOptions) (*Cell, error) {
				roots, _, err := FromBOCMultiRootReader(bytes.NewReader(boc), opts)
				if err != nil {
					return nil, err
				}
				return roots[0], nil
			},
		},
	}
}

func TestBOCIndexedDeclaredPayloadTail(t *testing.T) {
	parsers := indexedDeclaredTailParsers()

	for _, payloadSize := range []int{3, maxBOCDeclaredPayloadBytesPerCell} {
		for _, withCRC := range []bool{false, true} {
			crcName := "without-crc"
			if withCRC {
				crcName = "with-crc"
			}
			t.Run(fmt.Sprintf("payload-%d/%s", payloadSize, crcName), func(t *testing.T) {
				boc := indexedBOCWithDeclaredTail(payloadSize, withCRC)
				for _, parser := range parsers {
					t.Run(parser.name, func(t *testing.T) {
						root, err := parser.parse(boc, parser.opts)
						if err != nil {
							t.Fatalf("parse indexed BoC with declared tail: %v", err)
						}
						if root.BitsSize() != 0 || root.RefsNum() != 0 {
							t.Fatalf("unexpected root: %d bits, %d refs", root.BitsSize(), root.RefsNum())
						}
					})
				}

				view, err := OpenBOCView(bytes.NewReader(boc), int64(len(boc)), BOCViewOptions{
					RequireIndex: true,
					ValidateCRC:  true,
				})
				if err != nil {
					t.Fatalf("open indexed BoC view with declared tail: %v", err)
				}
				root, err := view.NewReader().ReadCell(0)
				if err != nil {
					t.Fatalf("read indexed BoC view root: %v", err)
				}
				if root.Bits != 0 || root.Refs.Count != 0 {
					t.Fatalf("unexpected view root: %d bits, %d refs", root.Bits, root.Refs.Count)
				}
			})
		}
	}
}

func TestBOCIndexedDeclaredPayloadLimit(t *testing.T) {
	for _, withCRC := range []bool{false, true} {
		boc := indexedBOCWithDeclaredTail(maxBOCDeclaredPayloadBytesPerCell+1, withCRC)
		for _, parser := range indexedDeclaredTailParsers() {
			t.Run(fmt.Sprintf("crc-%t/%s", withCRC, parser.name), func(t *testing.T) {
				if _, err := parser.parse(boc, parser.opts); err == nil {
					t.Fatal("parser accepted declared payload above the per-cell limit")
				}
			})
		}
		if _, err := OpenBOCView(bytes.NewReader(boc), int64(len(boc)), BOCViewOptions{RequireIndex: true}); err == nil {
			t.Fatal("BoC view accepted declared payload above the per-cell limit")
		}
	}
}

func TestBOCRejectsTrailingNonIndexedInput(t *testing.T) {
	boc := BeginCell().EndCell().ToBOC()
	boc = append(boc, 0xff)
	if _, err := FromBOC(boc); err == nil {
		t.Fatal("expected trailing non-indexed input to be rejected")
	}
}

func TestFromBOCMultiRootAcceptsEmptyInput(t *testing.T) {
	roots, err := FromBOCMultiRoot(nil)
	if err != nil {
		t.Fatalf("parse empty multi-root BoC: %v", err)
	}
	if len(roots) != 0 {
		t.Fatalf("empty multi-root BoC returned %d roots", len(roots))
	}

	if _, err = FromBOC(nil); err == nil {
		t.Fatal("single-root parser accepted empty input")
	}

	for _, opts := range []BOCParseOptions{
		{},
		{Lazy: true},
		{NoCopyPayload: true},
		{Lazy: true, NoCopyPayload: true},
	} {
		for _, reader := range []struct {
			name string
			r    io.Reader
		}{
			{name: "bytes", r: bytes.NewReader(nil)},
			{name: "stream", r: io.LimitReader(bytes.NewReader(nil), 0)},
			{name: "no-copy", r: NewBOCNoCopyReader(nil)},
		} {
			t.Run(fmt.Sprintf("reader/%s/lazy-%t/no-copy-%t", reader.name, opts.Lazy, opts.NoCopyPayload), func(t *testing.T) {
				roots, unique, readErr := FromBOCMultiRootReader(reader.r, opts)
				if readErr != nil {
					t.Fatalf("parse empty multi-root reader: %v", readErr)
				}
				if len(roots) != 0 || len(unique) != 0 {
					t.Fatalf("empty reader returned %d roots and %d cells", len(roots), len(unique))
				}
			})
		}
	}

	for size := 1; size < len(bocMagic); size++ {
		if _, _, err = FromBOCMultiRootReader(bytes.NewReader(bocMagic[:size]), BOCParseOptions{}); err == nil {
			t.Fatalf("reader accepted %d-byte truncated magic", size)
		}
	}
}
