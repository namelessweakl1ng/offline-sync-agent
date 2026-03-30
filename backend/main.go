package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Operation struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Data    string `json:"data"`
	Version int    `json:"version"`
}

type Record struct {
	Data      string
	Version   int
	UpdatedAt int64
}

var store = make(map[string]Record)
var mu sync.Mutex
var requestCount = 0

var AUTH_TOKEN = os.Getenv("AUTH_TOKEN")

type BatchRequest struct {
	Operations []Operation `json:"operations"`
}

func authenticate(r *http.Request) bool {
	token := r.Header.Get("Authorization")

	if token == "" {
		return false
	}

	if token != "Bearer "+AUTH_TOKEN {
		return false
	}

	return true
}

func handlePull(w http.ResponseWriter, r *http.Request) {
	if !authenticate(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	sinceStr := r.URL.Query().Get("since")

	var since int64 = 0
	if sinceStr != "" {
		fmt.Sscanf(sinceStr, "%d", &since)
	}

	type Response struct {
		ID        string `json:"id"`
		Data      string `json:"data"`
		Version   int    `json:"version"`
		UpdatedAt int64  `json:"updated_at"`
	}

	var result []Response

	mu.Lock()
	defer mu.Unlock()

	for id, rec := range store {
		if rec.UpdatedAt > since {
			result = append(result, Response{
				ID:        id,
				Data:      rec.Data,
				Version:   rec.Version,
				UpdatedAt: rec.UpdatedAt,
			})
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": result,
	})
}

func handleSync(w http.ResponseWriter, r *http.Request) {
	reqID := uuid.New().String()
	fmt.Println("[REQ]", reqID, "Incoming sync request")
	requestCount++
	if requestCount > 1000 {
		http.Error(w, "Too many requests", http.StatusTooManyRequests)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB max
	if !authenticate(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var req BatchRequest
	var reader = r.Body

	if r.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, "Failed to decompress", http.StatusBadRequest)
			return
		}
		defer gz.Close()
		reader = gz
	}

	err := json.NewDecoder(reader).Decode(&req)

	if err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	type Result struct {
		ID      string `json:"id"`
		Status  string `json:"status"`
		Version int    `json:"version,omitempty"`
	}

	var results []Result

	for _, op := range req.Operations {
		if op.Type == "DELETE" {
			mu.Lock()
			delete(store, op.ID)
			mu.Unlock()

			fmt.Println("🗑 Deleted:", op.ID)

			results = append(results, Result{
				ID:     op.ID,
				Status: "ok",
			})
			continue
		}
		if op.ID == "" || op.Type == "" {
			results = append(results, Result{
				ID:     op.ID,
				Status: "invalid",
			})
			continue
		}
		mu.Lock()
		currentVersion, exists := store[op.ID]

		if exists && op.Version <= currentVersion.Version {
			mu.Unlock() // 🔥 MUST UNLOCK BEFORE CONTINUE
			results = append(results, Result{
				ID:      op.ID,
				Status:  "conflict",
				Version: currentVersion.Version,
			})
			continue
		}

		store[op.ID] = Record{
			Data:      op.Data,
			Version:   op.Version,
			UpdatedAt: time.Now().Unix(),
		}
		mu.Unlock()

		fmt.Println("Accepted:", op.ID)

		results = append(results, Result{
			ID:     op.ID,
			Status: "ok",
		})
	}

	response, _ := json.Marshal(map[string]interface{}{
		"results": results,
	})

	w.WriteHeader(http.StatusOK)
	w.Write(response)
}
func main() {
	http.HandleFunc("/sync", handleSync)
	http.HandleFunc("/pull", handlePull)

	fmt.Println("Server running on https://localhost:8080")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("Server running on port", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
