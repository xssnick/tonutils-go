//go:build cgo && tvm_cross_emulator

package tvm

import (
	"fmt"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

// The native loader materializes the library target before checking its kind:
// ton-blockchain/ton ac605b3b, crypto/vm/cells/CellSlice.cpp:1074-1141.
func TestTVMCrossEmulatorLazyLibraryDictionary(t *testing.T) {
	libraryLoadLimitSkipIfReferenceUnavailable(t)
	library, collection := lazyLibraryDictionaryFixture(t)
	for _, version := range []int{0, 3, 4, 5, 16} {
		for _, signed := range []bool{false, true} {
			for _, lookups := range []int{1, 2} {
				t.Run(fmt.Sprintf("v%d/signed=%t/lookups=%d", version, signed, lookups), func(t *testing.T) {
					code := lazyLibraryDictionaryCode(signed, lookups)
					refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
					refCfg.Libs = collection
					ref, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(),
						lazyLibraryDictionaryStack(t, library, lookups), *refCfg)
					if err != nil {
						t.Fatal(err)
					}
					refStack, err := normalizeStackCell(ref.stack)
					if err != nil {
						t.Fatal(err)
					}
					for _, lazy := range []bool{false, true} {
						root := collection
						if lazy {
							root = lazyLibraryDictionaryCollection(t, root)
						}
						res, err := runGoCrossCodeWithVersionAndLibs(code, cell.BeginCell().EndCell(), tuple.Tuple{},
							[]*cell.Cell{root}, lazyLibraryDictionaryStack(t, library, lookups), version)
						if err != nil {
							t.Fatal(err)
						}
						stack, err := normalizeStackCell(res.stack)
						if err != nil {
							t.Fatal(err)
						}
						if ref.exitCode != 0 || res.exitCode != ref.exitCode || res.gasUsed != ref.gasUsed || stack.HashKey() != refStack.HashKey() {
							t.Fatalf("lazy=%t: exit/gas=%d/%d, reference=%d/%d; stack=%s reference=%s",
								lazy, res.exitCode, res.gasUsed, ref.exitCode, ref.gasUsed, stack.Dump(), refStack.Dump())
						}
					}
				})
			}
		}
	}
}
