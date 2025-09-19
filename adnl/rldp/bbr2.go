package rldp

import (
	"fmt"
	"log"
	"sync/atomic"
	"time"
)

type SendClock struct {
	mask      uint32
	startedAt int64
	slots     []atomic.Uint64 // packed: [seqno:32][t_ms:32]
}

func NewSendClock(capPow2 int) *SendClock {
	if capPow2&(capPow2-1) != 0 {
		panic("cap must be power of two")
	}

	s := &SendClock{
		startedAt: time.Now().UnixMilli(),
		mask:      uint32(capPow2 - 1),
		slots:     make([]atomic.Uint64, capPow2),
	}
	return s
}

func pack(seq, ms uint32) uint64       { return (uint64(seq) << 32) | uint64(ms) }
func unpack(v uint64) (seq, ms uint32) { return uint32(v >> 32), uint32(v) }

func (s *SendClock) OnSend(seq uint32, nowMs int64) {
	idx := seq & s.mask
	s.slots[idx].Store(pack(seq, uint32(nowMs-s.startedAt)))
}

func (s *SendClock) SentAt(seq uint32) (ms int64, ok bool) {
	idx := seq & s.mask
	v := s.slots[idx].Load()
	if hi, lo := unpack(v); hi == seq {
		return int64(lo) + s.startedAt, true
	}
	return 0, false
}

type BBRv2Options struct {
	// Time window for bottleneck bandwidth estimation (seconds)
	BtlBwWindowSec int

	// Minimum duration of a gain cycle in ProbeBW (ms)
	ProbeBwCycleMs int64

	// Duration of ProbeRTT phase (ms)
	ProbeRTTDurationMs int64

	// MinRTT staleness timeout (ms): enter ProbeRTT if minRTT hasn't been refreshed longer than this
	MinRTTExpiryMs int64

	// Lower and upper bounds for pacing (bytes/sec)
	MinRate int64
	MaxRate int64 // 0 = no cap

	// Threshold for "high loss" (fraction)
	HighLoss float64 // e.g., 0.02..0.1

	// Beta factor to shrink inflight_hi when losses are high
	Beta float64 // e.g., 0.85

	// Initial "guessed" RTT if ObserveRTT is unavailable
	DefaultRTTMs int64

	// Minimum ACK window duration (ms) to avoid updating too frequently
	MinSampleMs int64

	Name string
}

type BBRv2Controller struct {
	limiter *TokenBucket
	opts    BBRv2Options

	// Accumulators for input deltas
	_total   atomic.Int64
	_recv    atomic.Int64
	_samples atomic.Int64
	lastProc atomic.Int64 // unix ms of the last update

	// BBR state
	state       atomic.Int32 // 0=startup, 1=drain, 2=probebw, 3=probertt
	cycleStamp  atomic.Int64 // start time of the current gain cycle
	cycleIndex  atomic.Int32 // index within the gain table
	fullBW      atomic.Int64 // "full bandwidth" detection
	fullBWCount atomic.Int32

	// Filters and estimates
	btlbw      atomic.Int64 // bytes/sec (max filter)
	minRTT     atomic.Int64 // ms
	minRTTAt   atomic.Int64 // unix ms when minRTT was last updated
	inflight   atomic.Int64 // target inflight (bytes), roughly BtlBw * minRTT
	hiInflight atomic.Int64
	loInflight atomic.Int64

	// Loss accounting for the current window
	lossTotal atomic.Int64
	lossLost  atomic.Int64
	lastAckTs atomic.Int64 // unix ms marking the start of the ACK window

	// Current pacing rate (bytes/sec)
	pacingRate atomic.Int64

	appLimited atomic.Bool

	dbgLast atomic.Int64
}

