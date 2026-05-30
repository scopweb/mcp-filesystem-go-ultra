#!/bin/bash
# ============================================================
#  Build script for Linux / macOS (and Windows cross-compile)
#  MCP Filesystem Ultra v4+
#
#  Usage:
#    ./build-windows.sh              # Build for current platform (Linux/mac)
#    ./build-windows.sh windows      # Cross-compile all Windows binaries
#    ./build-windows.sh all          # Build native + Windows binaries
# ============================================================

set -e

GO_LDFLAGS="-ldflags=-s -w"
GO_FLAGS="-trimpath"

PLATFORM=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

echo ""
echo "=============================================="
echo "  MCP Filesystem Ultra - Build Script"
echo "  Host: $PLATFORM ($ARCH)"
echo "=============================================="
echo ""

build_native() {
    echo ">>> Building native binaries for $PLATFORM..."

    mkdir -p bin

    # Main server
    echo "[1/4] Building bin/filesystem-ultra-v4 ..."
    rm -f bin/filesystem-ultra-v4
    go build $GO_LDFLAGS $GO_FLAGS -o bin/filesystem-ultra-v4 .

    # Main server + embedded ripgrep (recommended)
    echo "[2/4] Building bin/filesystem-ultra-v4-embed_rg ..."
    rm -f bin/filesystem-ultra-v4-embed_rg
    go build $GO_LDFLAGS $GO_FLAGS -tags embed_rg -o bin/filesystem-ultra-v4-embed_rg .

    # Proxy (important: built from ./cmd/proxy)
    echo "[3/4] Building bin/mcp-proxy ..."
    rm -f bin/mcp-proxy
    go build $GO_LDFLAGS $GO_FLAGS -o bin/mcp-proxy ./cmd/proxy

    # Dashboard
    echo "[4/4] Building bin/filesystem-ultra-v4-dashboard ..."
    rm -f bin/filesystem-ultra-v4-dashboard
    go build $GO_LDFLAGS $GO_FLAGS -o bin/filesystem-ultra-v4-dashboard ./cmd/dashboard/

    echo ""
    echo "✅ Native build successful!"
    echo ""
    echo "   Binaries (in bin/):"
    echo "     bin/filesystem-ultra-v4"
    echo "     bin/filesystem-ultra-v4-embed_rg   (recommended for Claude)"
    echo "     bin/mcp-proxy                      (logging proxy)"
    echo "     bin/filesystem-ultra-v4-dashboard"
    echo ""
}

build_windows() {
    echo ">>> Cross-compiling Windows binaries (GOOS=windows GOARCH=amd64)..."
    echo ""

    mkdir -p bin

    export GOOS=windows
    export GOARCH=amd64

    # Main server (standard)
    echo "[1/4] Building bin/filesystem-ultra-v4.exe ..."
    rm -f bin/filesystem-ultra-v4.exe
    go build $GO_LDFLAGS $GO_FLAGS -o bin/filesystem-ultra-v4.exe .

    # Main server + embedded ripgrep
    echo "[2/4] Building bin/filesystem-ultra-v4-embed_rg.exe ..."
    rm -f bin/filesystem-ultra-v4-embed_rg.exe
    go build $GO_LDFLAGS $GO_FLAGS -tags embed_rg -o bin/filesystem-ultra-v4-embed_rg.exe .

    # Proxy (correct build)
    echo "[3/4] Building bin/mcp-proxy.exe ..."
    rm -f bin/filesystem-ultra-v4-proxy.exe 2>/dev/null || true
    rm -f bin/mcp-proxy.exe
    go build $GO_LDFLAGS $GO_FLAGS -o bin/mcp-proxy.exe ./cmd/proxy

    # Dashboard
    echo "[4/4] Building bin/filesystem-ultra-v4-dashboard.exe ..."
    rm -f bin/filesystem-ultra-v4-dashboard.exe
    go build $GO_LDFLAGS $GO_FLAGS -o bin/filesystem-ultra-v4-dashboard.exe ./cmd/dashboard/

    echo ""
    echo "✅ Windows cross-compile successful!"
    echo ""
    echo "   Binaries (in bin/):"
    echo "     bin/filesystem-ultra-v4.exe"
    echo "     bin/filesystem-ultra-v4-embed_rg.exe"
    echo "     bin/mcp-proxy.exe                   ← Use this one in Claude Desktop"
    echo "     bin/filesystem-ultra-v4-dashboard.exe"
    echo ""
    echo "   ⚠️  Remember: These .exe files are for Windows only."
    echo ""
}

case "$1" in
    windows)
        build_windows
        ;;
    all)
        build_native
        echo ""
        build_windows
        ;;
    *)
        # Default: native build for current OS (Linux or macOS)
        build_native
        ;;
esac

echo "=============================================="
echo "Build finished."
echo ""
echo "All binaries are now inside the bin/ directory."
echo "The project root stays clean."
echo ""
echo "Recommended in Claude Desktop:"
echo "  Use \"bin/mcp-proxy\" (or bin/mcp-proxy.exe on Windows)"
echo "=============================================="
echo ""
