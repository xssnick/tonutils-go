package overlay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/raptorq"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
	"reflect"
	"sync"
	"time"
)

type CertCheckResult int

const CertCheckResultForbidden CertCheckResult = 0
const CertCheckResultTrusted CertCheckResult = 1
const CertCheckResultNeedCheck CertCheckResult = 2

type fecBroadcastStream struct {
	decoder        *raptorq.Decoder
	finishedAt     *time.Time
	lastMessageAt  time.Time
	lastCompleteAt time.Time
	source         ed25519.PublicKey
	trusted        bool
	mx             sync.Mutex
}

type ADNLOverlayWrapper struct {
	overlayId      []byte
	authorizedKeys map[string]uint32

	maxUnauthSize     uint32
	allowFEC          bool
	trustUnauthorized bool

	customHandler     func(msg *adnl.MessageCustom) error
	queryHandler      func(msg *adnl.MessageQuery) error
	disconnectHandler func(addr string, key ed25519.PublicKey)

	broadcastStreams map[string]*fecBroadcastStream
	streamsMx        sync.RWMutex

	broadcastHandler func(msg tl.Serializable, trusted bool) error

	*ADNLWrapper
}

// WithOverlay - creates basic overlay with restrictive broadcast settings
func (a *ADNLWrapper) WithOverlay(id []byte) *ADNLOverlayWrapper {
	return a.CreateOverlayWithSettings(id, 0, true, false)
}

func (a *ADNLWrapper) CreateOverlayWithSettings(id []byte, maxUnauthBroadcastSize uint32,
	allowBroadcastFEC bool, trustUnauthorizedBroadcast bool) *ADNLOverlayWrapper {
	a.mx.Lock()
	defer a.mx.Unlock()

	strId := hex.EncodeToString(id)

	w := a.overlays[strId]
	if w != nil {
		return w
	}
	w = &ADNLOverlayWrapper{
		overlayId:         id,
		ADNLWrapper:       a,
		broadcastStreams:  map[string]*fecBroadcastStream{},
		allowFEC:          allowBroadcastFEC,
		maxUnauthSize:     maxUnauthBroadcastSize,
		trustUnauthorized: trustUnauthorizedBroadcast,
	}
	a.overlays[strId] = w

	return w
}

func (a *ADNLOverlayWrapper) SetAuthorizedKeys(keysWithMaxLen map[string]uint32) {
	a.mx.Lock()
	defer a.mx.Unlock()

	// reset and copy
	a.authorizedKeys = map[string]uint32{}
	for k, v := range keysWithMaxLen {
		a.authorizedKeys[k] = v
	}
}

func (a *ADNLWrapper) UnregisterOverlay(id []byte) {
	a.mx.Lock()
	defer a.mx.Unlock()

	delete(a.overlays, hex.EncodeToString(id))
}

func (a *ADNLOverlayWrapper) SendCustomMessage(ctx context.Context, req tl.Serializable) error {
	return a.ADNLWrapper.SendCustomMessage(ctx, []tl.Serializable{Message{Overlay: a.overlayId}, req})
}

func (a *ADNLOverlayWrapper) Query(ctx context.Context, req, result tl.Serializable) error {
	return a.ADNLWrapper.Query(ctx, []tl.Serializable{Query{Overlay: a.overlayId}, req}, result)
}

func (a *ADNLOverlayWrapper) GetRandomPeers(ctx context.Context) ([]Node, error) {
	var res NodesList
	err := a.Query(ctx, GetRandomPeers{}, &res)
	if err != nil {
		return nil, err
	}
	return res.List, nil
}

func (a *ADNLOverlayWrapper) SetCustomMessageHandler(handler func(msg *adnl.MessageCustom) error) {
	a.customHandler = handler
}

func (a *ADNLOverlayWrapper) SetQueryHandler(handler func(msg *adnl.MessageQuery) error) {
	a.queryHandler = handler
}

func (a *ADNLOverlayWrapper) SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey)) {
	a.disconnectHandler = handler
}

func (a *ADNLOverlayWrapper) SetBroadcastHandler(handler func(msg tl.Serializable, trusted bool) error) {
	a.broadcastHandler = handler
}

func (a *ADNLOverlayWrapper) Close() {
	a.ADNLWrapper.UnregisterOverlay(a.overlayId)
}

func (a *ADNLOverlayWrapper) checkRules(keyId string, dataSize uint32, isFEC bool) CertCheckResult {
	if dataSize == 0 {
		return CertCheckResultForbidden
	}

	a.mx.RLock()
	maxSz, authorized := a.authorizedKeys[keyId]
	a.mx.RUnlock()
	if authorized {
		if maxSz >= dataSize {
			return CertCheckResultTrusted
		}
		return CertCheckResultForbidden // too big size of data
	}

	if a.maxUnauthSize < dataSize {
		return CertCheckResultForbidden // too big size of data
	}
	if !a.allowFEC && isFEC {
		return CertCheckResultForbidden
	}
	if a.trustUnauthorized {
		return CertCheckResultTrusted
	}
	return CertCheckResultNeedCheck
}

