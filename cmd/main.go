package main

import (
	"fmt"
	"offline-sync-agent/internal/db"
	"offline-sync-agent/internal/models"
	"offline-sync-agent/internal/network"
	"offline-sync-agent/internal/queue"
	syncer "offline-sync-agent/internal/sync"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
)

func main() {
	db.InitDB()

	if len(os.Args) < 2 {
		fmt.Println("Usage: add | sync")
		return
	}

	switch os.Args[1] {

	case "delete":
		if len(os.Args) < 3 {
			fmt.Println("Usage: delete <id>")
			return
		}

		id := os.Args[2]

		op := models.Operation{
			ID:        id,
			Type:      models.DELETE,
			Data:      "",
			Timestamp: time.Now().Unix(),
			Synced:    false,
			Version:   int(time.Now().Unix()),
			Priority:  1,
		}

		queue.AddOperation(op)
		fmt.Println("🗑 Delete operation queued:", id)

	case "add":
		op := models.Operation{
			ID:        uuid.New().String(),
			Type:      models.CREATE,
			Data:      "sample data",
			Timestamp: time.Now().Unix(),
			Synced:    false,
			Version:   int(time.Now().Unix()),
			Priority:  1,
		}

		queue.AddOperation(op)
		fmt.Println("Added operation:", op.ID)

	case "conflicts":
		rows, err := db.DB.Query("SELECT id, version, timestamp, status FROM conflicts")
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		defer rows.Close()

		fmt.Println("⚠️ Conflict History:")

		for rows.Next() {
			var id string
			var version int
			var timestamp int64
			var status string

			rows.Scan(&id, &version, &timestamp, &status)

			fmt.Printf("ID: %s | Version: %d | Status: %s | Time: %d\n",
				id, version, status, timestamp)
		}

	case "resolve":
		if len(os.Args) < 3 {
			fmt.Println("Usage: resolve <id>")
			return
		}

		id := os.Args[2]

		_, err := db.DB.Exec(
			"UPDATE conflicts SET status = 'resolved' WHERE id = ?",
			id,
		)

		if err != nil {
			fmt.Println("Error resolving conflict:", err)
			return
		}

		fmt.Println("✅ Conflict marked as resolved:", id)

	case "status":
		rows, err := db.DB.Query("SELECT id, priority, synced FROM operations")
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		defer rows.Close()

		fmt.Println("📊 Queue Status:")

		count := 0
		for rows.Next() {
			var id string
			var priority int
			var synced bool

			rows.Scan(&id, &priority, &synced)

			status := "❌ unsynced"
			if synced {
				status = "✅ synced"
			}

			fmt.Printf("ID: %s | Priority: %d | %s\n", id, priority, status)
			count++
		}

		if count == 0 {
			fmt.Println("No operations found.")
		}

	case "debug":
		fmt.Println("Debug mode:")
		fmt.Println("Auth token:", network.GetAuthToken())

		ops, _ := queue.GetUnsynced()
		fmt.Println("Unsynced ops:", len(ops))

	case "sync":
		fmt.Println("Starting background sync loop...")

		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

		go func() bool {
			backoff := 2 * time.Second
			maxBackoff := 30 * time.Second

			for {
				online, _, _ := network.IsOnline()

				success := false

				if online {
					success = syncer.SyncNow()
					if success {
						fmt.Println("✅ Sync cycle complete")
					}
				} else {
					fmt.Println("📴 Waiting for network...")
				}

				if success {
					backoff = 2 * time.Second
				} else {

					ops, _ := queue.GetUnsynced()

					hasHigh := false
					for _, op := range ops {
						if op.Priority == 1 {
							hasHigh = true
							break
						}
					}

					if hasHigh {
						backoff = 2 * time.Second
						fmt.Println("⚡ High priority pending, retrying fast")
					} else {
						backoff *= 2
						if backoff > maxBackoff {
							backoff = maxBackoff
						}
						fmt.Println("🐢 Low priority, slower retry")
					}

					time.Sleep(backoff)
					continue
				}

				time.Sleep(5 * time.Second)
			}
		}()

		<-stop
		fmt.Println("\nStopping sync agent...")
	}
}
