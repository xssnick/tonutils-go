//go:build cgo && tvm_cross_emulator

package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
)

func TestReferenceTransactionConfigRootCapabilities(t *testing.T) {
	base := mustReferenceTransactionConfigRoot(t)
	baseGlobalVersion, err := (tlb.BlockchainConfig{Root: base}).GetGlobalVersion()
	if err != nil {
		t.Fatalf("failed to load base global version: %v", err)
	}

	versioned := referenceTransactionConfigRootWithGlobalVersion(t, base, 11)
	versionedGlobalVersion, err := (tlb.BlockchainConfig{Root: versioned}).GetGlobalVersion()
	if err != nil {
		t.Fatalf("failed to load versioned global version: %v", err)
	}
	if versionedGlobalVersion.Version != 11 {
		t.Fatalf("version = %d, want 11", versionedGlobalVersion.Version)
	}
	if versionedGlobalVersion.Capabilities != baseGlobalVersion.Capabilities {
		t.Fatalf("capabilities = %d, want preserved %d", versionedGlobalVersion.Capabilities, baseGlobalVersion.Capabilities)
	}

	for _, tt := range []struct {
		name         string
		version      uint32
		capabilities uint64
	}{
		{
			name:         "capability off",
			version:      12,
			capabilities: 0,
		},
		{
			name:         "capability on",
			version:      12,
			capabilities: 4,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := referenceTransactionConfigRootWithGlobalVersionAndCapabilities(t, base, tt.version, tt.capabilities)
			globalVersion, err := (tlb.BlockchainConfig{Root: root}).GetGlobalVersion()
			if err != nil {
				t.Fatalf("failed to load global version: %v", err)
			}
			if globalVersion.Version != tt.version {
				t.Fatalf("version = %d, want %d", globalVersion.Version, tt.version)
			}
			if globalVersion.Capabilities != tt.capabilities {
				t.Fatalf("capabilities = %d, want %d", globalVersion.Capabilities, tt.capabilities)
			}
		})
	}
}
