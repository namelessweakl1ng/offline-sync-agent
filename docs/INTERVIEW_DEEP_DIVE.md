# Offline Sync Agent: Detailed Code Walkthrough and Interview Guide

This document explains the project in a way that is useful for:

- understanding the codebase quickly
- explaining the architecture in interviews
- defending design decisions
- answering questions like:
  - "How does offline sync work?"
  - "What algorithm did you use?"
  - "Why did you structure it this way?"
  - "How do you handle conflicts, retries, and partial failures?"

---

## 1. What This Project Is

This project is an **offline-first synchronization system** written in Go.

It has two main executable programs:

- a **CLI client** in `cmd/`
- a **backend HTTP server** in `backend/`

The client stores work locally when the network is unavailable, then synchronizes that work with the server when connectivity returns.

In simple terms:

1. user creates local operations
2. operations are persisted in SQLite
3. sync service checks whether the server is reachable
4. queued operations are sent in batches
5. server accepts, rejects, or returns conflicts
6. client updates local state and retries when needed

This is a classic **offline-first architecture** using:

- a durable local queue
- optimistic version-based conflict detection
- retry with adaptive backoff
- pull-after-push reconciliation

---

## 2. The Big Picture Architecture

### Main flow

```text
CLI command
  ->
Queue repository writes operation to SQLite
  ->
Sync service reads unsynced operations
  ->
Network client checks server health
  ->
Network client pushes operations to /sync
  ->
Backend store applies operations
  ->
Client marks successful items synced
  ->
Client logs conflicts and requeues retries
  ->
Client pulls remote updates from /pull
```

### Why this architecture is good

- It separates command handling from sync logic.
- It separates sync logic from HTTP transport.
- It separates HTTP transport from persistence.
- It keeps the system testable because each layer can be mocked.
- It prepares the backend for a future Postgres implementation without changing the handler layer.

This is an example of **separation of concerns** plus **dependency inversion**.

---

## 3. Folder-by-Folder Explanation

## `backend/main.go`

This is the backend entrypoint.

### What it does

- loads server configuration from environment variables
- validates required config
- creates the logger
- creates the storage backend
- creates a context that is canceled on `SIGINT` or `SIGTERM`
- starts the HTTP server
- shuts down gracefully when the process is stopped

### Why it is small

A `main.go` file should usually be thin. It should mostly do wiring:

- create dependencies
- start the app
- exit on fatal startup errors

This is better than putting business logic directly in `main.go`, because that logic becomes hard to test and hard to reuse.

### Interview explanation

You can say:

> I kept the entrypoint intentionally small. Its responsibility is composition, not business logic. That makes the runtime easier to reason about and allows the actual server behavior to live in normal packages where it can be tested.

---

## `cmd/main.go`

This is the CLI entrypoint.

### What it does

- loads client configuration
- creates the logger
- opens the local SQLite database
- constructs the queue repository
- constructs the network client
- constructs the sync service
- constructs the CLI app
- runs the requested CLI command
- handles shutdown signals cleanly

### Why this structure matters

The CLI entrypoint is only responsible for dependency wiring. It does not contain:

- parsing logic for individual commands
- sync algorithm details
- direct SQL

All of that has been pushed into internal packages.

This is cleaner because each package has one job.

---

## `internal/config/config.go`

This file centralizes configuration loading.

### What it does

It defines:

- `ClientConfig`
- `ServerConfig`

It loads values from environment variables and parses:

- durations
- integers
- booleans
- log levels

It also validates required values like:

- `AUTH_TOKEN`
- `SERVER_URL`
- `PORT`

### Why this exists

Without a config package, environment reads get scattered everywhere in the codebase.

That causes problems:

- duplicated parsing logic
- inconsistent defaults
- hidden dependencies
- harder tests

Centralizing config makes startup explicit and predictable.

### Interview explanation

> I moved all env parsing into a dedicated config package so the rest of the code depends on typed config structs instead of raw environment variables. That improves consistency, readability, and testability.

---

## `internal/logging/logger.go`

This file creates the structured logger.

### What it does

