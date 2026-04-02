package network

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"offline-sync-agent/internal/models"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestClientCheck(t *testing.T) {
	client := NewClientWithHTTPClient("http://example.test", "token", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/healthz" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(`{"status":"ok"}`)),
			Header:     make(http.Header),
		}, nil
	}), discardLogger())

	status, err := client.Check(context.Background())
	if err != nil {
		t.Fatalf("check health: %v", err)
	}

	if !status.Online {
		t.Fatalf("expected client to report online status")
	}

	if status.Quality == QualityOffline {
		t.Fatalf("expected a non-offline quality classification")
	}
}

func TestClientPushUsesGzip(t *testing.T) {
	client := NewClientWithHTTPClient("http://example.test", "token", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("unexpected auth header %q", r.Header.Get("Authorization"))
		}

		if r.Header.Get("Content-Encoding") != "gzip" {
			t.Fatalf("expected gzip content encoding, got %q", r.Header.Get("Content-Encoding"))
		}

		reader, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Fatalf("open gzip body: %v", err)
		}
		defer reader.Close()

		var request models.SyncRequest
		if err := json.NewDecoder(reader).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		if len(request.Operations) != 1 || request.Operations[0].ID != "record-1" {
			t.Fatalf("unexpected request payload: %+v", request.Operations)
		}

		body, err := json.Marshal(models.SyncResponse{
			Results: []models.SyncResult{{
				ID:      "record-1",
				Status:  models.SyncStatusOK,
				Version: 2,
			}},
		})
		if err != nil {
			t.Fatalf("marshal sync response: %v", err)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(string(body))),
			Header:     make(http.Header),
		}, nil
	}), discardLogger())

	response, err := client.Push(context.Background(), []models.Operation{{
		ID:        "record-1",
		Type:      models.CREATE,
		Data:      "payload",
		Timestamp: 1,
		Version:   1,
		Priority:  models.DefaultPriority,
	}})
	if err != nil {
		t.Fatalf("push operations: %v", err)
	}

	if len(response.Results) != 1 {
		t.Fatalf("expected one sync result, got %d", len(response.Results))
	}

	if response.Results[0].Version != 2 {
		t.Fatalf("expected version 2 from server, got %d", response.Results[0].Version)
	}
}

func TestClientPull(t *testing.T) {
	client := NewClientWithHTTPClient("http://example.test", "token", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/pull" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}

		body, err := json.Marshal(models.PullResponse{
			Data: []models.Record{{
				ID:        "record-1",
				Data:      "server",
				Version:   3,
				UpdatedAt: 42,
			}},
		})
		if err != nil {
			t.Fatalf("marshal pull response: %v", err)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(string(body))),
			Header:     make(http.Header),
		}, nil
	}), discardLogger())

	response, err := client.Pull(context.Background(), 0)
	if err != nil {
		t.Fatalf("pull updates: %v", err)
	}

	if len(response.Data) != 1 {
		t.Fatalf("expected one pulled record, got %d", len(response.Data))
	}

	if response.Data[0].Data != "server" {
		t.Fatalf("expected pulled data %q, got %q", "server", response.Data[0].Data)
	}
}
