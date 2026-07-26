package tlb

import (
	"bytes"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestNewConsensusConfigSimplexV2BitLayout(t *testing.T) {
	const (
		flags                = uint8(0b10101)
		protocolVersion      = uint8(0b10)
		slotsPerLeaderWindow = uint32(0x01020304)
	)

	fixture := cell.BeginCell().
		MustStoreUInt(0x22, 8).
		MustStoreUInt(uint64(flags), 5).
		MustStoreUInt(uint64(protocolVersion), 2).
		MustStoreBoolBit(true).
		MustStoreUInt(uint64(slotsPerLeaderWindow), 32).
		MustStoreDict(nil).
		EndCell()

	var parsed NewConsensusConfigSimplexV2
	if err := LoadFromCell(&parsed, fixture.MustBeginParse()); err != nil {
		t.Fatalf("LoadFromCell failed: %v", err)
	}

	if parsed.Flags != flags {
		t.Fatalf("unexpected flags: got %07b want %05b", parsed.Flags, flags)
	}
	if parsed.ProtocolVersion != protocolVersion {
		t.Fatalf("unexpected protocol version: got %d want %d", parsed.ProtocolVersion, protocolVersion)
	}
	if !parsed.UseQUIC {
		t.Fatal("expected QUIC to be enabled")
	}
	if parsed.SlotsPerLeaderWindow != slotsPerLeaderWindow {
		t.Fatalf("unexpected slots per leader window: got %08x want %08x", parsed.SlotsPerLeaderWindow, slotsPerLeaderWindow)
	}
	if parsed.NoncriticalParams == nil || parsed.NoncriticalParams.GetKeySize() != 8 {
		t.Fatalf("unexpected noncritical params dictionary: %+v", parsed.NoncriticalParams)
	}

	roundTrip, err := ToCell(&parsed)
	if err != nil {
		t.Fatalf("ToCell failed: %v", err)
	}
	if roundTrip.BitsSize() != 49 || roundTrip.RefsNum() != 0 {
		t.Fatalf("unexpected round-trip shape: bits=%d refs=%d", roundTrip.BitsSize(), roundTrip.RefsNum())
	}
	if !bytes.Equal(roundTrip.Hash(), fixture.Hash()) {
		t.Fatalf("round-trip mismatch:\ngot  %s\nwant %s", roundTrip.Dump(), fixture.Dump())
	}
}

func TestNewConsensusConfigSimplexKeepsSevenBitFlags(t *testing.T) {
	const (
		flags                 = uint8(0b10101)
		protocolBits          = uint8(0b10)
		combinedFlags         = flags<<2 | protocolBits
		targetRateMS          = uint32(0x01020304)
		slotsPerLeaderWindow  = uint32(0x11121314)
		firstBlockTimeoutMS   = uint32(0x21222324)
		maxLeaderWindowDesync = uint32(0x31323334)
	)

	fixture := cell.BeginCell().
		MustStoreUInt(0x21, 8).
		MustStoreUInt(uint64(combinedFlags), 7).
		MustStoreBoolBit(true).
		MustStoreUInt(uint64(targetRateMS), 32).
		MustStoreUInt(uint64(slotsPerLeaderWindow), 32).
		MustStoreUInt(uint64(firstBlockTimeoutMS), 32).
		MustStoreUInt(uint64(maxLeaderWindowDesync), 32).
		EndCell()

	var parsed NewConsensusConfigSimplex
	if err := LoadFromCell(&parsed, fixture.MustBeginParse()); err != nil {
		t.Fatalf("LoadFromCell failed: %v", err)
	}

	if parsed.Flags != combinedFlags {
		t.Fatalf("old simplex flags were split: got %07b want %07b", parsed.Flags, combinedFlags)
	}
	if !parsed.UseQUIC {
		t.Fatal("expected QUIC to be enabled")
	}
	if parsed.TargetRateMS != targetRateMS ||
		parsed.SlotsPerLeaderWindow != slotsPerLeaderWindow ||
		parsed.FirstBlockTimeoutMS != firstBlockTimeoutMS ||
		parsed.MaxLeaderWindowDesync != maxLeaderWindowDesync {
		t.Fatalf("unexpected old simplex payload: %+v", parsed)
	}

	roundTrip, err := ToCell(&parsed)
	if err != nil {
		t.Fatalf("ToCell failed: %v", err)
	}
	if roundTrip.BitsSize() != 144 || roundTrip.RefsNum() != 0 {
		t.Fatalf("unexpected round-trip shape: bits=%d refs=%d", roundTrip.BitsSize(), roundTrip.RefsNum())
	}
	if !bytes.Equal(roundTrip.Hash(), fixture.Hash()) {
		t.Fatalf("round-trip mismatch:\ngot  %s\nwant %s", roundTrip.Dump(), fixture.Dump())
	}
}

func TestNewConsensusConfigAllDecodesConcreteValues(t *testing.T) {
	masterchainFixture := cell.BeginCell().
		MustStoreUInt(0x22, 8).
		MustStoreUInt(0b10101, 5).
		MustStoreUInt(0b10, 2).
		MustStoreBoolBit(true).
		MustStoreUInt(4, 32).
		MustStoreDict(nil).
		EndCell()
	shardFixture := cell.BeginCell().
		MustStoreUInt(0x21, 8).
		MustStoreUInt(0b1010110, 7).
		MustStoreBoolBit(false).
		MustStoreUInt(2400, 32).
		MustStoreUInt(4, 32).
		MustStoreUInt(1000, 32).
		MustStoreUInt(8, 32).
		EndCell()
	fixture := cell.BeginCell().
		MustStoreUInt(0x10, 8).
		MustStoreBoolBit(true).
		MustStoreRef(masterchainFixture).
		MustStoreBoolBit(true).
		MustStoreRef(shardFixture).
		EndCell()

	var parsed NewConsensusConfigAll
	if err := LoadFromCell(&parsed, fixture.MustBeginParse()); err != nil {
		t.Fatalf("LoadFromCell failed: %v", err)
	}

	masterchain, ok := parsed.Masterchain.(NewConsensusConfigSimplexV2)
	if !ok {
		t.Fatalf("unexpected masterchain runtime type: got %T want %T", parsed.Masterchain, NewConsensusConfigSimplexV2{})
	}
	if masterchain.Flags != 0b10101 || masterchain.ProtocolVersion != 0b10 {
		t.Fatalf("unexpected masterchain config: %+v", masterchain)
	}

	shard, ok := parsed.Shard.(NewConsensusConfigSimplex)
	if !ok {
		t.Fatalf("unexpected shard runtime type: got %T want %T", parsed.Shard, NewConsensusConfigSimplex{})
	}
	if shard.Flags != 0b1010110 {
		t.Fatalf("unexpected shard config: %+v", shard)
	}

	roundTrip, err := ToCell(&parsed)
	if err != nil {
		t.Fatalf("ToCell failed: %v", err)
	}
	if !bytes.Equal(roundTrip.Hash(), fixture.Hash()) {
		t.Fatalf("round-trip mismatch:\ngot  %s\nwant %s", roundTrip.Dump(), fixture.Dump())
	}
}
