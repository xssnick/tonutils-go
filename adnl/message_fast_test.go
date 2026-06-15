package adnl

import (
	"bytes"
	"testing"

	"github.com/xssnick/tonutils-go/tl"
)

func TestMessageQueryFastCodecCopyModes(t *testing.T) {
	src := MessageQuery{
		ID:   bytes.Repeat([]byte{0x11}, 32),
		Data: TestMsg{Data: []byte{0x22, 0x33, 0x44}},
	}

	data, err := tl.Serialize(src, true)
	if err != nil {
		t.Fatal(err)
	}

	var copied MessageQuery
	if _, err = tl.Parse(&copied, data, true); err != nil {
		t.Fatal(err)
	}

	var noCopy MessageQuery
	if _, err = tl.ParseNoCopy(&noCopy, data, true); err != nil {
		t.Fatal(err)
	}

	payloadOff := bytes.Index(data, []byte{0x22, 0x33, 0x44})
	if payloadOff < 0 {
		t.Fatal("payload not found")
	}
	data[4] = 0x99
	data[payloadOff] = 0x88

	copiedPayload := copied.Data.(TestMsg)
	if copied.ID[0] == 0x99 || copiedPayload.Data[0] == 0x88 {
		t.Fatal("regular Parse should copy message query byte fields")
	}

	noCopyPayload := noCopy.Data.(TestMsg)
	if noCopy.ID[0] != 0x99 || noCopyPayload.Data[0] != 0x88 {
		t.Fatal("ParseNoCopy should alias message query byte fields")
	}
}

func TestMessageAnswerFastCodecRoundTrip(t *testing.T) {
	src := MessageAnswer{
		ID:   bytes.Repeat([]byte{0x55}, 32),
		Data: TestMsg{Data: []byte{0x66, 0x77}},
	}

	data, err := tl.Serialize(src, true)
	if err != nil {
		t.Fatal(err)
	}

	var parsed tl.Serializable
	rest, err := tl.Parse(&parsed, data, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 0 {
		t.Fatalf("unexpected trailing bytes: %d", len(rest))
	}

	ans := parsed.(MessageAnswer)
	if !bytes.Equal(ans.ID, src.ID) {
		t.Fatal("answer id mismatch")
	}
	if !bytes.Equal(ans.Data.(TestMsg).Data, src.Data.(TestMsg).Data) {
		t.Fatal("answer payload mismatch")
	}
}
