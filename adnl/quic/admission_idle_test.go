package quic

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"testing"
	"time"
)

func TestIdleConnectionReleasesSharedStreamAdmission(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxConcurrentIncomingStreams = 2
	limits.MaxConcurrentIncomingStreamsPerConnection = 2
	limits.GuaranteedStreamsPerConnection = 1

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	started := make(chan string, 4)
	releaseFirst := make(chan struct{})
	releaseNext := make(chan struct{})
	handler := Handler{
		OnQuery: func(_ context.Context, _ ed25519.PublicKey, payload []byte) ([]byte, error) {
			started <- string(payload)
			release := releaseFirst
			if bytes.Equal(payload, []byte("next")) {
				release = releaseNext
			}

			select {
			case <-release:
				return payload, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
	addr, server := startPhase1ReviewServer(t, handler, limits, mustKey(t))

	dial := func() *Client {
		t.Helper()
		client, err := Dial(ctx, addr, mustKey(t), server.defaultID.PublicKey())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = client.Close() })
		return client
	}

	results := make(chan error, 4)
	query := func(client *Client, message string) {
		go func() {
			answer, err := client.Query(ctx, []byte(message), 0)
			if err == nil && !bytes.Equal(answer, []byte(message)) {
				err = fmt.Errorf("answer = %q, want %q", answer, message)
			}
			results <- err
		}()
	}
	waitStarted := func(message string) {
		t.Helper()
		select {
		case got := <-started:
			if got != message {
				t.Fatalf("started %q, want %q", got, message)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%q query blocked by an idle connection", message)
		}
	}
	waitAnswers := func() {
		t.Helper()
		for range 2 {
			select {
			case err := <-results:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	}

	first := dial()
	for range 2 {
		query(first, "first")
		waitStarted("first")
	}
	if got := server.admission.activeStreams(); got != 1 {
		t.Fatalf("shared slots during burst = %d, want 1", got)
	}
	close(releaseFirst)
	waitAnswers()

	// Leave the first connection open. Its next AcceptStream must not reserve
	// shared capacity needed by a real stream on another connection.
	next := dial()
	for range 2 {
		query(next, "next")
		waitStarted("next")
	}
	close(releaseNext)
	waitAnswers()

	waitPhase1ReviewCondition(t, time.Second, "shared slots released", func() bool {
		return server.admission.activeStreams() == 0
	})
}
