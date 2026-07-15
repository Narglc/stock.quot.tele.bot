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