- creates a `slog` JSON logger
- sets log level support
- normalizes timestamp output

### Why structured logging instead of `fmt.Println`

Structured logging is better in production because logs become machine-readable.

Example benefits:

- log aggregation systems can index fields like `request_id`
- easier filtering by `level`, `path`, `status`, `duration`
- consistent format across backend and client

### Interview explanation

> I replaced ad hoc prints with structured logging because production systems need searchable, filterable logs. JSON logs are much easier to consume in container and platform environments like Railway.

---

## `internal/models/models.go`

This file contains the shared domain model types.

### Core types

- `OperationType`
- `ConflictStrategy`
- `Operation`
- `SyncRequest`
- `SyncResult`
- `SyncResponse`
- `Record`
- `PullResponse`
- `ConflictRecord`

### Important methods

#### `OperationType.Valid()`

Checks whether the operation is one of:

- `CREATE`
- `UPDATE`
- `DELETE`

This prevents unsupported values from silently entering the system.

#### `Operation.Normalized()`

This method normalizes input by:

- trimming the ID
- uppercasing the type
- defaulting priority if missing
- defaulting timestamp if missing

This is useful because external input is rarely perfect.

#### `Operation.Validate()`

This enforces invariants:

- ID must exist
- type must be supported
- version must be greater than zero

This is important because validation should happen close to the boundary of the system.

### Why a shared models package is useful

Both client and server need the same message shapes. Defining them once avoids drift between:

- what the client sends
- what the server expects
- what tests verify

### Interview explanation

> I introduced typed request and response models because map-based JSON is fragile. Typed models give compile-time safety, clearer intent, and better validation boundaries.

---

## `internal/db/db.go`

This package is responsible for SQLite setup on the client side.

### What it does

- opens SQLite
- configures connection counts
- initializes schema
- creates indexes
- exposes a `Client` wrapper around `*sql.DB`

### Why `SetMaxOpenConns(1)`?

SQLite handles concurrency differently from server databases like Postgres. Using one open connection is a common and safe way to reduce lock contention in a write-heavy local embedded database setup.

### Tables

#### `operations`

Stores pending queue items.

Fields:

- `id`
- `type`
- `data`
- `timestamp`
- `synced`
- `version`
- `priority`

#### `conflicts`

Stores conflict history for later inspection and manual resolution.

#### `synced_data`

Stores the latest synced copy of records known by the client.

#### `metadata`

Stores key-value metadata like `last_sync`.

### Why indexes were added

Indexes help the most common queries:

- find unsynced operations quickly
- query by `(id, version)`
- inspect conflict history

### Interview explanation

> I used SQLite as the client-side durable queue because it gives persistence without requiring an external service. I also tuned the connection count and indexes to fit embedded database behavior rather than treating it like a server-side RDBMS.

---

## `internal/queue/queue.go`

This is the local queue repository.

### What it does

It wraps SQL access behind repository methods such as:

- `AddOperation`
- `GetUnsynced`
- `MarkSynced`
- `CleanupSynced`
- `SaveSyncedData`
- `DeleteSyncedData`
- `LogConflict`
- `ResolveConflict`
- `ListConflicts`
- `ListOperations`
- `PendingHighPriority`
- `CountUnsynced`

### Why this file matters

This is the abstraction between:

- business logic
- raw SQL

The sync service does not know SQL details. It only knows repository behavior.

That means:

- easier testing
- easier future storage changes
- better separation of concerns

### Important logic

#### `AddOperation`

This performs an **upsert**.

That means:

- insert if the operation does not exist
- update if the ID already exists

Why this is useful:

- the queue always keeps the latest known operation per ID
- repeated actions do not create uncontrolled duplication
- retries can replace older queued versions cleanly

This is a practical queue compaction strategy.

#### `GetUnsynced`

This orders operations by:

1. priority ascending
2. payload length ascending
3. timestamp ascending

Why this order:

- high-priority work goes first
- smaller payloads can complete faster
- older work still gets fairness

This is not a formal scheduling algorithm like shortest-job-first, but it borrows from those ideas.

### Algorithms used here

