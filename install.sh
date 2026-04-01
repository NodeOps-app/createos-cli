#!/bin/sh
set -e

# CreateOS CLI installer
# Usage:
#   curl -sfL https://raw.githubusercontent.com/NodeOps-app/createos-cli/main/install.sh | sh -
#
# Environment variables:
#   CREATEOS_VERSION      Pin a specific version (e.g. v1.2.3). Defaults to latest stable.
#   CREATEOS_CHANNEL      Set to "nightly" to install the nightly build.
#   CREATEOS_INSTALL_DIR  Override install directory. Defaults to /usr/local/bin.

GITHUB_REPO="NodeOps-app/createos-cli"
BINARY_NAME="createos"

# ---------------------------------------------------------------------------
# Colours (suppressed when not a TTY)
# ---------------------------------------------------------------------------
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    CYAN='\033[0;36m'
    BOLD='\033[1m'
    RESET='\033[0m'
else
    RED='' GREEN='' YELLOW='' CYAN='' BOLD='' RESET=''
fi

info()    { printf "${CYAN}[createos]${RESET} %s\n" "$*"; }
success() { printf "${GREEN}[createos]${RESET} %s\n" "$*"; }
warn()    { printf "${YELLOW}[createos]${RESET} %s\n" "$*" >&2; }
fatal()   { printf "${RED}[createos] error:${RESET} %s\n" "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Detect OS
# ---------------------------------------------------------------------------
detect_os() {
    OS="$(uname -s)"
    case "${OS}" in
        Linux)  OS="linux" ;;
        Darwin) OS="darwin" ;;
        *)      fatal "Unsupported operating system: ${OS}. Only Linux and macOS are supported." ;;
    esac
}

# ---------------------------------------------------------------------------
# Detect architecture
# ---------------------------------------------------------------------------
detect_arch() {
    ARCH="$(uname -m)"
    case "${ARCH}" in
        x86_64 | amd64)          ARCH="amd64" ;;
        aarch64 | arm64 | armv8) ARCH="arm64" ;;
        *) fatal "Unsupported architecture: ${ARCH}. Only x86_64 and arm64 are supported." ;;
    esac
}

# ---------------------------------------------------------------------------
# Prompt for channel if not set and stdin is a TTY
# ---------------------------------------------------------------------------
prompt_channel() {
    # Already set via env — skip prompt
    if [ -n "${CREATEOS_CHANNEL:-}" ]; then
        return
    fi
    # Pinned version set — stable implied, skip prompt
    if [ -n "${CREATEOS_VERSION:-}" ]; then
        CREATEOS_CHANNEL="stable"
        return
    fi
    # Non-interactive (piped): default to stable silently
    if [ ! -t 0 ]; then
        CREATEOS_CHANNEL="stable"
        return
    fi

    printf "\n${BOLD}  Which release channel would you like to install?${RESET}\n\n"
    printf "    ${BOLD}1) stable${RESET}  — official releases, production-ready (recommended)\n"
    printf "    ${BOLD}2) nightly${RESET} — built daily from main, may contain unreleased features\n\n"
    printf "  Enter 1 or 2 [default: 1]: "

    read -r CHOICE </dev/tty
    case "${CHOICE}" in
        2) CREATEOS_CHANNEL="nightly" ;;
        *) CREATEOS_CHANNEL="stable"  ;;
    esac
    printf "\n"
}

# ---------------------------------------------------------------------------
# Resolve the download version
# ---------------------------------------------------------------------------
resolve_version() {
    CHANNEL="${CREATEOS_CHANNEL:-stable}"

    if [ "${CHANNEL}" = "nightly" ]; then
        VERSION="nightly"
        info "Channel: nightly"
        return
    fi

    if [ -n "${CREATEOS_VERSION:-}" ]; then
        VERSION="${CREATEOS_VERSION}"
        info "Version: ${VERSION} (pinned)"
        return
    fi

    info "Fetching latest stable release..."
    LATEST_URL="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
    if command -v curl > /dev/null 2>&1; then
        VERSION="$(curl -fsSL "${LATEST_URL}" | grep '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')"
    elif command -v wget > /dev/null 2>&1; then
        VERSION="$(wget -qO- "${LATEST_URL}" | grep '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')"
    else
        fatal "curl or wget is required to download createos."
    fi

    [ -n "${VERSION}" ] || fatal "Could not determine the latest version. Check your internet connection."
    info "Latest version: ${VERSION}"
}

