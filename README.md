# Offline Sync Agent

`offline-sync-agent` is an offline-first sync system written in Go. It ships with a CLI client, an HTTP backend, a persistent local queue, batched sync, retry with backoff, conflict handling, gzip compression, and deployment-friendly server packaging.

The repository is structured as a maintainable open-source project rather than a one-off demo: configuration is centralized, logging is structured, the backend is middleware-driven, the sync engine is testable, and the storage layer is now abstracted so a Postgres-backed backend can be added without rewriting the HTTP API.

## Features

- Offline queue backed by SQLite on the client side
- Batched sync with gzip-compressed payloads
- Retry loop with adaptive backoff
- Conflict detection with merge-aware requeueing
- Structured JSON logging with `DEBUG`, `INFO`, `WARN`, and `ERROR`
- Graceful HTTP server shutdown
- Middleware for request IDs, request logging, auth, and basic rate limiting
- Typed request/response models and request validation
- Unit tests for queue, sync, and network layers

## Architecture

```text
           +-------------------+
           |  CLI client       |
           |  ./cmd            |
           +---------+---------+
                     |
                     v
           +-------------------+
           |  Sync service     |
           |  internal/sync    |
           +---------+---------+
                     |
      +--------------+--------------+
      |                             |
      v                             v
+-------------+             +---------------+
| Queue repo  |             | Network client|
| internal/   |             | internal/     |
| queue       |             | network       |
+------+------+             +-------+-------+
       |                            |
       v                            v
+-------------+             +---------------+
| SQLite       |             | Backend HTTP  |
| local state  |             | ./backend     |
+-------------+             +-------+-------+
                                        |
                                        v
                               +------------------+
                               | Store abstraction|
                               | internal/server  |
                               +------------------+
```

### Repository layout

```text
.
├── backend/                # Backend entrypoint
├── cmd/                    # CLI entrypoint
├── internal/
│   ├── cli/                # CLI command handling
│   ├── config/             # Environment-based config loading
│   ├── db/                 # SQLite setup for the local queue
│   ├── logging/            # Structured logger construction
│   ├── models/             # Shared request, response, and domain models
│   ├── network/            # Remote HTTP client
│   ├── queue/              # Local queue repository
│   ├── server/             # Backend HTTP server, middleware, storage abstraction
│   └── sync/               # Sync engine and retry loop
├── .env.example
├── CONTRIBUTING.md
├── LICENSE
├── Makefile
└── README.md
```

## How Sync Works

1. The CLI records operations locally in SQLite.
2. Unsynced operations are ordered by priority and timestamp.
3. The sync engine checks backend health and classifies network quality.
4. Pending operations are sent in batches to `POST /sync`.
5. Successful operations are marked synced and mirrored into `synced_data`.
6. Conflicts are logged locally. Non-delete conflicts are requeued with either:
   - a merged payload when the server has newer data
   - an incremented version when the payload is otherwise safe to retry
7. After a push cycle, the client calls `GET /pull?since=<unix-ts>` to fetch newer server-side records.
8. The retry loop adapts between a fast retry for high-priority work and exponential backoff for lower-priority failures.

### Conflict model

The backend returns a conflict when a client submits a version that is older than or equal to the server version for the same record. The client then:

- logs the conflict in the local `conflicts` table
- keeps delete conflicts manual by default
- merges non-delete payloads when server data differs
- bumps the local version and requeues the operation

## Quick Start

### 1. Clone the repository

```bash
git clone https://github.com/YOUR_USERNAME/offline-sync-agent.git
cd offline-sync-agent
```

### 2. Configure the environment

Copy `.env.example` into `.env` or export values manually.

Required for backend:

```bash
export AUTH_TOKEN=change-me
export PORT=8080
```

Required for sync-capable client commands:

```bash
export AUTH_TOKEN=change-me
export SERVER_URL=http://localhost:8080
```

Optional client settings:

```bash
export SYNC_DB_PATH=data.db
export SYNC_INTERVAL=5s
export SYNC_MAX_BACKOFF=30s
export LOG_LEVEL=INFO
```

### 3. Run the backend

```bash
make run-server
```

### 4. Queue some work

```bash
make run-client ARGS='add --data "hello offline world"'
make run-client ARGS='status'
```

