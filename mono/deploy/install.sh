#!/bin/bash
#=============================================================================
# AI PR Reviewer - Installation Script
#=============================================================================
#
# This script installs AIPR on Linux and macOS systems.
#
# Usage:
#   sudo ./install.sh [OPTIONS]
#
# Options:
#   --no-engine-service   Don't install/enable engine service
#   --no-server-service   Don't install/enable server service
#   --prefix <path>       Install prefix (default: /opt/aipr or /usr/local/opt/aipr)
#   --uninstall           Remove AIPR installation
#   --help                Show this help
#
# Requirements:
#   - Linux: systemd
#   - macOS: launchd
#   - Java 21+
#
#=============================================================================

set -e

# =============================================================================
# Configuration
# =============================================================================

VERSION="${AIPR_VERSION:-0.1.0}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Detect OS
OS="unknown"
if [[ "$OSTYPE" == "linux-gnu"* ]]; then
    OS="linux"
elif [[ "$OSTYPE" == "darwin"* ]]; then
    OS="macos"
else
    echo "Error: Unsupported operating system: $OSTYPE"
    exit 1
fi

# Default paths
if [[ "$OS" == "linux" ]]; then
    DEFAULT_PREFIX="/opt/aipr"
    CONFIG_DIR="/etc/aipr"
    LOG_DIR="/var/log/aipr"
    SYSTEMD_DIR="/etc/systemd/system"
    SERVICE_USER="aipr"
    SERVICE_GROUP="aipr"
else
    DEFAULT_PREFIX="/usr/local/opt/aipr"
    CONFIG_DIR="/usr/local/etc/aipr"
    LOG_DIR="/usr/local/var/log/aipr"
    LAUNCHD_DIR="/Library/LaunchDaemons"
    SERVICE_USER="_aipr"
    SERVICE_GROUP="_aipr"
fi

# Parse arguments
PREFIX="$DEFAULT_PREFIX"
INSTALL_ENGINE_SERVICE=true
INSTALL_SERVER_SERVICE=true
UNINSTALL=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --no-engine-service)
            INSTALL_ENGINE_SERVICE=false
            shift
            ;;
        --no-server-service)
            INSTALL_SERVER_SERVICE=false
            shift
            ;;
        --prefix)
            PREFIX="$2"
            shift 2
            ;;
        --uninstall)
            UNINSTALL=true
            shift
            ;;
        --help|-h)
            head -30 "$0" | tail -25
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# =============================================================================
# Helper Functions
# =============================================================================

log_info() {
    echo "[INFO] $1"
}

log_warn() {
    echo "[WARN] $1"
}

log_error() {
    echo "[ERROR] $1" >&2
}

log_ok() {
    echo "[OK] $1"
}

check_root() {
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root (sudo)"
        exit 1
    fi
}

check_java() {
    if ! command -v java &> /dev/null; then
        log_error "Java not found. Please install Java 21 or later."
        exit 1
    fi
    
    JAVA_VERSION=$(java -version 2>&1 | head -1 | cut -d'"' -f2 | cut -d'.' -f1)
    if [[ "$JAVA_VERSION" -lt 21 ]]; then
        log_error "Java 21 or later required. Found: Java $JAVA_VERSION"
        exit 1
    fi
    log_ok "Java $JAVA_VERSION found"
}

# =============================================================================
# Uninstall
# =============================================================================

