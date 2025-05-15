package dht

import (
	"bytes"
	"sort"
	"sync"
)

type Bucket struct {
	k     uint
	nodes dhtNodeList
	mx    sync.RWMutex
}

func newBucket(k uint) *Bucket {
	b := &Bucket{
		k:     k,
		nodes: make([]*dhtNode, 0),
	}
	return b
}

func (b *Bucket) getNodes() dhtNodeList {
	b.mx.RLock()
	defer b.mx.RUnlock()

	return append(dhtNodeList{}, b.nodes...)
}

func (b *Bucket) getNode(id string) *dhtNode {
	b.mx.RLock()
	defer b.mx.RUnlock()

	for _, n := range b.nodes {
		if n != nil && n.id() == id {
			return n
		}
	}

	return nil
}

func (b *Bucket) findNode(id []byte) *dhtNode {
	b.mx.RLock()
	defer b.mx.RUnlock()

	for _, n := range b.nodes {
		if n != nil && bytes.Equal(n.adnlId, id) {
			return n
		}
	}

	return nil
}

func (b *Bucket) addNode(node *dhtNode) {
	b.mx.Lock()
	defer b.mx.Unlock()
	defer b.sortAndFilter()

	for i, n := range b.nodes {
		if n != nil && bytes.Equal(n.adnlId, node.adnlId) {
			b.nodes[i] = node
			return
		}
	}

	b.nodes = append(b.nodes, node)
}

func (b *Bucket) sortAndFilter() {
	sort.Sort(b.nodes)
	if len(b.nodes) > int(b.k*5) {
		b.nodes = b.nodes[:b.k*5]
	}
}
