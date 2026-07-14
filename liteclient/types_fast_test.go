package liteclient

import (
	"bytes"
	"testing"

	"github.com/xssnick/tonutils-go/tl"
)

type liteclientFastCodecPayload struct {
	Data []byte `tl:"bytes"`
}

func init() {
	tl.Register(liteclientFastCodecPayload{}, "liteclient.fastCodecPayload data:bytes = liteclient.FastCodecPayload")
}

func TestLiteServerQueryFastCodecCopyModes(t *testing.T) {
	src := LiteServerQuery{
		Data: liteclientFastCodecPayload{Data: []byte{0x12, 0x34, 0x56}},
	}

	data, err := tl.Serialize(src, true)
	if err != nil {
		t.Fatal(err)
	}

	var copied LiteServerQuery
	if _, err = tl.Parse(&copied, data, true); err != nil {
		t.Fatal(err)
	}

	var noCopy LiteServerQuery
	if _, err = tl.ParseNoCopy(&noCopy, data, true); err != nil {
		t.Fatal(err)
	}

	payloadOff := bytes.Index(data, []byte{0x12, 0x34, 0x56})
	if payloadOff < 0 {
		t.Fatal("payload not found")
	}
	data[payloadOff] = 0xAA

	if copied.Data.(liteclientFastCodecPayload).Data[0] == 0xAA {
		t.Fatal("regular Parse should copy liteServer.query payload")
	}
	if noCopy.Data.(liteclientFastCodecPayload).Data[0] != 0xAA {
		t.Fatal("ParseNoCopy should alias liteServer.query payload")
	}
}

func TestLiteServerQueryFastCodecMultiplePayloads(t *testing.T) {
	packed, err := tl.Serialize([]tl.Serializable{
		liteclientFastCodecPayload{Data: []byte{1}},
		liteclientFastCodecPayload{Data: []byte{2}},
	}, true)
	if err != nil {
		t.Fatal(err)
	}

	data, err := tl.Serialize(LiteServerQuery{Data: tl.Raw(packed)}, true)
	if err != nil {
		t.Fatal(err)
	}

	var parsed LiteServerQuery
	if _, err = tl.Parse(&parsed, data, true); err != nil {
		t.Fatal(err)
	}

	list, ok := parsed.Data.([]tl.Serializable)
	if !ok {
		t.Fatalf("expected payload list, got %T", parsed.Data)
	}
	if len(list) != 2 {
		t.Fatalf("unexpected payload list len %d", len(list))
	}
	if list[0].(liteclientFastCodecPayload).Data[0] != 1 || list[1].(liteclientFastCodecPayload).Data[0] != 2 {
		t.Fatal("payload list mismatch")
	}
}

func TestTCPFastCodecRoundTrip(t *testing.T) {
	data, err := tl.Serialize(TCPPing{RandomID: 12345}, true)
	if err != nil {
		t.Fatal(err)
	}

	var msg tl.Serializable
	if _, err = tl.Parse(&msg, data, true); err != nil {
		t.Fatal(err)
	}

	if msg.(TCPPing).RandomID != 12345 {
		t.Fatal("tcp ping mismatch")
	}

	data, err = tl.Serialize(TCPPong{RandomID: 67890}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tl.Parse(&msg, data, true); err != nil {
		t.Fatal(err)
	}
	if msg.(TCPPong).RandomID != 67890 {
		t.Fatal("tcp pong mismatch")
	}
}
