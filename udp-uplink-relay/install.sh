#!/usr/bin/env bash
# Public one-click installer for udp-uplink-relay (no Go / no git needed).
#
#   curl -fsSL "$BASE/install.sh" | sudo bash -s -- \
#     --udp-listen 上行IP:443 --ack-listen 回程IP:8444 --xhttp-host 节点Host
#
# Set UDP_UPLINK_RELAY_BASE to override the download root (no trailing slash).
set -euo pipefail

# Baked at publish time. Override with env UDP_UPLINK_RELAY_BASE.
# Tag udp-uplink-relay-v1 on the public repo chongtou-ai/node.
BASE_URL="${UDP_UPLINK_RELAY_BASE:-https://raw.githubusercontent.com/chongtou-ai/node/udp-uplink-relay-v1/udp-uplink-relay}"

UDP_LISTEN=""
ACK_LISTEN=""
TARGET=""
LOCAL_PORT=""
XHTTP_HOST=""
XHTTP_PATH="/"
FORCE_KEY=0
START=1
VERBOSE=1
PREFIX="/usr/local"
ETC="/etc/udp-uplink-relay"

# Uncompressed binary sha256 (linux). Updated when binaries are rebuilt.
SHA_AMD64="aae94e93c6ef437b1bf5786c0f54716115da1d2a07a913343c098cdfbea766c8"
SHA_ARM64="f68736e943c5a684d427571df6493880a61bd9a4131fe6cea5e43fc869f67899"

usage() {
  cat <<'EOF'
usage: install.sh --udp-listen IP:PORT --ack-listen IP:PORT --xhttp-host HOST [options]

Required:
  --udp-listen ADDR     e.g. 1.2.3.4:443
  --ack-listen ADDR     return-path TCP ACK, e.g. 5.6.7.8:8444
  --xhttp-host HOST     must match local xhttp inbound Host

Optional:
  --local-port PORT     local xhttp inbound port on 127.0.0.1 (default 8081)
  --target HOST:PORT    full local forward address (overrides --local-port)
  --xhttp-path PATH     default /
  --force-key           rotate private.key
  --no-start            install only
  --quiet               verbose=false
  --base-url URL        download root (default baked / env UDP_UPLINK_RELAY_BASE)
EOF
  exit 2
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --udp-listen) UDP_LISTEN="${2:-}"; shift 2 ;;
    --ack-listen) ACK_LISTEN="${2:-}"; shift 2 ;;
    --target) TARGET="${2:-}"; shift 2 ;;
    --local-port) LOCAL_PORT="${2:-}"; shift 2 ;;
    --xhttp-host) XHTTP_HOST="${2:-}"; shift 2 ;;
    --xhttp-path) XHTTP_PATH="${2:-}"; shift 2 ;;
    --base-url) BASE_URL="${2:-}"; shift 2 ;;
    --force-key) FORCE_KEY=1; shift ;;
    --no-start) START=0; shift ;;
    --quiet) VERBOSE=0; shift ;;
    -h|--help) usage ;;
    *) echo "unknown arg: $1" >&2; usage ;;
  esac
done

[[ -n "$UDP_LISTEN" && -n "$ACK_LISTEN" && -n "$XHTTP_HOST" ]] || usage
if [[ -z "$TARGET" ]]; then
  LOCAL_PORT="${LOCAL_PORT:-8081}"
  [[ "$LOCAL_PORT" =~ ^[0-9]+$ ]] && (( LOCAL_PORT >= 1 && LOCAL_PORT <= 65535 )) || {
    echo "invalid --local-port: $LOCAL_PORT" >&2
    exit 1
  }
  TARGET="127.0.0.1:${LOCAL_PORT}"
fi
[[ "$(id -u)" -eq 0 ]] || { echo "run as root (sudo)" >&2; exit 1; }
if [[ -z "$BASE_URL" || "$BASE_URL" == "__BASE_URL__" ]]; then
  echo "set UDP_UPLINK_RELAY_BASE or --base-url to the download root" >&2
  exit 1
fi
BASE_URL="${BASE_URL%/}"

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64; WANT_SHA="$SHA_AMD64" ;;
  aarch64|arm64) ARCH=arm64; WANT_SHA="$SHA_ARM64" ;;
  *) echo "unsupported arch: $(uname -m) (need amd64 or arm64)" >&2; exit 1 ;;
