package queue

import (
	"context"
	"testing"

	"offline-sync-agent/internal/db"
	"offline-sync-agent/internal/models"
)

func newTestRepository(t *testing.T) *Repository {
	t.Helper()

	client, err := db.OpenTest()
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}

	t.Cleanup(func() {
		_ = client.Close()
	})

	return NewRepository(client)
}

func TestAddOperationUpsertsLatestVersion(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	err := repo.AddOperation(ctx, models.Operation{
		ID:        "record-1",
		Type:      models.CREATE,
		Data:      "first",
		Timestamp: 1,
		Version:   1,
		Priority:  models.DefaultPriority,
	})
	if err != nil {
		t.Fatalf("add first operation: %v", err)
	}

	err = repo.AddOperation(ctx, models.Operation{
		ID:        "record-1",
		Type:      models.UPDATE,
		Data:      "second",
		Timestamp: 2,
		Version:   2,
		Priority:  models.HighPriority,
	})
	if err != nil {
		t.Fatalf("add replacement operation: %v", err)
	}

	operations, err := repo.GetUnsynced(ctx)
	if err != nil {
		t.Fatalf("get unsynced operations: %v", err)
	}

	if len(operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(operations))
	}

	got := operations[0]
	if got.Type != models.UPDATE {
		t.Fatalf("expected operation type %s, got %s", models.UPDATE, got.Type)
	}

	if got.Data != "second" {
		t.Fatalf("expected latest payload to be saved, got %q", got.Data)
	}

	if got.Version != 2 {
		t.Fatalf("expected version 2, got %d", got.Version)
	}

	if got.Priority != models.HighPriority {
		t.Fatalf("expected priority %d, got %d", models.HighPriority, got.Priority)
	}
}

func TestPendingHighPriority(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	err := repo.AddOperation(ctx, models.Operation{
		ID:        "low-priority",
		Type:      models.CREATE,
		Data:      "payload",
		Timestamp: 1,
		Version:   1,
		Priority:  models.DefaultPriority,
	})
	if err != nil {
		t.Fatalf("add operation: %v", err)
	}

	hasHighPriority, err := repo.PendingHighPriority(ctx)
	if err != nil {
		t.Fatalf("check high priority status: %v", err)
	}

	if hasHighPriority {
		t.Fatalf("did not expect high-priority work before adding one")
	}

	err = repo.AddOperation(ctx, models.Operation{
		ID:        "high-priority",
		Type:      models.CREATE,
		Data:      "payload",
		Timestamp: 2,
		Version:   2,
		Priority:  models.HighPriority,
	})
	if err != nil {
		t.Fatalf("add high-priority operation: %v", err)
	}

	hasHighPriority, err = repo.PendingHighPriority(ctx)
	if err != nil {
		t.Fatalf("check high priority status after insert: %v", err)
	}

	if !hasHighPriority {
		t.Fatalf("expected a high-priority operation to be detected")
	}
}

func TestResolveConflict(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	op := models.Operation{
		ID:        "conflict-1",
		Type:      models.UPDATE,
		Data:      "local",
		Timestamp: 10,
		Version:   3,
		Priority:  models.DefaultPriority,
	}

	if err := repo.LogConflict(ctx, op, models.MERGED); err != nil {
		t.Fatalf("log conflict: %v", err)
	}

	if err := repo.ResolveConflict(ctx, op.ID); err != nil {
		t.Fatalf("resolve conflict: %v", err)
	}

	conflicts, err := repo.ListConflicts(ctx)
	if err != nil {
		t.Fatalf("list conflicts: %v", err)
	}

	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(conflicts))
	}

	if conflicts[0].Status != "resolved" {
		t.Fatalf("expected resolved status, got %q", conflicts[0].Status)
	}
}
