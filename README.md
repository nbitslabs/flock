# flock

Agent orchestration system that proxies and manages OpenCode instances.

## Run

```bash
go run ./cmd/flock
```

## Architecture

- **Proxy/Orchestrator**: Routes requests to child OpenCode instances
- **Instance Management**: Spawns/manages OpenCode processes, tracks state in SQLite
- **Real-time Events**: SSE pipeline from OpenCode -> Manager -> Browser
- **Database**: SQLite (`modernc.org/sqlite`) for instance/session metadata
- **Frontend**: Vanilla JS with TailwindCSS

## Tech Stack

- Go 1.22+
- SQLite (pure Go)
- TailwindCSS
- Server-Sent Events (SSE)
