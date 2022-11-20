package dht

import (
	"github.com/xssnick/tonutils-go/adnl"
	"sync"
)

type nodePriority struct {
	id       string
	conn     *adnl.ADNL
	priority int
	next     *nodePriority
}

type priorityList struct {
	list      *nodePriority
	usedNodes map[string]bool
	mx        sync.Mutex
}

func newPriorityList(root map[string]*adnl.ADNL) *priorityList {
	p := &priorityList{
		usedNodes: map[string]bool{},
	}
	for id, node := range root {
		p.list = &nodePriority{
			id:       id,
			conn:     node,
			priority: 0,
			next:     p.list,
		}
	}
	return p
}

func (p *priorityList) addNode(id string, conn *adnl.ADNL, priority int) bool {
	item := &nodePriority{
		id:       id,
		conn:     conn,
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
	for cur != nil {
		if item.priority >= cur.priority {
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

func (p *priorityList) getNode() (*adnl.ADNL, int) {
	p.mx.Lock()
	defer p.mx.Unlock()

	if p.list == nil {
		return nil, -1
	}

	res := p.list
	p.list = p.list.next

	p.usedNodes[res.id] = true

	return res.conn, res.priority
}
