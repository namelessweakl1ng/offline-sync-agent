package models

type OperationType string
type ConflictStrategy string

const (
	SERVER_WINS ConflictStrategy = "SERVER_WINS"
	CLIENT_WINS ConflictStrategy = "CLIENT_WINS"
)

const (
	CREATE OperationType = "CREATE"
	UPDATE OperationType = "UPDATE"
	DELETE OperationType = "DELETE"
)

type Operation struct {
	ID        string
	Type      OperationType
	Data      string
	Timestamp int64
	Synced    bool
	Version   int
	Priority  int
}
