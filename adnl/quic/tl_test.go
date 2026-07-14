package quic

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"github.com/xssnick/tonutils-go/tl"
)

func TestConstructorIDs(t *testing.T) {
	cases := map[string]uint32{
		"quic.message data:bytes = quic.Request": idQuicMessage,
		"quic.query data:bytes = quic.Request":   idQuicQuery,
		"quic.answer data:bytes = quic.Response": idQuicAnswer,
		"pub.ed25519 key:int256 = PublicKey":     0x4813b4c6,
	}
	for schema, want := range cases {
		if got := tl.CRC(schema); got != want {
			t.Errorf("CRC(%q) = 0x%08x, want 0x%08x", schema, got, want)
		}
	}
}

// TestPubEd25519Magic checks the well-known TON little-endian magic c6 b4 13 48.
func TestPubEd25519Magic(t *testing.T) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], tl.CRC("pub.ed25519 key:int256 = PublicKey"))
	if got := hex.EncodeToString(b[:]); got != "c6b41348" {
		t.Fatalf("pub.ed25519 LE magic = %s, want c6b41348", got)
	}
}

func TestBoxedRoundTrip(t *testing.T) {
	for _, payload := range [][]byte{
		nil,
		[]byte("x"),
		bytes.Repeat([]byte("a"), 253), // 1-byte length boundary
		bytes.Repeat([]byte("b"), 254), // 4-byte length boundary
		bytes.Repeat([]byte("c"), 1000),
	} {
		wire, err := serializeBoxed(idQuicQuery, payload)
		if err != nil {
			t.Fatalf("serialize len=%d: %v", len(payload), err)
		}
		if len(wire)%4 != 0 {
			t.Errorf("wire not 4-byte aligned: len=%d", len(wire))
		}
		id, got, err := parseBoxed(wire)
		if err != nil {
			t.Fatalf("parse len=%d: %v", len(payload), err)
		}
		if id != idQuicQuery {
			t.Errorf("id = 0x%08x, want quic.query", id)
		}
		if !bytes.Equal(got, payload) {
			t.Errorf("payload round-trip mismatch len=%d", len(payload))
		}
	}
}

func TestReadBoxedObjectRoundTrip(t *testing.T) {
	for _, payload := range [][]byte{
		nil,
		[]byte("x"),
		bytes.Repeat([]byte("a"), 253),
		bytes.Repeat([]byte("b"), 254),
		bytes.Repeat([]byte("c"), 1000),
	} {
		wire, err := serializeBoxed(idQuicQuery, payload)
		if err != nil {
			t.Fatalf("serialize len=%d: %v", len(payload), err)
		}

		id, got, err := readBoxedObject(bytes.NewReader(wire), int64(len(wire)))
		if err != nil {
			t.Fatalf("read len=%d: %v", len(payload), err)
		}
		if id != idQuicQuery {
			t.Errorf("id = 0x%08x, want quic.query", id)
		}
		if !bytes.Equal(got, payload) {
			t.Errorf("payload round-trip mismatch len=%d", len(payload))
		}
	}
}

func TestReadBoxedObjectRejectsOversizedHeaderBeforePayload(t *testing.T) {
	header, headerLen, _, total, err := boxedObjectHeader(idQuicQuery, 64<<10)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = readBoxedObject(bytes.NewReader(header[:headerLen]), int64(total-1))
	if err == nil {
		t.Fatal("expected oversized boxed object to fail")
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("oversized object should fail before reading payload, got %v", err)
	}
}

func TestReadBoxedObjectRejectsTrailingBytes(t *testing.T) {
	wire, err := serializeBoxed(idQuicQuery, []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	wire = append(wire, 0)

	if _, _, err = readBoxedObject(bytes.NewReader(wire), int64(len(wire))); err == nil {
		t.Fatal("expected trailing byte to fail")
	}
}

func TestReadBoxedObjectRespectsWholeObjectLimit(t *testing.T) {
	wire, err := serializeBoxed(idQuicQuery, []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err = readBoxedObject(bytes.NewReader(wire), int64(len(wire)-1)); err == nil {
		t.Fatal("expected whole object limit to fail")
	}
}

func TestWriteBoxedObjectToMatchesSerializeBoxed(t *testing.T) {
	for _, payload := range [][]byte{
		nil,
		[]byte("x"),
		bytes.Repeat([]byte("a"), 253),
		bytes.Repeat([]byte("b"), 254),
		bytes.Repeat([]byte("c"), 1000),
		bytes.Repeat([]byte("d"), 40<<10),
	} {
		want, err := serializeBoxed(idQuicAnswer, payload)
		if err != nil {
			t.Fatalf("serialize len=%d: %v", len(payload), err)
		}

		var got bytes.Buffer
		if err = writeBoxedObjectTo(&got, idQuicAnswer, payload); err != nil {
			t.Fatalf("write len=%d: %v", len(payload), err)
		}
		if !bytes.Equal(got.Bytes(), want) {
			t.Fatalf("wire mismatch len=%d", len(payload))
		}
	}
}

func TestSNIRoundTrip(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	id := adnlIDFromKey(pub)
	sni := id.sni()
	// shape: 32 + 1 + 32 + len(".adnl")
	if len(sni) != 32+1+32+5 {
		t.Fatalf("unexpected SNI shape: %q", sni)
	}
	back, err := parseSNI(sni)
	if err != nil {
		t.Fatalf("parseSNI: %v", err)
	}
	if back != id {
		t.Fatalf("SNI round-trip mismatch: %s != %s", back, id)
	}
}
