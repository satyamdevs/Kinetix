package limits

import (
	"context"
	"sync"
	"time"
)

// Memory implements Limiter in-process for tests and dev without Redis.
type Memory struct {
	mu       sync.Mutex
	requests map[string]map[int64]int64
	tokens   map[string]int64
}

func NewMemory() *Memory {
	return &Memory{
		requests: make(map[string]map[int64]int64),
		tokens:   make(map[string]int64),
	}
}

func (m *Memory) Allow(_ context.Context, id string, limit int64, window time.Duration) (Decision, error) {
	if limit <= 0 {
		return Decision{OK: true}, nil
	}
	if window <= 0 {
		window = DefaultWindow
	}
	bucket := time.Now().Unix() / int64(window.Seconds())

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.requests[id] == nil {
		m.requests[id] = make(map[int64]int64)
	}
	count := m.requests[id][bucket] + 1
	m.requests[id][bucket] = count
	return Decision{OK: count <= limit, Remaining: limit - count}, nil
}

func (m *Memory) Tokens(_ context.Context, id string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tokens[id], nil
}

func (m *Memory) AddTokens(_ context.Context, id string, n, cap int64) (Decision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := m.tokens[id] + n
	m.tokens[id] = total
	if cap > 0 && total > cap {
		return Decision{OK: false, Remaining: cap - total}, nil
	}
	return Decision{OK: true, Remaining: total}, nil
}

func (m *Memory) ResetTokens(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tokens, id)
	return nil
}
