#!/bin/bash
# =============================================================================
# AI PR Reviewer - Infrastructure Setup Script
# =============================================================================
# This script sets up the development/CI environment by downloading and
# configuring the required toolchains (JRE, Gradle, CMake).
#
# Usage:
#   ./scripts/setup.sh [options]
#
# Options:
#   --ci              CI mode (non-interactive, stricter)
#   --skip-jre        Skip JRE download (use system Java)
#   --skip-gradle     Skip Gradle download (use system Gradle)
#   --skip-cmake      Skip CMake setup
#   --clean           Clean existing downloads before setup
#   --help            Show this help message
# =============================================================================

set -euo pipefail

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
INFRA_DIR="$ROOT_DIR/infrastructure_src"

# Versions
JRE_VERSION="21.0.2+13"
JRE_MAJOR_VERSION="21"
GRADLE_VERSION="8.5"
CMAKE_VERSION="3.28.1"

# Detect OS and architecture
detect_platform() {
    local os=""
    local arch=""
    
    case "$(uname -s)" in
        Linux*)     os="linux" ;;
        Darwin*)    os="mac" ;;
        CYGWIN*|MINGW*|MSYS*) os="windows" ;;
        *)          os="unknown" ;;
    esac
    
    case "$(uname -m)" in
        x86_64|amd64)   arch="x64" ;;
        aarch64|arm64)  arch="aarch64" ;;
        *)              arch="x64" ;;  # Default fallback
    esac
    
    echo "${os}-${arch}"
}

# Logging functions
log_info() {
    echo "[INFO] $1"
}

log_warn() {
    echo "[WARN] $1" >&2
}

log_error() {
    echo "[ERROR] $1" >&2
}

log_success() {
    echo "[SUCCESS] $1"
}

# Download with progress
download_file() {
    local url="$1"
    local output="$2"
    
    log_info "Downloading: $url"
    
    if command -v curl &> /dev/null; then
        curl -fSL --progress-bar -o "$output" "$url"
    elif command -v wget &> /dev/null; then
        wget -q --show-progress -O "$output" "$url"
    else
        log_error "Neither curl nor wget is available"
        return 1
    fi
}

