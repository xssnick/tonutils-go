package adnl

import (
	"bytes"
	"testing"

	"github.com/xssnick/tonutils-go/tl"
)

func TestMessageFastCodecNoCopyOwnsEscapingFields(t *testing.T) {
	t.Run("custom", func(t *testing.T) {
		payload := []byte{0x22, 0x33, 0x44}
		data, err := tl.Serialize(MessageCustom{
			Data: TestMsg{Data: payload},
		}, true)
		if err != nil {
			t.Fatal(err)
		}

		var parsed MessageCustom
		if _, err = tl.ParseNoCopy(&parsed, data, true); err != nil {
			t.Fatal(err)
		}

		payloadOff := bytes.Index(data, payload)
		if payloadOff < 0 {
			t.Fatal("payload not found")
		}
		data[payloadOff] = 0x88

		if parsed.Data.(TestMsg).Data[0] == 0x88 {
			t.Fatal("custom payload aliases input buffer")
		}
	})

	t.Run("query", func(t *testing.T) {
		payload := []byte{0x22, 0x33, 0x44}
		data, err := tl.Serialize(MessageQuery{
			ID:   bytes.Repeat([]byte{0x11}, 32),
			Data: TestMsg{Data: payload},
		}, true)
		if err != nil {
			t.Fatal(err)
		}

		var parsed MessageQuery
		if _, err = tl.ParseNoCopy(&parsed, data, true); err != nil {
			t.Fatal(err)
		}

		payloadOff := bytes.Index(data, payload)
		if payloadOff < 0 {
			t.Fatal("payload not found")
		}
		data[4] = 0x99
		data[payloadOff] = 0x88

		if parsed.ID[0] == 0x99 {
			t.Fatal("query id aliases input buffer")
		}
		if parsed.Data.(TestMsg).Data[0] == 0x88 {
			t.Fatal("query payload aliases input buffer")
		}
	})

	t.Run("answer", func(t *testing.T) {
		payload := []byte{0x66, 0x77}
		data, err := tl.Serialize(MessageAnswer{
			ID:   bytes.Repeat([]byte{0x55}, 32),
			Data: TestMsg{Data: payload},
		}, true)
		if err != nil {
			t.Fatal(err)
		}

		var parsed MessageAnswer
		if _, err = tl.ParseNoCopy(&parsed, data, true); err != nil {
			t.Fatal(err)
		}

		payloadOff := bytes.Index(data, payload)
		if payloadOff < 0 {
			t.Fatal("payload not found")
		}
		data[4] = 0x99
		data[payloadOff] = 0x88

		if parsed.ID[0] == 0x99 {
			t.Fatal("answer id aliases input buffer")
		}
		if parsed.Data.(TestMsg).Data[0] == 0x88 {
			t.Fatal("answer payload aliases input buffer")
		}
	})
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
