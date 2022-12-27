package dht

import (
	"encoding/hex"
	"sync"
)

type nodePriority struct {
	id       string
	node     *dhtNode
	priority int
	next     *nodePriority
	used     bool
}

type priorityList struct {
	maxLen   int
	targetId []byte
	list     *nodePriority
	mx       sync.Mutex
}

func newPriorityList(maxLen int, targetId []byte) *priorityList {
	p := &priorityList{
		targetId: targetId,
		maxLen:   maxLen,
	}

	return p
}

func (p *priorityList) addNode(node *dhtNode) bool {
	id := hex.EncodeToString(node.id)

	item := &nodePriority{
		id:       id,
		node:     node,
		priority: node.weight(p.targetId),
	}

	p.mx.Lock()
	defer p.mx.Unlock()

	if p.list == nil {
		p.list = item
		return true
	}

	var prev, cur *nodePriority
	cur = p.list

	i := 0
	for cur != nil {
		if i > p.maxLen {
			return false
		}
		i++

		if cur.id == item.id {
			return false
		}

		if item.priority > cur.priority {
			item.next = cur
			if prev != nil {
				prev.next = item
			} else {
				p.list = item
			}
			break
		}

		if cur.next == nil {
			cur.next = item
			break
		}

		prev, cur = cur, cur.next
	}

	// check all nodes again to find if we already have it in list,
	// if yes, we need to leave only the most prioritized
	saw := false
	prev, cur = nil, p.list
	for cur != nil {
		if cur.id == id {
			if saw {
				// remove it from the list
				prev.next = cur.next
			} else {
				saw = true
			}
		}
		prev, cur = cur, cur.next
	}

	return true
}

func (p *priorityList) getNode() (*dhtNode, int) {
	p.mx.Lock()
	defer p.mx.Unlock()

	res := p.list
	for res != nil && res.used {
		res = res.next
	}

	if res == nil {
		return nil, 0
	}
	res.used = true

	return res.node, res.priority
}

func (p *priorityList) markNotUsed(node *dhtNode) {
	p.mx.Lock()
	defer p.mx.Unlock()

	id := hex.EncodeToString(node.id)

	res := p.list
	for res != nil && res.id == id {
		res.used = false
	}
}
