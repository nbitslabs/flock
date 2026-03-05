# Flock - Agent Orchestration System

## Architecture Decisions

- **SQLite driver**: `modernc.org/sqlite` (pure Go, no CGO)
- **Migrations**: goose with embedded SQL files, auto-run on startup
- **HTTP routing**: `net/http` with Go 1.22+ patterns (method + path)
- **Real-time**: Server-Sent Events (SSE)
- **CSS**: TailwindCSS standalone CLI, output committed to repo
- **Frontend**: Vanilla JS, no frameworks
- **Port discovery**: Parse OpenCode stderr for listening URL

## Key Patterns

- Flock is a proxy/orchestrator over OpenCode ACP instances
- Each OpenCode instance is a child process with its own port
- SSE events flow: OpenCode -> Manager -> SSE Broker -> Browser
- All instance metadata stored in SQLite
