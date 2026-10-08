package quic

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tl"
)

type boxedHeaderBoundaryCase struct {
	name       string
	payloadLen int
	tlHeader   []byte
	headerLen  int
	padding    int
}

// parseBoxed decodes a boxed `<name> data:bytes` object, returning its
// constructor id and the payload. It is the whole-buffer oracle for the
// round-trip tests; the receive path parses streams incrementally.
func parseBoxed(data []byte) (id uint32, payload []byte, err error) {
	if len(data) < 4 {
		return 0, nil, errors.New("quic: boxed object too short")
	}
	if len(data)%4 != 0 {
		return 0, nil, fmt.Errorf("quic: boxed object is not 4-byte aligned: %d bytes", len(data))
	}

	id = binary.LittleEndian.Uint32(data[:4])
	payload, rest, err := tl.FromBytesNoCopy(data[4:])
	if err != nil {
		return 0, nil, err
	}
	if len(rest) != 0 {
		return 0, nil, fmt.Errorf("quic: %d trailing bytes after boxed object", len(rest))
	}
	return id, payload, nil
}

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

func TestMaxPlumtreePayloadSizeMatchesMTU(t *testing.T) {
	if MaxPlumtreePayloadSize != (16<<20)+4096 {
		t.Fatalf(
			"MaxPlumtreePayloadSize = %d, want %d",
			MaxPlumtreePayloadSize,
			(16<<20)+4096,
		)
	}

	if _, _, _, _, err := boxedObjectHeader(idQuicQuery, MaxPlumtreePayloadSize); err != nil {
		t.Fatalf("maximum Plumtree payload rejected: %v", err)
	}
	if _, _, _, _, err := boxedObjectHeader(idQuicAnswer, MaxPlumtreePayloadSize+1); err != nil {
		t.Fatalf("answer above the default Plumtree limit rejected by wire encoder: %v", err)
	}
	if _, _, _, _, err := boxedObjectHeader(idQuicQuery, -1); err == nil {
		t.Fatal("negative payload length accepted")
	}

	if strconv.IntSize == 64 {
		maximum := int(maxTLBytesPayloadSize)
		header, headerLen, pad, total, err := boxedObjectHeader(idQuicAnswer, maximum)
		if err != nil {
			t.Fatalf("maximum uint32 payload: %v", err)
		}
		if headerLen != 12 || pad != 1 || int64(total) != maxBoxedObjectWireSize {
			t.Fatalf(
				"maximum wire shape = (header=%d pad=%d total=%d), want (12, 1, %d)",
				headerLen,
				pad,
				total,
				maxBoxedObjectWireSize,
			)
		}
		if got := header[4:12]; !bytes.Equal(got, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0, 0, 0}) {
			t.Fatalf("maximum uint32 TL header = %x", got)
		}

		tooLarge := maxTLBytesPayloadSize + 1
		if _, _, _, _, err = boxedObjectHeader(idQuicAnswer, int(tooLarge)); err == nil {
			t.Fatal("payload above uint32 was accepted")
		}
	}
}

func TestBoxedObjectHeaderExtendedBoundaryGolden(t *testing.T) {
	tests := []boxedHeaderBoundaryCase{
		{
			name:       "largest_24_bit",
			payloadLen: (1 << 24) - 1,
			tlHeader:   []byte{0xFE, 0xFF, 0xFF, 0xFF},
			headerLen:  8,
			padding:    1,
		},
		{
			name:       "first_extended",
			payloadLen: 1 << 24,
			tlHeader:   []byte{0xFF, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00},
			headerLen:  12,
			padding:    0,
		},
		{
			name:       "maximum_plumtree_envelope",
			payloadLen: MaxPlumtreePayloadSize,
			tlHeader:   []byte{0xFF, 0x00, 0x10, 0x00, 0x01, 0x00, 0x00, 0x00},
			headerLen:  12,
			padding:    0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			header, headerLen, pad, total, err := boxedObjectHeader(idQuicQuery, test.payloadLen)
			if err != nil {
				t.Fatal(err)
			}
			if headerLen != test.headerLen {
				t.Fatalf("header length = %d, want %d", headerLen, test.headerLen)
			}
			if pad != test.padding {
				t.Fatalf("padding = %d, want %d", pad, test.padding)
			}
			if total != headerLen+test.payloadLen+pad {
				t.Fatalf("total = %d, want %d", total, headerLen+test.payloadLen+pad)
			}

			var constructor [4]byte
			binary.LittleEndian.PutUint32(constructor[:], idQuicQuery)
			want := append(constructor[:], test.tlHeader...)
			if !bytes.Equal(header[:headerLen], want) {
				t.Fatalf("header = %x, want %x", header[:headerLen], want)
			}
		})
	}
}