- **upsert-based deduplication**
- **priority-first queue ordering**
- **persistent durable queue**

### Interview explanation

> I put all database access behind a repository interface so the sync layer never needs to know SQL. The queue uses upserts to compact repeated operations and preserve the latest intent for a given record.

---

## `internal/network/network.go`

This package is the client-side HTTP transport layer.

### What it does

It defines:

- `Client`
- `Status`
- `Quality`
- `HTTPDoer`

It performs:

- server health checks
- push requests to `/sync`
- pull requests from `/pull`
- gzip compression for sync payloads
- auth header management

### Why `HTTPDoer` exists

`HTTPDoer` is a tiny interface:

```go
type HTTPDoer interface {
    Do(req *http.Request) (*http.Response, error)
}
```

This is a classic testability pattern.

Instead of hardcoding `*http.Client`, the code depends on behavior. That lets tests inject fake transports without opening real ports.

### `Check`

This calls `/healthz` and measures latency.

Latency is converted into a network quality classification:

- `< 500ms` => `fast`
- `< 2s` => `medium`
- otherwise => `slow`

### `Push`

This:

1. validates the payload
2. marshals JSON
3. gzips the request body
4. sends a `POST /sync`
5. checks status code
6. decodes typed response

### `Pull`

This:

1. sends `GET /pull?since=<ts>`
2. checks status code
3. decodes typed response

### Why gzip is used

Batch sync payloads can grow. Compression reduces:

- bandwidth usage
- request size
- sync cost on slower networks

### Interview explanation

> I isolated HTTP transport inside a client package so the sync engine works at the domain level, not at the socket level. I also used a tiny `HTTPDoer` interface so tests can inject fake clients and verify behavior without real network listeners.

---

## `internal/sync/sync.go`

This is the heart of the system.

If you get asked about the most important file, it is this one.

### What it is responsible for

- deciding when sync should happen
- reading local queue state
- choosing batch sizes
- pushing operations concurrently
- interpreting server responses
- handling conflicts
- cleaning up synced items
- pulling newer remote state
- managing retry intervals

This package contains the actual **sync orchestration algorithm**.

---

## 4. The Sync Algorithm in Plain English

### Step 1: Check network

`SyncNow()` starts by calling `remote.Check()`.

This gives:

- connectivity status
- measured latency
- quality classification

Why do this first?

- no point reading and pushing queue data if the server is unreachable
- quality can influence batching strategy

### Step 2: Load unsynced operations

The service calls `repo.GetUnsynced()`.

If there is nothing to push, it still performs a pull to receive server updates.

Why?

Because sync is not only about outbound writes. A client with no pending writes may still need inbound changes.

### Step 3: Choose batch size based on quality

`batchSizeForQuality()` uses a simple adaptive strategy:

- fast => batch size `5`
- medium => batch size `2`
- slow/offline => batch size `1`

This is a heuristic, not a complex control system.

Why it works:

- fast network can tolerate larger requests
- slow network benefits from smaller, less risky batches

### Step 4: Split operations into chunks

`chunkOperations()` slices the queue into small batches.

This is a simple batching algorithm with time complexity roughly `O(n)`.

### Step 5: Push chunks with a worker pool

`pushChunks()` uses:

- a jobs channel
- a results channel
- a bounded number of workers
- a wait group

This is the standard Go **worker pool concurrency pattern**.

Why use a worker pool?

- limits concurrent load
- avoids spawning unbounded goroutines
- improves throughput over purely serial requests

### Step 6: Process each sync result

For every server response item:

- `ok` => mark synced and update local mirror
- `conflict` => log conflict and maybe requeue
- `invalid` => log error and count failure

### Step 7: Clean up synced queue rows

After successful processing, `CleanupSynced()` removes rows already marked synced.

Why delete later instead of immediately?

Because marking first, then cleaning up later, makes flow safer and easier to reason about than deleting rows at the exact first success point.

### Step 8: Pull remote updates

After push processing, `PullUpdates()` fetches changes newer than the local `last_sync`.

This is important because the backend may contain:

- updates from another device
- newer server-side values
- accepted records whose fresh timestamps should become visible to the client

