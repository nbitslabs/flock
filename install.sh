#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="${FLOCK_INSTALL_DIR:-$HOME/.flock}"
FLOCK_REPO="https://github.com/nbitslabs/flock.git"
NON_INTERACTIVE="${NON_INTERACTIVE:-false}"
SKIP_AUTH="${SKIP_AUTH:-false}"
DOMAIN=""

log() {
    echo "[flock-install] $1"
}

detect_os() {
    case "$(uname -s)" in
        Linux*)     echo "linux";;
        Darwin*)    echo "macos";;
        *)          echo "unknown";;
    esac
}

detect_package_manager() {
    if command -v apt-get &> /dev/null; then
        echo "apt"
    elif command -v yum &> /dev/null; then
        echo "yum"
    elif command -v brew &> /dev/null; then
        echo "brew"
    else
        echo "unknown"
    fi
}

install_golang() {
    if command -v go &> /dev/null; then
        local version
        version=$(go version 2>/dev/null | sed -n 's/.*go\([0-9]*\.[0-9]*\).*/\1/p')
        local major minor
        major="${version%%.*}"
        minor="${version#*.}"
        if [[ "$major" -ge 1 ]] && [[ "$minor" -ge 22 ]]; then
            log "Go ${version} already installed"
            return 0
        fi
    fi
    
    log "Installing Go..."
    local go_os
    case "$(uname -s)" in
        Linux*)  go_os="linux";;
        Darwin*) go_os="darwin";;
        *)       go_os="linux";;
    esac
    local arch
    arch=$(uname -m)
    case "$arch" in
        x86_64)  arch="amd64";;
        aarch64) arch="arm64";;
        arm64)   arch="arm64";;
    esac

    local go_version="1.24.7"
    local go_archive="go${go_version}.${go_os}-${arch}.tar.gz"
    local go_url="https://go.dev/dl/${go_archive}"

    log "Downloading ${go_archive}..."
    curl -fSL "$go_url" -o "/tmp/${go_archive}"
    sudo rm -rf /usr/local/go
    sudo tar -C /usr/local -xzf "/tmp/${go_archive}"
    rm "/tmp/${go_archive}"
    
    export PATH="/usr/local/go/bin:$PATH"
    log "Go installed successfully"
}

install_github_cli() {
    if command -v gh &> /dev/null; then
        log "GitHub CLI already installed"
        return 0
    fi
    
    log "Installing GitHub CLI..."
    local os_type
    os_type=$(detect_os)
    
    if [[ "$os_type" == "macos" ]]; then
        brew install gh
    else
        curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | sudo dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg
        echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | sudo tee /etc/apt/sources.list.d/github-cli.list > /dev/null
        sudo apt-get update -y
        sudo apt-get install -y gh
    fi
}

install_opencode() {
    if command -v opencode &> /dev/null; then
        log "OpenCode already installed"
        return 0
    fi

    log "Installing OpenCode..."
    local oc_os oc_arch
    case "$(uname -s)" in
        Linux*)  oc_os="linux";;
        Darwin*) oc_os="darwin";;
    esac
    case "$(uname -m)" in
        x86_64)  oc_arch="x64";;
        aarch64) oc_arch="arm64";;
        arm64)   oc_arch="arm64";;
    esac

    local oc_archive="opencode-${oc_os}-${oc_arch}.tar.gz"
    local oc_url="https://github.com/anomalyco/opencode/releases/latest/download/${oc_archive}"

    log "Downloading ${oc_archive}..."
    curl -fSL "$oc_url" -o "/tmp/${oc_archive}"
    tar -xzf "/tmp/${oc_archive}" -C "${INSTALL_DIR}/bin"
    rm "/tmp/${oc_archive}"
    chmod +x "${INSTALL_DIR}/bin/opencode"

    add_to_path
    log "OpenCode installed successfully"
}

download_flock() {
    log "Downloading Flock..."
    
    if [[ -d "${INSTALL_DIR}/flock/.git" ]]; then
        cd "${INSTALL_DIR}/flock"
        git fetch origin main
        git reset --hard origin/main
        log "Flock updated to latest"
    else
        rm -rf "${INSTALL_DIR}/flock"
        git clone --depth 1 "$FLOCK_REPO" "${INSTALL_DIR}/flock"
        log "Flock downloaded"
    fi
}

