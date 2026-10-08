package cell

import "bytes"

const cellLoadCacheSlots = 64

// cellLoadCache keeps short traversals on the stack. Cells already contain
// their hash, so storing only their pointers avoids a second copy of every
// 32-byte key. Larger walks spill once to a map with the inline prefix.
// A caller chooses raw cells or effective views and keys them by that form's
// own hash; lazy-body caches use raw cells, visited sets use effective views.
type cellLoadCache struct {
	inline [cellLoadCacheSlots]*Cell
	spill  map[Hash]*Cell
	count  uint8
}

func (c *cellLoadCache) lookup(hash Hash) *Cell {
	if c.spill != nil {
		return c.spill[hash]
	}
	pos := int(usageCellFingerprint(hash)) & (cellLoadCacheSlots - 1)
	for {
		entry := c.inline[pos]
		if entry == nil {
			return nil
		}
		if bytes.Equal(entry.getHash(_DataCellMaxLevel), hash[:]) {
			return entry
		}
		pos = (pos + 1) & (cellLoadCacheSlots - 1)
	}
}

// store inserts a cell after lookup has established that its hash is absent.
func (c *cellLoadCache) store(hash Hash, value *Cell) {
	if c.spill != nil {
		c.spill[hash] = value
		return
	}
	if c.count == cellLoadCacheSlots/2 {
		c.spill = make(map[Hash]*Cell, cellLoadCacheSlots)
		for _, entry := range c.inline {
			if entry != nil {
				c.spill[entry.HashKey()] = entry
			}
		}
		c.spill[hash] = value
		return
	}
	pos := int(usageCellFingerprint(hash)) & (cellLoadCacheSlots - 1)
	for c.inline[pos] != nil {
		pos = (pos + 1) & (cellLoadCacheSlots - 1)
	}
	c.inline[pos] = value
	c.count++
}
