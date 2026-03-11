# flock

Agent orchestration system that proxies and manages OpenCode instances.

## Installation

### Quick Install (One-Liner)

```bash
curl -sSL https://raw.githubusercontent.com/nbitslabs/flock/main/install.sh | bash
```

This will:
1. Install Go (if not present)
2. Install GitHub CLI (gh)
3. Install OpenCode
4. Build Flock from source
5. Create configuration files
6. Set up system services (systemd on Linux, launchd on macOS)
7. Configure auto-updates via cron

### Manual Installation

```bash
# Clone the repository
git clone https://github.com/nbitslabs/flock.git
cd flock

# Build the binary
go build -o flock ./cmd/flock

# Run
./flock
```

## Configuration

After installation, configure your environment:

```bash
# Authenticate with GitHub
gh auth login

# Authenticate with OpenCode
opencode auth login
```

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
