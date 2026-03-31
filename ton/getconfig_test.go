package ton

import (
	"bytes"
	"context"
	"testing"

	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestUnwrapLibraryResultCell(t *testing.T) {
	library := cell.BeginCell().MustStoreUInt(0xFF00F4A4, 32).EndCell()
	wrapped := cell.BeginCell().MustStoreRef(library).EndCell()

	if got := unwrapLibraryResultCell(library, library.Hash()); got != library {
		t.Fatalf("expected direct library cell to be returned")
	}

	if got := unwrapLibraryResultCell(wrapped, library.Hash()); got != library {
		t.Fatalf("expected wrapped library cell to be unwrapped")
	}

	if got := unwrapLibraryResultCell(wrapped, wrapped.Hash()); got != wrapped {
		t.Fatalf("expected wrapper hash to still resolve to wrapper")
	}

	nonCanonical := cell.BeginCell().MustStoreUInt(1, 1).MustStoreRef(library).EndCell()
	if got := unwrapLibraryResultCell(nonCanonical, library.Hash()); got != nil {
		t.Fatalf("expected non-canonical wrapper not to be unwrapped")
	}
}

func TestAPIClient_GetLibraries_UnwrapsEmptyRootWrapper(t *testing.T) {
	library := cell.BeginCell().MustStoreUInt(0xFF00F4A413F4BCF2, 64).EndCell()
	wrapped := cell.BeginCell().MustStoreRef(library).EndCell()
	missing := bytes.Repeat([]byte{0xAB}, 32)

	mock := &ValidationMock{
		Response: LibraryResult{
			Result: []*LibraryEntry{
				{
					Hash: library.Hash(),
					Data: wrapped,
				},
			},
		},
		CheckQuery: func(payload tl.Serializable) error {
			req, ok := payload.(GetLibraries)
			if !ok {
				t.Fatalf("unexpected request type: %T", payload)
			}
			if len(req.LibraryList) != 2 {
				t.Fatalf("unexpected request len: %d", len(req.LibraryList))
			}
			if !bytes.Equal(req.LibraryList[0], library.Hash()) {
				t.Fatalf("unexpected first hash")
			}
			if !bytes.Equal(req.LibraryList[1], missing) {
				t.Fatalf("unexpected second hash")
			}
			return nil
		},
	}

	api := NewAPIClient(mock)
	got, err := api.GetLibraries(context.Background(), library.Hash(), missing)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 {
		t.Fatalf("unexpected response len: %d", len(got))
	}
	if got[0] == nil {
		t.Fatalf("expected wrapped library to be found")
	}
	if !bytes.Equal(got[0].Hash(), library.Hash()) {
		t.Fatalf("unexpected returned hash: %x", got[0].Hash())
	}
	if got[1] != nil {
		t.Fatalf("expected missing library to stay nil")
	}
}
