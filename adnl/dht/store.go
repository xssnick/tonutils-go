package dht

import (
	"container/list"
	"sync"
)

type ValueStore interface {
	Get(keyID []byte) (*Value, error)
	Put(keyID []byte, value *Value) error
	Delete(keyID []byte) error
	ForEach(fn func(keyID []byte, value *Value) error) error
	Close() error
}

type valueStoreKeyLister interface {
	Keys() ([][]byte, error)
}

type valueStoreExpiredCleaner interface {
	DeleteExpired(now int64) error
}

type MemoryValueStore struct {
	mx      sync.RWMutex
	maxKeys int
	values  map[string]*memoryValueStoreItem
	order   *list.List
}

type memoryValueStoreItem struct {
	value *Value
	elem  *list.Element
}

func NewMemoryValueStore(maxKeys int) *MemoryValueStore {
	return &MemoryValueStore{
		maxKeys: maxKeys,
		values:  map[string]*memoryValueStoreItem{},
		order:   list.New(),
	}
}

func (m *MemoryValueStore) Get(keyID []byte) (*Value, error) {
	m.mx.RLock()
	defer m.mx.RUnlock()

	item := m.values[string(keyID)]
	if item == nil {
		return nil, nil
	}
	return cloneValue(item.value), nil
}

func (m *MemoryValueStore) Put(keyID []byte, value *Value) error {
	m.mx.Lock()
	defer m.mx.Unlock()

	key := string(keyID)
	cloned := cloneValue(value)

	if item := m.values[key]; item != nil {
		item.value = cloned
		m.order.MoveToBack(item.elem)
		return nil
	}

	elem := m.order.PushBack(key)
	m.values[key] = &memoryValueStoreItem{
		value: cloned,
		elem:  elem,
	}

	if m.maxKeys > 0 {
		for len(m.values) > m.maxKeys {
			oldest := m.order.Front()
			if oldest == nil {
				break
			}

			oldestKey := oldest.Value.(string)
			delete(m.values, oldestKey)
			m.order.Remove(oldest)
		}
	}

	return nil
}

func (m *MemoryValueStore) Delete(keyID []byte) error {
	m.mx.Lock()
	defer m.mx.Unlock()

	key := string(keyID)
	item := m.values[key]
	if item == nil {
		return nil
	}

	delete(m.values, key)
	m.order.Remove(item.elem)
	return nil
}

func (m *MemoryValueStore) ForEach(fn func(keyID []byte, value *Value) error) error {
	m.mx.RLock()
	keys := make([]string, 0, len(m.values))
	for keyID := range m.values {
		keys = append(keys, keyID)
	}
	m.mx.RUnlock()

	for _, keyID := range keys {
		m.mx.RLock()
		item := m.values[keyID]
		var value *Value
		if item != nil {
			value = cloneValue(item.value)
		}
		m.mx.RUnlock()
		if item == nil {
			continue
		}
		if err := fn([]byte(keyID), value); err != nil {
			return err
		}
	}
	return nil
}

func (m *MemoryValueStore) Keys() ([][]byte, error) {
	m.mx.RLock()
	defer m.mx.RUnlock()

	keys := make([][]byte, 0, len(m.values))
	for keyID := range m.values {
		keys = append(keys, []byte(keyID))
	}
	return keys, nil
}

func (m *MemoryValueStore) DeleteExpired(now int64) error {
	m.mx.Lock()
	defer m.mx.Unlock()

	for keyID, item := range m.values {
		if item == nil || item.value == nil || int64(item.value.TTL) <= now {
			delete(m.values, keyID)
			if item != nil && item.elem != nil {
				m.order.Remove(item.elem)
			}
		}
	}
	return nil
}

func (m *MemoryValueStore) Close() error {
	return nil
}

func cloneValue(value *Value) *Value {
	if value == nil {
		return nil
	}

	return &Value{
		KeyDescription: KeyDescription{
			Key: Key{
				ID:    append([]byte{}, value.KeyDescription.Key.ID...),
				Name:  append([]byte{}, value.KeyDescription.Key.Name...),
				Index: value.KeyDescription.Key.Index,
			},
			ID:         value.KeyDescription.ID,
			UpdateRule: value.KeyDescription.UpdateRule,
			Signature:  append([]byte{}, value.KeyDescription.Signature...),
		},
		Data:      append([]byte{}, value.Data...),
		TTL:       value.TTL,
		Signature: append([]byte{}, value.Signature...),
	}
}