func TestBoxedObjectExtendedRoundTrip(t *testing.T) {
	payload := bytes.Repeat([]byte{0xA7}, 1<<24)
	wire, err := serializeBoxed(idQuicQuery, payload)
	if err != nil {
		t.Fatal(err)
	}

	id, parsed, err := parseBoxed(wire)
	if err != nil {
		t.Fatal(err)
	}
	if id != idQuicQuery || !bytes.Equal(parsed, payload) {
		t.Fatalf("parsed object id=0x%08x payload_len=%d", id, len(parsed))
	}

	id, parsed, err = readBoxedObject(bytes.NewReader(wire), int64(len(wire)))
	if err != nil {
		t.Fatal(err)
	}
	if id != idQuicQuery || !bytes.Equal(parsed, payload) {
		t.Fatalf("streamed object id=0x%08x payload_len=%d", id, len(parsed))
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

func TestReadBoxedObjectRejectsMalformedExtendedHeader(t *testing.T) {
	header, headerLen, _, _, err := boxedObjectHeader(idQuicQuery, 1<<24)
	if err != nil {
		t.Fatal(err)
	}

	for size := 5; size < headerLen; size++ {
		_, payload, readErr := readBoxedObject(
			bytes.NewReader(header[:size]),
			DefaultMaxObjectSize,
		)
		if readErr == nil {
			t.Fatalf("truncated %d-byte header was accepted", size)
		}
		if payload != nil {
			t.Fatalf("truncated header allocated %d payload bytes", len(payload))
		}
	}

	aboveDefault, aboveDefaultLen, _, _, err := boxedObjectHeader(
		idQuicAnswer,
		MaxPlumtreePayloadSize+1,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, payload, readErr := readBoxedObject(
		bytes.NewReader(aboveDefault[:aboveDefaultLen]),
		DefaultMaxObjectSize,
	); readErr == nil {
		t.Fatal("extended payload above the supplied stream limit was accepted")
	} else if errors.Is(readErr, io.ErrUnexpectedEOF) {
		t.Fatalf("stream limit must be checked before payload read: %v", readErr)
	} else if payload != nil {
		t.Fatalf("over-limit header allocated %d payload bytes", len(payload))
	}

	var invalid [12]byte
	binary.LittleEndian.PutUint32(invalid[:4], idQuicQuery)
	invalid[4] = 0xFF
	invalid[9] = 1
	if _, payload, readErr := readBoxedObject(
		bytes.NewReader(invalid[:]),
		maxBoxedObjectWireSize,
	); readErr == nil {
		t.Fatal("extended length above uint32 was accepted")
	} else if payload != nil {
		t.Fatalf("invalid header allocated %d payload bytes", len(payload))
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

func TestReadBoxedObjectRejectsMissingPadding(t *testing.T) {
	wire, err := serializeBoxed(idQuicQuery, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}

	for missing := 1; missing <= 2; missing++ {
		if _, _, err = readBoxedObject(
			bytes.NewReader(wire[:len(wire)-missing]),
			int64(len(wire)),
		); err == nil {
			t.Fatalf("boxed object missing %d padding bytes was accepted", missing)
		}
	}
}

// The alignment bytes must arrive, but their content is not inspected -- same
// rule as tl.fromBytes, so a frame is accepted or rejected identically over
// QUIC and over ADNL/RLDP.
func TestReadBoxedObjectAcceptsNonZeroPadding(t *testing.T) {
	wire, err := serializeBoxed(idQuicQuery, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}

	for _, index := range []int{len(wire) - 2, len(wire) - 1} {
		relaxed := append([]byte(nil), wire...)
		relaxed[index] = 1
		id, payload, err := readBoxedObject(bytes.NewReader(relaxed), int64(len(relaxed)))
		if err != nil {
			t.Fatalf("boxed object with non-zero padding at %d was rejected: %v", index, err)
		}
		if id != idQuicQuery {
			t.Fatalf("id = %08x, want %08x", id, idQuicQuery)
		}
		if !bytes.Equal(payload, []byte("x")) {
			t.Fatalf("payload = %x, want %x", payload, []byte("x"))
		}
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
	if upper, err := parseSNI(strings.ToUpper(sni)); err != nil || upper != id {
		t.Fatalf("upper-case SNI normalization failed: id=%s err=%v", upper, err)
	}
	if _, err := parseSNI(sni + "."); err == nil {
		t.Fatal("SNI with a trailing dot was accepted")
	}
}
