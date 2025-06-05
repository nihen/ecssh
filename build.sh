#!/bin/bash

set -e

echo "Building ecssh for multiple platforms..."

# Create dist directory
mkdir -p dist

# Clean previous builds
rm -f dist/ecssh-*

# Build for macOS
echo "Building for macOS ARM64..."
GOOS=darwin GOARCH=arm64 go build -o dist/ecssh-darwin-arm64 main.go

echo "Building for macOS Intel..."
GOOS=darwin GOARCH=amd64 go build -o dist/ecssh-darwin-amd64 main.go

# Build for Linux
echo "Building for Linux x86_64..."
GOOS=linux GOARCH=amd64 go build -o dist/ecssh-linux main.go

echo "Building for Linux ARM64..."
GOOS=linux GOARCH=arm64 go build -o dist/ecssh-linux-arm64 main.go

# Build for Windows
echo "Building for Windows x86_64..."
GOOS=windows GOARCH=amd64 go build -o dist/ecssh-windows.exe main.go

echo "Building for Windows ARM64..."
GOOS=windows GOARCH=arm64 go build -o dist/ecssh-windows-arm64.exe main.go

echo "Build complete!"
echo ""
echo "Generated binaries:"
ls -la dist/ecssh-*

echo ""
echo "To use the universal launcher:"
echo "  ./ecssh [arguments]"
echo ""
echo "Or use specific binaries directly:"
echo "  macOS ARM64 (Apple Silicon): ./dist/ecssh-darwin-arm64"
echo "  macOS Intel:                 ./dist/ecssh-darwin-amd64"
echo "  Linux x86_64:                ./dist/ecssh-linux"
echo "  Linux ARM64:                 ./dist/ecssh-linux-arm64"
echo "  Windows x86_64:              dist\\ecssh-windows.exe"
echo "  Windows ARM64:               dist\\ecssh-windows-arm64.exe"