### Step 9: Backoff loop

`Run()` continuously calls `SyncNow()`.

If sync succeeds:

- reset backoff to base delay

If sync fails:

- if high-priority work exists, retry quickly
- otherwise double the backoff
- cap it at `maxBackoff`

This is **exponential backoff with a cap**, plus a **priority-based fast path**.

---

## 5. Conflict Resolution Strategy

The project uses **optimistic concurrency control** based on version numbers.

### Server-side rule

When the server receives an operation:

- if the record does not exist, accept it
- if the incoming version is greater than the server version, accept it
- if the incoming version is less than or equal to the server version, return a conflict

This is a classic version-check strategy.

### Why optimistic concurrency control?

Because in offline-first systems:

- the client may be disconnected for a long time
- pessimistic locks are not realistic
- you need independent progress while offline

So instead of preventing conflicts in advance, you detect them at sync time.

### Client conflict behavior

`handleConflict()` does the following:

#### Delete conflict

If the operation is a delete:

- log conflict
- do not automatically retry

Why?

Deletes are destructive. Auto-retrying deletes after a conflict is riskier than updates.

#### Update conflict with different server data

- merge local and server strings using `mergeData(local, server)`
- mark strategy as `MERGED`
- increase version
- requeue operation

#### Other retry-safe conflict

- mark strategy as `CLIENT_WINS`
- increase version
- requeue operation

### Important note

The merge algorithm here is intentionally simple:

```text
local + " | " + server
```

This is not a CRDT and not a semantic merge engine.

It is a practical placeholder that demonstrates the conflict-retry flow.

### Interview explanation

> I used optimistic concurrency with version checks because the system is offline-first, so pessimistic locking is not realistic. Conflicts are detected during sync, logged locally, and then either merged or retried depending on operation type and server state.

---

## 6. `internal/server/store.go`

This file defines the backend persistence abstraction.

### What it does

It defines the `Store` interface:

- `ApplyOperation`
- `PullSince`
- `Close`

It also provides `MemoryStore`, which is the current implementation.

### Why this file is important

This is what makes the backend **Postgres-ready**.

The HTTP layer only depends on the `Store` interface, not on an in-memory map directly.

So later, a Postgres store can be added by implementing the same methods.

### Memory store behavior

It keeps records in:

```go
map[string]models.Record
```

protected by:

```go
sync.RWMutex
```

### Why `RWMutex`?

- reads can happen concurrently
- writes remain exclusive

That is appropriate because:

- pulls are read-heavy
- apply operations are writes

### Conflict logic inside store

`ApplyOperation()` compares incoming version against current version.

This is where the backend decides:

- accept
- conflict
- invalid

### Interview explanation

> I extracted a `Store` interface so the HTTP API and request validation logic do not care whether storage is in-memory or database-backed. That gives a clean path to Postgres later without rewriting the handler layer.

---

## `internal/server/middleware.go`

This file contains cross-cutting HTTP concerns.

### Middleware included

#### Request ID middleware

- generates UUID per request
- stores it in context
- writes it back as `X-Request-ID`

Why?

- helps trace logs end-to-end
- useful for debugging production issues

#### Logging middleware

- captures status code
- measures duration
- logs method, path, request ID, status, remote address

Why a custom `statusRecorder`?

Because `http.ResponseWriter` does not expose status code after writing. So the code wraps it to capture the final status.

#### Auth middleware

- checks `Authorization: Bearer <token>`
- returns `401` otherwise

#### Rate limiting middleware

Uses a **fixed window rate limiter**.

### Fixed window algorithm

The limiter tracks:

- current window start time
- request count
- max allowed limit

If the minute changes:

- reset counter

If count exceeds limit:

- reject request

### Why fixed window?

It is simple and cheap.

Tradeoff:

- not as accurate as token bucket or sliding window
- good enough for basic protection in a small service

### Interview explanation

> I used middleware so auth, logging, and rate limiting stay out of handler code. The rate limiter is a simple fixed-window implementation because it is easy to reason about and sufficient for the current scale.

---

