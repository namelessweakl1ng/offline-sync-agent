package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

type Client struct {
	sqlDB *sql.DB
}

func Open(path string) (*Client, error) {
	if path == "" {
		path = "data.db"
	}

	sqlDB, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	client := &Client{sqlDB: sqlDB}
	if err := client.initSchema(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}

	return client, nil
}

func OpenTest() (*Client, error) {
	return Open(":memory:")
}

func (c *Client) Close() error {
	if c == nil || c.sqlDB == nil {
		return nil
	}

	return c.sqlDB.Close()
}

func (c *Client) SQLDB() *sql.DB {
	return c.sqlDB
}

func (c *Client) initSchema() error {
	statements := []string{
		`PRAGMA busy_timeout = 5000;`,
		`CREATE TABLE IF NOT EXISTS operations (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			data TEXT NOT NULL DEFAULT '',
			timestamp INTEGER NOT NULL,
			synced BOOLEAN NOT NULL DEFAULT 0,
			version INTEGER NOT NULL,
			priority INTEGER NOT NULL DEFAULT 10
		);`,
		`CREATE TABLE IF NOT EXISTS conflicts (
			id TEXT NOT NULL,
			data TEXT NOT NULL DEFAULT '',
			version INTEGER NOT NULL,
			timestamp INTEGER NOT NULL,
			status TEXT NOT NULL,
			strategy TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS synced_data (
			id TEXT PRIMARY KEY,
			data TEXT NOT NULL DEFAULT '',
			version INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS metadata (
			key TEXT PRIMARY KEY,
			value INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_operations_unsynced_priority ON operations(synced, priority, timestamp);`,
		`CREATE INDEX IF NOT EXISTS idx_operations_id_version ON operations(id, version);`,
		`CREATE INDEX IF NOT EXISTS idx_conflicts_id_timestamp ON conflicts(id, timestamp DESC);`,
	}

	for _, statement := range statements {
		if _, err := c.sqlDB.Exec(statement); err != nil {
			return fmt.Errorf("initialize database schema: %w", err)
		}
	}

	return nil
}
