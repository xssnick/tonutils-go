package adnl

import (
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"reflect"
	"testing"

	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

type dbgDHTFindNode struct {
	Key []byte `tl:"int256"`
	K   int32  `tl:"int"`
}

type dbgDHTFindValue struct {
	Key []byte `tl:"int256"`
	K   int32  `tl:"int"`
}

type dbgDHTSignedAddressListQuery struct{}

type dbgDHTKey struct {
	ID    []byte `tl:"int256"`
	Name  []byte `tl:"bytes"`
	Index int32  `tl:"int"`
}

type dbgDHTKeyDescription struct {
	Key        dbgDHTKey `tl:"struct"`
	ID         any       `tl:"struct boxed [pub.ed25519,pub.aes,pub.unenc,pub.overlay]"`
	UpdateRule any       `tl:"struct boxed [dht.updateRule.signature,dht.updateRule.anybody,dht.updateRule.overlayNodes]"`
	Signature  []byte    `tl:"bytes"`
}

type dbgDHTValue struct {
	KeyDescription dbgDHTKeyDescription `tl:"struct"`
	Data           []byte               `tl:"bytes"`
	TTL            int32                `tl:"int"`
	Signature      []byte               `tl:"bytes"`
}

type dbgDHTStore struct {
	Value dbgDHTValue `tl:"struct"`
}

type dbgDHTUpdateRuleSignature struct{}
type dbgDHTUpdateRuleAnybody struct{}
type dbgDHTUpdateRuleOverlayNodes struct{}

type dbgDHTNode struct {
	ID        any           `tl:"struct boxed [pub.ed25519,pub.aes]"`
	AddrList  *address.List `tl:"struct"`
	Version   int32         `tl:"int"`
	Signature []byte        `tl:"bytes"`
}

type dbgDHTQuery struct {
	Node dbgDHTNode `tl:"struct"`
}

func init() {
	tl.Register(dbgDHTFindNode{}, "dht.findNode key:int256 k:int = dht.Nodes")
	tl.Register(dbgDHTFindValue{}, "dht.findValue key:int256 k:int = dht.ValueResult")
	tl.Register(dbgDHTSignedAddressListQuery{}, "dht.getSignedAddressList = dht.Node")
	tl.Register(dbgDHTKey{}, "dht.key id:int256 name:bytes idx:int = dht.Key")
	tl.Register(dbgDHTKeyDescription{}, "dht.keyDescription key:dht.key id:PublicKey update_rule:dht.UpdateRule signature:bytes = dht.KeyDescription")
	tl.Register(dbgDHTValue{}, "dht.value key:dht.keyDescription value:bytes ttl:int signature:bytes = dht.Value")
	tl.Register(dbgDHTStore{}, "dht.store value:dht.value = dht.Stored")
	tl.Register(dbgDHTUpdateRuleSignature{}, "dht.updateRule.signature = dht.UpdateRule")
	tl.Register(dbgDHTUpdateRuleAnybody{}, "dht.updateRule.anybody = dht.UpdateRule")
	tl.Register(dbgDHTUpdateRuleOverlayNodes{}, "dht.updateRule.overlayNodes = dht.UpdateRule")
	tl.Register(dbgDHTNode{}, "dht.node id:PublicKey addr_list:adnl.addressList version:int signature:bytes = dht.Node")
	tl.Register(dbgDHTQuery{}, "dht.query node:dht.node = True")

	_ = keys.PublicKeyED25519{}
}

func TestDebugDecodeRootPacket(t *testing.T) {
	seedHex := os.Getenv("SEED_HEX")
	payloadHex := os.Getenv("PAYLOAD_HEX")
	if seedHex == "" || payloadHex == "" {
		t.Skip("SEED_HEX and PAYLOAD_HEX are required")
	}

	seed, err := hex.DecodeString(seedHex)
	if err != nil {
		t.Fatal(err)
	}
	if len(seed) != ed25519.SeedSize {
		t.Fatalf("unexpected seed size %d", len(seed))
	}

	payload, err := hex.DecodeString(payloadHex)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) < 32 {
		t.Fatalf("unexpected payload size %d", len(payload))
	}

	data, err := decodePacket(ed25519.NewKeyFromSeed(seed), payload[32:])
	if err != nil {
		t.Fatal(err)
	}

	packet, err := parsePacket(data)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("from=%v from_short=%x seq=%v confirm=%v recv_addr=%v recv_prio=%v reinit=%v dst_reinit=%v msgs=%d",
		packet.From != nil,
		packet.FromIDShort,
		ptrDebugInt64(packet.Seqno),
		ptrDebugInt64(packet.ConfirmSeqno),
		ptrDebugInt32(packet.RecvAddrListVersion),
		ptrDebugInt32(packet.RecvPriorityAddrListVersion),
		ptrDebugInt32(packet.ReinitDate),
		ptrDebugInt32(packet.DstReinitDate),
		len(packet.Messages),
	)

	for i, msg := range packet.Messages {
		t.Logf("msg[%d]=%s %#v", i, reflect.TypeOf(msg), msg)
	}
}

func ptrDebugInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func ptrDebugInt32(v *int32) any {
	if v == nil {
		return nil
	}
	return *v
}
