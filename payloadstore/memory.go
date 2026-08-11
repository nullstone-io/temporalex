package payloadstore

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

var _ Store = &MemoryStore{}

// MemoryStore is an in-process Store for tests and local development.
// It is safe for concurrent use, which matters because activities run in parallel.
type MemoryStore struct {
	// Bucket is reported on returned Refs. Purely cosmetic; MemoryStore ignores it on read.
	Bucket string

	mu      sync.RWMutex
	objects map[string][]byte
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{Bucket: "memory", objects: map[string][]byte{}}
}

func (m *MemoryStore) Put(ctx context.Context, data []byte) (Ref, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.objects == nil {
		m.objects = map[string][]byte{}
	}
	key := DefaultPrefix + uuid.NewString()
	// Copy: the caller owns data and may reuse the backing array.
	stored := make([]byte, len(data))
	copy(stored, data)
	m.objects[key] = stored
	return Ref{Bucket: m.Bucket, Key: key}, nil
}

func (m *MemoryStore) Get(ctx context.Context, ref Ref) ([]byte, error) {
	if ref.IsZero() {
		return nil, fmt.Errorf("cannot retrieve an empty payload reference")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, ok := m.objects[ref.Key]
	if !ok {
		return nil, fmt.Errorf("payload %s not found", ref)
	}
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}

// Len reports how many payloads are held. Tests use it to assert that a handoff actually
// went through the store rather than the Temporal payload.
func (m *MemoryStore) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.objects)
}
