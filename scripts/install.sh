#!/usr/bin/env bash
#
# elgit installer (curl | bash friendly)
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/EstebanForge/elgit/main/scripts/install.sh | bash
#
# Flags via env:
#   INSTALL_DIR=/path  # override install dir (default: /usr/local/bin or ~/.local/bin)
#   VERSION=x.y.z      # install a specific release tag (default: latest)
#   FORCE=1            # skip version check, always reinstall

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

info() { echo -e "${BLUE}ℹ  $*${NC}" >&2; }
success() { echo -e "${GREEN}✓ $*${NC}" >&2; }
warn() { echo -e "${YELLOW}⚠ $*${NC}" >&2; }
error() { echo -e "${RED}✗ $*${NC}" >&2; exit 1; }

REPO="EstebanForge/elgit"
BINARY="elgit"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-}"
FORCE="${FORCE:-0}"

require_cmd() {
    command -v "$1" >/dev/null 2>&1 || error "Missing required command: $1"
}

normalize_version() {
    local value="$1"
    value="$(echo "$value" | tr -d '[:space:]')"
    value="${value#v}"
    echo "$value"
}

detect_platform() {
    local os arch
    os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    arch="$(uname -m)"

    case "$os" in
        darwin) ;;
        linux) ;;
        *) error "Unsupported OS: $os" ;;
    esac

    case "$arch" in
        x86_64|amd64) arch="amd64" ;;
        arm64|aarch64) arch="arm64" ;;
        *) error "Unsupported architecture: $arch" ;;
    esac

    echo "${os}-${arch}"
}

latest_version() {
    local url="https://raw.githubusercontent.com/${REPO}/main/VERSION"
    if command -v curl >/dev/null 2>&1; then
        normalize_version "$(curl -fsSL "$url")"
    elif command -v wget >/dev/null 2>&1; then
        normalize_version "$(wget -qO- "$url")"
    else
        error "curl or wget required to resolve latest version"
    fi
}

get_installed_version() {
    local version_file="${HOME}/.config/elgit/.version"
    local binary_path="$1/${BINARY}"

    if [[ -f "$version_file" ]]; then
        normalize_version "$(cat "$version_file")"
        return
    fi

    if [[ -x "$binary_path" ]]; then
        normalize_version "$("$binary_path" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.]+)?' | head -1 || echo "")"
    else
        echo ""
    fi
}

pick_install_dir() {
    if [[ -n "$INSTALL_DIR" ]]; then
        echo "$INSTALL_DIR"
        return
    fi

    local default="/usr/local/bin"
    if [[ -w "$default" ]]; then
        echo "$default"
    else
        echo "${HOME}/.local/bin"
    fi
}

ensure_path_hint() {
    local dir="$1"
    case ":$PATH:" in
        *":$dir:"*) return ;;
        *) warn "Add $dir to PATH to use ${BINARY} globally" ;;
    esac
}

download() {
    local url="$1" tmp
    tmp="$(mktemp)"

    if command -v curl >/dev/null 2>&1; then
        curl -fsL "$url" -o "$tmp" || { rm -f "$tmp"; error "Download failed: $url"; }
    else
        wget -qO "$tmp" "$url" || { rm -f "$tmp"; error "Download failed: $url"; }
    fi

    if [[ ! -s "$tmp" ]]; then
        rm -f "$tmp"
        error "Download failed: empty file received from $url"
    fi

    echo "$tmp"
}

install_binary() {
    local src="$1" dest_dir="$2"
    mkdir -p "$dest_dir"
    local dest="${dest_dir}/${BINARY}"

    if install -m 0755 "$src" "$dest" 2>/dev/null; then
        :
    else
        info "Escalating to sudo for install into $dest_dir"
        sudo install -m 0755 "$src" "$dest"
    fi
    success "Installed: $dest"
}

main() {
    require_cmd uname
    require_cmd grep
    require_cmd tar

    local platform
    platform="$(detect_platform)"

    local dest_dir
    dest_dir="$(pick_install_dir)"

    if [[ "$VERSION" == "latest" || -z "$VERSION" ]]; then
        info "Resolving latest version..."
        VERSION="$(latest_version)"
    else
        VERSION="$(normalize_version "$VERSION")"
    fi

    if [[ "$FORCE" != "1" ]]; then
        local installed_version
        installed_version="$(get_installed_version "$dest_dir")"

        if [[ -n "$installed_version" && "$installed_version" == "$VERSION" ]]; then
            success "Already on latest version: ${VERSION}"
            echo -n "Do you want to reinstall? [y/N]: " >&2
            local response=""
            if ! read -r response </dev/tty; then
                response=""
            fi
            response=$(echo "$response" | tr '[:upper:]' '[:lower:]')
            if [[ "$response" != "y" && "$response" != "yes" ]]; then
                info "Installation cancelled."
                exit 0
            fi
        elif [[ -n "$installed_version" ]]; then
            info "Upgrading: ${installed_version} → ${VERSION}"
        fi
    fi

    local asset="${BINARY}-${platform}-${VERSION}.tar.gz"
    local url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"
    info "Platform: ${platform}"
    info "Version: ${VERSION}"

    info "Downloading ${asset}..."
    local tmp_tar
    tmp_tar="$(download "$url")"

    local tmp_dir
    tmp_dir="$(mktemp -d)"
    tar -xzf "$tmp_tar" -C "$tmp_dir"

    local tmp_bin="${tmp_dir}/${BINARY}-${platform}"
    if [[ ! -f "$tmp_bin" ]]; then
        if [[ -f "${tmp_dir}/${BINARY}" ]]; then
            tmp_bin="${tmp_dir}/${BINARY}"
        else
            error "Binary ${BINARY}-${platform} not found in archive"
        fi
    fi
    chmod +x "$tmp_bin"

    info "Install dir: ${dest_dir}"
    install_binary "$tmp_bin" "$dest_dir"

    mkdir -p "${HOME}/.config/elgit"
    echo "$VERSION" > "${HOME}/.config/elgit/.version"

    rm -rf "$tmp_dir" "$tmp_tar"

    ensure_path_hint "$dest_dir"

    echo
    success "elgit installed."
    echo "Verify with: ${BINARY} --version"
    echo "Then run:    ${BINARY} branches"
}

main "$@"
