#!/bin/bash
# Download script for ripgrep binaries
# Run from project root: ./embed/ripgrep/download.sh
#
# This downloads the official ripgrep binaries from GitHub releases.
# MIT licensed - ripgrep is © BurntSushi

set -e

VERSION="15.1.0"
DEST_DIR="$(dirname "$0")"

# Determine current platform
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

# Map architectures
case "$ARCH" in
    x86_64) ARCH_SUFFIX="x86_64" ;;
    aarch64|arm64) ARCH_SUFFIX="aarch64" ;;
    armv7l) ARCH_SUFFIX="armv7" ;;
    i386|i686) ARCH_SUFFIX="i686" ;;
    *)
        echo "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

# Map OS names
case "$OS" in
    linux) OS_NAME="unknown-linux-gnu" ;;
    darwin) OS_NAME="apple-darwin" ;;
    mingw*|cygwin*|windows*) OS_NAME="pc-windows-msvc" ;;
    *)
        echo "Unsupported OS: $OS"
        exit 1
        ;;
esac

# Download URL
if [ "$OS" = "windows" ] || [ "$OS_NAME" = "pc-windows-msvc" ]; then
    EXT="zip"
    BASE_NAME="ripgrep-${VERSION}-${ARCH_SUFFIX}-${OS_NAME}"
    URL="https://github.com/BurntSushi/ripgrep/releases/download/${VERSION}/${BASE_NAME}.${EXT}"
    echo "Downloading Windows binary..."
else
    EXT="tar.gz"
    BASE_NAME="ripgrep-${VERSION}-${ARCH_SUFFIX}-${OS_NAME}"
    # For musl, use musleabi variant
    if [ "$OS" = "linux" ]; then
        BASE_NAME="ripgrep-${VERSION}-${ARCH_SUFFIX}-unknown-linux-musleabi"
        # armv7 uses musleabihf
        if [ "$ARCH" = "armv7l" ]; then
            BASE_NAME="ripgrep-${VERSION}-${ARCH_SUFFIX}-unknown-linux-musleabihf"
        fi
    fi
    URL="https://github.com/BurntSushi/ripgrep/releases/download/${VERSION}/${BASE_NAME}.tar.gz"
    echo "Downloading Linux binary..."
fi

echo "URL: $URL"
echo "Destination: $DEST_DIR"

# Create destination directory if needed
mkdir -p "$DEST_DIR"

# Download with retry
for i in 1 2 3; do
    if curl -L --fail --progress-bar -o "${DEST_DIR}/rg.bin" "$URL"; then
        echo "Downloaded successfully"
        break
    fi
    echo "Retry $i..."
done

# Extract if tar.gz
if [ "$EXT" = "tar.gz" ]; then
    echo "Extracting..."
    tar -xzf "${DEST_DIR}/rg.bin" -C "$DEST_DIR"
    mv "${DEST_DIR}/rg" "${DEST_DIR}/rg-${OS}-${ARCH_SUFFIX}" 2>/dev/null || true
    rm -f "${DEST_DIR}/rg.bin"
fi

# Rename for Windows
if [ "$EXT" = "zip" ]; then
    echo "Extracting..."
    unzip -o "${DEST_DIR}/rg.bin" -d "$DEST_DIR"
    rm -f "${DEST_DIR}/rg.bin"
fi

# Create metadata file
echo "VERSION=$VERSION" > "${DEST_DIR}/.version"
echo "PLATFORM=${OS}-${ARCH}" >> "${DEST_DIR}/.version"
echo "DOWNLOAD_DATE=$(date -Iseconds)" >> "${DEST_DIR}/.version"

echo "Done! Binaries in $DEST_DIR"
ls -la "$DEST_DIR"