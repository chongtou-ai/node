# xboard-node

Node backend for [Xboard](https://github.com/cedar2025/Xboard). Supports `sing-box` / `xray-core` dual kernels.

This repository is the official `Xboard-Node-src-20260918` source snapshot, published at [chongtou-ai/node](https://github.com/chongtou-ai/node). The one-click installer matches the official Linux systemd script.

> **Disclaimer**: This project is for educational and learning purposes only.

## Features

- Protocols: V2Ray family, Trojan, Shadowsocks, Hysteria2, TUIC, AnyTLS
- Sync: WebSocket push + REST polling dual channel
- User controls: speed limit, device limit, alive-IP tracking, hot update
- Deploy modes: node mode, machine mode, standalone mode
- Multi-instance: single process binding multiple panels / nodes

## One-click install (Linux systemd)

Use the Release copy of `install.sh` (this works without merging to `main`). The installer is the same as official `Xboard-Node-src-20260918`: it installs `xboard-node` + `xbctl`, writes `/etc/xboard-node`, and enables `xboard-node.service`. Default kernel is `xray` (`--kernel singbox` to switch).

```bash
# Node mode (default kernel: xray)
curl -fsSL https://github.com/chongtou-ai/node/releases/latest/download/install.sh | \
  sudo bash -s -- --mode node --panel https://panel.example.com --token TOKEN --node-id 1

# Machine mode
curl -fsSL https://github.com/chongtou-ai/node/releases/latest/download/install.sh | \
  sudo bash -s -- --mode machine --panel https://panel.example.com --token TOKEN --machine-id 1
```

If GitHub is slow, use a mirror:

```bash
curl -fsSL https://ghfast.top/https://github.com/chongtou-ai/node/releases/latest/download/install.sh | \
  sudo bash -s -- --mode node --panel https://panel.example.com --token TOKEN --node-id 1
```

The installer first tries this repository's Release binaries, then falls back to official [cedar2025/xboard-node](https://github.com/cedar2025/xboard-node) binaries, including GitHub download mirrors.

Common follow-up actions:

```bash
sudo bash install.sh upgrade
sudo bash install.sh status
sudo bash install.sh uninstall --purge --yes
```

### Local source / binary

```bash
# Use binaries built in the current directory
sudo bash install.sh --panel https://panel.example.com --token TOKEN --node-id 1 \
  --binary ./xboard-node-linux-amd64 --xbctl-binary ./xbctl-linux-amd64

# Or build first, then install
make build-linux
sudo bash install.sh --panel https://panel.example.com --token TOKEN --node-id 1
```

## Docker

```bash
docker run -d --restart=always --network=host \
  -e apiHost=https://panel.com -e apiKey=TOKEN -e nodeID=1 \
  ghcr.io/chongtou-ai/node:latest
```

### Docker Compose

```bash
git clone -b compose --depth 1 https://github.com/cedar2025/xboard-node.git
cd xboard-node
vim config/config.yml   # set panel.url / token / node_id
docker compose up -d
```

## xbctl

Run `xbctl` after installation for help. Common commands:

```bash
xbctl list                          # list all instances
xbctl status                        # running status
xbctl bind add-node --panel URL --token TOKEN --node-id 1
xbctl bind add-machine --panel URL --token TOKEN --machine-id 1
xbctl bind remove-node --panel URL --node-id 1
xbctl service restart
```

## Configuration

Legacy single-panel config is fully compatible. Appending bindings auto-migrates to `instances` format. See `config.yml.example`.

## Extensions

- Custom routes: [docs-custom-routes.md](docs-custom-routes.md)
- Custom outbounds: [docs-custom-outbounds.md](docs-custom-outbounds.md)
- DNS providers (ACME DNS-01): [docs-dns-providers.md](docs-dns-providers.md)

## License

MPL-2.0. Upstream: [cedar2025/xboard-node](https://github.com/cedar2025/xboard-node).
