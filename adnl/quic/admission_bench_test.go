package quic

import (
	"bytes"
	"errors"
	"testing"
)

var admissionBenchErr error

func BenchmarkReadBoxedObjectHeaderRejection(b *testing.B) {
	header, headerLen, _, _, err := boxedObjectHeader(idQuicQuery, MaxPlumtreePayloadSize)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("object_limit", func(b *testing.B) {
		b.ReportAllocs()

		var err error
		for b.Loop() {
			_, payload, readErr := readBoxedObject(bytes.NewReader(header[:headerLen]), 1<<10)
			if readErr == nil {
				b.Fatal("oversized header was accepted")
			}
			if payload != nil {
				b.Fatalf("rejected payload allocated %d bytes", len(payload))
			}
			err = readErr
		}
		admissionBenchErr = err
	})

	b.Run("payload_admission", func(b *testing.B) {
		// Budget below one read chunk, so the very first charge is refused and
		// no payload buffer is ever handed back.
		admission := newStreamAdmission(1, 1<<10)
		b.ReportAllocs()

		var err error
		for b.Loop() {
			if !admission.tryAcquireSlot() {
				b.Fatal("slot was refused")
			}
			lease := streamAdmissionLease{admission: admission, globalSlot: true}

			_, payload, readErr := readBoxedObjectAdmitted(
				bytes.NewReader(header[:headerLen]),
				DefaultMaxObjectSize,
				&lease,
			)
			lease.release()
			if !errors.Is(readErr, errPayloadAdmissionFull) {
				b.Fatalf("error = %v, want %v", readErr, errPayloadAdmissionFull)
			}
			if payload != nil {
				b.Fatalf("rejected payload allocated %d bytes", len(payload))
			}
			err = readErr
		}
		admissionBenchErr = err
	})
}