### 5. Run sync

One-shot sync:

```bash
make run-client ARGS='sync --once'
```

Background loop:

```bash
make run-client ARGS='sync'
```

## CLI Usage

```bash
go run ./cmd help
go run ./cmd add --data "payload"
go run ./cmd delete --id record-123
go run ./cmd status
go run ./cmd conflicts
go run ./cmd resolve --id record-123
go run ./cmd sync --once
go run ./cmd sync --interval 3s --max-backoff 45s
```

The `add` command also accepts a positional payload for convenience:

```bash
go run ./cmd add "payload from argv"
```

## API Documentation

### `GET /healthz`

Returns server availability for client health checks.

Response:

```json
{
  "status": "ok"
}
```

### `POST /sync`

Accepts a batch of operations. The endpoint supports `Content-Encoding: gzip` and requires `Authorization: Bearer <AUTH_TOKEN>`.

Request:

```json
{
  "operations": [
    {
      "id": "record-1",
      "type": "CREATE",
      "data": "hello",
      "timestamp": 1710000000,
      "version": 1,
      "priority": 1
    }
  ]
}
```

Success response:

```json
{
  "results": [
    {
      "id": "record-1",
      "status": "ok",
      "version": 1
    }
  ]
}
```

Conflict response item:

```json
{
  "id": "record-1",
  "status": "conflict",
  "version": 5,
  "data": "server-side payload"
}
```

### `GET /pull?since=<unix-ts>`

Returns records updated after the provided Unix timestamp.

Response:

```json
{
  "data": [
    {
      "id": "record-1",
      "data": "latest payload",
      "version": 3,
      "updated_at": 1710000100
    }
  ]
}
```

## Configuration

### Client environment variables

| Variable | Default | Description |
| --- | --- | --- |
| `AUTH_TOKEN` | empty | Required for sync commands |
| `SERVER_URL` | empty | Required for sync commands |
| `SYNC_DB_PATH` | `data.db` | SQLite path for local queue state |
| `SYNC_INTERVAL` | `5s` | Delay between successful sync cycles |
| `SYNC_MAX_BACKOFF` | `30s` | Max retry backoff |
| `HTTP_TIMEOUT` | `10s` | HTTP client timeout |
| `INSECURE_SKIP_VERIFY` | `false` | Allow self-signed TLS in development |
| `LOG_LEVEL` | `INFO` | `DEBUG`, `INFO`, `WARN`, `ERROR` |

### Server environment variables

| Variable | Default | Description |
| --- | --- | --- |
| `AUTH_TOKEN` | none | Required bearer token |
| `PORT` | `8080` | HTTP listen port |
| `BACKEND_STORE` | `memory` | Current implementation: in-memory store |
| `RATE_LIMIT_PER_MINUTE` | `1000` | Basic fixed-window rate limit |
| `MAX_REQUEST_BODY_BYTES` | `1048576` | Max request body size |
| `HTTP_READ_TIMEOUT` | `10s` | Server read timeout |
| `HTTP_WRITE_TIMEOUT` | `15s` | Server write timeout |
| `HTTP_IDLE_TIMEOUT` | `60s` | Server idle timeout |
| `SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown timeout |
| `LOG_LEVEL` | `INFO` | `DEBUG`, `INFO`, `WARN`, `ERROR` |

## Development

Common commands:

```bash
make build
make test
make fmt
make run-server
make run-client ARGS='status'
```

Direct Go commands also work:

```bash
go build ./...
go test ./...
```

## Storage Roadmap

The backend currently ships with an in-memory implementation behind a `Store` interface. That interface was introduced specifically so a Postgres implementation can be added without changing the HTTP handlers, middleware, or request contracts.

## Deployment

The backend is already suitable for container-based deployment and Railway-style environments:

- reads `PORT` from the environment
- performs graceful shutdown on `SIGINT` and `SIGTERM`
- emits structured logs to standard error
- keeps transport concerns inside the backend process instead of inside handlers

## Testing

Unit tests cover:

- queue repository behavior
- sync orchestration and conflict requeueing
- network request encoding and response decoding

Run the full suite with:

```bash
make test
```

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md) for setup, workflow, and testing guidance.

## License

Released under the [MIT License](./LICENSE).
