package adnl

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"net"
	"sync"
	"time"
)

// Dial - can be changed to websockets for example, this way can be used from browser if compiled to wasm
var Dial = func(addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("udp", addr, timeout)
}

var ourDefaultKey ed25519.PrivateKey
var defaultKeyMX sync.RWMutex

// Connect - connect to ADNL UDP Peer
// Deprecated: Connect is DEPRECATED use Gateway
func Connect(ctx context.Context, addr string, peerKey ed25519.PublicKey, ourKey ed25519.PrivateKey) (_ *ADNL, err error) {
	if ourKey == nil {
		// we generate key once and then use it for further connections
		defaultKeyMX.Lock()
		if ourDefaultKey == nil {
			// new random key
			_, ourDefaultKey, err = ed25519.GenerateKey(nil)
			if err != nil {
				defaultKeyMX.Unlock()
				return nil, err
			}
		}
		ourKey = ourDefaultKey
		defaultKeyMX.Unlock()
	}

	a := initADNL(ourKey)
	a.peerKey = peerKey

	timeout := 30 * time.Second

	// add default timeout
	if till, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	} else {
		timeout = till.Sub(time.Now())
	}

	a.addr = addr
	conn, err := Dial(addr, timeout)
	if err != nil {
		return nil, err
	}

	a.writer = newWriter(func(p []byte, deadline time.Time) (err error) {
		_ = conn.SetWriteDeadline(deadline)
		n, err := conn.Write(p)
		if err != nil {
			return err
		}

		if n != len(p) {
			return fmt.Errorf("too big packet")
		}

		return nil
	})

	go func() {
		if err = listenPacketsAsClient(a, conn); err != nil {
			Logger("adnl connection with "+a.addr+" aborted:", err)
		}
	}()

	return a, nil
}

func listenPacketsAsClient(a *ADNL, conn net.Conn) error {
	defer func() {
		a.Close()
	}()

	rootID, err := ToKeyID(PublicKeyED25519{Key: a.ourKey.Public().(ed25519.PublicKey)})
	if err != nil {
		return err
	}

	for {
		select {
		case <-a.closer:
			return nil
		default:
		}

		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		if err != nil {
			return fmt.Errorf("failed to read data: %w", err)
		}

		buf = buf[:n]
		id := buf[:32]
		buf = buf[32:]

		if bytes.Equal(id, a.channel.id) { // message in channel
			err = a.channel.process(buf)
			if err != nil {
				Logger("failed to decode packet in channel:", err)
				continue
			}
		} else if bytes.Equal(id, rootID) { // message in root connection
			data, err := decodePacket(a.ourKey, buf)
			if err != nil {
				return fmt.Errorf("failed to decode packet: %w", err)
			}

			packet, err := parsePacket(data)
			if err != nil {
				Logger("failed to parse packet:", err)
				continue
			}

			err = a.processPacket(packet, nil)
			if err != nil {
				Logger("failed to process packet:", err)
				continue
			}
		} else {
			Logger("got message from unknown channel id:", hex.EncodeToString(id))
			continue
		}
	}
}