do_uninstall() {
    log_info "Uninstalling AIPR..."
    
    # Stop services
    if [[ "$OS" == "linux" ]]; then
        systemctl stop aipr-server 2>/dev/null || true
        systemctl stop aipr-engine 2>/dev/null || true
        systemctl disable aipr-server 2>/dev/null || true
        systemctl disable aipr-engine 2>/dev/null || true
        rm -f "$SYSTEMD_DIR/aipr-engine.service"
        rm -f "$SYSTEMD_DIR/aipr-server.service"
        systemctl daemon-reload
    else
        launchctl unload "$LAUNCHD_DIR/ai.auralith.aipr-server.plist" 2>/dev/null || true
        launchctl unload "$LAUNCHD_DIR/ai.auralith.aipr-engine.plist" 2>/dev/null || true
        rm -f "$LAUNCHD_DIR/ai.auralith.aipr-engine.plist"
        rm -f "$LAUNCHD_DIR/ai.auralith.aipr-server.plist"
    fi
    
    # Remove files (but keep data and logs)
    rm -rf "$PREFIX/bin"
    rm -rf "$PREFIX/lib"
    rm -rf "$PREFIX/docs"
    rm -f "$PREFIX/README.txt"
    rm -f "$PREFIX/setup.sh"
    rm -f "$PREFIX/run.sh"
    
    log_info "Preserved: $PREFIX/data (user data)"
    log_info "Preserved: $CONFIG_DIR (configuration)"
    log_info "Preserved: $LOG_DIR (logs)"
    
    # Remove user (optional)
    read -p "Remove service user '$SERVICE_USER'? [y/N] " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        if [[ "$OS" == "linux" ]]; then
            userdel "$SERVICE_USER" 2>/dev/null || true
            groupdel "$SERVICE_GROUP" 2>/dev/null || true
        else
            dscl . -delete /Users/"$SERVICE_USER" 2>/dev/null || true
            dscl . -delete /Groups/"$SERVICE_GROUP" 2>/dev/null || true
        fi
        log_ok "User removed"
    fi
    
    log_ok "AIPR uninstalled"
    log_info "To remove all data: rm -rf $PREFIX $CONFIG_DIR $LOG_DIR"
}

# =============================================================================
# Install
# =============================================================================

create_user() {
    if [[ "$OS" == "linux" ]]; then
        if ! id "$SERVICE_USER" &>/dev/null; then
            log_info "Creating system user: $SERVICE_USER"
            useradd -r -s /sbin/nologin -d "$PREFIX" -c "AIPR Service Account" "$SERVICE_USER"
        else
            log_info "User $SERVICE_USER already exists"
        fi
    else
        if ! dscl . -read /Users/"$SERVICE_USER" &>/dev/null; then
            log_info "Creating system user: $SERVICE_USER"
            
            # Find available UID
            local uid=400
            while dscl . -list /Users UniqueID | grep -q "\\b$uid\\b"; do
                uid=$((uid + 1))
            done
            
            # Create group
            dscl . -create /Groups/"$SERVICE_GROUP"
            dscl . -create /Groups/"$SERVICE_GROUP" PrimaryGroupID $uid
            
            # Create user
            dscl . -create /Users/"$SERVICE_USER"
            dscl . -create /Users/"$SERVICE_USER" UniqueID $uid
            dscl . -create /Users/"$SERVICE_USER" PrimaryGroupID $uid
            dscl . -create /Users/"$SERVICE_USER" UserShell /usr/bin/false
            dscl . -create /Users/"$SERVICE_USER" NFSHomeDirectory "$PREFIX"
            dscl . -create /Users/"$SERVICE_USER" RealName "AIPR Service"
        else
            log_info "User $SERVICE_USER already exists"
        fi
    fi
}

create_directories() {
    log_info "Creating directories..."
    
    mkdir -p "$PREFIX"/{bin,lib,data,config}
    mkdir -p "$CONFIG_DIR"
    mkdir -p "$LOG_DIR"
    
    # Set ownership
    chown -R "$SERVICE_USER:$SERVICE_GROUP" "$PREFIX"
    chown -R "$SERVICE_USER:$SERVICE_GROUP" "$LOG_DIR"
}

copy_files() {
    log_info "Copying files to $PREFIX..."
    
    # Copy from distribution
    cp -r "$SCRIPT_DIR/bin/"* "$PREFIX/bin/" 2>/dev/null || true
    cp -r "$SCRIPT_DIR/lib/"* "$PREFIX/lib/" 2>/dev/null || true
    cp -r "$SCRIPT_DIR/docs" "$PREFIX/" 2>/dev/null || true
    cp "$SCRIPT_DIR/README.txt" "$PREFIX/" 2>/dev/null || true
    cp "$SCRIPT_DIR/setup.sh" "$PREFIX/" 2>/dev/null || true
    cp "$SCRIPT_DIR/run.sh" "$PREFIX/" 2>/dev/null || true
    
    # Copy config (don't overwrite existing)
    if [[ ! -f "$CONFIG_DIR/engine.yml" ]]; then
        cp "$SCRIPT_DIR/config/default.yml" "$CONFIG_DIR/engine.yml"
    fi
    if [[ ! -f "$CONFIG_DIR/server.yml" ]]; then
        cp "$SCRIPT_DIR/config/default.yml" "$CONFIG_DIR/server.yml"
    fi
    if [[ -d "$SCRIPT_DIR/config/certificates" && ! -d "$CONFIG_DIR/certificates" ]]; then
        cp -r "$SCRIPT_DIR/config/certificates" "$CONFIG_DIR/"
    fi
    
    # Make binaries executable
    chmod +x "$PREFIX/bin/"*
    
    # Set ownership
    chown -R "$SERVICE_USER:$SERVICE_GROUP" "$PREFIX"
    chown -R "$SERVICE_USER:$SERVICE_GROUP" "$CONFIG_DIR"
}

