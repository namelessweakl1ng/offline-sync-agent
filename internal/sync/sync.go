package syncer

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"offline-sync-agent/internal/models"
	"offline-sync-agent/internal/network"
)

const defaultMaxWorkers = 3

type QueueRepository interface {
	GetLastSync(ctx context.Context) (int64, error)
	SetLastSync(ctx context.Context, ts int64) error
	GetUnsynced(ctx context.Context) ([]models.Operation, error)
	MarkSynced(ctx context.Context, id string) error
	CleanupSynced(ctx context.Context) error
	SaveSyncedData(ctx context.Context, op models.Operation) error
	DeleteSyncedData(ctx context.Context, id string) error
	LogConflict(ctx context.Context, op models.Operation, strategy models.ConflictStrategy) error
	AddOperation(ctx context.Context, op models.Operation) error
	PendingHighPriority(ctx context.Context) (bool, error)
}

type RemoteClient interface {
	Check(ctx context.Context) (network.Status, error)
	Push(ctx context.Context, operations []models.Operation) (models.SyncResponse, error)
	Pull(ctx context.Context, since int64) (models.PullResponse, error)
}

type Summary struct {
	NetworkQuality string
	Synced         int
	Conflicts      int
	Pulled         int
	Duration       time.Duration
	ChunkFailures  int
}

type Service struct {
	repo       QueueRepository
	remote     RemoteClient
	logger     *slog.Logger
	maxWorkers int
}

func NewService(repo QueueRepository, remote RemoteClient, logger *slog.Logger, maxWorkers int) *Service {
	if maxWorkers <= 0 {
		maxWorkers = defaultMaxWorkers
	}

	return &Service{
		repo:       repo,
		remote:     remote,
		logger:     logger,
		maxWorkers: maxWorkers,
	}
}

func (s *Service) PullUpdates(ctx context.Context) (int, error) {
	lastSync, err := s.repo.GetLastSync(ctx)
	if err != nil {
		return 0, err
	}

	response, err := s.remote.Pull(ctx, lastSync)
	if err != nil {
		return 0, err
	}

	latestSeen := lastSync
	for _, record := range response.Data {
		op := models.Operation{
			ID:        record.ID,
			Type:      models.UPDATE,
			Data:      record.Data,
			Version:   record.Version,
			Timestamp: record.UpdatedAt,
			Synced:    true,
			Priority:  models.DefaultPriority,
		}

		if err := s.repo.SaveSyncedData(ctx, op); err != nil {
			return 0, err
		}

		if record.UpdatedAt > latestSeen {
			latestSeen = record.UpdatedAt
		}
	}

	if latestSeen != lastSync {
		if err := s.repo.SetLastSync(ctx, latestSeen); err != nil {
			return 0, err
		}
	}

	return len(response.Data), nil
}

func (s *Service) SyncNow(ctx context.Context) (Summary, error) {
	startedAt := time.Now()

	status, err := s.remote.Check(ctx)
	if err != nil {
		return Summary{
			NetworkQuality: string(status.Quality),
			Duration:       time.Since(startedAt),
		}, err
	}

	s.logger.Info("network check complete", "quality", status.Quality, "latency", status.Latency.String())

	operations, err := s.repo.GetUnsynced(ctx)
	if err != nil {
		return Summary{
			NetworkQuality: string(status.Quality),
			Duration:       time.Since(startedAt),
		}, err
	}

	summary := Summary{
		NetworkQuality: string(status.Quality),
		Duration:       time.Since(startedAt),
	}

	if len(operations) == 0 {
		pulled, err := s.PullUpdates(ctx)
		summary.Pulled = pulled
		summary.Duration = time.Since(startedAt)
		return summary, err
	}

	chunks := chunkOperations(operations, batchSizeForQuality(status.Quality))
	results, workerErr := s.pushChunks(ctx, chunks)
	for _, response := range results {
		for _, result := range response.Results {
			op, ok := findOperation(operations, result.ID)
			if !ok {
				s.logger.Warn("sync response referenced unknown operation", "operation_id", result.ID)
				continue
			}

			switch result.Status {
			case models.SyncStatusOK:
				if err := s.handleSuccessfulSync(ctx, op, result.Version); err != nil {
					return summary, err
				}
				summary.Synced++
			case models.SyncStatusConflict:
				if err := s.handleConflict(ctx, op, result); err != nil {
					return summary, err
				}
				summary.Conflicts++
			case models.SyncStatusInvalid:
				s.logger.Error("server rejected operation", "operation_id", result.ID, "message", result.Message)
				summary.ChunkFailures++
			default:
				s.logger.Error("received unsupported sync status", "operation_id", result.ID, "status", result.Status)
				summary.ChunkFailures++
			}
		}
	}

	if err := s.repo.CleanupSynced(ctx); err != nil {
		return summary, err
	}

	pulled, pullErr := s.PullUpdates(ctx)
	summary.Pulled = pulled
	summary.Duration = time.Since(startedAt)

	if workerErr != nil {
		summary.ChunkFailures++
		return summary, workerErr
	}

	if pullErr != nil {
		return summary, pullErr
	}

	return summary, nil
}

