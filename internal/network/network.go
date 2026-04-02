package network

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"offline-sync-agent/internal/models"
)

type Quality string

const (
	QualityOffline Quality = "offline"
	QualityFast    Quality = "fast"
	QualityMedium  Quality = "medium"
	QualitySlow    Quality = "slow"
)

type Status struct {
	Online  bool
	Latency time.Duration
	Quality Quality
}

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type Client struct {
	httpClient HTTPDoer
	serverURL  string
	authToken  string
	logger     *slog.Logger
}

func NewClient(serverURL string, authToken string, timeout time.Duration, insecureSkipVerify bool, logger *slog.Logger) *Client {
	baseTransport, _ := http.DefaultTransport.(*http.Transport)
	transport := http.DefaultTransport
	if baseTransport != nil {
		clone := baseTransport.Clone()
		clone.TLSClientConfig = &tls.Config{InsecureSkipVerify: insecureSkipVerify}
		transport = clone
	}

	httpClient := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}

	return &Client{
		httpClient: httpClient,
		serverURL:  strings.TrimRight(serverURL, "/"),
		authToken:  strings.TrimSpace(authToken),
		logger:     logger,
	}
}

func NewClientWithHTTPClient(serverURL string, authToken string, httpClient HTTPDoer, logger *slog.Logger) *Client {
	return &Client{
		httpClient: httpClient,
		serverURL:  strings.TrimRight(serverURL, "/"),
		authToken:  strings.TrimSpace(authToken),
		logger:     logger,
	}
}

func (c *Client) Check(ctx context.Context) (Status, error) {
	if err := c.validateConfig(); err != nil {
		return Status{Quality: QualityOffline}, err
	}

	req, err := c.newRequest(ctx, http.MethodGet, "/healthz", nil)
	if err != nil {
		return Status{Quality: QualityOffline}, err
	}

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		return Status{
			Online:  false,
			Latency: latency,
			Quality: QualityOffline,
		}, fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Status{
			Online:  false,
			Latency: latency,
			Quality: QualityOffline,
		}, fmt.Errorf("health check returned %s", resp.Status)
	}

	return Status{
		Online:  true,
		Latency: latency,
		Quality: qualityFromLatency(latency),
	}, nil
}

func (c *Client) Push(ctx context.Context, operations []models.Operation) (models.SyncResponse, error) {
	if err := c.validateConfig(); err != nil {
		return models.SyncResponse{}, err
	}

	payload := models.SyncRequest{Operations: operations}
	if err := payload.Validate(); err != nil {
		return models.SyncResponse{}, err
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return models.SyncResponse{}, fmt.Errorf("marshal sync payload: %w", err)
	}

	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	if _, err := gzipWriter.Write(jsonBody); err != nil {
		return models.SyncResponse{}, fmt.Errorf("compress sync payload: %w", err)
	}

	if err := gzipWriter.Close(); err != nil {
		return models.SyncResponse{}, fmt.Errorf("finalize sync payload compression: %w", err)
	}

	req, err := c.newRequest(ctx, http.MethodPost, "/sync", &compressed)
	if err != nil {
		return models.SyncResponse{}, err
	}

	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return models.SyncResponse{}, fmt.Errorf("send sync request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return models.SyncResponse{}, fmt.Errorf("sync request failed: %s: %s", resp.Status, readErrorMessage(resp.Body))
	}

	var result models.SyncResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return models.SyncResponse{}, fmt.Errorf("decode sync response: %w", err)
	}

	return result, nil
}

func (c *Client) Pull(ctx context.Context, since int64) (models.PullResponse, error) {
	if err := c.validateConfig(); err != nil {
		return models.PullResponse{}, err
	}

	req, err := c.newRequest(ctx, http.MethodGet, fmt.Sprintf("/pull?since=%d", since), nil)
	if err != nil {
		return models.PullResponse{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return models.PullResponse{}, fmt.Errorf("pull request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return models.PullResponse{}, fmt.Errorf("pull request failed: %s: %s", resp.Status, readErrorMessage(resp.Body))
	}

	var result models.PullResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return models.PullResponse{}, fmt.Errorf("decode pull response: %w", err)
	}

	return result, nil
}

func (c *Client) newRequest(ctx context.Context, method string, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.serverURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("build request %s %s: %w", method, path, err)
	}

	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	return req, nil
}

func (c *Client) validateConfig() error {
	switch {
	case c.serverURL == "":
		return fmt.Errorf("server URL is not configured")
	case c.authToken == "":
		return fmt.Errorf("auth token is not configured")
	default:
		return nil
	}
}

func qualityFromLatency(latency time.Duration) Quality {
	switch {
	case latency < 500*time.Millisecond:
		return QualityFast
	case latency < 2*time.Second:
		return QualityMedium
	default:
		return QualitySlow
	}
}

func readErrorMessage(reader io.Reader) string {
	body, err := io.ReadAll(io.LimitReader(reader, 4<<10))
	if err != nil || len(body) == 0 {
		return "request failed"
	}

	return strings.TrimSpace(string(body))
}
