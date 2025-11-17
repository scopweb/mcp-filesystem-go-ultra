#!/bin/bash
# Build script for Windows executable
# This must be run when creating the .exe for Windows

echo "🔨 Building filesystem-ultra.exe for Windows..."
echo ""

# Clean old builds
rm -f filesystem-ultra.exe

# Build for Windows with optimizations
GOOS=windows GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -trimpath \
    -o filesystem-ultra.exe \
    .

if [ $? -eq 0 ]; then
    SIZE=$(stat -f%z filesystem-ultra.exe 2>/dev/null || stat -c%s filesystem-ultra.exe 2>/dev/null)
    SIZE_MB=$(echo "scale=2; $SIZE / 1048576" | bc 2>/dev/null || echo "?")
    echo ""
    echo "✅ Build successful!"
    echo "📦 Size: ${SIZE_MB} MB"
    echo "📍 Output: filesystem-ultra.exe"
    echo ""
    echo "⚠️  IMPORTANT: This .exe is compiled for Windows and will:"
    echo "   • Correctly recognize Windows paths (C:\..., D:\...)"
    echo "   • Run natively on Windows (not through WSL)"
    echo "   • Use Windows path separators (\\)"
else
    echo ""
    echo "❌ Build failed!"
    exit 1
fi
