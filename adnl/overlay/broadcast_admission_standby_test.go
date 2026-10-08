package overlay

import (
	"testing"
	"time"
)

// Plumtree fans the same broadcast out from every neighbour, so a node sees the
// same id many times while the first copy is still being admitted. Exactly one
// copy may park -- it is the one that takes over if the owner returns Retry --
// and every further copy must be dropped without blocking an ADNL listener
// goroutine.
func TestSimpleAdmissionParksOnlyOneStandby(t *testing.T) {
	state := NewBroadcastFECRelayState()
	var id broadcastSimpleIDKey
	id[0] = 1

	owner := state.beginSimpleAdmission(id)
	if owner.status != broadcastAdmissionOwner {
		t.Fatalf("first copy status = %d, want owner", owner.status)
	}

	standby := state.beginSimpleAdmission(id)
	if standby.status != broadcastAdmissionWait {
		t.Fatalf("second copy status = %d, want wait", standby.status)
	}
	if standby.admission != owner.admission {
		t.Fatal("the standby must wait on the owner's admission")
	}

	for i := 0; i < 32; i++ {
		extra := state.beginSimpleAdmission(id)
		if extra.status != broadcastAdmissionDuplicate {
			t.Fatalf("copy %d status = %d, want duplicate", i+3, extra.status)
		}
		if extra.admission != nil {
			t.Fatal("a dropped duplicate must not be handed an admission to wait on")
		}
	}

	// The standby still gets the owner's verdict, so a Retry is not lost.
	state.finishSimpleAdmission(id, owner.admission, BroadcastDispositionRetry)
	select {
	case <-standby.admission.done:
	case <-time.After(time.Second):
		t.Fatal("standby was not released when the owner finished")
	}
	if standby.admission.disposition != BroadcastDispositionRetry {
		t.Fatalf("standby saw disposition %d, want retry", standby.admission.disposition)
	}

	// After a Retry the id is free again, so the standby can take over.
	retry := state.beginSimpleAdmission(id)
	if retry.status != broadcastAdmissionOwner {
		t.Fatalf("post-retry status = %d, want owner", retry.status)
	}
}

func TestTwoStepSimpleAdmissionParksOnlyOneStandby(t *testing.T) {
	state := NewBroadcastTwoStepState()
	now := time.Now()
	var id broadcastTwoStepIDKey
	id[0] = 2

	owner := state.beginSimpleAdmission(id, now)
	if owner.status != broadcastAdmissionOwner {
		t.Fatalf("first copy status = %d, want owner", owner.status)
	}
	if standby := state.beginSimpleAdmission(id, now); standby.status != broadcastAdmissionWait {
		t.Fatalf("second copy status = %d, want wait", standby.status)
	}
	for i := 0; i < 32; i++ {
		if extra := state.beginSimpleAdmission(id, now); extra.status != broadcastAdmissionDuplicate {
			t.Fatalf("copy %d status = %d, want duplicate", i+3, extra.status)
		}
	}

	// A committed broadcast is dropped by the delivered cache, not by parking.
	state.finishSimpleAdmission(id, owner.admission, BroadcastDispositionAcceptAndRelay)
	if after := state.beginSimpleAdmission(id, now); after.status != broadcastAdmissionCommitted {
		t.Fatalf("post-commit status = %d, want committed", after.status)
	}
}

func BenchmarkSimpleAdmissionDuplicateDrop(b *testing.B) {
	state := NewBroadcastFECRelayState()
	var id broadcastSimpleIDKey
	id[0] = 3

	if owner := state.beginSimpleAdmission(id); owner.status != broadcastAdmissionOwner {
		b.Fatal("failed to take the admission")
	}
	if standby := state.beginSimpleAdmission(id); standby.status != broadcastAdmissionWait {
		b.Fatal("failed to park the standby")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if state.beginSimpleAdmission(id).status != broadcastAdmissionDuplicate {
			b.Fatal("duplicate was not dropped")
		}
	}
}
