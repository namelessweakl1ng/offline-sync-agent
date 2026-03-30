package queue

import (
	"offline-sync-agent/internal/db"
	"offline-sync-agent/internal/models"
	"sync"
)

var mu sync.Mutex

func SaveSyncedData(op models.Operation) error {
	_, err := db.DB.Exec(
		"INSERT OR REPLACE INTO synced_data (id, data, version, updated_at) VALUES (?, ?, ?, ?)",
		op.ID, op.Data, op.Version, op.Timestamp,
	)
	return err
}

func CleanupSynced() error {
	_, err := db.DB.Exec("DELETE FROM operations WHERE synced = 1")
	return err
}

func SetLastSync(ts int64) {
	db.DB.Exec(
		"INSERT OR REPLACE INTO metadata (key, value) VALUES ('last_sync', ?)",
		ts,
	)
}

func GetLastSync() int64 {
	var val int64
	row := db.DB.QueryRow("SELECT value FROM metadata WHERE key='last_sync'")
	row.Scan(&val)
	return val
}

func AddOperation(op models.Operation) error {
	mu.Lock()
	defer mu.Unlock()

	_, err := db.DB.Exec(
		"INSERT INTO operations (id, type, data, timestamp, synced, version, priority) VALUES (?, ?, ?, ?, ?, ?, ?)",
		op.ID, op.Type, op.Data, op.Timestamp, false, op.Version, op.Priority,
	)

	if err != nil {
		// ignore duplicate ID errors
		if err.Error() == "UNIQUE constraint failed: operations.id" {
			return nil
		}
		return err
	}

	return nil
}

func GetUnsynced() ([]models.Operation, error) {
	mu.Lock()
	defer mu.Unlock()

	rows, err := db.DB.Query(`
	SELECT id, type, data, timestamp, synced, version, priority
	FROM operations
	WHERE synced = 0
	ORDER BY priority ASC, LENGTH(data) ASC, timestamp ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ops []models.Operation
	for rows.Next() {
		var op models.Operation
		err := rows.Scan(&op.ID, &op.Type, &op.Data, &op.Timestamp, &op.Synced, &op.Version, &op.Priority)
		if err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}
	return ops, nil
}

func MarkSynced(id string) error {
	mu.Lock()
	defer mu.Unlock()
	_, err := db.DB.Exec("UPDATE operations SET synced = 1 WHERE id = ?", id)
	return err
}

func LogConflict(op models.Operation, strategy models.ConflictStrategy) error {
	mu.Lock()
	defer mu.Unlock()

	_, err := db.DB.Exec(
		"INSERT INTO conflicts (id, data, version, timestamp, status, strategy) VALUES (?, ?, ?, ?, ?, ?)",
		op.ID, op.Data, op.Version, op.Timestamp, "unresolved", strategy,
	)
	return err
}

func DeleteSyncedData(id string) error {
	_, err := db.DB.Exec("DELETE FROM synced_data WHERE id = ?", id)
	return err
}