func NewBBRv2Controller(l *TokenBucket, o BBRv2Options) *BBRv2Controller {
	applyBBRDefaults(&o)
	now := nowMs()
	c := &BBRv2Controller{
		limiter: l,
		opts:    o,
	}
	c.state.Store(0)
	c.cycleStamp.Store(now)
	c.lastProc.Store(now)
	c.lastAckTs.Store(now)

	if o.MinRate > 0 {
		c.pacingRate.Store(o.MinRate)
		l.SetRate(o.MinRate)
	}
	if o.DefaultRTTMs > 0 {
		c.minRTT.Store(o.DefaultRTTMs)
		c.minRTTAt.Store(now)
	} else {
		c.minRTT.Store(25) // 25ms дефолт
		c.minRTTAt.Store(now)
	}

	start := l.GetRate()
	if start <= 0 {
		start = max64(o.MinRate, 1024*64) // 64KiB/s как нижний разумный
	}
	c.btlbw.Store(start)
	c.pacingRate.Store(start)
	c.inflight.Store(rateToInflight(start, c.minRTT.Load()))
	c.hiInflight.Store(c.inflight.Load())
	c.loInflight.Store(0)

	return c
}

func applyBBRDefaults(o *BBRv2Options) {
	if o.BtlBwWindowSec == 0 {
		o.BtlBwWindowSec = 10
	}
	if o.ProbeBwCycleMs == 0 {
		o.ProbeBwCycleMs = 200
	}
	if o.ProbeRTTDurationMs == 0 {
		o.ProbeRTTDurationMs = 150
	}
	if o.MinRTTExpiryMs == 0 {
		o.MinRTTExpiryMs = 10_000 // 10s
	}
	if o.MinRate == 0 {
		o.MinRate = 32 * 1024 // 32 KiB/s
	}
	if o.HighLoss == 0 {
		o.HighLoss = 0.05 // 5%
	}
	if o.Beta == 0 {
		o.Beta = 0.85
	}
	if o.DefaultRTTMs == 0 {
		o.DefaultRTTMs = 25
	}
	if o.MinSampleMs == 0 {
		o.MinSampleMs = 25
	}
}

func (c *BBRv2Controller) SetAppLimited(v bool) { c.appLimited.Store(v) }

func (c *BBRv2Controller) ObserveDelta(total, recv int64) {
	if total == 0 {
		return
	}
	c._total.Add(total)
	c._recv.Add(recv)
	c._samples.Add(1)
	c.maybeUpdate()
}

func (c *BBRv2Controller) ObserveRTT(rttMs int64) {
	if rttMs <= 0 {
		return
	}

	now := nowMs()
	old := c.minRTT.Load()

	if old == 0 || rttMs < old {
		c.minRTT.Store(rttMs)
		c.minRTTAt.Store(now)
	} else if rttMs <= old+max64(1, old/8) { // <= 12.5% from min
		c.minRTTAt.Store(now)
	}

	if btl := c.btlbw.Load(); btl > 0 {
		c.inflight.Store(rateToInflight(btl, c.minRTT.Load()))
	}
}

func (c *BBRv2Controller) Tick() { c.maybeUpdate() }

func (c *BBRv2Controller) maybeUpdate() {
	now := nowMs()

	last := c.lastProc.Load()
	if last+c.opts.MinSampleMs > now {
		return
	}
	if !c.lastProc.CompareAndSwap(last, now) {
		return
	}

	prevAckTs := c.lastAckTs.Swap(now)
	elapsedMs := now - prevAckTs
	if elapsedMs < max64(10, c.opts.MinSampleMs/2) {
		return
	}

	total := c._total.Swap(0)
	acked := c._recv.Swap(0)
	c._samples.Store(0)
	if total <= 0 {
		return
	}

	lost := total - acked
	if lost < 0 {
		lost = 0
	}

	const minAckBytesForLoss = 2 * 1500 // min ~2 MSS confirmed
	if acked >= minAckBytesForLoss {
		c.lossTotal.Add(total)
		c.lossLost.Add(lost)
	}

	if acked > 0 {
		ackRate := int64(float64(acked) * 1000.0 / float64(elapsedMs)) // B/s
		c.updateBtlBw(ackRate, now)
	}

	c.checkProbeRTT(now)
	c.updateModelAndRate(now)

	if now-c.dbgLast.Load() >= 1000 {
		c.dbgLast.Store(now)

		// последний lossRate посчитан внутри updateModelAndRate, но мы можем вывести накопленное за окно:
		lt := c.lossTotal.Load()
		ll := c.lossLost.Load()
		var lossWin float64
		if lt > 0 {
			lossWin = float64(ll) / float64(lt)
		}

		var ackRateBps int64
		if elapsedMs > 0 {
			ackRateBps = int64(float64(acked) * 1000.0 / float64(elapsedMs))
		}
		lossPct := fmt.Sprintf("%.2f%%", lossWin*100.0)

		log.Printf("[BBR] %s win elapsed=%dms acked=%s total=%s loss=%s state=%d appLimited=%v ackRate=%s pacing=%s btlbw=%s minRTT=%dms",
			c.opts.Name,
			elapsedMs,
			humanBytes(acked),
			humanBytes(total),
			lossPct,
			c.state.Load(),
			c.appLimited.Load(),
			humanBps(ackRateBps),
			humanBps(c.pacingRate.Load()),
			humanBps(c.btlbw.Load()),
			c.minRTT.Load(),
		)
	}
}

