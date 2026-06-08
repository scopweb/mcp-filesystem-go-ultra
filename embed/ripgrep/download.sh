#!/bin/bash
# Download script for ripgrep binaries
# Run from project root: ./embed/ripgrep/download.sh
#
# This downloads the official ripgrep binaries from GitHub releases and
# places them with the exact filenames expected by embed.go so that
# `//go:embed` can pick them up at build time.
#
# MIT licensed - ripgrep is © BurntSushi

set -e

VERSION="15.1.0"
DEST_DIR="$(dirname "$0")"

# Map a Go (os, arch) pair to:
#   - the ripgrep release asset name fragment (uses kernel arch convention)
#   - the embedded filename expected by embed.go (uses Go convention)
# embed.go expects:  rg-windows-amd64.exe, rg-linux-amd64, rg-linux-arm64,
#                    rg-darwin-amd64, rg-darwin-arm64
download_one() {
    local goos="$1"       # windows | linux | darwin
    local goarch="$2"     # amd64 | arm64
    local rg_arch="$3"    # x86_64 | aarch64
    local rg_ext="$4"     # zip | tar.gz
    local out_name="$5"   # e.g. rg-linux-amd64

    local os_name
    case "$goos" in
        linux)  os_name="unknown-linux-musl" ;;
        darwin) os_name="apple-darwin" ;;
        windows) os_name="pc-windows-msvc" ;;
    esac

    local base="ripgrep-${VERSION}-${rg_arch}-${os_name}"
    local url="https://github.com/BurntSushi/ripgrep/releases/download/${VERSION}/${base}.${rg_ext}"
    local tmp="${DEST_DIR}/.rg-${goos}-${goarch}.${rg_ext}"

    echo ">>> ${goos}/${goarch} ← ${url}"
    for i in 1 2 3; do
        if curl -L --fail --progress-bar -o "$tmp" "$url"; then
            break
        fi
        echo "    retry $i..."
        sleep 2
    done

    case "$rg_ext" in
        zip)
            # Windows zip layout: ripgrep-VER-x86_64-pc-windows-msvc/rg.exe
            local extract_dir="${DEST_DIR}/.rg-extract-${goos}-${goarch}"
            mkdir -p "$extract_dir"
            unzip -o -q "$tmp" -d "$extract_dir"
            find "$extract_dir" -type f -name "rg.exe" -exec mv -f {} "${DEST_DIR}/${out_name}" \;
            rm -rf "$extract_dir" "$tmp"
            ;;
        tar.gz)
            # Linux/Darwin tarball layout: ripgrep-VER-.../rg
            tar -xzf "$tmp" -C "$DEST_DIR"
            local found
            found="$(find "$DEST_DIR" -maxdepth 3 -type f -name "rg" -path "*/${base}/*" | head -n 1 || true)"
            if [ -z "$found" ]; then
                echo "ERROR: could not find 'rg' inside ${base}" >&2
                exit 1
            fi
            mv -f "$found" "${DEST_DIR}/${out_name}"
            chmod 0755 "${DEST_DIR}/${out_name}"
            rm -f "$tmp"
            # Clean up the extracted directory tree
            find "$DEST_DIR" -maxdepth 1 -type d -name "${base}" -exec rm -rf {} +
            ;;
    esac
}

# Create destination directory
mkdir -p "$DEST_DIR"

# Download every supported platform (CI builds all, so it needs them all).
# Comment out any platform you don't need locally to save bandwidth.
download_one windows amd64 x86_64 zip      "rg-windows-amd64.exe"
download_one linux   amd64 x86_64 tar.gz   "rg-linux-amd64"
download_one linux   arm64 aarch64 tar.gz  "rg-linux-arm64"
download_one darwin  amd64 x86_64 tar.gz   "rg-darwin-amd64"
download_one darwin  arm64 aarch64 tar.gz  "rg-darwin-arm64"

# Metadata
echo "VERSION=$VERSION" > "${DEST_DIR}/.version"
echo "DOWNLOAD_DATE=$(date -Iseconds)" >> "${DEST_DIR}/.version"

echo
echo "Done. Embedded ripgrep binaries in ${DEST_DIR}:"
ls -la "$DEST_DIR"