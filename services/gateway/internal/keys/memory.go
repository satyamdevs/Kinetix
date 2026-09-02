package keys

import (
	"context"
	"sync"
)

// Memory is an in-process Store used for development and tests.
type Memory struct {
	mu     sync.RWMutex
	byHash map[string]*Key
}

func NewMemory() *Memory {
	return &Memory{byHash: make(map[string]*Key)}
}

// Upsert adds or replaces a key in the store.
func (m *Memory) Upsert(k *Key) {
	m.mu.Lock()
	defer m.mu.Unlock()
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

func (m *Memory) Close() error { return nil }