install_linux_services() {
    log_info "Installing systemd services..."
    
    if [[ "$INSTALL_ENGINE_SERVICE" == true ]]; then
        # Copy engine service
        if [[ -f "$SCRIPT_DIR/systemd/aipr-engine.service" ]]; then
            cp "$SCRIPT_DIR/systemd/aipr-engine.service" "$SYSTEMD_DIR/"
        else
            # Generate service file
            cat > "$SYSTEMD_DIR/aipr-engine.service" << EOF
[Unit]
Description=AIPR Indexing & Retrieval Engine
After=network.target

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_GROUP
WorkingDirectory=$PREFIX
Environment="ENGINE_HOST=0.0.0.0"
Environment="ENGINE_PORT=50051"
Environment="ENGINE_CONFIG=$CONFIG_DIR/engine.yml"
ExecStart=$PREFIX/bin/aipr-engine --config $CONFIG_DIR/engine.yml
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=aipr-engine

[Install]
WantedBy=multi-user.target
EOF
        fi
        log_ok "aipr-engine.service installed"
    fi
    
    if [[ "$INSTALL_SERVER_SERVICE" == true ]]; then
        # Copy server service
        if [[ -f "$SCRIPT_DIR/systemd/aipr-server.service" ]]; then
            cp "$SCRIPT_DIR/systemd/aipr-server.service" "$SYSTEMD_DIR/"
        else
            # Generate service file
            cat > "$SYSTEMD_DIR/aipr-server.service" << EOF
[Unit]
Description=AIPR Review Server
After=network.target aipr-engine.service
Requires=aipr-engine.service

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_GROUP
WorkingDirectory=$PREFIX
Environment="RT_HOME=$PREFIX"
Environment="JAVA_OPTS=-Xms512m -Xmx2g"
Environment="ENGINE_HOST=localhost"
Environment="ENGINE_PORT=50051"
ExecStart=$PREFIX/bin/aipr-server
Restart=on-failure
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=aipr-server

[Install]
WantedBy=multi-user.target
EOF
        fi
        log_ok "aipr-server.service installed"
    fi
    
    # Reload systemd
    systemctl daemon-reload
    
    # Enable services
    if [[ "$INSTALL_ENGINE_SERVICE" == true ]]; then
        systemctl enable aipr-engine
    fi
    if [[ "$INSTALL_SERVER_SERVICE" == true ]]; then
        systemctl enable aipr-server
    fi
}