build_flock() {
    log "Building Flock..."
    
    cd "${INSTALL_DIR}/flock"
    go build -o "${INSTALL_DIR}/bin/flock" ./cmd/flock
    
    log "Flock built successfully"
}

create_flock_config() {
    log "Creating Flock configuration..."
    
    mkdir -p "${INSTALL_DIR}"
    
    cat > "${INSTALL_DIR}/flock.toml" << EOF
opencode_url = "http://127.0.0.1:4096"
addr = ":7070"
db = "${INSTALL_DIR}/flock.db"
data_dir = "${INSTALL_DIR}"
base_path = "${INSTALL_DIR}/worktrees"

[agent]
enabled = true
heartbeat_interval_secs = 300
stuck_threshold_secs = 600
max_heartbeats_per_session = 20
wait_for_idle_timeout_secs = 180

[auth]
username = ""
password = ""
EOF
    
    log "Flock config created at ${INSTALL_DIR}/flock.toml"
}

create_opencode_config() {
    log "Creating OpenCode configuration..."
    
    mkdir -p "${INSTALL_DIR}/opencode"
    
    cat > "${INSTALL_DIR}/opencode/config.json" << 'EOF'
{
  "$schema": "https://opencode.ai/config.json",
  "server": {
    "port": 4096
  },
  "permission": {
    "*": {
      "*": "allow"
    }
  }
}
EOF
    
    log "OpenCode config created"
}

setup_systemd_services() {
    log "Setting up systemd services..."
    
    sudo tee /etc/systemd/system/flock.service > /dev/null << EOF
[Unit]
Description=Flock - Agent Orchestration System
After=network.target

[Service]
Type=simple
User=$USER
WorkingDirectory=${INSTALL_DIR}
Environment="PATH=${INSTALL_DIR}/bin:/usr/local/go/bin:$PATH"
ExecStart=${INSTALL_DIR}/bin/flock -config ${INSTALL_DIR}/flock.toml
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

    sudo tee /etc/systemd/system/opencode.service > /dev/null << EOF
[Unit]
Description=OpenCode Server
After=network.target

[Service]
Type=simple
User=$USER
WorkingDirectory=${INSTALL_DIR}
Environment="PATH=${INSTALL_DIR}/bin:/usr/local/go/bin:$PATH"
ExecStart=${INSTALL_DIR}/bin/opencode serve --config ${INSTALL_DIR}/opencode/config.json
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

    sudo systemctl daemon-reload
    sudo systemctl enable flock
    sudo systemctl enable opencode
    
    log "Systemd services configured"
}

setup_launchd_services() {
    log "Setting up launchd services..."
    
    mkdir -p "${HOME}/Library/LaunchAgents"
    cat > "${HOME}/Library/LaunchAgents/com.flock.flock.plist" << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.flock.flock</string>
    <key>ProgramArguments</key>
    <array>
        <string>${INSTALL_DIR}/bin/flock</string>
        <string>-config</string>
        <string>${INSTALL_DIR}/flock.toml</string>
    </array>
    <key>WorkingDirectory</key>
    <string>${INSTALL_DIR}</string>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>${INSTALL_DIR}/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin</string>
    </dict>
</dict>
</plist>
EOF

    cat > "${HOME}/Library/LaunchAgents/com.flock.opencode.plist" << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.flock.opencode</string>
    <key>ProgramArguments</key>
    <array>
        <string>${INSTALL_DIR}/bin/opencode</string>
        <string>serve</string>
        <string>--config</string>
        <string>${INSTALL_DIR}/opencode/config.json</string>
    </array>
    <key>WorkingDirectory</key>
    <string>${INSTALL_DIR}</string>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>${INSTALL_DIR}/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin</string>
    </dict>
</dict>
</plist>
EOF

    log "Launchd services configured"
}

setup_services() {
    local os_type
    os_type=$(detect_os)
    
    if [[ "$os_type" == "macos" ]]; then
        setup_launchd_services
    else
        setup_systemd_services
    fi
}

start_services() {
    local os_type
    os_type=$(detect_os)
    
    log "Starting services..."
    
    if [[ "$os_type" == "macos" ]]; then
        launchctl load "${HOME}/Library/LaunchAgents/com.flock.opencode.plist"
        launchctl load "${HOME}/Library/LaunchAgents/com.flock.flock.plist"
    else
        sudo systemctl start opencode
        sudo systemctl start flock
    fi
    
    log "Services started"
}

