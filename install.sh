#!/bin/bash
set -e

FLOCK_VERSION="latest"
FLOCK_DIR="$HOME/.flock"
FLOCK_BINARY="$FLOCK_DIR/bin/flock"
OPENCODE_DIR="$HOME/opencode"
CONFIG_DIR="$HOME/.config"
DATA_DIR="$FLOCK_DIR/data"
FLOCK_REPO="https://github.com/nbitslabs/flock.git"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_step() {
    echo -e "${BLUE}[STEP]${NC} $1"
}

detect_os() {
    if [[ "$OSTYPE" == "darwin"* ]]; then
        echo "macos"
    elif [[ "$OSTYPE" == "linux-gnu"* ]]; then
        echo "linux"
    else
        log_error "Unsupported operating system: $OSTYPE"
        exit 1
    fi
}

detect_package_manager() {
    local os="$1"
    if [[ "$os" == "macos" ]]; then
        if command -v brew &> /dev/null; then
            echo "brew"
        else
            log_error "Homebrew not found. Please install Homebrew first: https://brew.sh"
            exit 1
        fi
    elif [[ "$os" == "linux" ]]; then
        if command -v apt-get &> /dev/null; then
            echo "apt"
        elif command -v dnf &> /dev/null; then
            echo "dnf"
        elif command -v yum &> /dev/null; then
            echo "yum"
        else
            log_error "No supported package manager found (apt, dnf, yum)"
            exit 1
        fi
    fi
}

detect_architecture() {
    local arch
    arch=$(uname -m)
    if [[ "$arch" == "x86_64" ]]; then
        echo "amd64"
    elif [[ "$arch" == "arm64" ]] || [[ "$arch" == "aarch64" ]]; then
        echo "arm64"
    else
        log_error "Unsupported architecture: $arch"
        exit 1
    fi
}

install_go() {
    if command -v go &> /dev/null; then
        log_info "Go is already installed: $(go version)"
        return 0
    fi

    log_step "Installing Go..."

    local os="$1"
    local arch="$2"
    local go_version="1.23.5"
    local go_archive="go${go_version}.${os}-${arch}.tar.gz"

    curl -fsSL "https://go.dev/dl/${go_archive}" -o "/tmp/${go_archive}"
    sudo rm -rf /usr/local/go
    sudo tar -C /usr/local -xzf "/tmp/${go_archive}"
    rm "/tmp/${go_archive}"

    export PATH="/usr/local/go/bin:$PATH"
    export GOPATH="$HOME/go"
    export PATH="$GOPATH/bin:$PATH"

    if ! grep -q '/usr/local/go/bin' "$HOME/.bashrc" 2>/dev/null; then
        echo 'export PATH="/usr/local/go/bin:$PATH"' >> "$HOME/.bashrc"
    fi
    if ! grep -q '/usr/local/go/bin' "$HOME/.zshrc" 2>/dev/null; then
        echo 'export PATH="/usr/local/go/bin:$PATH"' >> "$HOME/.zshrc"
    fi

    log_info "Go installed successfully"
}

install_github_cli() {
    if command -v gh &> /dev/null; then
        log_info "GitHub CLI is already installed"
        return 0
    fi

    log_step "Installing GitHub CLI..."

    local pkg_mgr="$1"

    if [[ "$pkg_mgr" == "brew" ]]; then
        brew install gh
    elif [[ "$pkg_mgr" == "apt" ]]; then
        curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | sudo dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg
        echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | sudo tee /etc/apt/sources.list.d/github-cli.list > /dev/null
        sudo apt-get update
        sudo apt-get install gh
    elif [[ "$pkg_mgr" == "dnf" ]] || [[ "$pkg_mgr" == "yum" ]]; then
        sudo dnf config-manager --add-repo https://cli.github.com/packages/rpm/gh-cli.repo
        sudo dnf install gh
    fi

    log_info "GitHub CLI installed successfully"
}

