package audittest

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time interface checks.
var _ port.VersionWriterPort = (*InMemoryStore)(nil)
var _ port.VersionReaderPort = (*InMemoryStore)(nil)

// InMemoryStore is a thread-safe, in-memory version store suitable for
// unit tests and examples. It implements both [port.VersionWriterPort]
// and [port.VersionReaderPort].
//
// Do NOT use in production — data lives only in process memory.
type InMemoryStore struct {
	mu      sync.RWMutex
	records map[string][]domain.VersionRecord // key: entityType+":"+entityID
}

// NewInMemoryStore creates an empty [InMemoryStore].
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		records: make(map[string][]domain.VersionRecord),
	}
}

func (s *InMemoryStore) key(entityType, entityID string) string {
	return entityType + ":" + entityID
}

// Save persists a version record in memory.
func (s *InMemoryStore) Save(_ context.Context, rec domain.VersionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.key(rec.EntityType, rec.EntityID)
	s.records[k] = append(s.records[k], rec)
	return nil
}

// SaveInTx persists a version record. In-memory stores have no real
// transactions, so the tx parameter is ignored.
func (s *InMemoryStore) SaveInTx(_ context.Context, _ port.Transaction, rec domain.VersionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.key(rec.EntityType, rec.EntityID)
	s.records[k] = append(s.records[k], rec)
	return nil
}

// Update overwrites the data of an existing version record in-place.
func (s *InMemoryStore) Update(_ context.Context, rec domain.VersionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.key(rec.EntityType, rec.EntityID)
	for i, r := range s.records[k] {
		if r.Version == rec.Version {
			s.records[k][i] = rec
			return nil
		}
	}
	return domain.ErrNotFound
}

// GetByVersion retrieves a specific version of an entity.
func (s *InMemoryStore) GetByVersion(_ context.Context, entityType, entityID string, ver int64) (domain.VersionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.records[s.key(entityType, entityID)] {
		if r.Version == ver {
			return r, nil
		}
	}
	return domain.VersionRecord{}, domain.ErrNotFound
}

// GetLatest retrieves the most recent version of an entity.
func (s *InMemoryStore) GetLatest(_ context.Context, entityType, entityID string) (domain.VersionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	recs := s.records[s.key(entityType, entityID)]
	if len(recs) == 0 {
		return domain.VersionRecord{}, domain.ErrNotFound
	}
	best := recs[0]
	for _, r := range recs[1:] {
		if r.Version > best.Version {
			best = r
		}
	}
	return best, nil
}

// GetAtTime retrieves the version that was current at the given time.
func (s *InMemoryStore) GetAtTime(_ context.Context, entityType, entityID string, t time.Time) (domain.VersionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var best *domain.VersionRecord
	for i := range s.records[s.key(entityType, entityID)] {
		r := &s.records[s.key(entityType, entityID)][i]
		if !r.CreatedAt.After(t) {
			if best == nil || r.Version > best.Version {
				best = r
			}
		}
	}
	if best == nil {
		return domain.VersionRecord{}, domain.ErrNotFound
	}
	return *best, nil
}

// ListVersions returns a paginated list of version records for an entity.
func (s *InMemoryStore) ListVersions(_ context.Context, entityType, entityID string, opts ...port.ListOption) ([]domain.VersionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	o := port.ApplyListOptions(opts...)

	// Copy the slice to avoid holding the lock during sort.
	recs := make([]domain.VersionRecord, len(s.records[s.key(entityType, entityID)]))
	copy(recs, s.records[s.key(entityType, entityID)])

	// Sort by version.
	sort.Slice(recs, func(i, j int) bool {
		if o.OrderAsc {
			return recs[i].Version < recs[j].Version
		}
		return recs[i].Version > recs[j].Version
	})

	// Apply offset.
	if o.Offset >= len(recs) {
		return nil, nil
	}
	recs = recs[o.Offset:]

	// Apply limit.
	if o.Limit > 0 && o.Limit < len(recs) {
		recs = recs[:o.Limit]
	}

	return recs, nil
}

// Records returns a snapshot of all stored records (for test assertions).
func (s *InMemoryStore) Records() []domain.VersionRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var all []domain.VersionRecord
	for _, recs := range s.records {
		all = append(all, recs...)
	}
	return all
}

// RecordsFor returns a snapshot of records for a specific entity.
func (s *InMemoryStore) RecordsFor(entityType, entityID string) []domain.VersionRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	recs := s.records[s.key(entityType, entityID)]
	out := make([]domain.VersionRecord, len(recs))
	copy(out, recs)
	return out
}

// Len returns the total number of stored records.
func (s *InMemoryStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, recs := range s.records {
		n += len(recs)
	}
	return n
}

// Reset clears all stored records.
func (s *InMemoryStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = make(map[string][]domain.VersionRecord)
}