authenticate_github() {
    if [[ "$SKIP_AUTH" == "true" ]]; then
        log "Skipping GitHub authentication (SKIP_AUTH=true)"
        return 0
    fi

    if gh auth status &> /dev/null; then
        log "GitHub CLI already authenticated"
        return 0
    fi

    if [[ "$NON_INTERACTIVE" == "true" ]]; then
        log "GitHub CLI requires authentication. Run: gh auth login"
        return 0
    fi

    echo ""
    read -p "Would you like to authenticate GitHub CLI now? (Y/n): " gh_auth_choice < /dev/tty
    gh_auth_choice="${gh_auth_choice:-Y}"

    if [[ "$gh_auth_choice" =~ ^[Yy]$ ]]; then
        log "Please authenticate with GitHub CLI..."
        gh auth login
        log "GitHub CLI authenticated"
    else
        log "Skipping GitHub authentication. Run 'gh auth login' later."
    fi
}

authenticate_opencode() {
    if [[ "$SKIP_AUTH" == "true" ]]; then
        log "Skipping OpenCode authentication (SKIP_AUTH=true)"
        return 0
    fi

    if [[ "$NON_INTERACTIVE" == "true" ]]; then
        log "OpenCode requires authentication. Run: opencode auth login"
        return 0
    fi

    echo ""
    read -p "Would you like to authenticate OpenCode now? (Y/n): " oc_auth_choice < /dev/tty
    oc_auth_choice="${oc_auth_choice:-Y}"

    if [[ "$oc_auth_choice" =~ ^[Yy]$ ]]; then
        log "Please authenticate with OpenCode..."
        "${INSTALL_DIR}/bin/opencode" auth login
        log "OpenCode authenticated"
    else
        log "Skipping OpenCode authentication. Run 'opencode auth login' later."
    fi
}

setup_flock_auth() {
    if [[ "$SKIP_AUTH" == "true" ]]; then
        log "Skipping Flock auth setup (SKIP_AUTH=true)"
        return 0
    fi

    if [[ -n "${FLOCK_USERNAME:-}" ]] && [[ -n "${FLOCK_PASSWORD:-}" ]]; then
        log "Setting up Flock basic authentication..."

        sed -i.bak "s/username = \"\"/username = \"$FLOCK_USERNAME\"/" "${INSTALL_DIR}/flock.toml"
        sed -i.bak "s/password = \"\"/password = \"$FLOCK_PASSWORD\"/" "${INSTALL_DIR}/flock.toml"
        rm -f "${INSTALL_DIR}/flock.toml.bak"

        log "Flock auth configured"
    else
        log "Flock auth skipped (no credentials provided)"
    fi
}

install_nginx() {
    if command -v nginx &> /dev/null; then
        log "Nginx already installed"
        return 0
    fi
    
    log "Installing Nginx..."
    local pkg_manager
    pkg_manager=$(detect_package_manager)
    
    case "$pkg_manager" in
        apt)
            sudo apt update
            sudo apt install -y nginx
            ;;
        yum)
            sudo yum install -y nginx
            ;;
        brew)
            brew install nginx
            ;;
        *)
            log "Unknown package manager, cannot install nginx"
            return 1
            ;;
    esac
    
    log "Nginx installed"
}

setup_reverse_proxy() {
    if [[ -z "$DOMAIN" ]]; then
        log "No domain specified, skipping reverse proxy setup"
        return 0
    fi
    
    log "Setting up reverse proxy for domain: $DOMAIN"
    
    install_nginx
    
    log "Creating nginx configuration for $DOMAIN..."
    
    sudo tee "/etc/nginx/sites-available/flock" > /dev/null << EOF
server {
    listen 80;
    server_name $DOMAIN;

    location / {
        proxy_pass http://127.0.0.1:7070;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_cache_bypass \$http_upgrade;
        
        proxy_buffering off;
        proxy_read_timeout 86400;
    }

    location /event {
        proxy_pass http://127.0.0.1:7070;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        
        proxy_buffering off;
        proxy_read_timeout 86400;
    }
}
EOF

    sudo ln -sf "/etc/nginx/sites-available/flock" "/etc/nginx/sites-enabled/flock"
    sudo rm -f "/etc/nginx/sites-enabled/default"
    
    sudo nginx -t
    
    log "Nginx configuration created"
}