## `internal/server/server.go`

This file contains the HTTP server behavior.

### What it does

- creates the `http.Server`
- sets read/write/idle timeouts
- registers routes
- handles graceful shutdown
- validates requests
- decodes gzip if needed
- encodes JSON responses

### Route behavior

#### `/healthz`

Used by the client to check whether the server is reachable.

#### `/sync`

Accepts batch operations.

Main steps:

1. limit request body size
2. decode gzip if used
3. decode JSON with `DisallowUnknownFields`
4. validate request
5. apply each operation through the store
6. return typed response

### Why `DisallowUnknownFields`?

Because silent acceptance of unknown JSON fields can hide client-server contract bugs.

### `/pull`

Reads `since` timestamp and returns records with `updated_at > since`.

### Why graceful shutdown matters

Without graceful shutdown:

- requests may be interrupted mid-flight
- platforms like Railway may terminate the process during deploys or restarts

Graceful shutdown lets the server finish active requests within a configured timeout.

---

## `internal/cli/app.go`

This file contains CLI behavior.

### What it does

It parses commands and flags for:

- `add`
- `delete`
- `status`
- `conflicts`
- `resolve`
- `debug`
- `sync`

### Why a dedicated CLI package

If all CLI logic stayed in `cmd/main.go`, it would become a large, untestable switch statement mixed with startup wiring.

Moving it here gives:

- cleaner command boundaries
- easier future testing
- better help output

### Command behavior

#### `add`

- accepts `--data`
- also accepts positional payload
- generates UUID
- creates a `CREATE` operation
- enqueues it locally

#### `delete`

- accepts `--id`
- creates a `DELETE` operation
- enqueues it locally

#### `status`

- prints queued operations in tabular form

#### `conflicts`

- prints conflict history

#### `resolve`

- marks local conflict as resolved

#### `debug`

- prints local config status and queue count

#### `sync`

- supports `--once`
- supports interval and max-backoff flags
- validates required sync config

### Interview explanation

> I improved the CLI UX by moving command parsing into a dedicated package with command-specific flags, help output, and validation. That makes the client easier to use and easier to extend.

---

## 7. Tests

## `internal/queue/queue_test.go`

These tests verify repository behavior:

- upsert semantics
- priority detection
- conflict resolution updates

Why this matters:

- queue behavior is central to correctness
- bugs here can silently break sync order or retry behavior

## `internal/network/network_test.go`

These tests verify:

- health check behavior
- gzip encoding
- auth header use
- response decoding

The tests use a fake transport instead of real sockets.

Why?

- faster
- deterministic
- works in restricted environments

## `internal/sync/sync_test.go`

These tests verify the orchestrator:

- successful sync path
- conflict logging
- merged requeue logic
- pull-after-push flow

### Interview explanation

> I focused tests on the queue, transport, and sync orchestration layers because those contain the highest-value business behavior and the most failure-prone logic.

---

## 8. Algorithms and Patterns Used

This section is especially useful for interviews.

## Algorithm 1: Durable offline queue

### What it is

A persistent queue stored in SQLite.

### Why it is used

Because offline-first clients must survive:

- process restarts
- machine restarts
- network loss

An in-memory queue would lose pending operations.

---

## Algorithm 2: Optimistic concurrency control

### What it is

Conflict detection using version numbers.

### Rule

- incoming version must be greater than current version

### Why it is used

Because offline clients cannot hold locks while disconnected.

---

## Algorithm 3: Exponential backoff with cap

### What it is

On repeated failure:

- delay doubles
- upper bound prevents unbounded sleep

### Why it is used

- avoids hammering the server
- reduces waste on unstable networks
- still eventually retries

### Special improvement in this project

If high-priority work exists, the system retries quickly instead of waiting for full exponential delay.

This adds business-awareness to the retry loop.

---

## Algorithm 4: Worker pool

### What it is

A bounded number of goroutines process chunked batches concurrently.

### Why it is used

- better throughput than fully serial sync
- safer than unbounded goroutine spawning
- easier resource control

### Go primitives used

- channels
- goroutines
- wait groups

---

