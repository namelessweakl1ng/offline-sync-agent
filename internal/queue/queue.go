package queue

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"offline-sync-agent/internal/db"
	"offline-sync-agent/internal/models"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(client *db.Client) *Repository {
	return &Repository{db: client.SQLDB()}
}

func (r *Repository) SaveSyncedData(ctx context.Context, op models.Operation) error {
	if op.ID == "" {
		return fmt.Errorf("synced record id is required")
	}

	op = op.Normalized()
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO synced_data (id, data, version, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			data = excluded.data,
			version = excluded.version,
			updated_at = excluded.updated_at`,
		op.ID,
		op.Data,
		op.Version,
		op.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("save synced data for %s: %w", op.ID, err)
	}

	return nil
}

func (r *Repository) CleanupSynced(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, "DELETE FROM operations WHERE synced = 1"); err != nil {
		return fmt.Errorf("cleanup synced operations: %w", err)
	}

	return nil
}

func (r *Repository) SetLastSync(ctx context.Context, ts int64) error {
	if _, err := r.db.ExecContext(
		ctx,
		"INSERT INTO metadata (key, value) VALUES ('last_sync', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		ts,
	); err != nil {
		return fmt.Errorf("set last sync: %w", err)
	}

	return nil
}

func (r *Repository) GetLastSync(ctx context.Context) (int64, error) {
	var value sql.NullInt64
	err := r.db.QueryRowContext(ctx, "SELECT value FROM metadata WHERE key = 'last_sync'").Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}

		return 0, fmt.Errorf("get last sync: %w", err)
	}

	if !value.Valid {
		return 0, nil
	}

	return value.Int64, nil
}

func (r *Repository) AddOperation(ctx context.Context, op models.Operation) error {
	op = op.Normalized()
	if err := op.Validate(); err != nil {
		return err
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO operations (id, type, data, timestamp, synced, version, priority)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			type = excluded.type,
			data = excluded.data,
			timestamp = excluded.timestamp,
			synced = 0,
			version = excluded.version,
			priority = excluded.priority`,
		op.ID,
		op.Type,
		op.Data,
		op.Timestamp,
		false,
		op.Version,
		op.Priority,
	)
	if err != nil {
		return fmt.Errorf("add operation %s: %w", op.ID, err)
	}

	return nil
}

func (r *Repository) GetUnsynced(ctx context.Context) ([]models.Operation, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, type, data, timestamp, synced, version, priority
		 FROM operations
		 WHERE synced = 0
		 ORDER BY priority ASC, LENGTH(data) ASC, timestamp ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query unsynced operations: %w", err)
	}
	defer rows.Close()

	var operations []models.Operation
	for rows.Next() {
		var op models.Operation
		if err := rows.Scan(&op.ID, &op.Type, &op.Data, &op.Timestamp, &op.Synced, &op.Version, &op.Priority); err != nil {
			return nil, fmt.Errorf("scan unsynced operation: %w", err)
		}

		operations = append(operations, op)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unsynced operations: %w", err)
	}

	return operations, nil
}

func (r *Repository) MarkSynced(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx, "UPDATE operations SET synced = 1 WHERE id = ?", id); err != nil {
		return fmt.Errorf("mark operation %s synced: %w", id, err)
	}

	return nil
}

func (r *Repository) LogConflict(ctx context.Context, op models.Operation, strategy models.ConflictStrategy) error {
	op = op.Normalized()
	if op.Timestamp == 0 {
		op.Timestamp = time.Now().Unix()
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO conflicts (id, data, version, timestamp, status, strategy)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		op.ID,
		op.Data,
		op.Version,
		op.Timestamp,
		"unresolved",
		strategy,
	)
	if err != nil {
		return fmt.Errorf("log conflict for %s: %w", op.ID, err)
	}

	return nil
}

func (r *Repository) DeleteSyncedData(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx, "DELETE FROM synced_data WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete synced data for %s: %w", id, err)
	}

	return nil
}

func (r *Repository) ResolveConflict(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx, "UPDATE conflicts SET status = 'resolved' WHERE id = ?", id); err != nil {
		return fmt.Errorf("resolve conflict for %s: %w", id, err)
	}

	return nil
}

func (r *Repository) ListConflicts(ctx context.Context) ([]models.ConflictRecord, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, data, version, timestamp, status, strategy
		 FROM conflicts
		 ORDER BY timestamp DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query conflicts: %w", err)
	}
	defer rows.Close()

	var conflicts []models.ConflictRecord
	for rows.Next() {
		var record models.ConflictRecord
		if err := rows.Scan(
			&record.ID,
			&record.Data,
			&record.Version,
			&record.Timestamp,
			&record.Status,
			&record.Strategy,
		); err != nil {
			return nil, fmt.Errorf("scan conflict: %w", err)
		}

		conflicts = append(conflicts, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conflicts: %w", err)
	}

	return conflicts, nil
}

func (r *Repository) ListOperations(ctx context.Context) ([]models.Operation, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, type, data, timestamp, synced, version, priority
		 FROM operations
		 ORDER BY priority ASC, timestamp ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query operations: %w", err)
	}
	defer rows.Close()

	var operations []models.Operation
	for rows.Next() {
		var op models.Operation
		if err := rows.Scan(&op.ID, &op.Type, &op.Data, &op.Timestamp, &op.Synced, &op.Version, &op.Priority); err != nil {
			return nil, fmt.Errorf("scan operation: %w", err)
		}

		operations = append(operations, op)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate operations: %w", err)
	}

	return operations, nil
}

func (r *Repository) PendingHighPriority(ctx context.Context) (bool, error) {
	var exists int
	err := r.db.QueryRowContext(
		ctx,
		`SELECT EXISTS(
			SELECT 1
			FROM operations
			WHERE synced = 0 AND priority <= ?
			LIMIT 1
		)`,
		models.HighPriority,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check pending high priority operations: %w", err)
	}

	return exists == 1, nil
}

func (r *Repository) CountUnsynced(ctx context.Context) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM operations WHERE synced = 0").Scan(&count); err != nil {
		return 0, fmt.Errorf("count unsynced operations: %w", err)
	}

	return count, nil
}