install_certbot() {
    if command -v certbot &> /dev/null; then
        log "Certbot already installed"
        return 0
    fi
    
    log "Installing Certbot..."
    local pkg_manager
    pkg_manager=$(detect_package_manager)
    
    case "$pkg_manager" in
        apt)
            sudo apt update
            sudo apt install -y certbot python3-certbot-nginx
            ;;
        yum)
            sudo yum install -y certbot python3-certbot-nginx
            ;;
        brew)
            brew install certbot
            ;;
        *)
            log "Unknown package manager, cannot install certbot"
            return 1
            ;;
    esac
    
    log "Certbot installed"
}

setup_ssl() {
    if [[ -z "$DOMAIN" ]]; then
        return 0
    fi
    
    log "Setting up SSL certificate for $DOMAIN..."
    
    install_certbot
    
    log "Requesting SSL certificate..."
    
    if [[ "$NON_INTERACTIVE" == "true" ]]; then
        sudo certbot --nginx -d "$DOMAIN" --agree-tos --email "admin@$DOMAIN" --redirect
    else
        sudo certbot --nginx -d "$DOMAIN" --redirect
    fi
    
    log "SSL certificate configured"
    
    sudo systemctl restart nginx
    sudo systemctl restart flock
}

setup_cronjob() {
    log "Setting up cronjob for automatic updates..."
    
    local cron_script="${INSTALL_DIR}/bin/flock-update.sh"
    
    cat > "$cron_script" << 'CRONSCRIPT'
#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="${FLOCK_INSTALL_DIR:-$HOME/.flock}"
LOG_FILE="${INSTALL_DIR}/logs/update.log"

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a "$LOG_FILE"
}

log "Starting Flock and OpenCode update..."

if [[ -d "${INSTALL_DIR}/flock/.git" ]]; then
    cd "${INSTALL_DIR}/flock"
    git fetch origin main
    if git diff --quiet origin/main -- .; then
        log "Flock already up to date"
    else
        git reset --hard origin/main
        go build -o "${INSTALL_DIR}/bin/flock" ./cmd/flock
        log "Flock updated"
    fi
fi

if command -v opencode &> /dev/null; then
    current_version=$(opencode --version 2>/dev/null || echo "unknown")
    opencode --version-check || true
    log "OpenCode current version: $current_version"
fi

log "Restarting services..."

if command -v systemctl &> /dev/null; then
    sudo systemctl restart opencode
    sleep 2
    sudo systemctl restart flock
elif command -v launchctl &> /dev/null; then
    launchctl unload "${HOME}/Library/LaunchAgents/com.flock.opencode.plist" 2>/dev/null || true
    launchctl unload "${HOME}/Library/LaunchAgents/com.flock.flock.plist" 2>/dev/null || true
    sleep 1
    launchctl load "${HOME}/Library/LaunchAgents/com.flock.opencode.plist"
    launchctl load "${HOME}/Library/LaunchAgents/com.flock.flock.plist"
fi

log "Update complete"
CRONSCRIPT

    chmod +x "$cron_script"
    
    (crontab -l 2>/dev/null | grep -v "flock-update.sh"; echo "0 3 * * * ${INSTALL_DIR}/bin/flock-update.sh >> ${INSTALL_DIR}/logs/update.log 2>&1") | crontab -
    
    log "Cronjob configured to run daily at 3 AM"
}

add_to_path() {
    local shell_rc
    case "${SHELL:-bash}" in
        *zsh) shell_rc="${HOME}/.zshrc";;
        *)   shell_rc="${HOME}/.bashrc";;
    esac
    
    if ! grep -q "${INSTALL_DIR}/bin" "$shell_rc" 2>/dev/null; then
        echo "export PATH=\"${INSTALL_DIR}/bin:\$PATH\"" >> "$shell_rc"
    fi
    export PATH="${INSTALL_DIR}/bin:$PATH"
}

