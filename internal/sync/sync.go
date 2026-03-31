package sync

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"offline-sync-agent/internal/models"
	"offline-sync-agent/internal/network"
	"offline-sync-agent/internal/queue"
	"os"
	"time"
)

const maxWorkers = 3

var errorCount int

func log(level string, msg string) {
	entry := map[string]interface{}{
		"time":  time.Now().Format(time.RFC3339),
		"level": level,
		"msg":   msg,
	}

	b, _ := json.Marshal(entry)
	fmt.Println(string(b))
}

func logInfo(msg string)  { log("INFO", msg) }
func logError(msg string) { log("ERROR", msg) }
func logDebug(msg string) { log("DEBUG", msg) }

func chunkOps(ops []map[string]interface{}, size int) [][]map[string]interface{} {
	var chunks [][]map[string]interface{}

	for i := 0; i < len(ops); i += size {
		end := i + size
		if end > len(ops) {
			end = len(ops)
		}
		chunks = append(chunks, ops[i:end])
	}

	return chunks
}

func mergeData(local string, server string) string {
	if local == server {
		return local
	}

	// simple merge: combine both values
	return local + " | " + server
}

func PullUpdates() {
	last := queue.GetLastSync()

	serverURL := os.Getenv("SERVER_URL")

	url := fmt.Sprintf("%s/pull?since=%d", serverURL, last)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+network.GetAuthToken())

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		logError("Pull failed: " + err.Error())
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if resp.StatusCode != http.StatusOK {
		logError("Pull failed with status: " + resp.Status)
		return
	}

	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		logError("Decode error: " + err.Error())
		return
	}

	data, ok := result["data"].([]interface{})
	if !ok {
		return
	}

	var latest int64 = last

	for _, item := range data {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		id, _ := m["id"].(string)
		dataStr, _ := m["data"].(string)

		versionFloat, _ := m["version"].(float64)
		version := int(versionFloat)

		updatedFloat, _ := m["updated_at"].(float64)
		updated := int64(updatedFloat)

		op := models.Operation{
			ID:        id,
			Data:      dataStr,
			Version:   version,
			Timestamp: updated,
			Synced:    true,
		}

		queue.SaveSyncedData(op)

		if updated > latest {
			latest = updated
		}

		logInfo("Pulled: " + id)
	}

	queue.SetLastSync(latest)
}

func SyncNow() bool {
	start := time.Now()
	errorCount = 0
	logInfo("Checking network...")
	online, latency, quality := network.IsOnline()
	logInfo("Network quality: " + quality)

	if !online {
		logError("No internet connection. Skipping sync.")
		return false
	}

	logInfo("Latency: " + latency.String())

	logInfo("Online. Starting sync...")

	ops, err := queue.GetUnsynced()
	if err != nil {
		fmt.Println("Error fetching ops:", err)
		return false
	}

	if len(ops) == 0 {
		logInfo("Nothing to sync")
		return true
	}

	var batch []map[string]interface{}

	latest := make(map[string]models.Operation)

	for _, op := range ops {
		latest[op.ID] = op
	}

	for _, op := range latest {
		payload := map[string]interface{}{
			"id":      op.ID,
			"type":    op.Type,
			"version": op.Version,
		}

		if op.Type != models.DELETE {
			payload["data"] = op.Data
		}

		batch = append(batch, payload)
	}

	chunkSize := 2

	if quality == "slow" {
		logError("Poor network, skipping sync")
		return false
	}

	if quality == "medium" {
		logInfo("Medium network, reducing batch size")
		chunkSize = 1
	}

	chunks := chunkOps(batch, chunkSize)

	jobs := make(chan []map[string]interface{}, len(chunks))
	resultsChan := make(chan bool, len(chunks))

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	// start workers
	for w := 0; w < maxWorkers; w++ {
		go func() {
			for c := range jobs {

				payload := map[string]interface{}{
					"operations": c,
				}

				jsonData, err := json.Marshal(payload)
				if err != nil {
					logError("JSON marshal failed: " + err.Error())
					resultsChan <- false
					continue
				}

				var buf bytes.Buffer
				gz := gzip.NewWriter(&buf)

				_, err = gz.Write(jsonData)
				if err != nil {
					logError("Compression error: " + err.Error())
					errorCount++
					resultsChan <- false
					continue
				}

				gz.Close()
				serverURL := os.Getenv("SERVER_URL")

				req, _ := http.NewRequest("POST", serverURL+"/sync", &buf)
				req.Header.Set("Authorization", "Bearer "+network.GetAuthToken())
				req.Header.Set("Content-Encoding", "gzip")
				req.Header.Set("Content-Type", "application/json")

				resp, err := client.Do(req)
				if err != nil {
					logError("Worker failed: " + err.Error())
					errorCount++
					resultsChan <- false
					continue
				}
				defer resp.Body.Close()

				if err != nil {
					logError("Worker failed: " + err.Error())
					errorCount++
					resultsChan <- false
					continue
				}

				var result map[string]interface{}
				if resp.StatusCode != http.StatusOK {
					logError("Sync failed with status: " + resp.Status)
					errorCount++
					resultsChan <- false
					continue
				}

				err = json.NewDecoder(resp.Body).Decode(&result)
				if err != nil {
					logError("Decode error: " + err.Error())
					errorCount++
					resultsChan <- false
					continue
				}

				resultsRaw, ok := result["results"].([]interface{})
				if !ok {
					errorCount++
					resultsChan <- false
					continue
				}

				for _, r := range resultsRaw {
					item, ok := r.(map[string]interface{})
					if !ok {
						continue
					}
					id := item["id"].(string)
					status, _ := item["status"].(string)

					if status == "ok" {
						logInfo("Synced: " + id)
						queue.MarkSynced(id)

						for _, op := range ops {
							if op.ID == id {
								if op.Type == models.DELETE {
									queue.DeleteSyncedData(id)
								} else {
									queue.SaveSyncedData(op)
								}
								break
							}
						}
					} else {
						logError("Conflict detected: " + id)

						for _, op := range ops {
							if op.ID == id {

								strategy := models.SERVER_WINS

								if op.Version > int(item["version"].(float64)) {
									strategy = models.CLIENT_WINS
								}

								queue.LogConflict(op, strategy)

								if strategy == models.CLIENT_WINS {
									logInfo("Client wins, retrying")

									op.Version++
									queue.AddOperation(op)

								} else {
									logInfo("Merging with server data")

									serverData := ""
									if d, ok := item["data"].(string); ok {
										serverData = d
									}

									merged := mergeData(op.Data, serverData)

									op.Data = merged
									op.Version++

									queue.AddOperation(op)
								}

								break
							}
						}
					}

				}

				resultsChan <- true
			}
		}()
	}
	for _, chunk := range chunks {
		jobs <- chunk
	}
	close(jobs)

	allSuccess := true

	for i := 0; i < len(chunks); i++ {
		result := <-resultsChan
		if !result {
			allSuccess = false
		}
	}
	if allSuccess {
		logInfo("All workers finished successfully")
	} else {
		logError("Some workers failed")
	}
	queue.CleanupSynced()
	PullUpdates()
	logInfo(fmt.Sprintf("Errors: %d", errorCount))
	logInfo("Sync duration: " + time.Since(start).String())
	return allSuccess
}
