# Flock MVP - Agent Orchestration System

## Context

We're building Flock, a local agent orchestration system for development work, inspired by [antfarm](https://github.com/snarktank/antfarm). Rather than building our own agent runtime, we build around **OpenCode** (v1.2.13), which provides an ACP (Agent Client Protocol) server with a full REST API for session management, messaging, and event streaming.

The MVP goal: a Go server + vanilla web UI that can spawn multiple OpenCode instances, create sessions within them, send messages, and stream responses in real-time.

## Architecture

```
Browser (vanilla HTML/JS/CSS + TailwindCSS)
    |  HTTP + SSE
    v
Flock Go Server (:8080)
    |  HTTP proxy + SSE forwarding
    |  Manages N child processes
    v
OpenCode ACP Instance #1 (:auto-port, --cwd /project/a)
OpenCode ACP Instance #2 (:auto-port, --cwd /project/b)
...
```

Flock is a **proxy/orchestrator**: it spawns OpenCode ACP servers as child processes, tracks them in SQLite, and presents a unified web UI that fans out requests to the correct OpenCode instance.

## Key Decisions

| Decision | Choice | Rationale |
|---|---|---|
| SQLite driver | `modernc.org/sqlite` (pure Go) | No CGO needed, simplifies builds |
| Migrations | goose embedded via `embed.FS`, auto-run on startup | No CLI dependency at runtime |
| HTTP routing | `net/http` with Go 1.22+ patterns | No external router needed |
| Real-time | SSE (Server-Sent Events) | Simple, browser-native, no websocket libraries |
| CSS | TailwindCSS standalone CLI, built ahead of time | Committed to repo, embedded in binary |
| Frontend | Vanilla JS, no frameworks | Per requirements |
| Port discovery | Parse OpenCode's stderr for listening URL | More robust than pre-allocating ports |

## Project Structure

```
flock/
├── cmd/flock/main.go                    # Entry point
├── internal/
│   ├── server/
│   │   ├── server.go                    # HTTP server, router, middleware
│   │   ├── handlers_instances.go        # /api/instances CRUD
│   │   ├── handlers_sessions.go         # /api/sessions proxy to OpenCode
│   │   └── sse.go                       # SSE broker for web UI clients
│   ├── opencode/
│   │   ├── manager.go                   # Instance lifecycle (spawn/stop/health)
│   │   ├── client.go                    # HTTP client for OpenCode ACP API
│   │   └── types.go                     # OpenCode API types
│   └── db/
│       ├── db.go                        # Open DB + auto-migrate
│       ├── queries.sql                  # sqlc query definitions
│       └── sqlc/                        # Generated code
├── migrations/
│   ├── 001_initial.sql                  # Schema
│   └── embed.go                         # //go:embed
├── web/
│   ├── static/app.js                    # Frontend logic
│   ├── static/styles.css                # TailwindCSS output (committed)
│   ├── templates/index.html             # Main page
│   ├── input.css                        # TailwindCSS source
│   ├── tailwind.config.js               # TailwindCSS config
│   └── embed.go                         # //go:embed
├── sqlc.yaml
├── Makefile
├── AGENTS.md                            # Decision log
├── go.mod / go.sum
```

## Database Schema (001_initial.sql)

```sql
CREATE TABLE instances (
    id TEXT PRIMARY KEY,
    pid INTEGER NOT NULL,
    port INTEGER NOT NULL,
    working_directory TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'starting',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    instance_id TEXT NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    title TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
```

## API Routes

| Method | Path | Description |
|---|---|---|
| GET | `/` | Serve web UI |
| GET | `/api/instances` | List all instances |
| POST | `/api/instances` | Spawn new OpenCode instance |
| GET | `/api/instances/{id}` | Get instance details |
| DELETE | `/api/instances/{id}` | Stop and remove instance |
| GET | `/api/instances/{id}/sessions` | List sessions for instance |
| POST | `/api/instances/{id}/sessions` | Create session in instance |
| GET | `/api/sessions/{id}` | Get session details |
| GET | `/api/sessions/{id}/messages` | Get session messages |
| POST | `/api/sessions/{id}/messages` | Send message to session |
| GET | `/api/sessions/{id}/events` | SSE stream for session events |

## Event Flow (Real-time Streaming)

1. Manager spawns OpenCode ACP, subscribes to its `/event` SSE endpoint
2. Web UI opens `EventSource` to `/api/sessions/{id}/events`
3. When OpenCode streams tokens, events flow: OpenCode -> Manager -> SSE Broker -> Browser
4. For MVP: broadcast all instance events to connected clients, filter by session ID in JS
5. Key events: `message.part.updated` (streaming tokens), `session.updated`, `session.idle` (generation complete)

## Implementation Steps (Git Commits)

### 1. `init: go module, project structure, makefile, and AGENTS.md`
- `go mod init`, add dependencies (goose, modernc/sqlite)
- Create Makefile (build, dev, css, sqlc targets)
- Create AGENTS.md with architectural decisions
- Stub files for directory structure

### 2. `feat: database schema, embedded migrations, and sqlc queries`
- `migrations/001_initial.sql` + `migrations/embed.go`
- `internal/db/db.go` (open + auto-migrate)
- `sqlc.yaml` + `internal/db/queries.sql`
- Generate sqlc code

### 3. `feat: opencode ACP HTTP client and types`
- `internal/opencode/types.go` (Session, Message, Part, Event types)
- `internal/opencode/client.go` (HTTP client: ListSessions, CreateSession, GetMessages, SendMessage, SubscribeEvents)

### 4. `feat: opencode instance lifecycle manager`
- `internal/opencode/manager.go`
- Spawn/stop/health-check OpenCode processes
- Port discovery by parsing stderr output
- Event forwarding goroutines
- Graceful shutdown (SIGTERM -> SIGKILL)

### 5. `feat: HTTP server, API routes, and SSE streaming`
- `internal/server/server.go` (mux, middleware, startup)
- `internal/server/handlers_instances.go`
- `internal/server/handlers_sessions.go`
- `internal/server/sse.go` (broker pattern)

### 6. `feat: web UI with session management and real-time streaming`
- `web/templates/index.html` (sidebar + main area + input)
- `web/static/app.js` (state, API calls, SSE, DOM rendering)
- TailwindCSS setup + build
- `web/embed.go`

### 7. `feat: wire up main entrypoint and graceful shutdown`
- `cmd/flock/main.go` (flags, init DB, create manager, start server, signal handling)
- Mark stale instances as stopped on startup

## Web UI Design

- Dark theme (gray-950 background)
- Left sidebar (w-72): instance list with status dots + session list
- Main area: message history (chat bubbles) + input textarea
- Modal for creating new instances (working directory picker)
- Ctrl/Cmd+Enter to send messages
- Status indicators: green=running, yellow=starting, red=error, gray=stopped

## Verification

1. `make build` compiles successfully
2. `./bin/flock` starts, auto-migrates DB, serves UI on :8080
3. Create instance via UI -> OpenCode ACP spawns (check `ps aux | grep opencode`)
4. Create session -> appears in sidebar
5. Send message -> response streams in real-time via SSE
6. Stop instance -> process terminated, status updated
7. Ctrl+C flock -> all child processes cleaned up
