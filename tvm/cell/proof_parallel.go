package cell

import (
	"sync"
	"sync/atomic"
)

const (
	proofParallelCacheShards = 16
	proofParallelMinCells    = 256
)

type proofParallelCacheShard struct {
	mu sync.RWMutex

	boundaries map[proofBodyKey]*Cell
}

// proofParallelCache is shared only by the bounded branches of one destination
// proof walk. Cells and build memos stay branch-local; the shared state contains
// only the boundaries the paired source proof reuses and the next-walk size
// observation.
type proofParallelCache struct {
	shards [proofParallelCacheShards]proofParallelCacheShard
	memo   atomic.Int64
}

func newProofParallelCache(memoHint int) *proofParallelCache {
	cache := new(proofParallelCache)
	boundaryHint := memoHint/2/proofParallelCacheShards + 1
	for i := range cache.shards {
		cache.shards[i].boundaries = make(map[proofBodyKey]*Cell, boundaryHint)
	}

	return cache
}

func (c *proofParallelCache) shard(key proofBodyKey) *proofParallelCacheShard {
	fingerprint := proofBuildFingerprint(key.hash, key.merkleDepth)
	return &c.shards[fingerprint&(proofParallelCacheShards-1)]
}

func (c *proofParallelCache) size() int {
	return int(c.memo.Load())
}

func (c *proofParallelCache) loadBoundary(key proofBodyKey) (*Cell, bool) {
	shard := c.shard(key)
	shard.mu.RLock()
	boundary, ok := shard.boundaries[key]
	shard.mu.RUnlock()

	return boundary, ok
}

func (c *proofParallelCache) storeBoundary(key proofBodyKey, boundary *Cell) {
	shard := c.shard(key)
	shard.mu.Lock()
	if _, exists := shard.boundaries[key]; !exists {
		shard.boundaries[key] = boundary
	}
	shard.mu.Unlock()
}

type proofParallelPlan struct {
	branch           int
	branchWorkers    int
	remainingWorkers int
	branchHint       int
	remainingHint    int
}

// planProofParallelBranch gives one significant reference to a new goroutine;
// the caller continues through all remaining references. Cached cell depth is a
// cheap proxy for subtree work and prevents a shallow sibling from taking half
// of the worker budget. The hint stops small proofs before goroutine overhead can
// dominate them.
func planProofParallelBranch(refs []*Cell, workers, hint int) (proofParallelPlan, bool) {
	if workers <= 1 || len(refs) < 2 || hint < proofParallelMinCells {
		return proofParallelPlan{}, false
	}

	totalWeight := 0
	branchWeight := 0
	branch := 0
	for i, ref := range refs {
		weight := int(ref.Depth()) + 1
		totalWeight += weight
		if weight > branchWeight {
			branch = i
			branchWeight = weight
		}
	}
	if branchWeight*workers < totalWeight {
		return proofParallelPlan{}, false
	}

	branchWorkers := workers * branchWeight / totalWeight
	branchWorkers = max(1, min(branchWorkers, workers-1))
	branchHint := hint * branchWeight / totalWeight
	branchHint = max(1, min(branchHint, hint-1))

	return proofParallelPlan{
		branch:           branch,
		branchWorkers:    branchWorkers,
		remainingWorkers: workers - branchWorkers,
		branchHint:       branchHint,
		remainingHint:    hint - branchHint,
	}, true
}
