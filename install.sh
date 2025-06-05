#!/bin/bash
set -euo pipefail

GITHUB_REPO="nihen/ecssh"
RELEASE_TAG="${ECSSH_VERSION:-latest}"
INSTALL_DIR="${ECSSH_INSTALL_DIR:-.}"
BINARY_NAME="ecssh"

detect_platform() {
    local os=$(uname -s | tr '[:upper:]' '[:lower:]')
    local arch=$(uname -m)
    
    # build.shの命名規則に合わせる
    case "$os" in
        darwin)
            case "$arch" in
                x86_64) echo "darwin-amd64" ;;
                aarch64|arm64) echo "darwin-arm64" ;;
                *) echo "Unsupported macOS architecture: $arch" >&2; exit 1 ;;
            esac
            ;;
        linux)
            case "$arch" in
                x86_64) echo "linux" ;;
                aarch64|arm64) echo "linux-arm64" ;;
                *) echo "Unsupported Linux architecture: $arch" >&2; exit 1 ;;
            esac
            ;;
        *) echo "Unsupported OS: $os" >&2; exit 1 ;;
    esac
}

get_download_url() {
    local platform=$1
    local tag=$2
    
    if [ "$tag" = "latest" ]; then
        local api_url="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
    else
        local api_url="https://api.github.com/repos/${GITHUB_REPO}/releases/tags/${tag}"
    fi
    
    local response=$(curl -s "$api_url")
    
    # バイナリ名を決定 (ecssh-macos-arm64 または ecssh-windows.exe など)
    local binary_name="ecssh-${platform}"
    if [[ "$platform" == "windows" ]]; then
        binary_name="ecssh-windows.exe"
    elif [[ "$platform" == "windows-arm64" ]]; then
        binary_name="ecssh-windows-arm64.exe"
    fi
    
    local download_url=$(echo "$response" | grep -o "\"browser_download_url\": *\"[^\"]*/${binary_name}\"" | sed 's/.*"\(https[^"]*\)".*/\1/')
    
    if [ -z "$download_url" ]; then
        echo "Could not find download URL for binary: ${binary_name}" >&2
        echo "Available assets:" >&2
        echo "$response" | grep -o '"name": *"[^"]*"' | sed 's/"name": *"/  - /' >&2
        exit 1
    fi
    
    echo "$download_url"
}

main() {
    echo "Installing ecssh..."
    
    local platform=$(detect_platform)
    echo "Detected platform: $platform"
    
    local download_url=$(get_download_url "$platform" "$RELEASE_TAG")
    echo "Downloading from: $download_url"
    
    local temp_file=$(mktemp)
    trap "rm -f $temp_file" EXIT
    
    if ! curl -L -o "$temp_file" "$download_url"; then
        echo "Failed to download ecssh" >&2
        exit 1
    fi
    
    chmod +x "$temp_file"
    
    if [ ! -d "$INSTALL_DIR" ]; then
        mkdir -p "$INSTALL_DIR"
    fi
    
    mv "$temp_file" "${INSTALL_DIR}/${BINARY_NAME}"
    
    echo "ecssh installed successfully to ${INSTALL_DIR}/${BINARY_NAME}"
    
    if [ "$INSTALL_DIR" = "." ]; then
        echo "You can now run: ./ecssh"
    else
        echo "You can now run: ${INSTALL_DIR}/${BINARY_NAME}"
    fi
}

main "$@"