func (a *ADNLOverlayWrapper) processFECBroadcast(t *BroadcastFEC) error {
	broadcastHash, err := t.CalcID()
	if err != nil {
		return fmt.Errorf("failed to calc broadcast hash: %w", err)
	}

	id := string(broadcastHash)
	a.streamsMx.RLock()
	stream := a.broadcastStreams[id]
	a.streamsMx.RUnlock()

	partDataHash := sha256.Sum256(t.Data)

	partHash, err := tl.Hash(&BroadcastFECPartID{
		BroadcastHash: broadcastHash,
		DataHash:      partDataHash[:],
		Seqno:         t.Seqno,
	})
	if err != nil {
		return fmt.Errorf("failed to compute hash id of the part: %w", err)
	}

	toSign, err := tl.Serialize(&BroadcastToSign{
		Hash: partHash,
		Date: t.Date,
	}, true)
	if err != nil {
		return fmt.Errorf("failed to serialize broadcast for sign check: %w", err)
	}

	sourceKey, ok := t.Source.(keys.PublicKeyED25519)
	if !ok {
		return fmt.Errorf("invalid signer key format")
	}

	if !ed25519.Verify(sourceKey.Key, toSign, t.Signature) {
		return fmt.Errorf("invalid broadcast signature")
	}

	if stream == nil {
		fec, ok := t.FEC.(rldp.FECRaptorQ)
		if !ok {
			return fmt.Errorf("not supported fec type")
		}

		if fec.DataSize != t.DataSize {
			return fmt.Errorf("incorrect data size")
		}

		srcId, err := tl.Hash(t.Source)
		if err != nil {
			return fmt.Errorf("source key id serialize failed: %w", err)
		}

		checkRes := a.checkRules(string(srcId), t.DataSize, true)
		if checkRes != CertCheckResultTrusted && t.Certificate != nil {
			var issuerId []byte

			var certRes CertCheckResult
			switch crt := t.Certificate.(type) {
			case CheckableCert:
				certRes, err = crt.Check(srcId, a.overlayId, t.DataSize, true)
				if err != nil {
					return fmt.Errorf("cert check failed: %w", err)
				}
				if certRes == CertCheckResultForbidden {
					break
				}

				var issuedBy any
				switch cert := crt.(type) {
				case Certificate:
					issuedBy = cert.IssuedBy
				case CertificateV2:
					issuedBy = cert.IssuedBy
				}

				issuerId, err = tl.Hash(issuedBy)
				if err != nil {
					return fmt.Errorf("issuer key id serialize failed: %w", err)
				}
			case CertificateEmpty:
			default:
				return fmt.Errorf("not supported cert type %s", reflect.TypeOf(t.Certificate).String())
			}

			if issuerId != nil {
				issuerRes := a.checkRules(string(issuerId), t.DataSize, true)
				if issuerRes > certRes {
					// we consider minimal of these 2
					issuerRes = certRes
				}

				if issuerRes > checkRes {
					// we consider maximal of these 2
					checkRes = issuerRes
				}
			}
		}

		if checkRes == CertCheckResultForbidden {
			return fmt.Errorf("not allowed")
		}

		dec, err := raptorq.NewRaptorQ(uint32(fec.SymbolSize)).CreateDecoder(uint32(fec.DataSize))
		if err != nil {
			return fmt.Errorf("failed to init raptorq decoder: %w", err)
		}

		stream = &fecBroadcastStream{
			decoder:       dec,
			lastMessageAt: time.Now(),
			source:        sourceKey.Key,
			trusted:       checkRes == CertCheckResultTrusted,
		}

		a.streamsMx.Lock()
		// check again because of possible concurrency
		if a.broadcastStreams[id] != nil {
			stream = a.broadcastStreams[id]
		} else {
			a.broadcastStreams[id] = stream
		}
		a.streamsMx.Unlock()
	} else if !bytes.Equal(stream.source, sourceKey.Key) {
		return fmt.Errorf("malformed source")
	}

	stream.mx.Lock()
	defer stream.mx.Unlock()

	if stream.finishedAt != nil {
		var received tl.Serializable = FECCompleted{
			Hash: broadcastHash,
		}

		// got packet for a finished stream, let them know that it is received
		err := a.ADNL.SendCustomMessage(context.Background(), received)
		if err != nil {
			return fmt.Errorf("failed to send overlay fec received message: %w", err)
		}

		return nil
	}

	tm := time.Now()
	stream.lastMessageAt = tm

	canTryDecode, err := stream.decoder.AddSymbol(uint32(t.Seqno), t.Data)
	if err != nil {
		return fmt.Errorf("failed to add raptorq symbol %d: %w", t.Seqno, err)
	}

	if canTryDecode {
		decoded, data, err := stream.decoder.Decode()
		if err != nil {
			return fmt.Errorf("failed to decode raptorq packet: %w", err)
		}

		// it may not be decoded due to unsolvable math system, it means we need more symbols
		if decoded {
			stream.finishedAt = &tm
			stream.decoder = nil

			a.streamsMx.Lock()
			if len(a.broadcastStreams) > 100 {
				for sID, s := range a.broadcastStreams {
					// remove streams that was finished more than 180 sec ago and stuck streams when it was no messages for 60 sec.
					if s.lastMessageAt.Add(40*time.Second).Before(tm) ||
						(s.finishedAt != nil && s.finishedAt.Add(90*time.Second).Before(tm)) {
						delete(a.broadcastStreams, sID)
					}
				}
			}
			a.streamsMx.Unlock()

			dHash := sha256.Sum256(data)
			if !bytes.Equal(dHash[:], t.DataHash) {
				return fmt.Errorf("incorrect data hash")
			}

			var res any
			_, err = tl.Parse(&res, data, true)
			if err != nil {
				return fmt.Errorf("failed to parse decoded broadcast message: %w", err)
			}

			var complete tl.Serializable = FECCompleted{
				Hash: broadcastHash,
			}

			err = a.ADNL.SendCustomMessage(context.Background(), complete)
			if err != nil {
				return fmt.Errorf("failed to send rldp complete message: %w", err)
			}

			if bHandler := a.broadcastHandler; bHandler != nil {
				// handle result
				err = bHandler(res, stream.trusted)
				if err != nil {
					return fmt.Errorf("failed to process broadcast message: %w", err)
				}
			}
		}
	}
	return nil
}