verify_installation() {
    log "Verifying installation..."
    
    command -v flock || { log "ERROR: flock not found in PATH"; return 1; }
    command -v opencode || { log "ERROR: opencode not found in PATH"; return 1; }
    command -v gh || { log "ERROR: gh not found in PATH"; return 1; }
    
    [[ -f "${INSTALL_DIR}/flock.toml" ]] || { log "ERROR: flock.toml not found"; return 1; }
    [[ -f "${INSTALL_DIR}/opencode/config.json" ]] || { log "ERROR: opencode config not found"; return 1; }
    
    log "Installation verified successfully!"
    return 0
}

print_summary() {
    echo ""
    echo "============================================"
    echo "  Flock Installation Complete!"
    echo "============================================"
    echo ""
    echo "Installation directory: ${INSTALL_DIR}"
    echo ""
    echo "Services:"
    echo "  - Flock:     ${INSTALL_DIR}/bin/flock"
    echo "  - OpenCode:  ${INSTALL_DIR}/bin/opencode"
    echo ""
    echo "Configuration:"
    echo "  - Flock:     ${INSTALL_DIR}/flock.toml"
    echo "  - OpenCode:  ${INSTALL_DIR}/opencode/config.json"
    echo ""
    
    if [[ -n "$DOMAIN" ]]; then
        echo "Reverse Proxy:"
        echo "  - Domain:    https://${DOMAIN}"
        echo "  - SSL:       Enabled (certbot)"
        echo "  - Nginx:     Configured"
        echo ""
    else
        echo "Next steps:"
        echo "  1. Add to PATH: export PATH=\"${INSTALL_DIR}/bin:\$PATH\""
        echo "  2. Authenticate GitHub: gh auth login"
        echo "  3. Authenticate OpenCode: opencode auth login"
        echo "  4. Access Flock UI: http://localhost:7070"
        echo ""
    fi
    
    echo "To start services manually:"
    if [[ "$(detect_os)" == "macos" ]]; then
        echo "  launchctl load ${HOME}/Library/LaunchAgents/com.flock.flock.plist"
    else
        echo "  sudo systemctl start flock"
    fi
    echo ""
}

prompt_config() {
    if [[ "$NON_INTERACTIVE" == "true" ]]; then
        return 0
    fi

    echo ""
    echo "============================================"
    echo "  Flock Installer"
    echo "============================================"
    echo ""

    read -p "Install directory [${INSTALL_DIR}]: " input_install_dir < /dev/tty
    if [[ -n "$input_install_dir" ]]; then
        INSTALL_DIR="$input_install_dir"
    fi

    read -p "Domain for reverse proxy (leave blank to skip): " input_domain < /dev/tty
    DOMAIN="${input_domain:-}"

    echo ""
    echo "Configure Flock basic authentication:"
    read -p "Username: " input_username < /dev/tty
    if [[ -n "$input_username" ]]; then
        read -s -p "Password: " input_password < /dev/tty
        echo ""
        FLOCK_USERNAME="$input_username"
        FLOCK_PASSWORD="${input_password:-}"
    else
        FLOCK_USERNAME=""
        FLOCK_PASSWORD=""
        log "Flock auth skipped (no username provided)"
    fi

    echo ""
    log "Install directory: ${INSTALL_DIR}"
    if [[ -n "$DOMAIN" ]]; then
        log "Domain: ${DOMAIN}"
    fi
    echo ""
}

main() {
    log "Starting Flock installation..."
    log "OS: $(detect_os)"

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --install-dir)
                INSTALL_DIR="$2"
                shift 2
                ;;
            --domain)
                DOMAIN="$2"
                shift 2
                ;;
            --non-interactive)
                NON_INTERACTIVE="true"
                shift
                ;;
            --skip-auth)
                SKIP_AUTH="true"
                shift
                ;;
            *)
                echo "Unknown option: $1"
                exit 1
                ;;
        esac
    done

    prompt_config

    mkdir -p "${INSTALL_DIR}/bin"
    mkdir -p "${INSTALL_DIR}/logs"

    install_golang
    install_github_cli
    install_opencode

    download_flock
    build_flock

    create_flock_config
    create_opencode_config

    setup_flock_auth

    setup_services
    start_services

    authenticate_github
    authenticate_opencode

    setup_reverse_proxy
    setup_ssl

    setup_cronjob

    add_to_path
    verify_installation
    print_summary
    
    log "Installation complete!"
}

main "$@"
