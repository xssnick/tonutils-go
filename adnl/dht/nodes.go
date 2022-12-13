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
	used     bool
}

type priorityList struct {
	maxLen int
	list   *nodePriority
	mx     sync.Mutex
}

func newPriorityList(maxLen int) *priorityList {
	p := &priorityList{
		maxLen: maxLen,
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

	res := p.list
	for res != nil && res.used {
		res = res.next
	}

	if res == nil {
		return nil, nil
	}
	res.used = true

	return res.node, res.priority
}