func humanBps(bps int64) string {
	if bps <= 0 {
		return "0 B/s (0 Mbit/s)"
	}
	miBps := float64(bps) / (1024.0 * 1024.0) // MiB/s
	mbps := float64(bps*8) / 1e6              // мегабит/с (десятичные)
	return fmt.Sprintf("%.2f MiB/s (%.2f Mbit/s)", miBps, mbps)
}

// объём в байтах → "X B / KiB / MiB / GiB"
func humanBytes(n int64) string {
	const (
		KiB = 1024
		MiB = 1024 * KiB
		GiB = 1024 * MiB
	)
	switch {
	case n >= GiB:
		return fmt.Sprintf("%.2f GiB", float64(n)/float64(GiB))
	case n >= MiB:
		return fmt.Sprintf("%.2f MiB", float64(n)/float64(MiB))
	case n >= KiB:
		return fmt.Sprintf("%.2f KiB", float64(n)/float64(KiB))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func (c *BBRv2Controller) updateBtlBw(sample int64, now int64) {
	if sample <= 0 {
		return
	}

	if !c.appLimited.Load() {
		cur := c.btlbw.Load()
		if sample > cur {
			c.btlbw.Store(sample)
		}

		// full bandwidth reached
		if cur > 0 {
			if float64(sample) < float64(cur)*1.25 {
				if c.fullBWCount.Add(1) >= 3 && c.fullBW.Load() == 0 {
					c.fullBW.Store(cur)
				}
			} else {
				c.fullBWCount.Store(0)
				c.fullBW.Store(0)
				c.btlbw.Store(max64(cur, sample))
			}
		}
	}

	// Soft decay of an overly old max (emulates a time window)
	// Every BtlBwWindowSec seconds decrease by 10% if no larger samples arrived
	winMs := int64(c.opts.BtlBwWindowSec * 1000)
	if c.cycleStamp.Load()+winMs < now {
		c.cycleStamp.Store(now) // reuse stamp
		decayed := int64(float64(c.btlbw.Load()) * 0.9)
		if decayed < c.opts.MinRate {
			decayed = c.opts.MinRate
		}
		c.btlbw.Store(decayed)
	}
}

func (c *BBRv2Controller) checkProbeRTT(now int64) {
	if c.state.Load() != 3 && now-c.minRTTAt.Load() > c.opts.MinRTTExpiryMs &&
		!c.appLimited.Load() && (c._recv.Load() > 0) {

		c.state.Store(3)
		c.cycleStamp.Store(now)
	}

	if c.state.Load() == 3 && now-c.cycleStamp.Load() >= c.opts.ProbeRTTDurationMs {
		c.state.Store(2)
		c.cycleStamp.Store(now)
		c.cycleIndex.Store(0)
	}
}

func (c *BBRv2Controller) updateModelAndRate(now int64) {
	state := c.state.Load()
	bw := c.btlbw.Load()
	if bw <= 0 {
		bw = c.opts.MinRate
	}

	// Update inflight target = bw * minRTT
	inflight := rateToInflight(bw, c.minRTT.Load())
	if inflight <= 0 {
		inflight = 2 * 1500 // at least two MSS-equivalents
	}
	c.inflight.Store(inflight)

	// Losses in the last window → decide whether to lower inflight_hi
	var lossRate float64
	lt := c.lossTotal.Swap(0)
	ll := c.lossLost.Swap(0)
	if lt > 0 {
		lossRate = float64(ll) / float64(lt)
	}

	// BBRv2: if loss is high — tighten the upper bound inflight_hi BELOW the model
	hi := c.hiInflight.Load()
	if hi == 0 {
		hi = inflight
	}
	if lossRate >= c.opts.HighLoss {
		// multiplicative decrease like BBRv2
		newHi := int64(float64(hi) * c.opts.Beta)
		// allow going below the model to drain the queue, but keep a sane floor
		floor := max64(2*1500, inflight/2) // >=2*MSS and not below ~0.5*model
		if newHi < floor {
			newHi = floor
		}
		c.hiInflight.Store(newHi)
	} else {
		// Slowly relax upward
		relax := hi + max64(inflight/16, 1500) // +~6% or at least one MSS
		c.hiInflight.Store(min64(relax, inflight*4))
	}

	// Choose pacing_gain by state
	var pacingGain = 1.0
	switch state {
	case 0: // Startup
		pacingGain = 2.885 // classic BBR startup
		// Transition to Drain once "full bandwidth" is reached
		if c.fullBW.Load() > 0 {
			c.state.Store(1)
			c.cycleStamp.Store(now)
		}
	case 1: // Drain
		pacingGain = 1.0 / 2.885
		// Finish drain relatively quickly
		if now-c.cycleStamp.Load() >= 200 {
			c.state.Store(2) // ProbeBW
			c.cycleStamp.Store(now)
			c.cycleIndex.Store(0)
		}
	case 2: // ProbeBW
		// Moderate BBRv2 gain cycle: {1.25, 0.75, 1,1,1,1,1,1}
		gains := [...]float64{1.25, 0.75, 1, 1, 1, 1, 1, 1}
		idx := int(c.cycleIndex.Load())
		if idx < 0 || idx >= len(gains) {
			idx = 0
			c.cycleIndex.Store(0)
		}
		pacingGain = gains[idx]
		// Advance the cycle
		if now-c.cycleStamp.Load() >= c.opts.ProbeBwCycleMs {
			c.cycleStamp.Store(now)
			c.cycleIndex.Store(int32((idx + 1) % len(gains)))
		}
	case 3: // ProbeRTT
		pacingGain = 0.5 // send less to probe RTT
	}

	// Map inflight_hi into a rate cap (upper bound)
	// targetRate = min(bw * pacingGain, hiInflight / minRTT)
	targetByGain := float64(bw) * pacingGain
	minRtt := max64(c.minRTT.Load(), 1)
	hiBytesPerSec := float64(c.hiInflight.Load()) * 1000.0 / float64(minRtt)
	target := min64(int64(targetByGain), int64(hiBytesPerSec))

	// Lower/upper bounds
	if target < c.opts.MinRate {
		target = c.opts.MinRate
	}
	if c.opts.MaxRate > 0 && target > c.opts.MaxRate {
		target = c.opts.MaxRate
	}

	prev := c.pacingRate.Load()
	// Smoothing: limit step changes up/down (except during Startup/ProbeRTT)
	maxUp := int64(float64(prev) * 1.5)
	maxDown := int64(float64(prev) * 0.7)
	if state != 0 && state != 3 { // don't limit in Startup/ProbeRTT
		if target > maxUp {
			target = maxUp
		}
		if target < maxDown {
			target = maxDown
		}
	}

	if target <= 0 {
		target = c.opts.MinRate
	}

	if target != prev {
		c.pacingRate.Store(target)
		c.limiter.SetRate(target)
	}
}

func rateToInflight(rateBytesPerSec int64, rttMs int64) int64 {
	if rateBytesPerSec <= 0 {
		return 0
	}
	if rttMs <= 0 {
		rttMs = 1
	}
	return int64(float64(rateBytesPerSec) * float64(rttMs) / 1000.0)
}

func nowMs() int64 { return time.Now().UnixMilli() }

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
