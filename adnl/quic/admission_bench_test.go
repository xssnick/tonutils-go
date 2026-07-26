package quic

import (
	"bytes"
	"context"
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
		admission := newStreamAdmission(1, 1<<10)
		ctx := context.Background()
		b.ReportAllocs()

		var err error
		for b.Loop() {
			lease, acquireErr := admission.acquireStream(ctx)
			if acquireErr != nil {
				b.Fatal(acquireErr)
			}

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
