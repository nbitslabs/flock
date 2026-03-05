# Flock - Agent Orchestration System

## Architecture Decisions

- **SQLite driver**: `modernc.org/sqlite` (pure Go, no CGO)
- **Migrations**: goose with embedded SQL files, auto-run on startup via `goose.Up()`
- **HTTP routing**: `net/http` with Go 1.22+ patterns (method + path)
- **Real-time**: Server-Sent Events (SSE)
- **CSS**: TailwindCSS standalone CLI with `-c web/tailwind.config.js`, output committed to repo
- **Frontend**: Vanilla JS (IIFE, no global scope pollution), no frameworks
- **OpenCode mode**: `opencode serve` (NOT `opencode acp` — acp disposes immediately)
- **Port discovery**: Parse OpenCode stdout for `listening on http://...` line
- **Binary resolution**: `exec.LookPath("opencode")` at startup, stored as absolute path

## Key Patterns

- Flock is a proxy/orchestrator over OpenCode server instances
- Each OpenCode instance is a child process with its own port
- SSE events flow: OpenCode `/event` -> Manager -> SSE Broker (per-session routing) -> Browser
- All instance/session metadata stored in SQLite for quick lookups
- Session-to-instance mapping in our DB enables proxying without knowing which instance a session belongs to

## OpenCode API Reference

- `POST /session` — create session (returns `{id, slug, title, time: {created, updated}}`)
- `GET /session` — list sessions
- `GET /session/{id}/message` — get messages (returns `[{info: {id, role, time}, parts: [{type, text}]}]`)
- `POST /session/{id}/message` — send message (body: `{parts: [{type: "text", text: "..."}]}`)
- `GET /event` — SSE stream (data-only, no `event:` field)
  - Key event types: `message.part.delta` (streaming tokens), `message.part.updated`, `session.status`, `session.idle`
  - Session ID found in: `properties.sessionID`, `properties.info.sessionID`, or `properties.part.sessionID`

## Lessons Learned

- `opencode acp` mode creates an instance then immediately disposes it — use `opencode serve` instead
- OpenCode API uses **singular** paths (`/session`, `/session/{id}/message`, `/event`) not plural
- OpenCode SSE sends `data:` lines only (no `event:` field) — use `EventSource.onmessage` not `addEventListener`
- TailwindCSS CLI needs explicit `-c path/to/config.js` or it can't find content paths to scan
- `exec.Command("opencode", ...)` may fail in subprocess if PATH differs — resolve with `LookPath` at startup
- OpenCode messages have `{info: {role, ...}, parts: [...]}` structure, not flat `{role, content}`
- Message parts include `step-start`, `reasoning`, `text`, `step-finish` — only render `text` type for clean output