func (s *Service) Run(ctx context.Context, interval time.Duration, maxBackoff time.Duration) error {
	if interval <= 0 {
		interval = 5 * time.Second
	}

	if maxBackoff <= 0 {
		maxBackoff = 30 * time.Second
	}

	baseBackoff := 2 * time.Second
	backoff := baseBackoff

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		summary, err := s.SyncNow(ctx)
		if err == nil {
			backoff = baseBackoff
			s.logger.Info(
				"sync cycle complete",
				"synced", summary.Synced,
				"conflicts", summary.Conflicts,
				"pulled", summary.Pulled,
				"duration", summary.Duration.String(),
			)

			if !wait(ctx, interval) {
				return nil
			}

			continue
		}

		hasHighPriority, highPriorityErr := s.repo.PendingHighPriority(ctx)
		if highPriorityErr != nil {
			s.logger.Warn("failed to inspect queue priority", "error", highPriorityErr)
		}

		retryIn := backoff
		if hasHighPriority {
			retryIn = baseBackoff
		} else {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			retryIn = backoff
		}

		s.logger.Warn(
			"sync cycle failed",
			"error", err,
			"network_quality", summary.NetworkQuality,
			"retry_in", retryIn.String(),
			"high_priority_pending", hasHighPriority,
		)

		if !wait(ctx, retryIn) {
			return nil
		}
	}
}

func (s *Service) pushChunks(ctx context.Context, chunks [][]models.Operation) ([]models.SyncResponse, error) {
	if len(chunks) == 0 {
		return nil, nil
	}

	type response struct {
		result models.SyncResponse
		err    error
	}

	workerCount := s.maxWorkers
	if workerCount > len(chunks) {
		workerCount = len(chunks)
	}

	jobs := make(chan []models.Operation, len(chunks))
	results := make(chan response, len(chunks))

	var waitGroup sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()

			for chunk := range jobs {
				result, err := s.remote.Push(ctx, chunk)
				results <- response{result: result, err: err}
			}
		}()
	}

	for _, chunk := range chunks {
		jobs <- chunk
	}
	close(jobs)

	go func() {
		waitGroup.Wait()
		close(results)
	}()

	var (
		allResponses []models.SyncResponse
		firstErr     error
	)

	for result := range results {
		if result.err != nil && firstErr == nil {
			firstErr = result.err
		}

		if result.err == nil {
			allResponses = append(allResponses, result.result)
		}
	}

	return allResponses, firstErr
}

func (s *Service) handleSuccessfulSync(ctx context.Context, op models.Operation, version int) error {
	if err := s.repo.MarkSynced(ctx, op.ID); err != nil {
		return err
	}

	if op.Type == models.DELETE {
		return s.repo.DeleteSyncedData(ctx, op.ID)
	}

	if version > 0 {
		op.Version = version
	}
	op.Synced = true
	op.Timestamp = time.Now().Unix()

	return s.repo.SaveSyncedData(ctx, op)
}

func (s *Service) handleConflict(ctx context.Context, op models.Operation, result models.SyncResult) error {
	strategy := models.SERVER_WINS
	retry := false
	next := op

	switch {
	case op.Type == models.DELETE:
		strategy = models.SERVER_WINS
	case result.Data != "" && result.Data != op.Data:
		next.Data = mergeData(op.Data, result.Data)
		strategy = models.MERGED
		retry = true
	default:
		strategy = models.CLIENT_WINS
		retry = true
	}

	if err := s.repo.LogConflict(ctx, op, strategy); err != nil {
		return err
	}

	if !retry {
		return nil
	}

	if result.Version >= next.Version {
		next.Version = result.Version + 1
	} else {
		next.Version++
	}
	next.Timestamp = time.Now().Unix()
	next.Synced = false

	return s.repo.AddOperation(ctx, next)
}

func chunkOperations(operations []models.Operation, size int) [][]models.Operation {
	if size <= 0 {
		size = 1
	}

	chunks := make([][]models.Operation, 0, (len(operations)+size-1)/size)
	for start := 0; start < len(operations); start += size {
		end := start + size
		if end > len(operations) {
			end = len(operations)
		}

		chunks = append(chunks, operations[start:end])
	}

	return chunks
}

func batchSizeForQuality(quality network.Quality) int {
	switch quality {
	case network.QualityFast:
		return 5
	case network.QualityMedium:
		return 2
	default:
		return 1
	}
}

func mergeData(local string, server string) string {
	switch {
	case local == server:
		return local
	case local == "":
		return server
	case server == "":
		return local
	default:
		return fmt.Sprintf("%s | %s", local, server)
	}
}

func findOperation(operations []models.Operation, id string) (models.Operation, bool) {
	for _, op := range operations {
		if op.ID == id {
			return op, true
		}
	}

	return models.Operation{}, false
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
