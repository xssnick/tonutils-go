package ton

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tl"
)

type retryClient struct {
	maxRetries int
	timeout    time.Duration
	LiteClient
}

type nodeEnricherWrapper struct {
	LiteClient
}

func (w *retryClient) QueryLiteserver(ctx context.Context, payload tl.Serializable, result tl.Serializable) error {
	const maxRounds = 2

	tries, rounds := 0, 0
	ctxBackup := ctx

	for {
		attemptCtx := ctx
		var cancel context.CancelFunc
		if w.timeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, w.timeout)
		}

		err := w.LiteClient.QueryLiteserver(attemptCtx, payload, result)
		if cancel != nil {
			cancel()
		}

		if w.maxRetries > 0 && tries >= w.maxRetries {
			return err
		}
		tries++

		if err != nil {
			if w.timeout > 0 && errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				err = attemptTimeoutError{cause: err}
			}

			if !errors.Is(err, liteclient.ErrADNLReqTimeout) {
				return err
			}

			// try next node
			ctx, err = w.LiteClient.StickyContextNextNodeBalanced(ctx)
			if err != nil {
				rounds++
				if rounds < maxRounds {
					// try same nodes one more time
					ctx = ctxBackup
					continue
				}

				return fmt.Errorf("timeout error received, but failed to try with next node, "+
					"looks like all active nodes was already tried, original error: %w", err)
			}

			continue
		}

		if tmp, ok := result.(*tl.Serializable); ok && tmp != nil {
			if lsErr, ok := (*tmp).(LSError); ok && (lsErr.Code == 651 ||
				lsErr.Code == 652 ||
				lsErr.Code == -400 ||
				lsErr.Code == -503 ||
				lsErr.Code == 502 ||
				lsErr.Code == 228 ||
				lsErr.Code == 429 ||
				(lsErr.Code == 0 && strings.Contains(lsErr.Text, "Failed to get account state"))) {
				if ctx, err = w.LiteClient.StickyContextNextNodeBalanced(ctx); err != nil { // try next node
					rounds++
					if rounds < maxRounds {
						// try same nodes one more time
						ctx = ctxBackup
						continue
					}

					// no more nodes left, return as it is
					return nil
				}
				continue
			}
		}
		return nil
	}
}

func (w *nodeEnricherWrapper) QueryLiteserver(ctx context.Context, payload tl.Serializable, result tl.Serializable) error {
	inf := liteclient.LSInfo{}
	ctx = context.WithValue(ctx, liteclient.CtxLSInfoKey, &inf)

	err := w.LiteClient.QueryLiteserver(ctx, payload, result)
	if err != nil {
		return err
	}

	if tmp, ok := result.(*tl.Serializable); ok && tmp != nil {
		if lsErr, ok := (*tmp).(LSError); ok {
			// enrich with server addr
			lsErr.Servers = inf.Details
			reflect.ValueOf(result).Elem().Set(reflect.ValueOf(lsErr))
		}
	}

	return nil
}
