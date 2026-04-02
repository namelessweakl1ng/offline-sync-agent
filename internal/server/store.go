package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"offline-sync-agent/internal/models"
)

type Store interface {
	ApplyOperation(ctx context.Context, op models.Operation) (models.SyncResult, error)
	PullSince(ctx context.Context, since int64) ([]models.Record, error)
	Close() error
}

type MemoryStore struct {
	mu      sync.RWMutex
	records map[string]models.Record
}

func NewStore(kind string) (Store, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "memory", "inmemory", "in-memory":
		return &MemoryStore{
			records: make(map[string]models.Record),
		}, nil
	case "postgres":
		return nil, fmt.Errorf("postgres storage is not implemented yet; the backend now exposes a Store abstraction so a Postgres implementation can be added without changing the HTTP layer")
	default:
		return nil, fmt.Errorf("unsupported backend store %q", kind)
	}
}

func (s *MemoryStore) ApplyOperation(_ context.Context, op models.Operation) (models.SyncResult, error) {
	op = op.Normalized()
	if err := op.Validate(); err != nil {
		return models.SyncResult{
			ID:      op.ID,
			Status:  models.SyncStatusInvalid,
			Message: err.Error(),
		}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current, exists := s.records[op.ID]
	if exists && op.Version <= current.Version {
		return models.SyncResult{
			ID:      op.ID,
			Status:  models.SyncStatusConflict,
			Version: current.Version,
			Data:    current.Data,
			Message: "client version is stale",
		}, nil
	}

	if op.Type == models.DELETE {
		delete(s.records, op.ID)
		return models.SyncResult{
			ID:      op.ID,
			Status:  models.SyncStatusOK,
			Version: op.Version,
		}, nil
	}

	record := models.Record{
		ID:        op.ID,
		Data:      op.Data,
		Version:   op.Version,
		UpdatedAt: time.Now().Unix(),
	}
	s.records[op.ID] = record

	return models.SyncResult{
		ID:      op.ID,
		Status:  models.SyncStatusOK,
		Version: record.Version,
	}, nil
}

func (s *MemoryStore) PullSince(_ context.Context, since int64) ([]models.Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	records := make([]models.Record, 0, len(s.records))
	for _, record := range s.records {
		if record.UpdatedAt > since {
			records = append(records, record)
		}
	}

	sort.Slice(records, func(i int, j int) bool {
		if records[i].UpdatedAt == records[j].UpdatedAt {
			return records[i].ID < records[j].ID
		}

		return records[i].UpdatedAt < records[j].UpdatedAt
	})

	return records, nil
}

func (s *MemoryStore) Close() error {
	return nil
}
