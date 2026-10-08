package cell

const (
	merkleProofCombineCellInlineBuckets  = 64
	merkleProofCombineStateInlineBuckets = 32
)

// merkleProofCombineCellTable stores the loaded representations available for
// every proof hash. Proof combines are usually small enough to stay in the
// inline buckets; larger proofs grow geometrically without sizing from depth.
type merkleProofCombineCellTable struct {
	count   int
	buckets []merkleProofCombineCellBucket
	inline  [merkleProofCombineCellInlineBuckets]merkleProofCombineCellBucket
}

type merkleProofCombineCellBucket struct {
	hash Hash
	info merkleProofCombineInfo
	used bool
}

func (t *merkleProofCombineCellTable) lookup(hash Hash) (*merkleProofCombineInfo, bool) {
	buckets := t.currentBuckets()
	mask := len(buckets) - 1
	pos := int(usageCellFingerprint(hash)) & mask
	for {
		bucket := &buckets[pos]
		if !bucket.used {
			return nil, false
		}
		if bucket.hash == hash {
			return &bucket.info, true
		}
		pos = (pos + 1) & mask
	}
}

func (t *merkleProofCombineCellTable) getOrInsert(hash Hash) *merkleProofCombineInfo {
	if info, ok := t.lookup(hash); ok {
		return info
	}

	buckets := t.currentBuckets()
	if (t.count+1)*4 > len(buckets)*3 {
		t.grow()
		buckets = t.buckets
	}

	mask := len(buckets) - 1
	pos := int(usageCellFingerprint(hash)) & mask
	for buckets[pos].used {
		pos = (pos + 1) & mask
	}
	bucket := &buckets[pos]
	bucket.hash = hash
	bucket.used = true
	t.count++
	return &bucket.info
}

func (t *merkleProofCombineCellTable) currentBuckets() []merkleProofCombineCellBucket {
	if t.buckets != nil {
		return t.buckets
	}
	return t.inline[:]
}

func (t *merkleProofCombineCellTable) grow() {
	old := t.currentBuckets()
	buckets := make([]merkleProofCombineCellBucket, len(old)*2)
	mask := len(buckets) - 1
	for i := range old {
		if !old[i].used {
			continue
		}
		pos := int(usageCellFingerprint(old[i].hash)) & mask
		for buckets[pos].used {
			pos = (pos + 1) & mask
		}
		buckets[pos] = old[i]
	}
	t.buckets = buckets
}

// merkleProofCombineStateTable combines the visited and ready maps. The two
// namespaces remain independent through separate fields on each key: load uses
// the actual Merkle depth, while create uses the output proof depth.
type merkleProofCombineStateTable struct {
	count   int
	buckets []merkleProofCombineStateBucket
	inline  [merkleProofCombineStateInlineBuckets]merkleProofCombineStateBucket
}

type merkleProofCombineStateBucket struct {
	hash        Hash
	merkleDepth int32
	ready       *Cell
	used        bool
	visited     bool
}

func (t *merkleProofCombineStateTable) wasVisited(hash Hash, merkleDepth int) bool {
	bucket := t.lookup(hash, merkleDepth)
	return bucket != nil && bucket.visited
}

func (t *merkleProofCombineStateTable) markVisited(hash Hash, merkleDepth int) {
	t.getOrInsert(hash, merkleDepth).visited = true
}

func (t *merkleProofCombineStateTable) readyCell(hash Hash, proofDepth int) *Cell {
	bucket := t.lookup(hash, proofDepth)
	if bucket == nil {
		return nil
	}
	return bucket.ready
}

func (t *merkleProofCombineStateTable) storeReady(hash Hash, proofDepth int, ready *Cell) {
	t.getOrInsert(hash, proofDepth).ready = ready
}

func (t *merkleProofCombineStateTable) lookup(hash Hash, merkleDepth int) *merkleProofCombineStateBucket {
	buckets := t.currentBuckets()
	mask := len(buckets) - 1
	pos := int(proofBuildFingerprint(hash, merkleDepth)) & mask
	for {
		bucket := &buckets[pos]
		if !bucket.used {
			return nil
		}
		if bucket.merkleDepth == int32(merkleDepth) && bucket.hash == hash {
			return bucket
		}
		pos = (pos + 1) & mask
	}
}

func (t *merkleProofCombineStateTable) getOrInsert(hash Hash, merkleDepth int) *merkleProofCombineStateBucket {
	if bucket := t.lookup(hash, merkleDepth); bucket != nil {
		return bucket
	}

	buckets := t.currentBuckets()
	if (t.count+1)*4 > len(buckets)*3 {
		t.grow()
		buckets = t.buckets
	}

	mask := len(buckets) - 1
	pos := int(proofBuildFingerprint(hash, merkleDepth)) & mask
	for buckets[pos].used {
		pos = (pos + 1) & mask
	}
	bucket := &buckets[pos]
	bucket.hash = hash
	bucket.merkleDepth = int32(merkleDepth)
	bucket.used = true
	t.count++
	return bucket
}

func (t *merkleProofCombineStateTable) currentBuckets() []merkleProofCombineStateBucket {
	if t.buckets != nil {
		return t.buckets
	}
	return t.inline[:]
}

func (t *merkleProofCombineStateTable) grow() {
	old := t.currentBuckets()
	newSize := len(old) * 2
	if t.buckets == nil {
		newSize = len(old) * 4
	}
	buckets := make([]merkleProofCombineStateBucket, newSize)
	mask := len(buckets) - 1
	for i := range old {
		if !old[i].used {
			continue
		}
		pos := int(proofBuildFingerprint(old[i].hash, int(old[i].merkleDepth))) & mask
		for buckets[pos].used {
			pos = (pos + 1) & mask
		}
		buckets[pos] = old[i]
	}
	t.buckets = buckets
}
