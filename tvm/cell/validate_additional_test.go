package cell

import "testing"

func TestValidateLoadedCellSpecialVariants(t *testing.T) {
	t.Run("OrdinaryCellsStayPermissive", func(t *testing.T) {
		raw := makeManualCellForTest(false, LevelMask{Mask: 7}, 1, []byte{0x80}, nil)
		if err := validateLoadedCell(raw); err != nil {
			t.Fatalf("ordinary cells should stay permissive, got %v", err)
		}
	})

	t.Run("LibraryAndShortErrors", func(t *testing.T) {
		short := makeManualCellForTest(true, LevelMask{}, 0, nil, nil)
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

		badLib := makeManualCellForTest(true, LevelMask{}, 8, []byte{byte(LibraryCellType)}, nil)
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

		badProof := makeManualCellForTest(true, LevelMask{Mask: 1}, proof.BitsSize(), proof.data, proof.rawRefs())
		if err := validateLoadedCell(badProof); err == nil {
			t.Fatal("merkle proof with mismatched level mask should fail")
		}

		left := BeginCell().MustStoreUInt(0xAA, 8).EndCell()
		right := BeginCell().MustStoreUInt(0xBB, 8).EndCell()
		update := mustMerkleUpdateCell(t, left, right)
		if err := validateLoadedCell(update); err != nil {
			t.Fatalf("valid merkle update should pass validation: %v", err)
		}

		badUpdate := makeManualCellForTest(true, LevelMask{Mask: 1}, update.BitsSize(), update.data, update.rawRefs())
		if err := validateLoadedCell(badUpdate); err == nil {
			t.Fatal("merkle update with mismatched level mask should fail")
		}
	})
}
