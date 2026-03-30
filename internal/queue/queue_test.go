package queue

import (
	"offline-sync-agent/internal/db"
	"offline-sync-agent/internal/models"
	"os"
	"testing"
)

func TestAddOperation(t *testing.T) {
	os.Setenv("TEST_MODE", "true")
	// 🔥 IMPORTANT: initialize DB
	db.InitDB()

	op := models.Operation{
		ID:      "test-id",
		Data:    "test-data",
		Version: 1,
	}

	err := AddOperation(op)

	if err != nil {
		t.Error("Failed to add operation:", err)
	}
}
