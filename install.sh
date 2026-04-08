#!/bin/sh
set -eu

REPO="alexandre-daubois/ember"
INSTALL_DIR="${EMBER_INSTALL_DIR:-/usr/local/bin}"

fail() {
    printf "error: %s\n" "$1" >&2
    exit 1
}

detect_os() {
    case "$(uname -s)" in
        Linux*)  echo "linux" ;;
        Darwin*) echo "darwin" ;;
        *)       fail "unsupported OS: $(uname -s)" ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)  echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *)             fail "unsupported architecture: $(uname -m)" ;;
    esac
}

need_cmd() {
    if ! command -v "$1" > /dev/null 2>&1; then
        fail "'$1' is required but not found"
    fi
}

need_cmd uname
need_cmd mktemp
need_cmd tar

OS=$(detect_os)
ARCH=$(detect_arch)

if command -v curl > /dev/null 2>&1; then
    fetch() { curl -fsSL "$1"; }
    download() { curl -fsSL -o "$1" "$2"; }
elif command -v wget > /dev/null 2>&1; then
    fetch() { wget -qO- "$1"; }
    download() { wget -qO "$1" "$2"; }
else
    fail "'curl' or 'wget' is required but neither was found"
fi

printf "Detecting platform... %s/%s\n" "$OS" "$ARCH"

LATEST_URL="https://api.github.com/repos/${REPO}/releases/latest"
TAG=$(fetch "$LATEST_URL" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"//;s/".*//')

if [ -z "$TAG" ]; then
    fail "could not determine latest release"
fi

VERSION="${TAG#v}"
printf "Latest version: %s\n" "$VERSION"

ARCHIVE="ember_${VERSION}_${OS}_${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${TAG}/${ARCHIVE}"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

printf "Downloading %s...\n" "$ARCHIVE"
download "${TMPDIR}/${ARCHIVE}" "$DOWNLOAD_URL"

printf "Extracting...\n"
tar -xzf "${TMPDIR}/${ARCHIVE}" -C "$TMPDIR"

if [ ! -f "${TMPDIR}/ember" ]; then
    fail "binary not found in archive"
fi

printf "Installing to %s...\n" "$INSTALL_DIR"
if [ -w "$INSTALL_DIR" ]; then
    mv "${TMPDIR}/ember" "${INSTALL_DIR}/ember"
else
    sudo mv "${TMPDIR}/ember" "${INSTALL_DIR}/ember"
fi
chmod +x "${INSTALL_DIR}/ember"

printf "ember %s installed successfully!\n" "$VERSION"
