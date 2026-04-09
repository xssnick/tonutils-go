package cell

import "testing"

func TestValidateLoadedCellSpecialVariants(t *testing.T) {
	t.Run("OrdinaryCellsStayPermissive", func(t *testing.T) {
		raw := FromRawUnsafe(RawUnsafeCell{
			IsSpecial: false,
			LevelMask: LevelMask{Mask: 7},
			BitsSz:    1,
			Data:      []byte{0x80},
		})
		if err := validateLoadedCell(raw); err != nil {
			t.Fatalf("ordinary cells should stay permissive, got %v", err)
		}
	})

	t.Run("LibraryAndShortErrors", func(t *testing.T) {
		short := FromRawUnsafe(RawUnsafeCell{
			IsSpecial: true,
			BitsSz:    0,
			Data:      nil,
		})
		if err := validateLoadedCell(short); err == nil {
			t.Fatal("short special cell should fail validation")
		}

		lib := BeginCell().MustStoreUInt(uint64(LibraryCellType), 8).MustStoreSlice(make([]byte, 32), 256)
		libCell, err := lib.EndCellSpecial(true)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateLoadedCell(libCell); err != nil {
			t.Fatalf("valid library cell should pass validation: %v", err)
		}

		badLib := FromRawUnsafe(RawUnsafeCell{
			IsSpecial: true,
			BitsSz:    8,
			Data:      []byte{byte(LibraryCellType)},
		})
		if err := validateLoadedCell(badLib); err == nil {
			t.Fatal("library cell with wrong size should fail")
		}
	})

	t.Run("MerkleProofAndUpdateValidation", func(t *testing.T) {
		leaf := BeginCell().MustStoreUInt(0x1234, 16).EndCell()
		sk := CreateProofSkeleton()
		sk.SetRecursive()
		proof, err := leaf.CreateProof(sk)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateLoadedCell(proof); err != nil {
			t.Fatalf("valid merkle proof should pass validation: %v", err)
		}

		badProof := FromRawUnsafe(RawUnsafeCell{
			IsSpecial: true,
			LevelMask: LevelMask{Mask: 1},
			BitsSz:    proof.bitsSz,
			Data:      append([]byte(nil), proof.data...),
			Refs:      proof.refs,
		})
		if err := validateLoadedCell(badProof); err == nil {
			t.Fatal("merkle proof with mismatched level mask should fail")
		}

		left := BeginCell().MustStoreUInt(0xAA, 8).EndCell()
		right := BeginCell().MustStoreUInt(0xBB, 8).EndCell()
		update := mustMerkleUpdateCell(t, left, right)
		if err := validateLoadedCell(update); err != nil {
			t.Fatalf("valid merkle update should pass validation: %v", err)
		}

		badUpdate := FromRawUnsafe(RawUnsafeCell{
			IsSpecial: true,
			LevelMask: LevelMask{Mask: 1},
			BitsSz:    update.bitsSz,
			Data:      append([]byte(nil), update.data...),
			Refs:      update.refs,
		})
		if err := validateLoadedCell(badUpdate); err == nil {
			t.Fatal("merkle update with mismatched level mask should fail")
		}
	})
}
