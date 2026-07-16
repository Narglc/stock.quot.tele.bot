# 本地 TTS worker（split 架构）

把"重"的语音合成放在**本地**（edge-tts / 未来 ChatTTS+GPU / 转码），VPS 只发请求、收音频、转发到 Telegram。VPS 保持精简（distroless，无需 Python/ffmpeg），本地也**不需要公网端口**（用 ssh 反向隧道出站）。

```
[本地] serve.py :5000  ← edge-tts / GPU 模型 + 转码（出 Ogg/Opus）
   │  ssh -R 5000:127.0.0.1:5000 user@vps   （本地主动出站，无公网端口）
   ▼
[VPS] bot（TTS_PROVIDER=http）→ POST http://127.0.0.1:5000/tts → 拿 ogg → sendVoice
   ▼
Telegram 语音气泡
```

## 1. 本地起 worker

```bash
pip install edge-tts
sudo apt install -y mpg123 opus-tools     # 转码(~700KB)，或 ffmpeg
python3 serve.py                          # 默认 127.0.0.1:5000
```

自测：
```bash
curl -X POST http://127.0.0.1:5000/tts \
  -H 'Content-Type: application/json' \
  -d '{"text":"测试","voice":"zh-CN-XiaoyiNeural"}' -o t.ogg && ffprobe t.ogg
```

## 2. 反向隧道（本地执行，把本地 5000 映到 VPS 回环 5000）

```bash
# 推荐 autossh 常驻自愈；无 autossh 用 ssh -N -R 亦可
autossh -M 0 -N -R 5000:127.0.0.1:5000 user@your-vps

ssh -N -R 5000:127.0.0.1:5000 root@192.129.148.175
```
`ssh -R` 是**本地出站** SSH，NAT / 无公网 IP 都不影响；VPS 侧只在回环 `127.0.0.1:5000` 可达，不对公网暴露。

## 3. VPS 端 bot 配置

```bash
TTS_PROVIDER=http
TTS_HTTP_URL=http://127.0.0.1:5000/tts
# 可选：与 worker 的 TTS_TOKEN 一致，做共享密钥校验
TTS_HTTP_TOKEN=<随机串>
```
worker 直出 Ogg/Opus → VPS 端 `ToOggOpus` 直接放行，**VPS 完全不需要 ffmpeg/转码工具**。

## 3.5 Docker 下（bot 在容器里）—— 必看

容器的 `127.0.0.1` 是**容器自己的回环**，不是宿主机的。`ssh -R` 默认把隧道挂在宿主的
`127.0.0.1:5000`，容器里 `http://127.0.0.1:5000` 连的是容器自身 → 报
`dial tcp 127.0.0.1:5000: connect: connection refused`，TTS 被降级发文本。三处一起改：

**① 隧道监听所有接口**（让容器经宿主网关够得到）。本地执行：

```bash
ssh -N -R 0.0.0.0:5000:127.0.0.1:5000 <user>@<vps>
# 常驻：autossh -M 0 -N -R 0.0.0.0:5000:127.0.0.1:5000 <user>@<vps>
```

并在 **VPS 的 `/etc/ssh/sshd_config`** 打开（默认 `no` 会强制只绑回环，容器够不到）：

```
GatewayPorts clientspecified
```

改后 `sudo systemctl restart sshd`。

**② compose 给 bot 加宿主网关解析**（`deploy/docker-compose.yml` 的 `bot` 服务）：

```yaml
  bot:
    extra_hosts:
      - "host.docker.internal:host-gateway"
```

**③ `bot.env` 的 URL 改成宿主网关**：

```bash
TTS_HTTP_URL=http://host.docker.internal:5000/tts
```

改完 `docker compose up -d bot` 重建。

排错：

```bash
docker ps | grep bot                     # 确认 bot 在容器里
ss -ltnp | grep 5000                     # 宿主上隧道应监听 0.0.0.0:5000（不是 127.0.0.1:5000）
# 从宿主经 docker 网关测（容器走的就是这条链路；网关默认 172.17.0.1，
# 自定义 compose 网络可能是 172.18.0.1 等，用 docker network inspect 查）：
curl -s -X POST http://172.17.0.1:5000/tts -H 'Content-Type: application/json' \
  -d '{"text":"测试"}' -o t.ogg && ls -l t.ogg
```

> 裸机（bot 直接跑在宿主）无此问题：`127.0.0.1:5000` 即可，隧道也用回环版
> `ssh -R 5000:127.0.0.1:5000`，无需 `GatewayPorts` / `extra_hosts`。

## 4. 接口约定

`POST /tts`，body `{"text": "...", "voice": "..."}`（voice 可空，用 worker 默认）。
响应：`200`，body = 音频字节，头 `Content-Type: audio/ogg` + `X-Audio-Format: ogg`。
非 200 视为失败，VPS 端会降级发纯文本，内容不丢。

## 5. 未来：接本地 GPU 模型（ChatTTS 等）

worker 已预留后端切换——把合成换成你本地那个 docker GPU 服务，只改环境变量：

```bash
TTS_BACKEND=gpu
GPU_TTS_URL=http://127.0.0.1:8001/tts     # 你的 ChatTTS/GPU 容器地址
```
`serve.py` 的 `_gpu()` 会 POST `{text, voice}` 给该地址，拿回音频；若模型输出不是 ogg（多为 wav/mp3），worker 本地转码成 Ogg/Opus 再回给 VPS。按你模型实际的请求/响应字段微调 `_gpu()` 即可。这样 GPU 重活全在本地，VPS 与接口都不用动。

## 环境变量一览

| 变量 | 默认 | 说明 |
|---|---|---|
| `TTS_HOST` / `TTS_PORT` | `127.0.0.1` / `5000` | 监听地址 |
| `TTS_VOICE` | `zh-CN-XiaoyiNeural` | 默认声色 |
| `TTS_TOKEN` | 空 | 非空则校验 `X-Auth-Token` |
| `TTS_BACKEND` | `edge` | `edge` / `gpu` |
| `GPU_TTS_URL` | 空 | `gpu` 后端的模型地址 |
