package dht

import (
	"sort"
	"sync"
)

// priorityList maintains a fixed-size list of nodes ordered by closeness to a target ID.
// It's safe for concurrent use.
// maxLen: maximum number of nodes to keep.
// targetID: ID we're measuring affinity against.
type priorityList struct {
	maxLen   int
	targetID []byte
	items    []*nodePriority
	mu       sync.Mutex
}

// nodePriority holds a node reference along with its computed priority.
type nodePriority struct {
	id       string
	node     *dhtNode
	priority int
	used     bool
}

// newPriorityList constructs a new priorityList with the given capacity and target ID.
func newPriorityList(maxLen int, targetID []byte) *priorityList {
	return &priorityList{
		maxLen:   maxLen,
		targetID: targetID,
		items:    make([]*nodePriority, 0, maxLen),
	}
}

// Add inserts or updates a node's priority. Returns true if the list changed.
func (p *priorityList) Add(node *dhtNode) bool {
	id := node.id()
	pr := int(affinity(node.adnlId, p.targetID))

	p.mu.Lock()
	defer p.mu.Unlock()

	// Update existing
	for _, np := range p.items {
		if np.id == id {
			if pr <= np.priority {
				return false // no improvement
			}
			np.priority = pr
			np.used = false
			p.sort()
			return true
		}
	}

	// Trim if full
	if len(p.items) >= p.maxLen {
		// skip if new is worse than last
		sorted := p.items[len(p.items)-1].priority
		if pr <= sorted {
			return false
		}
		p.items = p.items[:len(p.items)-1]
	}

	// Append new
	p.items = append(p.items, &nodePriority{id: id, node: node, priority: pr})
	p.sort()
	return true
}

// Get returns the next unused node and its priority, marking it used.
// Returns (nil,0) if all nodes are used.
func (p *priorityList) Get() (*dhtNode, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, np := range p.items {
		if !np.used {
			np.used = true
			return np.node, np.priority
		}
	}
	return nil, 0
}

func (p *priorityList) GetBestAffinity() int {
	if len(p.items) == 0 {
		return 0
	}
	return p.items[0].priority
}

// Len reports the current number of items in the list.
func (p *priorityList) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.items)
}

// MarkUsed sets or clears the used flag for a given node, adding it if absent.
func (p *priorityList) MarkUsed(node *dhtNode, used bool) {
	// Ensure it exists
	p.Add(node)

	p.mu.Lock()
	defer p.mu.Unlock()
	id := node.id()
	for _, np := range p.items {
		if np.id == id {
			np.used = used
			break
		}
	}
}

// sort orders items by descending priority.
func (p *priorityList) sort() {
	sort.Slice(p.items, func(i, j int) bool {
		return p.items[i].priority > p.items[j].priority
	})
}
