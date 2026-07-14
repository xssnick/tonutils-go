package quic

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"testing"

	"github.com/xssnick/tonutils-go/tl"
)

var (
	idBenchmarkQueryAuto = tl.Register(BenchmarkQueryAuto{}, "bench.quic.query data:bytes = quic.Request")
	benchPayload         = bytes.Repeat([]byte{0xA5}, 1024)
	benchWire            []byte
	benchID              adnlID
)

type BenchmarkQueryAuto struct {
	Data []byte `tl:"bytes"`
}

func BenchmarkSerializeBoxed(b *testing.B) {
	b.Run("tl_append_bytes", func(b *testing.B) {
		b.ReportAllocs()

		var wire []byte
		for b.Loop() {
			var err error
			wire, err = serializeBoxed(idQuicQuery, benchPayload)
			if err != nil {
				b.Fatal(err)
			}
		}
		benchWire = wire
	})

	b.Run("auto_precompiled", func(b *testing.B) {
		b.ReportAllocs()

		var wire []byte
		for b.Loop() {
			var err error
			wire, err = tl.Append(make([]byte, 0, len(benchPayload)+12), BenchmarkQueryAuto{Data: benchPayload}, true)
			if err != nil {
				b.Fatal(err)
			}
		}
		benchWire = wire
	})

	b.Run("manual_old", func(b *testing.B) {
		b.ReportAllocs()

		var wire []byte
		for b.Loop() {
			var err error
			wire, err = serializeBoxedManual(idQuicQuery, benchPayload)
			if err != nil {
				b.Fatal(err)
			}
		}
		benchWire = wire
	})
}

func BenchmarkParseBoxed(b *testing.B) {
	customWire, err := serializeBoxed(idQuicQuery, benchPayload)
	if err != nil {
		b.Fatal(err)
	}
	autoWire, err := tl.Append(make([]byte, 0, len(benchPayload)+12), BenchmarkQueryAuto{Data: benchPayload}, true)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("tl_bytes_nocopy", func(b *testing.B) {
		b.ReportAllocs()

		var payload []byte
		for b.Loop() {
			var id uint32
			id, payload, err = parseBoxed(customWire)
			if err != nil {
				b.Fatal(err)
			}
			if id != idQuicQuery {
				b.Fatalf("id = 0x%08x, want 0x%08x", id, idQuicQuery)
			}
		}
		benchWire = payload
	})

	b.Run("auto_precompiled", func(b *testing.B) {
		b.ReportAllocs()

		var query BenchmarkQueryAuto
		for b.Loop() {
			var rest []byte
			rest, err = tl.ParseNoCopy(&query, autoWire, true)
			if err != nil {
				b.Fatal(err)
			}
			if len(rest) != 0 {
				b.Fatalf("%d trailing bytes after boxed object", len(rest))
			}
		}
		benchWire = query.Data
	})

	b.Run("manual_old", func(b *testing.B) {
		b.ReportAllocs()

		var payload []byte
		for b.Loop() {
			var id uint32
			id, payload, err = parseBoxedManual(customWire)
			if err != nil {
				b.Fatal(err)
			}
			if id != idQuicQuery {
				b.Fatalf("id = 0x%08x, want 0x%08x", id, idQuicQuery)
			}
		}
		benchWire = payload
	})
}

func BenchmarkADNLShortIDFromKey(b *testing.B) {
	pub := ed25519.PublicKey(bytes.Repeat([]byte{0x11}, ed25519.PublicKeySize))

	b.Run("current", func(b *testing.B) {
		b.ReportAllocs()

		var id adnlID
		for b.Loop() {
			id = adnlIDFromKey(pub)
		}
		benchID = id
	})

	b.Run("manual_old", func(b *testing.B) {
		b.ReportAllocs()

		var id adnlID
		for b.Loop() {
			id = adnlID(adnlIDFromKeyManual(pub))
		}
		benchID = id
	})
}

func BenchmarkReadBoxedObject(b *testing.B) {
	for _, size := range []int{1 << 10, 64 << 10, 1 << 20} {
		payload := bytes.Repeat([]byte{0xA5}, size)
		wire, err := serializeBoxed(idQuicQuery, payload)
		if err != nil {
			b.Fatal(err)
		}
		limit := int64(len(wire))

		b.Run(fmt.Sprintf("read_all/size=%dKB", size>>10), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size))

			var id uint32
			var got []byte
			for b.Loop() {
				id, got, err = readBoxedObjectReadAll(bytes.NewReader(wire), limit)
				if err != nil {
					b.Fatal(err)
				}
				if id != idQuicQuery || len(got) != size {
					b.Fatalf("bad object id=0x%08x len=%d", id, len(got))
				}
			}
			benchWire = got
		})

		b.Run(fmt.Sprintf("structured/size=%dKB", size>>10), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size))

			var id uint32
			var got []byte
			for b.Loop() {
				id, got, err = readBoxedObject(bytes.NewReader(wire), limit)
				if err != nil {
					b.Fatal(err)
				}
				if id != idQuicQuery || len(got) != size {
					b.Fatalf("bad object id=0x%08x len=%d", id, len(got))
				}
			}
			benchWire = got
		})
	}
}