esac

command -v curl >/dev/null 2>&1 || { echo "need curl" >&2; exit 1; }
command -v gzip >/dev/null 2>&1 || { echo "need gzip" >&2; exit 1; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
GZ="$TMP/udp-uplink-relay-linux-${ARCH}.gz"
BIN="$TMP/udp-uplink-relay"
URL="$BASE_URL/udp-uplink-relay-linux-${ARCH}.gz"

echo "== download $URL =="
curl -fsSL "$URL" -o "$GZ"
gzip -dc "$GZ" >"$BIN"
chmod 755 "$BIN"

got="$(sha256sum "$BIN" | awk '{print $1}')"
if [[ "$got" != "$WANT_SHA" ]]; then
  echo "sha256 mismatch for $ARCH binary" >&2
  echo "  want $WANT_SHA" >&2
  echo "  got  $got" >&2
  exit 1
fi

install -d -m 755 "$ETC" "$PREFIX/bin"
install -m 755 "$BIN" "$PREFIX/bin/udp-uplink-relay"

KEY="$ETC/private.key"
if [[ ! -f "$KEY" || "$FORCE_KEY" -eq 1 ]]; then
  mapfile -t lines < <("$PREFIX/bin/udp-uplink-relay" keygen)
  priv=""; pub=""
  for line in "${lines[@]}"; do
    case "$line" in
      private-key:*) priv="${line#private-key: }" ;;
      public-key:*) pub="${line#public-key: }" ;;
    esac
  done
  [[ -n "$priv" && -n "$pub" ]] || { echo "keygen failed" >&2; exit 1; }
  umask 077
  printf '%s\n' "$priv" >"$KEY"
  chmod 600 "$KEY"
else
  pub="$("$PREFIX/bin/udp-uplink-relay" pubkey -key "$KEY")"
fi

CFG="$ETC/config.json"
cat >"$CFG" <<EOF
{
  "udp-listen": ["$UDP_LISTEN"],
  "ack-listen": ["$ACK_LISTEN"],
  "target": "$TARGET",
  "private-key-file": "$KEY",
  "stats-interval-secs": 300,
  "verbose": $([[ "$VERBOSE" -eq 1 ]] && echo true || echo false),
  "xhttp": {
    "host": "$XHTTP_HOST",
    "path": "$XHTTP_PATH",
    "x-padding-bytes": "100-1000",
    "x-padding-obfs-mode": true,
    "x-padding-key": "_dc",
    "x-padding-header": "Referer",
    "x-padding-placement": "queryInHeader",
    "x-padding-method": "tokenish",
    "uplink-http-method": "POST",
    "session-placement": "path",
    "seq-placement": "query",
    "seq-key": "n",
    "uplink-data-placement": "auto",
    "sc-max-each-post-bytes": "1000000"
  }
}
EOF
chmod 644 "$CFG"

cat >/etc/sysctl.d/90-udp-uplink-relay.conf <<'EOF'
net.core.rmem_max = 16777216
net.core.wmem_max = 16777216
EOF
sysctl --system >/dev/null 2>&1 || true

cat >/etc/systemd/system/udp-uplink-relay.service <<'EOF'
[Unit]
Description=xhttp packet-up one-way UDP uplink relay
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/udp-uplink-relay run -c /etc/udp-uplink-relay/config.json
Restart=always
RestartSec=2
LimitNOFILE=65535
AmbientCapabilities=CAP_NET_BIND_SERVICE
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
if [[ "$START" -eq 1 ]]; then
  systemctl enable --now udp-uplink-relay
  sleep 1
  systemctl --no-pager --full status udp-uplink-relay || true
fi

udp_host="${UDP_LISTEN%:*}"
udp_port="${UDP_LISTEN##*:}"
ack_host="${ACK_LISTEN%:*}"
ack_port="${ACK_LISTEN##*:}"

cat <<EOF

OK  udp-uplink-relay installed ($ARCH)
  binary: $PREFIX/bin/udp-uplink-relay
  config: $CFG
  public-key: $pub

Paste into this node's xhttp-opts:

  udp-uplink:
    server: $udp_host
    port: $udp_port
    public-key: $pub
    ack-server: $ack_host
    ack-port: $ack_port

Logs: journalctl -u udp-uplink-relay -f
EOF
