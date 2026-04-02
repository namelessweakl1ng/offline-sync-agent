package syncer

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"offline-sync-agent/internal/models"
	"offline-sync-agent/internal/network"
)

type mockQueueRepository struct {
	lastSync     int64
	unsynced     []models.Operation
	markedSynced []string
	saved        []models.Operation
	deleted      []string
	conflicts    []models.ConflictRecord
	requeued     []models.Operation
	cleanupCalls int
	highPriority bool
}

type mockRemoteClient struct {
	status       network.Status
	pushResults  []models.SyncResponse
	pullResponse models.PullResponse
}

func (m *mockQueueRepository) GetLastSync(context.Context) (int64, error) {
	return m.lastSync, nil
}

func (m *mockQueueRepository) SetLastSync(_ context.Context, ts int64) error {
	m.lastSync = ts
	return nil
}

func (m *mockQueueRepository) GetUnsynced(context.Context) ([]models.Operation, error) {
	return append([]models.Operation(nil), m.unsynced...), nil
}

func (m *mockQueueRepository) MarkSynced(_ context.Context, id string) error {
	m.markedSynced = append(m.markedSynced, id)
	return nil
}

func (m *mockQueueRepository) CleanupSynced(context.Context) error {
	m.cleanupCalls++
	return nil
}

func (m *mockQueueRepository) SaveSyncedData(_ context.Context, op models.Operation) error {
	m.saved = append(m.saved, op)
	return nil
}

func (m *mockQueueRepository) DeleteSyncedData(_ context.Context, id string) error {
	m.deleted = append(m.deleted, id)
	return nil
}

func (m *mockQueueRepository) LogConflict(_ context.Context, op models.Operation, strategy models.ConflictStrategy) error {
	m.conflicts = append(m.conflicts, models.ConflictRecord{
		ID:        op.ID,
		Data:      op.Data,
		Version:   op.Version,
		Timestamp: op.Timestamp,
		Status:    "unresolved",
		Strategy:  strategy,
	})
	return nil
}

func (m *mockQueueRepository) AddOperation(_ context.Context, op models.Operation) error {
	m.requeued = append(m.requeued, op)
	return nil
}

func (m *mockQueueRepository) PendingHighPriority(context.Context) (bool, error) {
	return m.highPriority, nil
}

func (m *mockRemoteClient) Check(context.Context) (network.Status, error) {
	return m.status, nil
}

func (m *mockRemoteClient) Push(_ context.Context, _ []models.Operation) (models.SyncResponse, error) {
	result := m.pushResults[0]
	m.pushResults = m.pushResults[1:]
	return result, nil
}

func (m *mockRemoteClient) Pull(context.Context, int64) (models.PullResponse, error) {
	return m.pullResponse, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSyncNowMarksOperationsSyncedAndPullsUpdates(t *testing.T) {
	repo := &mockQueueRepository{
		unsynced: []models.Operation{{
			ID:        "record-1",
			Type:      models.CREATE,
			Data:      "local",
			Timestamp: 1,
			Version:   1,
			Priority:  models.HighPriority,
		}},
	}
	remote := &mockRemoteClient{
		status: network.Status{
			Online:  true,
			Latency: 50 * time.Millisecond,
			Quality: network.QualityFast,
		},
		pushResults: []models.SyncResponse{{
			Results: []models.SyncResult{{
				ID:      "record-1",
				Status:  models.SyncStatusOK,
				Version: 2,
			}},
		}},
		pullResponse: models.PullResponse{
			Data: []models.Record{{
				ID:        "record-2",
				Data:      "server",
				Version:   3,
				UpdatedAt: 25,
			}},
		},
	}

	service := NewService(repo, remote, testLogger(), 1)
	summary, err := service.SyncNow(context.Background())
	if err != nil {
		t.Fatalf("sync now: %v", err)
	}

	if summary.Synced != 1 {
		t.Fatalf("expected 1 synced record, got %d", summary.Synced)
	}

	if summary.Pulled != 1 {
		t.Fatalf("expected 1 pulled record, got %d", summary.Pulled)
	}

	if len(repo.markedSynced) != 1 || repo.markedSynced[0] != "record-1" {
		t.Fatalf("expected record-1 to be marked synced, got %+v", repo.markedSynced)
	}

	if repo.cleanupCalls != 1 {
		t.Fatalf("expected cleanup to run once, got %d", repo.cleanupCalls)
	}

	if repo.lastSync != 25 {
		t.Fatalf("expected last sync timestamp 25, got %d", repo.lastSync)
	}
}

func TestSyncNowRequeuesMergedConflict(t *testing.T) {
	repo := &mockQueueRepository{
		unsynced: []models.Operation{{
			ID:        "record-1",
			Type:      models.UPDATE,
			Data:      "local",
			Timestamp: 5,
			Version:   3,
			Priority:  models.HighPriority,
		}},
	}
	remote := &mockRemoteClient{
		status: network.Status{
			Online:  true,
			Latency: 50 * time.Millisecond,
			Quality: network.QualityFast,
		},
		pushResults: []models.SyncResponse{{
			Results: []models.SyncResult{{
				ID:      "record-1",
				Status:  models.SyncStatusConflict,
				Version: 7,
				Data:    "server",
			}},
		}},
	}

	service := NewService(repo, remote, testLogger(), 1)
	summary, err := service.SyncNow(context.Background())
	if err != nil {
		t.Fatalf("sync now: %v", err)
	}

	if summary.Conflicts != 1 {
		t.Fatalf("expected 1 conflict, got %d", summary.Conflicts)
	}

	if len(repo.conflicts) != 1 {
		t.Fatalf("expected one conflict log entry, got %d", len(repo.conflicts))
	}

	if repo.conflicts[0].Strategy != models.MERGED {
		t.Fatalf("expected merged conflict strategy, got %s", repo.conflicts[0].Strategy)
	}

	if len(repo.requeued) != 1 {
		t.Fatalf("expected one requeued operation, got %d", len(repo.requeued))
	}

	if repo.requeued[0].Version != 8 {
		t.Fatalf("expected requeued version 8, got %d", repo.requeued[0].Version)
	}

	if repo.requeued[0].Data != "local | server" {
		t.Fatalf("expected merged payload, got %q", repo.requeued[0].Data)
	}
}