## Algorithm 5: Fixed-window rate limiting

### What it is

Count requests in a one-minute window and reject once the limit is exceeded.

### Why it is used

- simple to implement
- low memory overhead
- sufficient for basic protection

### Tradeoff

It is not as smooth as token bucket or sliding window rate limiting.

---

## Algorithm 6: Batching and chunking

### What it is

Split a list of operations into small groups before sending.

### Why it is used

- reduces request overhead compared to sending one request per operation
- avoids giant payloads
- allows partial progress

---

## Algorithm 7: Pull-after-push reconciliation

### What it is

After pushing local changes, fetch remote changes newer than `last_sync`.

### Why it is used

Because sync is bidirectional. The client must not only upload changes, it must also learn about newer server state.

---

## 9. Why the Code Is Written This Way

This is the section to use when someone asks "Why not just do X?"

## Why not keep everything in one file?

Because:

- hard to test
- hard to extend
- hard to reason about boundaries

The current structure separates:

- transport
- storage
- orchestration
- config
- CLI UX

## Why SQLite on the client?

Because it is:

- embedded
- durable
- easy to ship
- good for local state

## Why not Postgres on the backend already?

The project is prepared for it via the `Store` abstraction, but the current implementation deliberately keeps backend persistence simple so the architecture remains easy to understand and demo.

## Why use interfaces?

Interfaces were added where substitution actually matters:

- sync service depends on repository behavior, not concrete SQL
- sync service depends on remote client behavior, not concrete `http.Client`
- HTTP server depends on store behavior, not concrete map storage

This is useful because it improves:

- testability
- extensibility
- decoupling

## Why not use unsafe type assertions?

Because they panic at runtime if the JSON shape is not exactly what you expected.

Typed structs and safe decoding are much safer for production code.

## Why not auto-resolve every conflict?

Because destructive or semantically important operations should not always be retried blindly. The current design is intentionally conservative for delete conflicts.

---

## 10. Concurrency and Safety

### Client-side concurrency

The sync service uses concurrent workers for pushing chunks.

Safety measures:

- bounded worker count
- channels for communication
- wait group for shutdown coordination

### Backend concurrency

The in-memory store uses `sync.RWMutex`.

Why:

- multiple pulls can read concurrently
- writes still remain exclusive

### HTTP resource cleanup

The code consistently closes:

- request bodies
- response bodies
- gzip readers

This is important to avoid leaks.

### Graceful shutdown

The backend uses shutdown with timeout so it does not drop requests abruptly.

---

## 11. How to Explain the Project in an Interview

Here is a strong short answer:

> I built an offline-first sync system in Go with a CLI client and HTTP backend. The client persists operations locally in SQLite, then a sync service batches and compresses them, checks connectivity, pushes them concurrently with a worker pool, handles optimistic version-based conflicts, and pulls newer remote updates after each cycle. I refactored the system into clean layers for config, logging, storage, transport, and orchestration so it is easier to test, extend, and productionize.

Here is a stronger deeper answer:

> The key design problem was making sync reliable under intermittent connectivity. I used a durable SQLite queue on the client so operations survive restarts, optimistic concurrency control with versions because offline clients cannot hold locks, capped exponential backoff so failures do not hammer the backend, and a worker-pool-based batch sender for throughput. On the backend I separated HTTP handling from storage through a `Store` interface, which makes the system ready for a future Postgres implementation without changing the API layer.

---

## 12. Likely Interview Questions and Good Answers

## Q1. What conflict resolution strategy did you use?

**Answer:**

I used optimistic concurrency control with version numbers. The backend accepts writes only when the incoming version is newer than the stored version. Otherwise it returns a conflict. On the client side I log the conflict locally and either requeue a merged update or retry with a higher version depending on the operation type and returned server data.

## Q2. Why optimistic concurrency instead of locking?

**Answer:**

Because the system is offline-first. A disconnected client cannot hold a distributed lock for an extended time. Optimistic concurrency is a much better fit because clients can progress independently and reconcile later.

## Q3. How do you avoid hammering the server during failures?

**Answer:**

