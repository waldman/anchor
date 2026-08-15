#!/usr/bin/env bash
# install.sh — Anchor installer
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/waldman/anchor/master/install.sh | bash -s -- [OPTIONS]
#   ./install.sh [OPTIONS]
#
# Options:
#   --bucket            S3 bucket (required)
#   --node              Node path, e.g. home/production/web_server (required)
#   --region            AWS region (required)
#   --dynamodb-table    DynamoDB table name (default: fleet-state)
#   --poll-interval     Poll interval (default: 5m)
#   --access-key-id     AWS access key ID
#   --secret-access-key AWS secret access key
#   --aws-profile       AWS credentials profile
#   --version           Anchor version to install (default: latest)
#   --install-dir       Binary install directory (default: /usr/local/bin)
#   --setup-systemd     Install and enable systemd service

set -euo pipefail

# ── Variables ────────────────────────────────────────────────────────────────

BUCKET=""
NODE=""
REGION=""
DYNAMODB_TABLE="fleet-state"
POLL_INTERVAL="5m"
ACCESS_KEY_ID=""
SECRET_ACCESS_KEY=""
AWS_PROFILE=""
VERSION=""
INSTALL_DIR="/usr/local/bin"
SETUP_SYSTEMD=false

CONFIG_DIR="/etc/anchor"
CONFIG_FILE="$CONFIG_DIR/anchor.toml"
SERVICE_FILE="/etc/systemd/system/anchor.service"
REPO="waldman/anchor"

TMP=""
trap '[[ -n "$TMP" ]] && rm -rf "$TMP"' EXIT

RED=$(tput setaf 1 2>/dev/null || true)
GRN=$(tput setaf 2 2>/dev/null || true)
YLW=$(tput setaf 3 2>/dev/null || true)
RST=$(tput sgr0 2>/dev/null || true)

info()  { echo "${GRN}==>${RST} $*"; }
warn()  { echo "${YLW}WARN:${RST} $*" >&2; }
error() { echo "${RED}ERROR:${RST} $*" >&2; exit 1; }

usage() {
  grep '^#' "$0" | sed 's/^# \?//'
  exit 0
}

# ── Argument parsing ──────────────────────────────────────────────────────────

[[ $# -eq 0 ]] && usage

while [[ $# -gt 0 ]]; do
  case "$1" in
    --bucket)            BUCKET="$2";            shift 2 ;;
    --node)              NODE="$2";              shift 2 ;;
    --region)            REGION="$2";            shift 2 ;;
    --dynamodb-table)    DYNAMODB_TABLE="$2";    shift 2 ;;
    --poll-interval)     POLL_INTERVAL="$2";     shift 2 ;;
    --access-key-id)     ACCESS_KEY_ID="$2";     shift 2 ;;
    --secret-access-key) SECRET_ACCESS_KEY="$2"; shift 2 ;;
    --aws-profile)       AWS_PROFILE="$2";       shift 2 ;;
    --version)           VERSION="$2";           shift 2 ;;
    --install-dir)       INSTALL_DIR="$2";       shift 2 ;;
    --setup-systemd)     SETUP_SYSTEMD=true;     shift ;;
    --help|-h)           usage ;;
    *) error "Unknown option: $1" ;;
  esac
done

# ── Validation ────────────────────────────────────────────────────────────────

[[ -z "$BUCKET" ]]  && error "--bucket is required"
[[ -z "$NODE" ]]    && error "--node is required"
[[ -z "$REGION" ]]  && error "--region is required"

# ── Platform detection ────────────────────────────────────────────────────────

detect_platform() {
  local os arch
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  arch=$(uname -m)

  case "$arch" in
    x86_64)  arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) error "Unsupported architecture: $arch" ;;
  esac

  case "$os" in
    linux|darwin) ;;
    *) error "Unsupported OS: $os" ;;
  esac

  echo "${os}_${arch}"
}

# ── Binary installation ───────────────────────────────────────────────────────

install_binary() {
  local platform="$1"
  TMP=$(mktemp -d)

  if [[ -z "$VERSION" ]]; then
    info "Fetching latest release..."
    VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
      | grep '"tag_name"' | sed 's/.*"tag_name": *"v\([^"]*\)".*/\1/')
    [[ -z "$VERSION" ]] && error "Could not determine latest version"
  fi

  local url="https://github.com/${REPO}/releases/download/v${VERSION}/anchor_${VERSION}_${platform}.tar.gz"
  info "Downloading anchor v${VERSION} for ${platform}..."
  curl -fsSL "$url" -o "$TMP/anchor.tar.gz"

  tar -xzf "$TMP/anchor.tar.gz" -C "$TMP"
  install -m 0755 "$TMP/anchor" "$INSTALL_DIR/anchor"
  info "Installed anchor to $INSTALL_DIR/anchor"
}

# ── Config file ───────────────────────────────────────────────────────────────

write_config() {
  mkdir -p "$CONFIG_DIR"
  chmod 750 "$CONFIG_DIR"

  cat > "$CONFIG_FILE" <<EOF
[daemon]
node          = "${NODE}"
poll_interval = "${POLL_INTERVAL}"

[s3]
bucket = "${BUCKET}"

[aws]
region = "${REGION}"
EOF

  if [[ -n "$ACCESS_KEY_ID" && -n "$SECRET_ACCESS_KEY" ]]; then
    cat >> "$CONFIG_FILE" <<EOF
access_key_id     = "${ACCESS_KEY_ID}"
secret_access_key = "${SECRET_ACCESS_KEY}"
EOF
  elif [[ -n "$AWS_PROFILE" ]]; then
    cat >> "$CONFIG_FILE" <<EOF
profile = "${AWS_PROFILE}"
EOF
  fi

  cat >> "$CONFIG_FILE" <<EOF

[state]
dynamodb_table = "${DYNAMODB_TABLE}"

[log]
level  = "info"
format = "json"
EOF

  chmod 600 "$CONFIG_FILE"
  info "Config written to $CONFIG_FILE"
}

# ── Systemd ───────────────────────────────────────────────────────────────────

setup_systemd() {
  cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=Anchor configuration management daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/anchor -config ${CONFIG_FILE} --daemon
Restart=on-failure
RestartSec=30s
StandardOutput=journal
StandardError=journal

NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/var/lib/anchor
ProtectHome=true

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable --now anchor
  info "systemd service installed and started"
}

# ── Main ──────────────────────────────────────────────────────────────────────

PLATFORM=$(detect_platform)

install_binary "$PLATFORM"
write_config

if [[ "$SETUP_SYSTEMD" == true ]]; then
  [[ $EUID -ne 0 ]] && error "--setup-systemd requires root"
  setup_systemd
else
  warn "--setup-systemd not set — run manually or add to systemd yourself"
  echo
  echo "  To run once:   anchor -config $CONFIG_FILE"
  echo "  To run daemon: anchor -config $CONFIG_FILE --daemon"
fi
