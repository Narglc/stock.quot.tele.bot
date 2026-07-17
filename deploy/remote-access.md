# 本地 worker → VPS 的接入方式（TTS / 生图 worker 共用）

我们的架构：重活（TTS、生图）跑在**本地**，VPS 上的 bot 通过 HTTP 调本地 worker。
本地通常在 NAT 后、无公网端口，所以要打通"VPS → 本地"这条路。三种方式，按推荐度：

| 方式 | 依赖 | 掉线自愈 | 适合 |
|---|---|---|---|
| **WireGuard（裸）** ⭐ | 无（全自建 FOSS，无账号） | ✅（PersistentKeepalive） | **有公网 VPS + 少量机器（本项目）** |
| **Tailscale** | 账号 + 其协调服务器（闭源） | ✅ | 全在 NAT 后 / 多设备 / 要零配置 |
| **ssh -R + autossh** | 无 | 靠 autossh 重连 | 临时/应急 |

打通后，bot 的 `TTS_HTTP_URL` / `IMAGE_URL` 填**本地机器的隧道 IP**即可。

---

## 方式一：裸 WireGuard（推荐——你有公网 VPS）

本地主动连 VPS 的公网 UDP 端口，无需打洞。给两端各分一个隧道 IP：VPS `10.8.0.1`、本地 `10.8.0.2`。

**装**（两端）：`sudo apt install -y wireguard`

**生成密钥**（两端各一次）：
```bash
wg genkey | tee privatekey | wg pubkey > publickey
```

**VPS `/etc/wireguard/wg0.conf`**：
```ini
[Interface]
Address = 10.8.0.1/24
ListenPort = 51820
PrivateKey = <VPS 的 privatekey>

[Peer]                       # 本地机器
PublicKey = <本地的 publickey>
AllowedIPs = 10.8.0.2/32
```

**本地 `/etc/wireguard/wg0.conf`**：
```ini
[Interface]
Address = 10.8.0.2/24
PrivateKey = <本地的 privatekey>

[Peer]                       # VPS
PublicKey = <VPS 的 publickey>
Endpoint = <VPS 公网IP>:51820
AllowedIPs = 10.8.0.1/32
PersistentKeepalive = 25     # 关键：定期心跳保住 NAT 映射，不断线
```

**启用**（两端）：
```bash
sudo wg-quick up wg0
sudo systemctl enable wg-quick@wg0     # 开机自启
```
**VPS 防火墙放通 UDP 51820**。验证：`sudo wg`（看到 latest handshake 即通），`ping 10.8.0.2`（VPS 上 ping 本地）。

**bot 配置**：worker 监听 `0.0.0.0`，bot 填本地隧道 IP：
```
TTS_HTTP_URL=http://10.8.0.2:5000/tts
IMAGE_URL=http://10.8.0.2:5001/gen
```
> docker 下 bot 容器访问 `10.8.0.2`：VPS 主机有到 `10.8.0.2` 的路由，容器经主机路由过去通常可达；若不通，给 bot 加 `network_mode: host` 或路由，或参考 tts-worker 的 host-gateway 方案。

---

## 方式二：Tailscale（图省事）

两端各跑：
```bash
curl -fsSL https://tailscale.com/install.sh | sh && sudo tailscale up
tailscale ip -4      # 记下本机 100.x.x.x
```
装前确认 VPS 支持 TUN：`ls /dev/net/tun`。bot 填本机 `100.x`：`IMAGE_URL=http://100.x.x.x:5001/gen`。
个人版免费（100 设备），客户端开源（BSD）；想全自建可用 Headscale 自搭控制面。

---

## 方式三：ssh -R + autossh（应急）

```bash
autossh -M 0 -N \
  -o ServerAliveInterval=30 -o ServerAliveCountMax=3 -o ExitOnForwardFailure=yes \
  -R 0.0.0.0:5001:127.0.0.1:5001 <user>@<vps>
```
`ServerAliveInterval` 防空闲断（解决 `client_loop: send disconnect: Broken pipe`）；autossh 断了自动重连。
docker 下还需 VPS sshd `GatewayPorts clientspecified` + compose `extra_hosts: host.docker.internal:host-gateway`，`URL` 用 `host.docker.internal`（见 tts-worker/README.md）。

---

## 一句话选择
- **要纯 FOSS、不依赖第三方、你有公网 VPS** → **WireGuard**；
- **懒得配、要零配置** → Tailscale；
- **临时试一下** → ssh -R + autossh（会偶发断线，靠 autossh 兜）。