I used exponential backoff with a cap. After each failure the retry interval grows, which reduces load during outages. I also added a high-priority fast path so urgent work can retry sooner.

## Q4. Why did you use interfaces?

**Answer:**

I used interfaces at the layer boundaries where substitution matters: queue repository, remote client, and backend store. That kept the sync engine independent of SQL and independent of concrete HTTP transport, which made testing and future extension much easier.

## Q5. How does batching work?

**Answer:**

The sync service reads unsynced operations, chooses batch size based on measured network quality, chunks the operations into slices, and pushes those slices using a bounded worker pool. That improves throughput while keeping concurrency controlled.

## Q6. What are the main tradeoffs in your current design?

**Answer:**

The backend currently uses an in-memory store, so it is not yet durable across backend restarts. The merge strategy is intentionally simple string merging, so it demonstrates conflict flow rather than providing domain-aware merging. The rate limiter is fixed-window, which is simpler but less precise than a token-bucket approach.

## Q7. How is the backend prepared for Postgres?

**Answer:**

The HTTP layer depends on a `Store` interface rather than directly on an in-memory map. That means I can add a Postgres-backed implementation of `ApplyOperation`, `PullSince`, and `Close` without rewriting the handlers or middleware.

## Q8. What would you improve next?

**Answer:**

- implement a Postgres store for backend persistence
- add idempotency keys if cross-client duplicate submission becomes a concern
- replace fixed-window rate limiting with token bucket
- add metrics and tracing
- make merge behavior domain-specific instead of simple string concatenation

---

## 13. Good Technical Keywords to Use

These are strong terms you can naturally use in interviews:

- offline-first architecture
- durable local queue
- optimistic concurrency control
- version-based conflict detection
- worker pool
- bounded concurrency
- exponential backoff
- adaptive batch sizing
- bidirectional sync
- pull-after-push reconciliation
- repository pattern
- dependency inversion
- structured logging
- graceful shutdown
- middleware pipeline

---

## 14. One-Line Summary Per File

Use this section as a fast revision sheet.

- `backend/main.go`: wires together the backend process and graceful shutdown
- `cmd/main.go`: wires together the client process and CLI runtime
- `internal/config/config.go`: loads and validates environment-based configuration
- `internal/logging/logger.go`: creates structured JSON loggers
- `internal/models/models.go`: defines shared domain and API models plus validation
- `internal/db/db.go`: opens SQLite and initializes client-side schema
- `internal/queue/queue.go`: implements durable queue and conflict/history persistence
- `internal/network/network.go`: handles HTTP push, pull, health check, auth, and gzip
- `internal/sync/sync.go`: orchestrates the sync algorithm, retries, batching, and conflicts
- `internal/server/store.go`: defines backend storage abstraction and memory store
- `internal/server/middleware.go`: implements request ID, logging, auth, and rate limiting
- `internal/server/server.go`: runs the HTTP API and request validation flow
- `internal/cli/app.go`: implements CLI commands and UX
- `internal/*_test.go`: verifies queue, network, and sync behavior

---

## 15. Final Interview Story

If you need one polished answer, use this:

> I built an offline-first sync system in Go where the client persists operations locally in SQLite, then synchronizes them to an HTTP backend once connectivity is available. The core algorithm combines a durable local queue, optimistic version-based conflict detection, adaptive batching, gzip-compressed transport, a worker pool for bounded concurrent sync, and capped exponential backoff for retry behavior. I refactored the project into clean packages for config, logging, queue persistence, network transport, sync orchestration, and backend storage so the system is easier to test, maintain, and extend. The backend currently uses an in-memory store behind an interface, so moving to Postgres is a storage implementation change rather than an API rewrite.

---

## 16. How to Study This File

Best order:

1. read `cmd/main.go` and `backend/main.go`
2. read `internal/models/models.go`
3. read `internal/queue/queue.go`
4. read `internal/network/network.go`
5. read `internal/sync/sync.go`
6. read `internal/server/server.go`
7. read `internal/server/store.go`
8. review the tests

If you can clearly explain those eight parts, you can handle most interview questions about this project.