func BenchmarkWriteBoxedObject(b *testing.B) {
	for _, size := range []int{1 << 10, 64 << 10, 1 << 20} {
		payload := bytes.Repeat([]byte{0xA5}, size)

		b.Run(fmt.Sprintf("serialize/size=%dKB", size>>10), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size))

			var wire []byte
			var err error
			for b.Loop() {
				wire, err = serializeBoxed(idQuicAnswer, payload)
				if err != nil {
					b.Fatal(err)
				}
			}
			benchWire = wire
		})

		b.Run(fmt.Sprintf("direct_discard/size=%dKB", size>>10), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size))

			var err error
			for b.Loop() {
				if err = writeBoxedObjectTo(io.Discard, idQuicAnswer, payload); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func readBoxedObjectReadAll(r io.Reader, maxSize int64) (uint32, []byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxSize+1))
	if err != nil {
		return 0, nil, err
	}
	if int64(len(data)) > maxSize {
		return 0, nil, fmt.Errorf("quic: stream object exceeds %d bytes", maxSize)
	}
	if len(data) == 0 {
		return 0, nil, io.ErrUnexpectedEOF
	}
	return parseBoxed(data)
}

func serializeBoxedManual(id uint32, payload []byte) ([]byte, error) {
	out := make([]byte, 4, 4+len(payload)+4)
	binary.LittleEndian.PutUint32(out, id)
	return appendTLBytesManual(out, payload)
}

func appendTLBytesManual(dst, data []byte) ([]byte, error) {
	sz, err := tlBytesEncodedSizeManual(len(data))
	if err != nil {
		return nil, err
	}

	start := len(dst)
	if len(data) >= 0xFE {
		var hdr [4]byte
		binary.LittleEndian.PutUint32(hdr[:], uint32(len(data)<<8)|0xFE)
		dst = append(dst, hdr[:]...)
	} else {
		dst = append(dst, byte(len(data)))
	}
	dst = append(dst, data...)
	for len(dst)-start < sz {
		dst = append(dst, 0)
	}
	return dst, nil
}

func tlBytesEncodedSizeManual(dataLen int) (int, error) {
	if dataLen >= 1<<24 {
		return 0, fmt.Errorf("quic: TL bytes too big (%d), limited to 1<<24", dataLen)
	}

	header := 1
	if dataLen >= 0xFE {
		header = 4
	}

	sz := dataLen + header
	if pad := sz % 4; pad != 0 {
		sz += 4 - pad
	}

	return sz, nil
}

func parseBoxedManual(data []byte) (id uint32, payload []byte, err error) {
	if len(data) < 4 {
		return 0, nil, fmt.Errorf("quic: boxed object too short")
	}

	id = binary.LittleEndian.Uint32(data[:4])
	payload, rest, err := readTLBytesManual(data[4:])
	if err != nil {
		return 0, nil, err
	}
	if len(rest) != 0 {
		return 0, nil, fmt.Errorf("quic: %d trailing bytes after boxed object", len(rest))
	}
	return id, payload, nil
}

func readTLBytesManual(data []byte) (payload, rest []byte, err error) {
	if len(data) == 0 {
		return nil, nil, fmt.Errorf("quic: TL bytes: empty input")
	}

	offset := 1
	ln := int(data[0])
	if ln == 0xFE {
		if len(data) < 4 {
			return nil, nil, fmt.Errorf("quic: TL bytes: truncated long length")
		}
		ln = int(binary.LittleEndian.Uint32(data)) >> 8
		offset = 4
	}

	total := ln + offset
	if pad := total % 4; pad != 0 {
		total += 4 - pad
	}
	if len(data) < total {
		return nil, nil, fmt.Errorf("quic: TL bytes: need %d bytes, have %d", total, len(data))
	}

	payload = data[offset : offset+ln]
	rest = data[total:]
	return payload, rest, nil
}

func adnlIDFromKeyManual(pub ed25519.PublicKey) [32]byte {
	var buf [4 + ed25519.PublicKeySize]byte
	binary.LittleEndian.PutUint32(buf[:4], 0x4813b4c6)
	copy(buf[4:], pub)
	return sha256.Sum256(buf[:])
}