install_opencode() {
    if command -v opencode &> /dev/null; then
        log_info "OpenCode is already installed: $(opencode --version 2>/dev/null || echo 'version unknown')"
        return 0
    fi

    log_step "Installing OpenCode..."

    local os="$1"
    local arch="$2"

    local opencode_version
    opencode_version=$(curl -sSL https://api.github.com/repos/opencodeai/opencode/releases/latest | grep '"tag_name"' | sed 's/.*v\([0-9.]*\).*/\1/')

    local opencode_filename="opencode-${os}-${arch}"
    local opencode_url="https://github.com/opencodeai/opencode/releases/download/v${opencode_version}/${opencode_filename}.tar.gz"

    mkdir -p "$OPENCODE_DIR/bin"
    curl -fsSL "$opencode_url" -o "/tmp/opencode.tar.gz"
    tar -C "$OPENCODE_DIR/bin" -xzf "/tmp/opencode.tar.gz"
    rm "/tmp/opencode.tar.gz"

    if [[ ! "$PATH" == *"$OPENCODE_DIR/bin"* ]]; then
        echo "export PATH=\"$OPENCODE_DIR/bin:\$PATH\"" >> "$HOME/.bashrc"
        echo "export PATH=\"$OPENCODE_DIR/bin:\$PATH\"" >> "$HOME/.zshrc"
    fi
    export PATH="$OPENCODE_DIR/bin:$PATH"

    log_info "OpenCode installed successfully"
}

build_flock() {
    log_step "Building Flock..."

    if [[ -d "$FLOCK_DIR/source" ]]; then
        log_info "Updating existing Flock source..."
        cd "$FLOCK_DIR/source"
        git pull origin main 2>/dev/null || git pull origin master 2>/dev/null || true
    else
        log_info "Cloning Flock repository..."
        git clone "$FLOCK_REPO" "$FLOCK_DIR/source"
        cd "$FLOCK_DIR/source"
    fi

    export PATH="/usr/local/go/bin:$PATH"
    export GOPATH="$HOME/go"
    export PATH="$GOPATH/bin:$PATH"

    go build -o "$FLOCK_BINARY" ./cmd/flock

    mkdir -p "$FLOCK_DIR/bin"

    log_info "Flock built successfully"
}

create_flock_config() {
    log_step "Creating Flock configuration..."

    mkdir -p "$FLOCK_DIR"

    if [[ ! -f "$FLOCK_DIR/flock.toml" ]]; then
        cat > "$FLOCK_DIR/flock.toml" << 'EOF'
opencode_url = "http://127.0.0.1:3000"
addr = ":7070"
db = "~/.flock/flock.db"
data_dir = "~/.flock"

[agent]
enabled = true
heartbeat_interval_secs = 300
stuck_threshold_secs = 600
max_heartbeats_per_session = 20
wait_for_idle_timeout_secs = 180
EOF
        log_info "Flock config created at $FLOCK_DIR/flock.toml"
    else
        log_info "Flock config already exists, skipping"
    fi
}

create_opencode_config() {
    log_step "Creating OpenCode configuration..."

    mkdir -p "$CONFIG_DIR/opencode"

    if [[ ! -f "$CONFIG_DIR/opencode/config.json" ]]; then
        cat > "$CONFIG_DIR/opencode/config.json" << 'EOF'
{
  "$schema": "https://opencode.ai/config.json",
  "server": {
    "port": 3000
  },
  "permission": {
    "*": {
      "*": "allow"
    }
  }
}
EOF
        log_info "OpenCode config created at $CONFIG_DIR/opencode/config.json"
    else
        log_info "OpenCode config already exists, skipping"
    fi
}

setup_systemd_services() {
    log_step "Setting up systemd services..."

    local user="$USER"
    local home="$HOME"

    sudo tee /etc/systemd/system/flock.service > /dev/null << EOF
[Unit]
Description=Flock - Agent Orchestration System
After=network.target

[Service]
Type=simple
User=$user
WorkingDirectory=$home/.flock
ExecStart=$home/.flock/bin/flock
Restart=on-failure
Environment="OPENCODE_URL=http://127.0.0.1:3000"
Environment="FLOCK_DATA_DIR=$home/.flock"

[Install]
WantedBy=multi-user.target
EOF

    sudo tee /etc/systemd/system/opencode.service > /dev/null << EOF
[Unit]
Description=OpenCode Server
After=network.target

[Service]
Type=simple
User=$user
ExecStart=$home/opencode/bin/opencode serve
Restart=on-failure
WorkingDirectory=$home

[Install]
WantedBy=multi-user.target
EOF

    sudo systemctl daemon-reload
    sudo systemctl enable flock
    sudo systemctl enable opencode

    log_info "Systemd services configured"
}

setup_launchd_services() {
    log_step "Setting up launchd services..."

    local home="$HOME"

    mkdir -p "$HOME/Library/LaunchAgents"

    cat > "$HOME/Library/LaunchAgents/com.flock.agent.plist" << 'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.flock.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>/Users/username/.flock/bin/flock</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>EnvironmentVariables</key>
    <dict>
        <key>OPENCODE_URL</key>
        <string>http://127.0.0.1:3000</string>
        <key>FLOCK_DATA_DIR</key>
        <string>/Users/username/.flock</string>
    </dict>
    <key>WorkingDirectory</key>
    <string>/Users/username/.flock</string>
</dict>
</plist>
EOF

    sed -i '' "s|/Users/username|$home|g" "$HOME/Library/LaunchAgents/com.flock.agent.plist"

    cat > "$HOME/Library/LaunchAgents/com.opencode.agent.plist" << 'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.opencode.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>/Users/username/opencode/bin/opencode</string>
        <string>serve</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>WorkingDirectory</key>
    <string>/Users/username</string>
</dict>
</plist>
EOF

    sed -i '' "s|/Users/username|$home|g" "$HOME/Library/LaunchAgents/com.opencode.agent.plist"

    log_info "Launchd services configured"
}

setup_cron_updates() {
    log_step "Setting up auto-update cron job..."

    mkdir -p "$FLOCK_DIR"

    cat > "$FLOCK_DIR/update.sh" << 'EOF'
#!/bin/bash
set -e

FLOCK_DIR="$HOME/.flock"
OPENCODE_DIR="$HOME/opencode"

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

log "Starting update..."

if [[ -d "$FLOCK_DIR/source" ]]; then
    cd "$FLOCK_DIR/source"
    git fetch origin
    LOCAL=$(git rev-parse HEAD)
    REMOTE=$(git rev-parse origin/main 2>/dev/null || git rev-parse origin/master 2>/dev/null)
    
    if [[ "$LOCAL" != "$REMOTE" ]]; then
        log "Updating Flock from $LOCAL to $REMOTE"
        git pull origin main 2>/dev/null || git pull origin master 2>/dev/null
        export PATH="/usr/local/go/bin:$PATH"
        go build -o "$FLOCK_DIR/bin/flock" ./cmd/flock
        log "Flock updated successfully"
    else
        log "Flock already up to date"
    fi
fi

log "Update complete"
EOF

    chmod +x "$FLOCK_DIR/update.sh"

    local cron_entry="0 3 * * * $FLOCK_DIR/update.sh >> $FLOCK_DIR/update.log 2>&1"

    if crontab -l 2>/dev/null | grep -q "flock/update.sh"; then
        log_info "Cron job already exists, skipping"
    else
        (crontab -l 2>/dev/null; echo "$cron_entry") | crontab -
        log_info "Cron job added for daily updates at 3 AM"
    fi
}

print_post_install_instructions() {
    echo ""
    echo "=============================================="
    echo -e "${GREEN}Flock installation complete!${NC}"
    echo "=============================================="
    echo ""
    echo "Next steps:"
    echo ""
    echo "1. Authenticate with GitHub CLI:"
    echo "   gh auth login"
    echo ""
    echo "2. Authenticate with OpenCode:"
    echo "   opencode auth login"
    echo ""
    echo "3. Start the services:"
    if [[ "$OS" == "linux" ]]; then
        echo "   sudo systemctl start flock"
        echo "   sudo systemctl start opencode"
    else
        echo "   launchctl load ~/Library/LaunchAgents/com.flock.agent.plist"
        echo "   launchctl load ~/Library/LaunchAgents/com.opencode.agent.plist"
    fi
    echo ""
    echo "4. Access Flock at: http://localhost:7070"
    echo ""
    echo "For auto-updates, a cron job has been set up to run daily at 3 AM."
    echo ""
}

main() {
    echo ""
    echo "=============================================="
    echo -e "${GREEN}Flock Installation Script${NC}"
    echo "=============================================="
    echo ""

    local os
    local pkg_mgr
    local arch

    os=$(detect_os)
    log_info "Detected OS: $os"

    pkg_mgr=$(detect_package_manager "$os")
    log_info "Detected package manager: $pkg_mgr"

    arch=$(detect_architecture)
    log_info "Detected architecture: $arch"

    export PATH="/usr/local/go/bin:$PATH"
    export GOPATH="$HOME/go"
    export PATH="$GOPATH/bin:$PATH"
    export PATH="$OPENCODE_DIR/bin:$PATH"

    install_go "$os" "$arch"
    install_github_cli "$pkg_mgr"
    install_opencode "$os" "$arch"
    build_flock
    create_flock_config
    create_opencode_config

    if [[ "$os" == "linux" ]]; then
        setup_systemd_services
    else
        setup_launchd_services
    fi

    setup_cron_updates

    print_post_install_instructions
}

main "$@"
