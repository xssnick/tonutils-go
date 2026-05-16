package dht

import (
	"bytes"
	"sort"
	"sync"
)

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
	return append(dhtNodeList{}, nodes...)
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

	for _, list := range []dhtNodeList{b.active, b.backup} {
		for _, n := range list {
			if n != nil && bytes.Equal(n.adnlId, id) {
				return n
			}
		}
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
		if setActive {
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
		if idx := b.findActiveToDemoteLocked(); idx >= 0 {
			demoted := b.active[idx]
			b.active[idx] = node
			b.addBackupLocked(demoted)
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
	b.promoteReadyLocked()
	b.sortLocked()
}

func (b *Bucket) sortLocked() {
	sort.Sort(b.active)
	sort.Sort(b.backup)
	if len(b.active) > b.k {
		b.backup = append(b.backup, b.active[b.k:]...)
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
		return
	}

	demoteIdx := b.findActiveToDemoteLocked()
	if demoteIdx < 0 {
		return
	}

	demoted := b.active[demoteIdx]
	b.active[demoteIdx] = node
	b.backup[idx] = demoted
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
	for i, node := range b.backup {
		if node == nil {
			return i
		}
		if node.isReady() {
			continue
		}
		if best < 0 || node.failedAt() < b.backup[best].failedAt() {
			best = i
		}
	}
	if best >= 0 {
		return best
	}
	return len(b.backup) - 1
}

func (b *Bucket) findActiveToDemoteLocked() int {
	for i, node := range b.active {
		if node != nil && !node.isReady() {
			return i
		}
	}
	if len(b.active) == 0 {
		return -1
	}
	return len(b.active) - 1
}

func findNodeInList(list dhtNodeList, id []byte) (*dhtNode, int) {
	for i, n := range list {
		if n != nil && bytes.Equal(n.adnlId, id) {
			return n, i
		}
	}
	return nil, -1
}