install_macos_services() {
    log_info "Installing launchd daemons..."
    
    if [[ "$INSTALL_ENGINE_SERVICE" == true ]]; then
        if [[ -f "$SCRIPT_DIR/launchd/ai.auralith.aipr-engine.plist" ]]; then
            cp "$SCRIPT_DIR/launchd/ai.auralith.aipr-engine.plist" "$LAUNCHD_DIR/"
        fi
        # Update paths in plist
        sed -i '' "s|/usr/local/opt/aipr|$PREFIX|g" "$LAUNCHD_DIR/ai.auralith.aipr-engine.plist" 2>/dev/null || true
        sed -i '' "s|/usr/local/etc/aipr|$CONFIG_DIR|g" "$LAUNCHD_DIR/ai.auralith.aipr-engine.plist" 2>/dev/null || true
        sed -i '' "s|/usr/local/var/log/aipr|$LOG_DIR|g" "$LAUNCHD_DIR/ai.auralith.aipr-engine.plist" 2>/dev/null || true
        
        launchctl load "$LAUNCHD_DIR/ai.auralith.aipr-engine.plist"
        log_ok "ai.auralith.aipr-engine daemon installed"
    fi
    
    if [[ "$INSTALL_SERVER_SERVICE" == true ]]; then
        if [[ -f "$SCRIPT_DIR/launchd/ai.auralith.aipr-server.plist" ]]; then
            cp "$SCRIPT_DIR/launchd/ai.auralith.aipr-server.plist" "$LAUNCHD_DIR/"
        fi
        # Update paths in plist
        sed -i '' "s|/usr/local/opt/aipr|$PREFIX|g" "$LAUNCHD_DIR/ai.auralith.aipr-server.plist" 2>/dev/null || true
        sed -i '' "s|/usr/local/etc/aipr|$CONFIG_DIR|g" "$LAUNCHD_DIR/ai.auralith.aipr-server.plist" 2>/dev/null || true
        sed -i '' "s|/usr/local/var/log/aipr|$LOG_DIR|g" "$LAUNCHD_DIR/ai.auralith.aipr-server.plist" 2>/dev/null || true
        
        launchctl load "$LAUNCHD_DIR/ai.auralith.aipr-server.plist"
        log_ok "ai.auralith.aipr-server daemon installed"
    fi
}

create_env_files() {
    # Create environment file templates for secrets
    if [[ ! -f "$CONFIG_DIR/engine.env" ]]; then
        cat > "$CONFIG_DIR/engine.env" << 'EOF'
# AIPR Engine Environment Variables
# Uncomment and set values as needed

# ENGINE_HOST=0.0.0.0
# ENGINE_PORT=50051
# ENGINE_TLS_ENABLED=true
# ENGINE_TLS_CERT=/etc/aipr/certificates/server.crt
# ENGINE_TLS_KEY=/etc/aipr/certificates/server.key
EOF
        chmod 600 "$CONFIG_DIR/engine.env"
        chown "$SERVICE_USER:$SERVICE_GROUP" "$CONFIG_DIR/engine.env"
    fi
    
    if [[ ! -f "$CONFIG_DIR/server.env" ]]; then
        cat > "$CONFIG_DIR/server.env" << 'EOF'
# AIPR Server Environment Variables
# Set your secrets here (this file should be mode 600)

# Database
# DATABASE_PASSWORD=your_database_password

# LLM API
# LLM_API_KEY=your_openai_api_key

# JWT
# JWT_SECRET=your_jwt_secret_here
EOF
        chmod 600 "$CONFIG_DIR/server.env"
        chown "$SERVICE_USER:$SERVICE_GROUP" "$CONFIG_DIR/server.env"
    fi
}

print_summary() {
    echo ""
    echo "=============================================="
    echo " AIPR Installation Complete"
    echo "=============================================="
    echo ""
    echo "Installation directory: $PREFIX"
    echo "Configuration:          $CONFIG_DIR"
    echo "Logs:                   $LOG_DIR"
    echo ""
    
    if [[ "$OS" == "linux" ]]; then
        echo "Start services:"
        echo "  sudo systemctl start aipr-engine"
        echo "  sudo systemctl start aipr-server"
        echo ""
        echo "View logs:"
        echo "  journalctl -u aipr-engine -f"
        echo "  journalctl -u aipr-server -f"
    else
        echo "Services are now running via launchd."
        echo ""
        echo "View logs:"
        echo "  tail -f $LOG_DIR/engine.log"
        echo "  tail -f $LOG_DIR/server.log"
    fi
    
    echo ""
    echo "Next steps:"
    echo "  1. Edit configuration:  $CONFIG_DIR/engine.yml"
    echo "  2. Set secrets:         $CONFIG_DIR/server.env"
    echo "  3. Web Dashboard:       http://localhost:8080"
    echo ""
}

# =============================================================================
# Main
# =============================================================================

main() {
    echo "=============================================="
    echo " AI PR Reviewer - Installation Script"
    echo " Version: $VERSION"
    echo " OS: $OS"
    echo "=============================================="
    echo ""
    
    check_root
    
    if [[ "$UNINSTALL" == true ]]; then
        do_uninstall
        exit 0
    fi
    
    check_java
    create_user
    create_directories
    copy_files
    
    if [[ "$OS" == "linux" ]]; then
        install_linux_services
    else
        install_macos_services
    fi
    
    create_env_files
    print_summary
}

main "$@"