# Extract archive
extract_archive() {
    local archive="$1"
    local dest="$2"
    
    log_info "Extracting: $archive"
    
    mkdir -p "$dest"
    
    case "$archive" in
        *.tar.gz|*.tgz)
            tar -xzf "$archive" -C "$dest" --strip-components=1
            ;;
        *.zip)
            if command -v unzip &> /dev/null; then
                # Extract to temp, then move contents
                local temp_dir=$(mktemp -d)
                unzip -q "$archive" -d "$temp_dir"
                # Find the top-level directory and move its contents
                local top_dir=$(ls -1 "$temp_dir" | head -1)
                mv "$temp_dir/$top_dir"/* "$dest/" 2>/dev/null || mv "$temp_dir"/* "$dest/"
                rm -rf "$temp_dir"
            else
                log_error "unzip is not available"
                return 1
            fi
            ;;
        *)
            log_error "Unknown archive format: $archive"
            return 1
            ;;
    esac
}

# Setup JRE
setup_jre() {
    local platform="$1"
    local jre_dir="$INFRA_DIR/jre"
    
    if [[ -d "$jre_dir/bin" ]]; then
        log_info "JRE already exists at $jre_dir"
        return 0
    fi
    
    log_info "Setting up JRE $JRE_VERSION for $platform..."
    
    local os=""
    local arch=""
    local ext=""
    
    case "$platform" in
        linux-x64)      os="linux"; arch="x64"; ext="tar.gz" ;;
        linux-aarch64)  os="linux"; arch="aarch64"; ext="tar.gz" ;;
        mac-x64)        os="mac"; arch="x64"; ext="tar.gz" ;;
        mac-aarch64)    os="mac"; arch="aarch64"; ext="tar.gz" ;;
        windows-x64)    os="windows"; arch="x64"; ext="zip" ;;
        *)
            log_error "Unsupported platform for JRE: $platform"
            return 1
            ;;
    esac
    
    # Adoptium (Eclipse Temurin) download URL
    local jre_url="https://github.com/adoptium/temurin${JRE_MAJOR_VERSION}-binaries/releases/download/jdk-${JRE_VERSION}/OpenJDK${JRE_MAJOR_VERSION}U-jre_${arch}_${os}_hotspot_$(echo $JRE_VERSION | tr '+' '_').${ext}"
    
    local archive_file="$INFRA_DIR/downloads/jre.${ext}"
    mkdir -p "$INFRA_DIR/downloads"
    
    download_file "$jre_url" "$archive_file" || {
        log_warn "Failed to download Adoptium JRE, trying alternative..."
        # Alternative: Amazon Corretto
        if [[ "$os" == "windows" ]]; then
            jre_url="https://corretto.aws/downloads/latest/amazon-corretto-${JRE_MAJOR_VERSION}-${arch}-windows-jdk.zip"
        else
            jre_url="https://corretto.aws/downloads/latest/amazon-corretto-${JRE_MAJOR_VERSION}-${arch}-${os}-jdk.tar.gz"
        fi
        download_file "$jre_url" "$archive_file"
    }
    
    mkdir -p "$jre_dir"
    extract_archive "$archive_file" "$jre_dir"
    
    # macOS has an extra 'Contents/Home' directory
    if [[ "$os" == "mac" && -d "$jre_dir/Contents/Home" ]]; then
        mv "$jre_dir/Contents/Home"/* "$jre_dir/"
        rm -rf "$jre_dir/Contents"
    fi
    
    # Verify
    if [[ -x "$jre_dir/bin/java" ]]; then
        log_success "JRE installed: $("$jre_dir/bin/java" -version 2>&1 | head -1)"
    else
        log_error "JRE installation failed"
        return 1
    fi
}

# Setup Gradle
setup_gradle() {
    local gradle_dir="$INFRA_DIR/gradle"
    
    if [[ -d "$gradle_dir/bin" ]]; then
        log_info "Gradle already exists at $gradle_dir"
        return 0
    fi
    
    log_info "Setting up Gradle $GRADLE_VERSION..."
    
    local gradle_url="https://services.gradle.org/distributions/gradle-${GRADLE_VERSION}-bin.zip"
    local archive_file="$INFRA_DIR/downloads/gradle.zip"
    
    mkdir -p "$INFRA_DIR/downloads"
    download_file "$gradle_url" "$archive_file"
    
    mkdir -p "$gradle_dir"
    extract_archive "$archive_file" "$gradle_dir"
    
    # Verify
    if [[ -x "$gradle_dir/bin/gradle" ]]; then
        log_success "Gradle installed: $("$gradle_dir/bin/gradle" --version | grep 'Gradle' | head -1)"
    else
        log_error "Gradle installation failed"
        return 1
    fi
}

# Setup CMake (for C++ engine)
setup_cmake() {
    local platform="$1"
    local cmake_dir="$INFRA_DIR/cmake"
    
    # Check if CMake is already available on system
    if command -v cmake &> /dev/null; then
        local system_cmake_version=$(cmake --version | head -1 | awk '{print $3}')
        log_info "System CMake found: $system_cmake_version"
        
        # Check if version is sufficient (3.20+)
        if [[ "$(echo -e "3.20\n$system_cmake_version" | sort -V | head -1)" == "3.20" ]]; then
            log_info "System CMake version is sufficient, skipping download"
            return 0
        fi
    fi
    
    if [[ -d "$cmake_dir/bin" ]]; then
        log_info "CMake already exists at $cmake_dir"
        return 0
    fi
    
    log_info "Setting up CMake $CMAKE_VERSION for $platform..."
    
    local os=""
    local arch=""
    local ext=""
    
    case "$platform" in
        linux-x64)      os="linux"; arch="x86_64"; ext="tar.gz" ;;
        linux-aarch64)  os="linux"; arch="aarch64"; ext="tar.gz" ;;
        mac-x64)        os="macos"; arch="universal"; ext="tar.gz" ;;
        mac-aarch64)    os="macos"; arch="universal"; ext="tar.gz" ;;
        windows-x64)    os="windows"; arch="x86_64"; ext="zip" ;;
        *)
            log_warn "Unsupported platform for CMake download: $platform"
            log_warn "Please install CMake manually"
            return 0
            ;;
    esac
    
    local cmake_url="https://github.com/Kitware/CMake/releases/download/v${CMAKE_VERSION}/cmake-${CMAKE_VERSION}-${os}-${arch}.${ext}"
    local archive_file="$INFRA_DIR/downloads/cmake.${ext}"
    
    mkdir -p "$INFRA_DIR/downloads"
    download_file "$cmake_url" "$archive_file"
    
    mkdir -p "$cmake_dir"
    extract_archive "$archive_file" "$cmake_dir"
    
    # macOS puts things in CMake.app/Contents
    if [[ "$os" == "macos" && -d "$cmake_dir/CMake.app" ]]; then
        mv "$cmake_dir/CMake.app/Contents"/* "$cmake_dir/"
        rm -rf "$cmake_dir/CMake.app"
    fi
    
    # Verify
    if [[ -x "$cmake_dir/bin/cmake" ]]; then
        log_success "CMake installed: $("$cmake_dir/bin/cmake" --version | head -1)"
    else
        log_error "CMake installation failed"
        return 1
    fi
}

# Generate environment file
generate_env_file() {
    local env_file="$INFRA_DIR/env.sh"
    
    log_info "Generating environment file: $env_file"
    
    cat > "$env_file" << 'ENVEOF'
#!/bin/bash
# AI PR Reviewer - Environment Configuration
# Source this file to set up your environment:
#   source infrastructure_src/env.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
INFRA_DIR="$SCRIPT_DIR"

# Java
if [[ -d "$INFRA_DIR/jre/bin" ]]; then
    export JAVA_HOME="$INFRA_DIR/jre"
    export PATH="$JAVA_HOME/bin:$PATH"
fi

# Gradle
if [[ -d "$INFRA_DIR/gradle/bin" ]]; then
    export GRADLE_HOME="$INFRA_DIR/gradle"
    export PATH="$GRADLE_HOME/bin:$PATH"
fi

# CMake
if [[ -d "$INFRA_DIR/cmake/bin" ]]; then
    export CMAKE_HOME="$INFRA_DIR/cmake"
    export PATH="$CMAKE_HOME/bin:$PATH"
fi

# AI PR Reviewer
export AIPR_HOME="$ROOT_DIR"
export AIPR_VERSION="$(cat "$ROOT_DIR/VERSION" 2>/dev/null || echo 'dev')"

echo "Environment configured for AI PR Reviewer"
echo "  JAVA_HOME:   ${JAVA_HOME:-system}"
echo "  GRADLE_HOME: ${GRADLE_HOME:-system}"
echo "  CMAKE_HOME:  ${CMAKE_HOME:-system}"
echo "  AIPR_HOME:   $AIPR_HOME"
ENVEOF

    chmod +x "$env_file"
    log_success "Environment file generated"
}

# Clean downloads
clean_downloads() {
    log_info "Cleaning downloads..."
    rm -rf "$INFRA_DIR/downloads"
    rm -rf "$INFRA_DIR/jre"
    rm -rf "$INFRA_DIR/gradle"
    rm -rf "$INFRA_DIR/cmake"
    log_success "Cleaned infrastructure downloads"
}

# Print help
print_help() {
    head -n 18 "$0" | tail -n 14 | sed 's/^# //' | sed 's/^#//'
}

# Main
main() {
    local ci_mode=false
    local skip_jre=false
    local skip_gradle=false
    local skip_cmake=false
    local do_clean=false
    
    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --ci)           ci_mode=true ;;
            --skip-jre)     skip_jre=true ;;
            --skip-gradle)  skip_gradle=true ;;
            --skip-cmake)   skip_cmake=true ;;
            --clean)        do_clean=true ;;
            --help)         print_help; exit 0 ;;
            *)              log_error "Unknown option: $1"; print_help; exit 1 ;;
        esac
        shift
    done
    
    echo "=============================================="
    echo "AI PR Reviewer - Infrastructure Setup"
    echo "=============================================="
    echo ""
    
    # Detect platform
    local platform=$(detect_platform)
    log_info "Detected platform: $platform"
    
    # Clean if requested
    if [[ "$do_clean" == true ]]; then
        clean_downloads
    fi
    
    # Create infrastructure directory
    mkdir -p "$INFRA_DIR"
    
    # Setup components
    if [[ "$skip_jre" != true ]]; then
        setup_jre "$platform" || {
            if [[ "$ci_mode" == true ]]; then
                log_error "JRE setup failed in CI mode"
                exit 1
            else
                log_warn "JRE setup failed, will use system Java"
            fi
        }
    fi
    
    if [[ "$skip_gradle" != true ]]; then
        setup_gradle || {
            if [[ "$ci_mode" == true ]]; then
                log_error "Gradle setup failed in CI mode"
                exit 1
            else
                log_warn "Gradle setup failed, will use system Gradle"
            fi
        }
    fi
    
    if [[ "$skip_cmake" != true ]]; then
        setup_cmake "$platform" || {
            log_warn "CMake setup failed, will use system CMake"
        }
    fi
    
    # Generate environment file
    generate_env_file
    
    # Cleanup downloads to save space
    if [[ "$ci_mode" == true ]]; then
        log_info "Cleaning up download archives..."
        rm -rf "$INFRA_DIR/downloads"
    fi
    
    echo ""
    echo "=============================================="
    log_success "Setup complete!"
    echo ""
    echo "To configure your environment, run:"
    echo "  source infrastructure_src/env.sh"
    echo "=============================================="
}

main "$@"
