package keys

import (
	"context"
	"sync"
)

// Memory is an in-process Store/Keeper used for development and tests.
type Memory struct {
	mu     sync.RWMutex
	byHash map[string]*Key
	byID   map[string]*Key
	order  []string
}

func NewMemory() *Memory {
	return &Memory{
		byHash: make(map[string]*Key),
		byID:   make(map[string]*Key),
	}
}

// Upsert adds or replaces a key in the store.
func (m *Memory) Upsert(k *Key) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.put(k)
}

// put assumes the lock is held.
func (m *Memory) put(k *Key) {
	if _, exists := m.byID[k.ID]; !exists {
		m.order = append(m.order, k.ID)
	}
	m.byID[k.ID] = k
	m.byHash[k.Hash] = k
}

func (m *Memory) Lookup(_ context.Context, hash string) (*Key, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	k, ok := m.byHash[hash]
	if !ok {
		return nil, ErrNotFound
	}
	return k, nil
}

func (m *Memory) Create(_ context.Context, k *Key) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.byID[k.ID]; exists {
		return ErrNotFound
	}
	m.put(k)
	return nil
}

func (m *Memory) SetEnabled(_ context.Context, id string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.byID[id]
	if !ok {
		return ErrNotFound
	}
	k.Enabled = enabled
	return nil
}

func (m *Memory) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.byID[id]
	if !ok {
		return ErrNotFound
	}
	delete(m.byID, id)
	delete(m.byHash, k.Hash)
	return nil
}

func (m *Memory) List(_ context.Context) ([]*Key, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Key, 0, len(m.order))
	for _, id := range m.order {
		if k, ok := m.byID[id]; ok {
			out = append(out, k)
		}
	}
	return out, nil
}

func (m *Memory) Close() error { return nil }
