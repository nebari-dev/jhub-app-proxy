#!/bin/bash
set -e

# JHub App Proxy installer script
# Downloads and installs the latest release from GitHub

# Configuration
REPO="nebari-dev/jhub-app-proxy"
BINARY_NAME="jhub-app-proxy"
DEFAULT_INSTALL_DIR="${HOME}/.local/bin"

# Default values
VERSION=""
INSTALL_DIR=""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Functions
info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# Detect OS
detect_os() {
    case "$(uname -s)" in
        Linux*)     echo "Linux";;
        Darwin*)    echo "Darwin";;
        *)          error "Unsupported OS: $(uname -s)";;
    esac
}

# Detect architecture
detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)   echo "x86_64";;
        aarch64|arm64)  echo "arm64";;
        *)              error "Unsupported architecture: $(uname -m)";;
    esac
}

# Get latest release version from GitHub API
get_latest_version() {
    local version

    # Try /releases/latest first
    version=$(curl -s "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

    # If that fails, get the first non-draft, non-prerelease from /releases
    if [ -z "$version" ]; then
        version=$(curl -s "https://api.github.com/repos/${REPO}/releases" | \
                  grep -m 1 '"tag_name":' | \
                  sed -E 's/.*"([^"]+)".*/\1/')
    fi

    if [ -z "$version" ]; then
        error "Failed to fetch latest version from GitHub"
    fi

    echo "$version"
}

# Show usage
usage() {
    cat << EOF
Usage: $0 [OPTIONS]

Install jhub-app-proxy from GitHub releases

OPTIONS:
    -v, --version VERSION    Install specific version (e.g., v0.1)
    -d, --dir DIR           Installation directory (default: ~/.local/bin)
    -h, --help              Show this help message

EXAMPLES:
    $0                      # Install latest version
    $0 -v v0.1              # Install version v0.1
    $0 -d /usr/local/bin    # Install to /usr/local/bin

ENVIRONMENT VARIABLES:
    VERSION                 Version to install (CLI flag takes precedence)
    INSTALL_DIR             Installation directory (CLI flag takes precedence)

EOF
    exit 0
}

# Parse command line arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -v|--version)
                VERSION="$2"
                shift 2
                ;;
            -d|--dir)
                INSTALL_DIR="$2"
                shift 2
                ;;
            -h|--help)
                usage
                ;;
            *)
                error "Unknown option: $1\nRun '$0 --help' for usage"
                ;;
        esac
    done
}

# Main installation
main() {
    local version="${VERSION}"
    local install_dir="${INSTALL_DIR:-$DEFAULT_INSTALL_DIR}"

    info "Installing jhub-app-proxy..."

    # Detect system
    local os=$(detect_os)
    local arch=$(detect_arch)
    info "Detected system: $os $arch"

    # Get version
    if [ -z "$version" ]; then
        info "Fetching latest version..."
        version=$(get_latest_version)
        info "Latest version: $version"
    else
        info "Installing version: $version"
    fi

    # Build download URL (GoReleaser format: project_OS_arch.tar.gz)
    local archive_name="jhub-app-proxy_${os}_${arch}.tar.gz"
    local download_url="https://github.com/${REPO}/releases/download/${version}/${archive_name}"

    info "Archive: $archive_name"

    info "Downloading from: $download_url"

    # Create temporary directory
    local tmp_dir=$(mktemp -d)
    trap "rm -rf $tmp_dir" EXIT

    # Download and extract
    if ! curl -sL "$download_url" -o "${tmp_dir}/${archive_name}"; then
        error "Failed to download release"
    fi

    info "Extracting archive..."
    tar -xzf "${tmp_dir}/${archive_name}" -C "$tmp_dir"

    # Create install directory if it doesn't exist
    mkdir -p "$install_dir"

    # Install binary
    info "Installing to: ${install_dir}/${BINARY_NAME}"
    mv "${tmp_dir}/${BINARY_NAME}" "${install_dir}/${BINARY_NAME}"
    chmod +x "${install_dir}/${BINARY_NAME}"

    # Verify installation
    if ! "${install_dir}/${BINARY_NAME}" --version >/dev/null 2>&1; then
        error "Installation failed: binary is not executable"
    fi

    info "Successfully installed jhub-app-proxy $version"

    # Check if install_dir is in PATH
    if [[ ":$PATH:" != *":${install_dir}:"* ]]; then
        warn "Installation directory is not in PATH"
        warn "Add to your shell profile: export PATH=\"${install_dir}:\$PATH\""
    fi

    info "Run 'jhub-app-proxy --help' to get started"
}

# Parse arguments and run main
parse_args "$@"
main
