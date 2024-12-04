package liteclient

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
)

func (n *connection) authRequest() error {
	n.authLock.Lock()
	defer n.authLock.Unlock()

	n.ourNonce = make([]byte, 32)
	if _, err := rand.Read(n.ourNonce); err != nil {
		return err
	}

	payload, err := tl.Serialize(TCPAuthenticate{n.ourNonce}, true)
	if err != nil {
		return fmt.Errorf("failed to serialize request, err: %w", err)
	}
	return n.send(payload)
}

func (n *connection) authSignComplete(nonce []byte) error {
	n.authLock.Lock()
	defer n.authLock.Unlock()

	if n.authed {
		return nil
	}

	if n.pool.authKey == nil {
		return fmt.Errorf("pool has no auth key")
	}

	if len(nonce) > 512 {
		return fmt.Errorf("too long nonce")
	}

	payload, err := tl.Serialize(TCPAuthenticationComplete{
		PublicKey: adnl.PublicKeyED25519{Key: n.pool.authKey.Public().(ed25519.PublicKey)},
		Signature: ed25519.Sign(n.pool.authKey, append(append([]byte{}, n.ourNonce...), nonce...)),
	}, true)
	if err != nil {
		return fmt.Errorf("failed to serialize auth sign request, err: %w", err)
	}

	err = n.send(payload)
	if err != nil {
		return fmt.Errorf("failed to send auth sign request, err: %w", err)
	}

	close(n.authEvt)
	n.authed = true
	return nil
}
