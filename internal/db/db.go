package db

import (
	"database/sql"
	"log"
	"os"

	_ "github.com/mattn/go-sqlite3"
)

var DB *sql.DB

func InitDB() {
	var err error

	dbPath := "data.db"

	// 🔥 if test mode → use memory DB
	if os.Getenv("TEST_MODE") == "true" {
		dbPath = ":memory:"
	}

	DB, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatal(err)
	}

	createTable := `
	CREATE TABLE IF NOT EXISTS operations (
	id TEXT PRIMARY KEY,
	type TEXT,
	data TEXT,
	timestamp INTEGER,
	synced BOOLEAN,
	version INTEGER,
	priority INTEGER
	);`

	_, err = DB.Exec(createTable)
	if err != nil {
		log.Fatal(err)
	}

	createConflictTable := `
	CREATE TABLE IF NOT EXISTS conflicts (
	id TEXT,
	data TEXT,
	version INTEGER,
	timestamp INTEGER,
	status TEXT,
	strategy TEXT
	);`

	_, err = DB.Exec(createConflictTable)
	if err != nil {
		log.Fatal(err)
	}

	createSyncedTable := `
	CREATE TABLE IF NOT EXISTS synced_data (
    id TEXT PRIMARY KEY,
    data TEXT,
    version INTEGER,
    updated_at INTEGER
	);`

	_, err = DB.Exec(createSyncedTable)
	if err != nil {
		log.Fatal(err)
	}

	createMeta := `
	CREATE TABLE IF NOT EXISTS metadata (
    key TEXT PRIMARY KEY,
    value TEXT
	);`

	_, err = DB.Exec(createMeta)
	if err != nil {
		log.Fatal(err)
	}

	_, err = DB.Exec(`CREATE INDEX IF NOT EXISTS idx_unsynced ON operations(synced);`)
	if err != nil {
		log.Fatal(err)
	}

	_, err = DB.Exec(`CREATE INDEX IF NOT EXISTS idx_id_version ON operations(id, version);`)
	if err != nil {
		log.Fatal(err)
	}
}
