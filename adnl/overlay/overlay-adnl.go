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
	"maps"
	"reflect"
	"sync"
	"time"
)

type CertCheckResult int

const CertCheckResultForbidden CertCheckResult = 0
const CertCheckResultTrusted CertCheckResult = 1
const CertCheckResultNeedCheck CertCheckResult = 2

type fecBroadcastStream struct {
	decoder       *raptorq.Decoder
	encoder       *raptorq.Encoder
	parts         map[uint32][]byte
	finishedAt    *time.Time
	completedAt   *time.Time
	lastMessageAt time.Time
	source        ed25519.PublicKey
	fec           rldp.FECRaptorQ
	date          uint32
	trusted       bool
	mx            sync.Mutex
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
	maps.Copy(a.authorizedKeys, keysWithMaxLen)
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

func broadcastFECSeqnoLimit(symbolsCount uint32) uint32 {
	return symbolsCount*2 + 4
}

func (a *ADNLOverlayWrapper) cleanupBroadcastStreams(now time.Time) {
	a.streamsMx.Lock()
	if len(a.broadcastStreams) > 100 {
		for sID, s := range a.broadcastStreams {
			// remove streams that was finished more than 180 sec ago and stuck streams when it was no messages for 60 sec.
			if s.lastMessageAt.Add(40*time.Second).Before(now) ||
				(s.finishedAt != nil && s.finishedAt.Add(90*time.Second).Before(now)) {
				delete(a.broadcastStreams, sID)
			}
		}
	}
	a.streamsMx.Unlock()
}

func (a *ADNLOverlayWrapper) sendFECControlMessage(msg tl.Serializable) error {
	if err := a.ADNL.SendCustomMessage(context.Background(), msg); err != nil {
		return fmt.Errorf("failed to send overlay fec control message: %w", err)
	}
	return nil
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

	sourceKey, ok := t.Source.(keys.PublicKeyED25519)
	if !ok {
		return fmt.Errorf("invalid signer key format")
	}

	partHash, _, err := calcBroadcastFECPartData(broadcastHash, t.Data, t.Seqno)
	if err != nil {
		return err
	}
	if err = verifyBroadcastFECPartSignature(t.Source, partHash, t.Date, t.Signature); err != nil {
		return err
	}

	if stream == nil {
		fec, ok := t.FEC.(rldp.FECRaptorQ)
		if !ok {
			return fmt.Errorf("not supported fec type")
		}

		if fec.DataSize != t.DataSize {
			return fmt.Errorf("incorrect data size")
		}
		if t.Seqno >= broadcastFECSeqnoLimit(fec.SymbolsCount) {
			return fmt.Errorf("too big seqno")
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
			parts:         map[uint32][]byte{},
			lastMessageAt: time.Now(),
			source:        sourceKey.Key,
			fec:           fec,
			date:          t.Date,
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
	}

	var (
		decodedRes     any
		decoded        bool
		ackReceived    bool
		ackCompleted   bool
		cleanupStreams bool
	)

	stream.mx.Lock()
	if !bytes.Equal(stream.source, sourceKey.Key) {
		stream.mx.Unlock()
		return fmt.Errorf("malformed source")
	}
	if t.Seqno >= broadcastFECSeqnoLimit(stream.fec.SymbolsCount) {
		stream.mx.Unlock()
		return fmt.Errorf("too big seqno")
	}

	if stream.finishedAt != nil {
		ackCompleted = true
		stream.mx.Unlock()
		if ackCompleted {
			return a.sendFECControlMessage(FECCompleted{Hash: broadcastHash})
		}
		return nil
	}

	tm := time.Now()
	stream.lastMessageAt = tm

	if existing, ok := stream.parts[t.Seqno]; ok {
		stream.mx.Unlock()
		if !bytes.Equal(existing, t.Data) {
			return fmt.Errorf("conflicting data for seqno %d", t.Seqno)
		}
		return nil
	}
	stream.parts[t.Seqno] = append([]byte(nil), t.Data...)

	canTryDecode, err := stream.decoder.AddSymbol(t.Seqno, t.Data)
	if err != nil {
		delete(stream.parts, t.Seqno)
		stream.mx.Unlock()
		return fmt.Errorf("failed to add raptorq symbol %d: %w", t.Seqno, err)
	}

	if canTryDecode {
		decodedNow, data, err := stream.decoder.Decode()
		if err != nil {
			stream.mx.Unlock()
			return fmt.Errorf("failed to decode raptorq packet: %w", err)
		}

		// it may not be decoded due to unsolvable math system, it means we need more symbols
		if decodedNow {
			dHash := sha256.Sum256(data)
			if !bytes.Equal(dHash[:], t.DataHash) {
				stream.mx.Unlock()
				return fmt.Errorf("incorrect data hash")
			}

			enc, err := raptorq.NewRaptorQ(uint32(stream.fec.SymbolSize)).CreateEncoder(data)
			if err != nil {
				stream.mx.Unlock()
				return fmt.Errorf("failed to init raptorq encoder: %w", err)
			}

			var res any
			_, err = tl.Parse(&res, data, true)
			if err != nil {
				stream.mx.Unlock()
				return fmt.Errorf("failed to parse decoded broadcast message: %w", err)
			}

			stream.finishedAt = &tm
			stream.decoder = nil
			stream.encoder = enc
			stream.parts = nil
			decodedRes = res
			ackReceived = true
			decoded = true
			cleanupStreams = true
		}
	}

	stream.mx.Unlock()

	if cleanupStreams {
		a.cleanupBroadcastStreams(tm)
	}

	if ackReceived {
		if err = a.sendFECControlMessage(FECReceived{Hash: broadcastHash}); err != nil {
			return err
		}
	}

	if decoded {
		if bHandler := a.broadcastHandler; bHandler != nil {
			if err = bHandler(decodedRes, stream.trusted); err != nil {
				return fmt.Errorf("failed to process broadcast message: %w", err)
			}
		}

		ackCompleted = true
		stream.mx.Lock()
		if stream.completedAt == nil {
			stream.completedAt = &tm
		}
		stream.mx.Unlock()
	}

	if ackCompleted {
		if err = a.sendFECControlMessage(FECCompleted{Hash: broadcastHash}); err != nil {
			return err
		}
	}
	return nil
}

func (a *ADNLOverlayWrapper) processFECBroadcastShort(t *BroadcastFECShort) error {
	if t.Seqno < 0 {
		return fmt.Errorf("invalid seqno")
	}

	a.streamsMx.RLock()
	stream := a.broadcastStreams[string(t.BroadcastHash)]
	a.streamsMx.RUnlock()
	if stream == nil {
		return fmt.Errorf("short part of unknown broadcast")
	}

	sourceKey, ok := t.Source.(keys.PublicKeyED25519)
	if !ok {
		return fmt.Errorf("invalid signer key format")
	}

	var (
		partData      []byte
		date          uint32
		ackCompleted  bool
		knownPartOnly bool
	)

	stream.mx.Lock()
	if !bytes.Equal(stream.source, sourceKey.Key) {
		stream.mx.Unlock()
		return fmt.Errorf("malformed source")
	}

	seqno := uint32(t.Seqno)
	if seqno >= broadcastFECSeqnoLimit(stream.fec.SymbolsCount) {
		stream.mx.Unlock()
		return fmt.Errorf("too big seqno")
	}

	date = stream.date
	stream.lastMessageAt = time.Now()

	if knownPart, ok := stream.parts[seqno]; ok {
		partData = append([]byte(nil), knownPart...)
		knownPartOnly = true
	} else if stream.encoder != nil {
		partData = stream.encoder.GenSymbol(seqno)
		if stream.parts != nil {
			stream.parts[seqno] = append([]byte(nil), partData...)
		}
	} else {
		stream.mx.Unlock()
		return fmt.Errorf("short part of unfinished broadcast")
	}

	ackCompleted = stream.completedAt != nil
	stream.mx.Unlock()

	if !bytes.Equal(calcBroadcastFECPartDataHash(partData), t.PartDataHash) {
		return fmt.Errorf("wrong part data hash")
	}

	if err := t.VerifySignature(date); err != nil {
		return err
	}

	if ackCompleted {
		return a.sendFECControlMessage(FECCompleted{Hash: t.BroadcastHash})
	}

	if knownPartOnly {
		return nil
	}

	return nil
}
