package server

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"offline-sync-agent/internal/config"
	"offline-sync-agent/internal/models"
)

type Server struct {
	cfg        config.ServerConfig
	logger     *slog.Logger
	store      Store
	httpServer *http.Server
}

func New(cfg config.ServerConfig, store Store, logger *slog.Logger) *Server {
	server := &Server{
		cfg:    cfg,
		logger: logger,
		store:  store,
	}

	server.httpServer = &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      server.routes(),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	return server
}

func (s *Server) Run(ctx context.Context) error {
	errorChannel := make(chan error, 1)

	go func() {
		s.logger.Info("starting backend server", "port", s.cfg.Port, "store_backend", s.cfg.StoreBackend)

		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errorChannel <- err
			return
		}

		errorChannel <- nil
	}()

	select {
	case <-ctx.Done():
		s.logger.Info("shutting down backend server")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
		defer cancel()

		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown http server: %w", err)
		}

		if err := s.store.Close(); err != nil {
			return fmt.Errorf("close store: %w", err)
		}

		return nil
	case err := <-errorChannel:
		if err != nil {
			_ = s.store.Close()
			return fmt.Errorf("listen and serve: %w", err)
		}

		return s.store.Close()
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)

	protected := chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/sync":
				s.handleSync(w, r)
			case r.Method == http.MethodGet && r.URL.Path == "/pull":
				s.handlePull(w, r)
			default:
				writeJSONError(w, http.StatusNotFound, "not found")
			}
		}),
		requestIDMiddleware,
		loggingMiddleware(s.logger),
		rateLimitMiddleware(s.cfg.RateLimitPerMinute, s.logger),
		authMiddleware(s.cfg.AuthToken),
	)

	mux.Handle("/sync", protected)
	mux.Handle("/pull", protected)

	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handlePull(w http.ResponseWriter, r *http.Request) {
	since := int64(0)
	sinceQuery := r.URL.Query().Get("since")
	if sinceQuery != "" {
		parsed, err := strconv.ParseInt(sinceQuery, 10, 64)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid since parameter")
			return
		}
		since = parsed
	}

	records, err := s.store.PullSince(r.Context(), since)
	if err != nil {
		s.logger.Error("pull failed", "request_id", RequestIDFromContext(r.Context()), "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to fetch updates")
		return
	}

	writeJSON(w, http.StatusOK, models.PullResponse{Data: records})
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxRequestBodyBytes)
	defer r.Body.Close()

	reader, cleanup, err := requestReader(r.Body, r.Header.Get("Content-Encoding"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer cleanup()

	var request models.SyncRequest
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := request.Validate(); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	results := make([]models.SyncResult, 0, len(request.Operations))
	for _, operation := range request.Operations {
		result, err := s.store.ApplyOperation(r.Context(), operation)
		if err != nil {
			s.logger.Error(
				"apply operation failed",
				"request_id", RequestIDFromContext(r.Context()),
				"operation_id", operation.ID,
				"error", err,
			)
			writeJSONError(w, http.StatusInternalServerError, "failed to apply operation")
			return
		}

		results = append(results, result)
	}

	writeJSON(w, http.StatusOK, models.SyncResponse{Results: results})
}

func requestReader(body io.Reader, encoding string) (io.Reader, func(), error) {
	switch encoding {
	case "", "identity":
		return body, func() {}, nil
	case "gzip":
		reader, err := gzip.NewReader(body)
		if err != nil {
			return nil, func() {}, fmt.Errorf("failed to decode gzip body")
		}

		return reader, func() {
			_ = reader.Close()
		}, nil
	default:
		return nil, func() {}, fmt.Errorf("unsupported content encoding")
	}
}
