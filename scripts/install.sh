#!/usr/bin/env bash
# ==============================================================================
# VIB (Visual Image Builder) - One-Command Installer
# Installs VIB, Packer, and AWS CLI v2 into ~/.vib/bin
# ==============================================================================

set -euo pipefail

VIB_HOME="${HOME}/.vib"
INSTALL_DIR="${VIB_HOME}/bin"
PACKER_VERSION="1.11.2"

echo "================================================================"
echo "⚡ Visual Image Builder (VIB) - One-Command Installer"
echo "================================================================"
echo "Installation Directory: ${INSTALL_DIR}"
mkdir -p "${INSTALL_DIR}"

# 1. Detect OS and Architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "${ARCH}" in
  x86_64|amd64)
    PACKER_ARCH="amd64"
    AWS_ARCH="x86_64"
    ;;
  aarch64|arm64)
    PACKER_ARCH="arm64"
    AWS_ARCH="aarch64"
    ;;
  *)
    echo "⚠️  Unsupported CPU architecture: ${ARCH}"
    exit 1
    ;;
esac

echo "✓ Detected Platform: ${OS} (${PACKER_ARCH})"

# 2. Check / Install HashiCorp Packer
if command -v packer >/dev/null 2>&1; then
  echo "✓ HashiCorp Packer already installed: $(packer version | head -n 1)"
elif [ -x "${INSTALL_DIR}/packer" ]; then
  echo "✓ HashiCorp Packer found in ${INSTALL_DIR}: $("${INSTALL_DIR}/packer" version | head -n 1)"
else
  echo "==> Downloading official HashiCorp Packer v${PACKER_VERSION}..."
  PACKER_ZIP="packer_${PACKER_VERSION}_${OS}_${PACKER_ARCH}.zip"
  PACKER_URL="https://releases.hashicorp.com/packer/${PACKER_VERSION}/${PACKER_ZIP}"
  TMP_DIR="$(mktemp -d)"

  curl -fsSL "${PACKER_URL}" -o "${TMP_DIR}/${PACKER_ZIP}"
  unzip -q -o "${TMP_DIR}/${PACKER_ZIP}" -d "${INSTALL_DIR}"
  chmod +x "${INSTALL_DIR}/packer"
  rm -rf "${TMP_DIR}"

  echo "✓ HashiCorp Packer v${PACKER_VERSION} installed into ${INSTALL_DIR}/packer"
fi

# 3. Check / Install AWS CLI v2
if command -v aws >/dev/null 2>&1; then
  echo "✓ AWS CLI already installed: $(aws --version | head -n 1)"
elif [ -x "${INSTALL_DIR}/aws" ]; then
  echo "✓ AWS CLI found in ${INSTALL_DIR}: $("${INSTALL_DIR}/aws" --version | head -n 1)"
else
  echo "==> Downloading official AWS CLI v2..."
  TMP_DIR="$(mktemp -d)"
  if [ "${OS}" = "linux" ]; then
    AWS_ZIP_URL="https://awscli.amazonaws.com/awscli-exe-linux-${AWS_ARCH}.zip"
    curl -fsSL "${AWS_ZIP_URL}" -o "${TMP_DIR}/awscliv2.zip"
    unzip -q -o "${TMP_DIR}/awscliv2.zip" -d "${TMP_DIR}"
    "${TMP_DIR}/aws/install" --bin-dir "${INSTALL_DIR}" --install-dir "${VIB_HOME}/aws-cli" --update
    rm -rf "${TMP_DIR}"
    echo "✓ AWS CLI v2 installed into ${INSTALL_DIR}/aws"
  else
    echo "ℹ️  On macOS, install AWS CLI via: brew install awscli"
  fi
fi

# 4. Build and Install VIB Executable
echo "==> Compiling VIB single standalone binary..."
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "${SCRIPT_DIR}")"

if [ -f "${PROJECT_ROOT}/go.mod" ]; then
  cd "${PROJECT_ROOT}"
  
  # Ensure frontend dist exists
  if [ ! -f "web/dist/index.html" ]; then
    echo "==> Building React Web UI..."
    (cd web && npm install && npm run build)
  fi

  echo "==> Compiling Go binary with embedded frontend..."
  go build -trimpath -ldflags="-s -w" -o "${INSTALL_DIR}/vib" ./cmd/vib
  chmod +x "${INSTALL_DIR}/vib"
  echo "✓ VIB binary installed to ${INSTALL_DIR}/vib"
elif [ -f "./bin/vib" ]; then
  cp "./bin/vib" "${INSTALL_DIR}/vib"
  chmod +x "${INSTALL_DIR}/vib"
  echo "✓ VIB binary copied to ${INSTALL_DIR}/vib"
else
  echo "⚠️  Could not locate VIB source files to compile."
fi

# 5. PATH Verification and Setup
echo "----------------------------------------------------------------"
if [[ ":$PATH:" != *":${INSTALL_DIR}:"* ]]; then
  echo "🔔 IMPORTANT: Add ${INSTALL_DIR} to your PATH to run 'vib' anywhere:"
  echo ""
  echo "   echo 'export PATH=\"${INSTALL_DIR}:\$PATH\"' >> ~/.bashrc"
  echo "   # Or for zsh:"
  echo "   echo 'export PATH=\"${INSTALL_DIR}:\$PATH\"' >> ~/.zshrc"
  echo "   source ~/.bashrc"
  echo ""
fi

echo "🎉 VIB Installation Complete!"
echo "To start Visual Image Builder, run:"
echo "   ${INSTALL_DIR}/vib --serve --port 8080"
echo "================================================================"
