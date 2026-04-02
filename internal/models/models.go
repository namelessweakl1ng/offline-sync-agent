package models

import (
	"fmt"
	"strings"
	"time"
)

type OperationType string
type ConflictStrategy string

const (
	SERVER_WINS ConflictStrategy = "SERVER_WINS"
	CLIENT_WINS ConflictStrategy = "CLIENT_WINS"
	MERGED      ConflictStrategy = "MERGED"
)

const (
	CREATE OperationType = "CREATE"
	UPDATE OperationType = "UPDATE"
	DELETE OperationType = "DELETE"
)

const (
	SyncStatusOK       = "ok"
	SyncStatusConflict = "conflict"
	SyncStatusInvalid  = "invalid"
)

const (
	DefaultPriority = 10
	HighPriority    = 1
)

type Operation struct {
	ID        string        `json:"id"`
	Type      OperationType `json:"type"`
	Data      string        `json:"data,omitempty"`
	Timestamp int64         `json:"timestamp,omitempty"`
	Synced    bool          `json:"synced,omitempty"`
	Version   int           `json:"version"`
	Priority  int           `json:"priority,omitempty"`
}

type SyncRequest struct {
	Operations []Operation `json:"operations"`
}

type SyncResult struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Version int    `json:"version,omitempty"`
	Data    string `json:"data,omitempty"`
	Message string `json:"message,omitempty"`
}

type SyncResponse struct {
	Results []SyncResult `json:"results"`
}

type Record struct {
	ID        string `json:"id"`
	Data      string `json:"data"`
	Version   int    `json:"version"`
	UpdatedAt int64  `json:"updated_at"`
}

type PullResponse struct {
	Data []Record `json:"data"`
}

type ConflictRecord struct {
	ID        string
	Data      string
	Version   int
	Timestamp int64
	Status    string
	Strategy  ConflictStrategy
}

func (t OperationType) Valid() bool {
	switch t {
	case CREATE, UPDATE, DELETE:
		return true
	default:
		return false
	}
}

func (o Operation) Normalized() Operation {
	o.ID = strings.TrimSpace(o.ID)
	o.Type = OperationType(strings.ToUpper(strings.TrimSpace(string(o.Type))))

	if o.Priority <= 0 {
		o.Priority = DefaultPriority
	}

	if o.Timestamp == 0 {
		o.Timestamp = time.Now().Unix()
	}

	return o
}

func (o Operation) Validate() error {
	o = o.Normalized()

	if o.ID == "" {
		return fmt.Errorf("operation id is required")
	}

	if !o.Type.Valid() {
		return fmt.Errorf("unsupported operation type %q", o.Type)
	}

	if o.Version <= 0 {
		return fmt.Errorf("operation version must be greater than zero")
	}

	return nil
}

func (r SyncRequest) Validate() error {
	if len(r.Operations) == 0 {
		return fmt.Errorf("operations cannot be empty")
	}

	for i, op := range r.Operations {
		if err := op.Validate(); err != nil {
			return fmt.Errorf("operation %d: %w", i, err)
		}
	}

	return nil
}
