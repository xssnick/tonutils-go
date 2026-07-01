package dht

import (
	"bytes"
	"sort"
	"sync"
	"time"
)

const backupReplaceAfter = time.Minute

type Bucket struct {
	k      int
	active dhtNodeList
	backup dhtNodeList
	mx     sync.RWMutex
}

func newBucket(k int) *Bucket {
	return &Bucket{
		k:      k,
		active: make(dhtNodeList, 0, k),
		backup: make(dhtNodeList, 0, k),
	}
}

func (b *Bucket) getNodes() dhtNodeList {
	b.mx.RLock()
	defer b.mx.RUnlock()

	nodes := make(dhtNodeList, 0, len(b.active)+len(b.backup))
	nodes = append(nodes, b.active...)
	nodes = append(nodes, b.backup...)
	return nodes
}

func (b *Bucket) getActiveNodes() dhtNodeList {
	b.mx.RLock()
	defer b.mx.RUnlock()

	return append(dhtNodeList{}, b.active...)
}

func (b *Bucket) activeCount() int {
	b.mx.RLock()
	defer b.mx.RUnlock()
	return len(b.active)
}

func (b *Bucket) findNode(id []byte) *dhtNode {
	b.mx.RLock()
	defer b.mx.RUnlock()

	if node, _ := findNodeInList(b.active, id); node != nil {
		return node
	}
	if node, _ := findNodeInList(b.backup, id); node != nil {
		return node
	}
	return nil
}

func (b *Bucket) addNode(node *dhtNode, setActive bool) *dhtNode {
	b.mx.Lock()
	defer b.mx.Unlock()

	if node == nil {
		return nil
	}

	if existing, idx := findNodeInList(b.active, node.adnlId); existing != nil {
		existing.absorb(node)
		if setActive {
			existing.markPingSuccess()
		}
		b.active[idx] = existing
		sort.Sort(b.active)
		return existing
	}

	if existing, idx := findNodeInList(b.backup, node.adnlId); existing != nil {
		existing.absorb(node)
		if setActive {
			existing.markPingSuccess()
		}
		b.backup[idx] = existing
		if setActive && len(b.active) < b.k {
			b.promoteBackupLocked(idx)
		}
		b.sortLocked()
		return existing
	}

	if setActive {
		node.markPingSuccess()
		if len(b.active) < b.k {
			b.active = append(b.active, node)
			b.sortLocked()
			return node
		}
	}

	b.addBackupLocked(node)
	b.promoteReadyLocked()
	b.sortLocked()
	return node
}

func (b *Bucket) promoteReady() {
	b.mx.Lock()
	defer b.mx.Unlock()
	b.demoteUnreadyActiveLocked()
	b.promoteReadyLocked()
	b.sortLocked()
}

func (b *Bucket) sortLocked() {
	sort.Sort(b.active)
	sort.Sort(b.backup)
	if len(b.active) > b.k {
		for _, node := range b.active[b.k:] {
			b.addBackupLocked(node)
		}
		b.active = b.active[:b.k]
	}
	if len(b.backup) > b.k {
		b.backup = b.backup[:b.k]
	}
}

func (b *Bucket) promoteReadyLocked() {
	for len(b.active) < b.k {
		idx := -1
		for i, node := range b.backup {
			if node != nil && node.isReady() {
				idx = i
				break
			}
		}
		if idx < 0 {
			return
		}
		b.promoteBackupLocked(idx)
	}
}

func (b *Bucket) promoteBackupLocked(idx int) {
	if idx < 0 || idx >= len(b.backup) {
		return
	}

	node := b.backup[idx]
	if node == nil {
		return
	}
	if len(b.active) < b.k {
		b.active = append(b.active, node)
		b.backup = append(b.backup[:idx], b.backup[idx+1:]...)
	}
}

func (b *Bucket) addBackupLocked(node *dhtNode) {
	if node == nil {
		return
	}
	if len(b.backup) < b.k {
		b.backup = append(b.backup, node)
		return
	}

	idx := b.selectBackupToDropLocked()
	if idx >= 0 && idx < len(b.backup) {
		b.backup[idx] = node
	}
}

func (b *Bucket) selectBackupToDropLocked() int {
	if len(b.backup) == 0 {
		return -1
	}

	best := -1
	var bestFailed int64
	oldestAllowed := time.Now().Add(-backupReplaceAfter).UnixNano()
	for i, node := range b.backup {
		if node == nil {
			return i
		}
		if node.isReady() {
			continue
		}
		failedAt := node.failedAt()
		if failedAt > oldestAllowed {
			continue
		}
		if best < 0 || failedAt < bestFailed {
			best = i
			bestFailed = failedAt
		}
	}
	if best >= 0 {
		return best
	}
	return -1
}

func (b *Bucket) demoteUnreadyActiveLocked() {
	active := b.active[:0]
	for _, node := range b.active {
		if node == nil {
			continue
		}
		if !node.isReady() {
			b.addBackupLocked(node)
			continue
		}
		active = append(active, node)
	}
	b.active = active
}

func findNodeInList(list dhtNodeList, id []byte) (*dhtNode, int) {
	for i, n := range list {
		if n != nil && bytes.Equal(n.adnlId, id) {
			return n, i
		}
	}
	return nil, -1
}
