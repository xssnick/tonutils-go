package quic

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"testing"
	"time"
)

func TestMessageCancellationReleasesBlockedWrite(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "without deadline"
		if deadline {
			name = "with deadline"
		}
		t.Run(name, func(t *testing.T) {
			blocked := make(chan struct{})
			addr, server := startServerWithConfig(t, Handler{
				OnQuery: func(ctx context.Context, _ ed25519.PublicKey, _ []byte) ([]byte, error) {
					close(blocked)
					<-ctx.Done()
					return nil, ctx.Err()
				},
			}, func(server *Server) {
				server.limits.MaxConcurrentIncomingStreamsPerConnection = 1
				server.quicConf.InitialStreamReceiveWindow = 4096
				server.quicConf.MaxStreamReceiveWindow = 4096
				server.quicConf.InitialConnectionReceiveWindow = 8192
				server.quicConf.MaxConnectionReceiveWindow = 8192
			}, mustKey(t))

			dialCtx, cancelDial := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancelDial()
			client, err := Dial(dialCtx, addr, mustKey(t), server.defaultID.PublicKey())
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			queryCtx, cancelQuery := context.WithCancel(t.Context())
			defer cancelQuery()
			queryResult := make(chan error, 1)
			go func() {
				_, err := client.Query(queryCtx, []byte("hold-handler-slot"), 0)
				queryResult <- err
			}()
			select {
			case <-blocked:
			case <-time.After(3 * time.Second):
				t.Fatal("query did not occupy the handler slot")
			}

			messageCtx, cancelMessage := context.WithCancel(t.Context())
			if deadline {
				cancelMessage()
				messageCtx, cancelMessage = context.WithTimeout(t.Context(), 10*time.Second)
			}
			defer cancelMessage()
			result := make(chan error, 1)
			go func() {
				result <- client.SendMessageParts(messageCtx, []byte("prefix:"), bytes.Repeat([]byte{1}, 1<<20))
			}()
			// The server's only handler slot is occupied and the message exceeds
			// the receive window, so its write must wait for peer credit.
			select {
			case err := <-result:
				t.Fatalf("message did not block: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			cancelMessage()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("canceled message returned %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("message remained blocked after cancellation")
			}
			cancelQuery()
			select {
			case <-queryResult:
			case <-time.After(time.Second):
				t.Fatal("query remained blocked after cancellation")
			}
		})
	}
}
