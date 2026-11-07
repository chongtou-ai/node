# node

## udp-uplink-relay

xhttp packet-up 单向 UDP 上行中继。公开标签 `udp-uplink-relay-v1`（二进制对应 flutter 仓库的 `udp-uplink-relay-v2`）。在服务器上执行：

```bash
curl -fsSL https://raw.githubusercontent.com/chongtou-ai/node/udp-uplink-relay-v1/udp-uplink-relay/install.sh | sudo bash -s -- \
  --udp-listen 上行IP:443 \
  --ack-listen 回程IP:8444 \
  --xhttp-host 节点Host \
  --local-port 8081
```

安装文件在 `udp-uplink-relay/`。`--local-port` 是本机 xhttp 入站端口，默认 `127.0.0.1:8081`。也可以用 `--target HOST:PORT`。
