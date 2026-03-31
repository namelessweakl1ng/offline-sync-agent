![Go](https://img.shields.io/badge/go-1.22-blue) ![License](https://img.shields.io/badge/license-MIT-green)

# 🚀 Offline Sync Agent

A lightweight **offline-first sync system** built in Go that reliably stores data locally and synchronizes it with a remote server when connectivity is available.

---

## ✨ Features

* 📦 **Persistent Queue** — Stores operations locally using SQLite
* 🔄 **Automatic Sync** — Detects network and syncs when online
* ⚡ **Retry with Backoff** — Handles unstable networks gracefully
* ⚔️ **Conflict Resolution** — Version-based detection + resolution strategies
* 🧵 **Concurrent Sync Workers** — Efficient batch processing
* 📡 **Network Awareness** — Adjusts behavior based on connection quality
* 🔐 **Token-based Authentication** — Secure API communication
* 🗑 **CRUD Support** — Create, Update, Delete operations

---

## 🧠 How It Works

1. Operations are stored locally when offline
2. They are queued in a persistent database
3. When network is available:

   * Batched requests are sent to server
   * Conflicts are detected and resolved
4. Server returns updated data
5. Local store is updated accordingly

---

## 🏗 Project Structure

```
.
├── backend/        # API server
├── cmd/            # CLI client
├── internal/
│   ├── db/         # SQLite database
│   ├── models/     # Data models
│   ├── network/    # Connectivity checks
│   ├── queue/      # Operation queue
│   └── sync/       # Sync engine
├── Dockerfile
├── README.md
```

---

## ⚙️ Setup

### 1. Clone the repository

```
git clone https://github.com/YOUR_USERNAME/offline-sync-agent.git
cd offline-sync-agent
```

---

### 2. Set Environment Variables

#### Backend

```
export AUTH_TOKEN=yourtoken
export PORT=8080
```

#### Client

```
export AUTH_TOKEN=yourtoken
export SERVER_URL=http://localhost:8080
```

⚠️ **Important:** The token must match between client and server.

---

## 🚀 Running the Project

### Start Backend Server

```
go run backend/main.go
```

---

### Run Client Commands

#### Add Operation

```
go run cmd/main.go add
```

#### Delete Operation

```
go run cmd/main.go delete <id>
```

#### Start Sync Loop

```
go run cmd/main.go sync
```

#### Check Queue Status

```
go run cmd/main.go status
```

#### View Conflicts

```
go run cmd/main.go conflicts
```

---

## 🔄 API Endpoints

### `POST /sync`

* Accepts batched operations
* Handles conflicts
* Returns sync status

### `GET /pull?since=<timestamp>`

* Fetches updated records from server

---

## ⚔️ Conflict Resolution

The system uses **version-based conflict detection**:

* If client version ≤ server version → conflict
* Strategies:

  * **Server Wins**
  * **Client Wins**
  * **Merge (basic implementation)**

Conflicts are logged and can be resolved manually.

---

## 📦 Database Schema

* `operations` → pending sync queue
* `synced_data` → latest synced state
* `conflicts` → conflict history
* `metadata` → last sync timestamp

---

## 🧪 Testing

```
go test ./...
```

---

## 🐳 Docker

Build and run:

```
docker build -t sync-agent .
docker run -p 8080:8080 sync-agent
```

---

## ⚠️ Limitations (Current)

* In-memory backend storage (no persistence yet)
* Simple conflict merge strategy
* Basic rate limiting

---

## 🚀 Future Improvements

* Delta sync (only changed fields)
* Persistent backend database
* Advanced merge strategies (CRDTs)
* Observability (metrics + logs)
* Real rate limiting
* Multi-device sync

---

## 💡 Use Cases

* Offline-first mobile/web apps
* Field data collection (low connectivity areas)
* Notes / task syncing systems
* Distributed data sync systems

---

## 🧑‍💻 Author

Built as a **systems design + backend engineering project** to demonstrate:

* distributed systems concepts
* offline-first architecture
* conflict resolution strategies

---

## ⭐ Contributing

Feel free to fork, improve, and submit PRs!

---

## 📜 License

MIT License