# ---------------------------------------------------------------------------
# Build asset names and URLs
# ---------------------------------------------------------------------------
build_urls() {
    ASSET="${BINARY_NAME}-${OS}-${ARCH}"
    CHECKSUM_ASSET="${ASSET}.sha256"

    if [ "${VERSION}" = "nightly" ]; then
        BASE_URL="https://github.com/${GITHUB_REPO}/releases/download/nightly"
    else
        BASE_URL="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}"
    fi

    BINARY_URL="${BASE_URL}/${ASSET}"
    CHECKSUM_URL="${BASE_URL}/${CHECKSUM_ASSET}"
}

# ---------------------------------------------------------------------------
# Resolve install directory (prefer /usr/local/bin, fall back to ~/.local/bin)
# ---------------------------------------------------------------------------
resolve_install_dir() {
    if [ -n "${CREATEOS_INSTALL_DIR:-}" ]; then
        INSTALL_DIR="${CREATEOS_INSTALL_DIR}"
        return
    fi

    if [ -w "/usr/local/bin" ]; then
        INSTALL_DIR="/usr/local/bin"
    elif [ "$(id -u)" = "0" ]; then
        INSTALL_DIR="/usr/local/bin"
    else
        INSTALL_DIR="${HOME}/.local/bin"
        mkdir -p "${INSTALL_DIR}"
        # Warn if not on PATH
        case ":${PATH}:" in
            *":${INSTALL_DIR}:"*) ;;
            *) warn "${INSTALL_DIR} is not in your PATH. Add it: export PATH=\"\$HOME/.local/bin:\$PATH\"" ;;
        esac
    fi
}

# ---------------------------------------------------------------------------
# Download helper (curl or wget)
# ---------------------------------------------------------------------------
download() {
    URL="$1"
    DEST="$2"
    if command -v curl > /dev/null 2>&1; then
        curl -fsSL --retry 3 --retry-delay 2 -o "${DEST}" "${URL}"
    elif command -v wget > /dev/null 2>&1; then
        wget -qO "${DEST}" "${URL}"
    else
        fatal "curl or wget is required."
    fi
}

# ---------------------------------------------------------------------------
# Verify SHA256 checksum
# ---------------------------------------------------------------------------
verify_checksum() {
    BINARY_PATH="$1"
    EXPECTED="$2"

    if command -v sha256sum > /dev/null 2>&1; then
        ACTUAL="$(sha256sum "${BINARY_PATH}" | awk '{print $1}')"
    elif command -v shasum > /dev/null 2>&1; then
        ACTUAL="$(shasum -a 256 "${BINARY_PATH}" | awk '{print $1}')"
    else
        warn "No sha256sum or shasum found — skipping checksum verification."
        return
    fi

    if [ "${ACTUAL}" != "${EXPECTED}" ]; then
        fatal "Checksum mismatch — the download may be corrupted or tampered with.\n  expected: ${EXPECTED}\n  got:      ${ACTUAL}"
    fi

    success "Checksum verified."
}

# ---------------------------------------------------------------------------
# Install
# ---------------------------------------------------------------------------
install_binary() {
    INSTALL_PATH="${INSTALL_DIR}/${BINARY_NAME}"
    TMP_DIR="$(mktemp -d)"
    TMP_BINARY="${TMP_DIR}/${ASSET}"
    TMP_CHECKSUM="${TMP_DIR}/${CHECKSUM_ASSET}"

    trap 'rm -rf "${TMP_DIR}"' EXIT

    info "Downloading ${ASSET}..."
    download "${BINARY_URL}" "${TMP_BINARY}" \
        || fatal "Failed to download binary from ${BINARY_URL}"

    info "Downloading checksum..."
    download "${CHECKSUM_URL}" "${TMP_CHECKSUM}" \
        || fatal "Failed to download checksum from ${CHECKSUM_URL}"

    EXPECTED_HASH="$(cat "${TMP_CHECKSUM}" | tr -d '[:space:]')"
    verify_checksum "${TMP_BINARY}" "${EXPECTED_HASH}"

    chmod 755 "${TMP_BINARY}"

    # Use sudo only if we need it
    if [ -w "${INSTALL_DIR}" ]; then
        mv "${TMP_BINARY}" "${INSTALL_PATH}"
    else
        info "Root access required to install to ${INSTALL_DIR}."
        sudo mv "${TMP_BINARY}" "${INSTALL_PATH}"
    fi
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
main() {
    printf "\n${BOLD}  CreateOS CLI Installer${RESET}\n\n"

    detect_os
    detect_arch
    prompt_channel
    resolve_version
    build_urls
    resolve_install_dir

    info "OS: ${OS} / Arch: ${ARCH}"
    info "Install directory: ${INSTALL_DIR}"

    install_binary

    success "createos ${VERSION} installed to ${INSTALL_DIR}/${BINARY_NAME}"
    printf "\n  Run ${CYAN}createos --help${RESET} to get started.\n\n"
}

main
