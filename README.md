# 🚀 Offline-First Sync Engine (Go)

A production-style offline-first background sync agent built in Go, designed to reliably synchronize data between client and server under unreliable or intermittent network conditions.

---

# 🌍 Problem This Solves

Many real-world applications must function **without reliable internet access**.

### Examples:

* Field data collection (rural areas)
* Mobile apps with unstable connectivity
* Offline-first note-taking apps
* POS systems in low-network regions

### Without proper sync systems:

* ❌ Data is lost
* ❌ Conflicts overwrite user changes
* ❌ Apps become unusable offline

---

# 💡 Solution Overview

This project implements an **offline-first architecture** where:

1. All user actions are stored locally
2. Operations are queued instead of sent immediately
3. A background sync agent pushes data when online
4. The system resolves conflicts automatically
5. Data remains consistent across client and server

---

# 🧠 Core Concepts

### 1. Offline-First Design

* System works fully without internet
* Sync happens opportunistically

---

### 2. Operation-Based Sync (Event Queue)

Instead of syncing full state:

```text
CREATE → UPDATE → DELETE
```

Each action is stored as an operation in a queue.

---

### 3. Eventual Consistency

System guarantees:

> Data will become consistent over time

Not instantly — but reliably.

---

### 4. Conflict Resolution

When multiple updates happen:

* Version-based detection
* Client-wins retry strategy
* Merge fallback
* Conflict logging

---

# 🏗️ System Architecture

## 📦 Client (Sync Agent)

* SQLite database
* Persistent operation queue
* Background sync loop
* Network detection module
* Conflict resolution engine

---

## 🌐 Server

* REST API (`/sync`, `/pull`)
* In-memory datastore (demo)
* Version-based conflict handling

---

# 🔄 Sync Flow (Step-by-Step)

```text
[User Action]
     ↓
[Stored Locally]
     ↓
[Added to Queue]
     ↓
[Sync Agent Running in Background]
     ↓
[Network Available?]
     ↓ YES
[Batch + Compress Operations]
     ↓
[Send to Server (/sync)]
     ↓
[Server Validates + Applies]
     ↓
[Client Pulls Updates (/pull)]
     ↓
[Resolve Conflicts]
     ↓
[Mark Synced]
```

---

# ⚙️ Key Features

## 📦 Storage

* SQLite for persistence
* Operations table (queue)
* Synced data table
* Metadata (last sync timestamp)

---

## ⚡ Performance

* Batch processing
* Gzip compression
* Worker pool (parallel sync)
* Adaptive batch size (based on latency)

---

## 🌐 Network Awareness

* Detects online/offline state
* Measures latency
* Skips sync on poor networks

---

## ⚔️ Conflict Handling

* Version-based detection
* Client-wins retry
* Merge strategy fallback
* Conflict history tracking

---

## 🧵 Concurrency

* Worker pool for parallel uploads
* Mutex for thread safety
* Channel-based job distribution

---

## 🔐 Security

* HTTPS (TLS)
* Token-based authentication
* Request size limits

---

## 🖥️ Observability

* Structured logs (INFO / ERROR)
* Request tracing (IDs)
* Sync duration tracking
* Debug CLI

---

## 🧪 Testing

* Unit test for queue operations
* In-memory DB support for test isolation

---

# 🧰 Tech Stack

* Go (Golang)
* SQLite
* HTTP (REST)
* Gzip
* TLS (HTTPS)

---

# ▶️ How to Run

## 1. Start backend

```bash
go run backend/main.go
```

---

## 2. Set environment variable

```bash
export AUTH_TOKEN=mysecrettoken123
```

---

## 3. Run client

### Add operation

```bash
go run cmd/main.go add
```

### Start sync agent

```bash
go run cmd/main.go sync
```

### Delete operation

```bash
go run cmd/main.go delete <id>
```

---

# 📊 Example Logs

```text
18:42:01 [INFO] Online. Starting sync
18:42:01 [INFO] Synced: abc123
18:42:01 [ERROR] Conflict detected: xyz456
```

---

# 📈 Design Decisions

### Why queue-based sync?

* Reliable under failures
* Supports retries
* Enables batching

---

### Why versioning?

* Simple conflict detection
* Lightweight alternative to CRDTs

---

### Why batching + compression?

* Reduces network overhead
* Improves performance on slow connections

---

# 🚧 Limitations (Honest)

* Backend uses in-memory store (not persistent)
* No multi-client coordination yet
* Uses static token instead of JWT

---

# 🚀 Future Improvements

* Persistent backend database (PostgreSQL)
* JWT authentication
* Rate limiting
* Horizontal scaling (multiple servers)
* CRDT-based conflict resolution

---

# 🎯 What This Demonstrates

This project showcases:

* Distributed systems thinking
* Offline-first architecture
* Eventual consistency
* Concurrency patterns in Go
* Real-world backend engineering

---

# 👨‍💻 Author

Built as a systems-focused backend project to demonstrate real-world sync engine design and distributed system principles.

---
