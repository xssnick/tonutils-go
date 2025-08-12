package rldp

import (
	"math"
	"sync/atomic"
	"time"
)

type AdaptiveRateOptions struct {
	// samples to recalc
	MinSamples int64

	// delay = clamp(DelayBaseMs + rate/DelayPerRateDiv, DelayMinMs, DelayMaxMs).
	DelayBaseMs     int64
	DelayPerRateDiv int64
	DelayMinMs      int64
	DelayMaxMs      int64

	// For EWMA
	TargetLoss float64
	HighLoss   float64

	IncreaseFactor     float64 // +6.25% => 0.0625
	DecreaseFactor     float64 // -33%    => 0.33
	MildDecreaseFactor float64 // -5%     => 0.05

	// if diff less, dont touch rate
	Deadband float64 // 0.02 = 2%

	// smoothing
	EWMAAlpha float64 // 0.1

	MinRate int64
	MaxRate int64 // 0 = no limit

	IncreaseOnlyWhenTokensBelow float64

	EnableSlowStart     bool // fast rate scale at startup when enabled
	SlowStartMultiplier float64
	SlowStartExitLoss   float64
}

type AdaptiveRateController struct {
	limiter *TokenBucket
	opts    AdaptiveRateOptions

	total    atomic.Int64
	recv     atomic.Int64
	samples  atomic.Int64
	lastProc atomic.Int64 // unix ms

	lossEWMA atomic.Uint64 // float64 bits

	inSlowStart atomic.Bool
}

func NewAdaptiveRateController(l *TokenBucket, o AdaptiveRateOptions) *AdaptiveRateController {
	applyDefaults(&o)

	rc := &AdaptiveRateController{
		limiter: l,
		opts:    o,
	}
	if o.EnableSlowStart {
		rc.inSlowStart.Store(true)
	}
	return rc
}

func (rc *AdaptiveRateController) ObserveDelta(total, recv uint32) {
	if total == 0 {
		return
	}

	rc.total.Add(int64(total))
	rc.recv.Add(int64(recv))
	samples := rc.samples.Add(1)

	nowMs := time.Now().UnixMilli()
	rate := rc.limiter.GetRate()

	delay := rc.opts.DelayBaseMs
	if rc.opts.DelayPerRateDiv > 0 {
		delay += rate / rc.opts.DelayPerRateDiv
	}
	if delay < rc.opts.DelayMinMs {
		delay = rc.opts.DelayMinMs
	} else if delay > rc.opts.DelayMaxMs {
		delay = rc.opts.DelayMaxMs
	}

	last := rc.lastProc.Load()
	if samples < rc.opts.MinSamples || last+delay > nowMs {
		return
	}
	if !rc.lastProc.CompareAndSwap(last, nowMs) {
		return
	}

	totalN := rc.total.Swap(0)
	recvN := rc.recv.Swap(0)
	rc.samples.Store(0)

	if totalN <= 0 {
		return
	}

	loss := float64(totalN-recvN) / float64(totalN)

	prevEWMA := math.Float64frombits(rc.lossEWMA.Load())
	if prevEWMA == 0 {
		prevEWMA = loss
	}
	ewma := prevEWMA*(1-rc.opts.EWMAAlpha) + loss*rc.opts.EWMAAlpha
	rc.lossEWMA.Store(math.Float64bits(ewma))

	newRate := rate

	if rc.inSlowStart.Load() {
		// aggressive while loss is low
		if ewma < rc.opts.TargetLoss {
			m := rc.opts.SlowStartMultiplier
			if m < 1.1 {
				m = 2.0
			}
			up := int64(float64(rate) * (m - 1))
			if up < 1 {
				up = 1
			}
			newRate = rate + up
		} else {
			// too high loss
			rc.inSlowStart.Store(false)
		}

		if rc.opts.SlowStartExitLoss > 0 && ewma > rc.opts.SlowStartExitLoss {
			rc.inSlowStart.Store(false)
		}
	}

	if !rc.inSlowStart.Load() {
		switch {
		case ewma < rc.opts.TargetLoss:
			if rc.opts.IncreaseOnlyWhenTokensBelow > 0 {
				tokens := rc.limiter.GetTokensLeft()
				threshold := int64(float64(rate) * rc.opts.IncreaseOnlyWhenTokensBelow)
				if tokens < threshold {
					newRate = rate + int64(float64(rate)*rc.opts.IncreaseFactor)
				}
			} else {
				newRate = rate + int64(float64(rate)*rc.opts.IncreaseFactor)
			}
		case ewma > rc.opts.HighLoss:
			newRate = rate - int64(float64(rate)*rc.opts.DecreaseFactor)
		default:
			newRate = rate - int64(float64(rate)*rc.opts.MildDecreaseFactor)
		}
	}

	if newRate < rc.opts.MinRate {
		newRate = rc.opts.MinRate
	}
	if rc.opts.MaxRate > 0 && newRate > rc.opts.MaxRate {
		newRate = rc.opts.MaxRate
	}

	if rate > 0 {
		diff := math.Abs(float64(newRate-rate)) / float64(rate)
		if diff < rc.opts.Deadband {
			return
		}
	}

	if newRate != rate {
		rc.limiter.SetRate(newRate)
	}
}

func applyDefaults(o *AdaptiveRateOptions) {
	if o.MinSamples == 0 {
		o.MinSamples = 3
	}
	if o.DelayBaseMs == 0 {
		o.DelayBaseMs = 10
	}
	if o.DelayPerRateDiv == 0 {
		o.DelayPerRateDiv = 2000
	}
	if o.DelayMinMs == 0 {
		o.DelayMinMs = 10
	}
	if o.DelayMaxMs == 0 {
		o.DelayMaxMs = 500
	}
	if o.TargetLoss == 0 {
		o.TargetLoss = 0.02
	}
	if o.HighLoss == 0 {
		o.HighLoss = 0.10
	}
	if o.IncreaseFactor == 0 {
		o.IncreaseFactor = 0.0625 // +6.25%
	}
	if o.DecreaseFactor == 0 {
		o.DecreaseFactor = 0.33 // -33%
	}
	if o.MildDecreaseFactor == 0 {
		o.MildDecreaseFactor = 0.05 // -5%
	}
	if o.Deadband == 0 {
		o.Deadband = 0.02
	}
	if o.EWMAAlpha == 0 {
		o.EWMAAlpha = 0.1
	}
	if o.MinRate == 0 {
		o.MinRate = 6000
	}
	if o.IncreaseOnlyWhenTokensBelow == 0 {
		o.IncreaseOnlyWhenTokensBelow = 0.0
	}
	if o.EnableSlowStart {
		if o.SlowStartMultiplier == 0 {
			o.SlowStartMultiplier = 2.0
		}
		if o.SlowStartExitLoss == 0 {
			o.SlowStartExitLoss = o.TargetLoss * 2
		}
	}
}
