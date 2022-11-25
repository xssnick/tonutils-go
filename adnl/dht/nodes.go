package dht

import (
	"encoding/hex"
	"math/big"
	"sync"
)

type nodePriority struct {
	id       string
	node     *dhtNode
	priority *big.Int
	next     *nodePriority
}

type priorityList struct {
	maxLen    int
	list      *nodePriority
	usedNodes map[string]bool
	mx        sync.Mutex
}

func newPriorityList(maxLen int) *priorityList {
	p := &priorityList{
		maxLen:    maxLen,
		usedNodes: map[string]bool{},
	}

	return p
}

func (p *priorityList) addNode(node *dhtNode, priority *big.Int) bool {
	id := hex.EncodeToString(node.id)

	item := &nodePriority{
		id:       id,
		node:     node,
		priority: priority,
	}

	p.mx.Lock()
	defer p.mx.Unlock()

	if p.usedNodes[id] {
		// we already processed this node
		return false
	}

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

		if item.priority.Cmp(cur.priority) != 1 {
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

func (p *priorityList) getNode() (*dhtNode, *big.Int) {
	p.mx.Lock()
	defer p.mx.Unlock()

	if p.list == nil {
		return nil, nil
	}

	res := p.list
	p.list = p.list.next

	p.usedNodes[res.id] = true

	return res.node, res.priority
}
