#!/usr/bin/env python3
"""本地 TTS worker —— 接收 {text, voice}，返回 Ogg/Opus 音频。

架构：本地跑本 worker（扛重依赖：edge-tts / 未来 ChatTTS+GPU / 转码），
经 ssh -R 反向隧道映射到 VPS 回环；VPS 上 bot 的 http provider 调它、拿到音频再发 Telegram。
这样 VPS 保持精简（distroless，无 Python/ffmpeg），也不需要本地有公网端口。

依赖（edge 后端）：
    pip install edge-tts
    # 语音气泡转码，二选一：
    sudo apt install -y mpg123 opus-tools      # 极小(~700KB)
    # 或 sudo apt install -y ffmpeg

运行：
    python3 serve.py                            # 默认 127.0.0.1:5000
本地建反向隧道（把本地 5000 映到 VPS 回环 5000）：
    autossh -M 0 -N -R 5000:127.0.0.1:5000 user@your-vps
VPS 端 bot：
    TTS_PROVIDER=http  TTS_HTTP_URL=http://127.0.0.1:5000/tts  [TTS_HTTP_TOKEN=...]

环境变量：
    TTS_HOST/TTS_PORT   监听地址（默认 127.0.0.1:5000）
    TTS_VOICE           默认声色（默认 zh-CN-XiaoyiNeural）
    TTS_TOKEN           非空则校验请求头 X-Auth-Token（与 VPS 的 TTS_HTTP_TOKEN 一致）
    TTS_BACKEND         edge（默认）| gpu
    GPU_TTS_URL         TTS_BACKEND=gpu 时你的本地 GPU 模型(ChatTTS 等)HTTP 地址
"""
import json
import os
import shutil
import subprocess
import tempfile
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

HOST = os.environ.get("TTS_HOST", "127.0.0.1")
PORT = int(os.environ.get("TTS_PORT", "5000"))
DEFAULT_VOICE = os.environ.get("TTS_VOICE", "zh-CN-XiaoyiNeural")
AUTH_TOKEN = os.environ.get("TTS_TOKEN", "")
BACKEND = os.environ.get("TTS_BACKEND", "edge")
GPU_TTS_URL = os.environ.get("GPU_TTS_URL", "")


def synthesize(text: str, voice: str) -> bytes:
    """把文本合成为 Ogg/Opus 字节。按 TTS_BACKEND 分派。"""
    if BACKEND == "gpu":
        return _gpu(text, voice)
    return _edge(text, voice)


def _edge(text: str, voice: str) -> bytes:
    """edge-tts 合成 mp3，再转码为 Ogg/Opus。"""
    with tempfile.TemporaryDirectory() as d:
        mp3 = os.path.join(d, "a.mp3")
        ogg = os.path.join(d, "a.ogg")
        subprocess.run(
            ["edge-tts", "--voice", voice, "--text", text, "--write-media", mp3],
            check=True, capture_output=True,
        )
        _mp3_to_ogg(mp3, ogg)
        with open(ogg, "rb") as f:
            return f.read()


def _gpu(text: str, voice: str) -> bytes:
    """== 未来接本地 GPU 模型（ChatTTS 等容器）的集成点 ==
    默认实现：POST {text, voice} 到 GPU_TTS_URL，拿回音频字节；
    若模型输出不是 ogg（多为 wav/mp3），落地转码成 Ogg/Opus。
    按你模型的实际请求/响应字段调整这里即可。
    """
    if not GPU_TTS_URL:
        raise RuntimeError("TTS_BACKEND=gpu 但未设 GPU_TTS_URL")
    req = urllib.request.Request(
        GPU_TTS_URL,
        data=json.dumps({"text": text, "voice": voice}).encode(),
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=120) as resp:
        audio = resp.read()
        ctype = (resp.headers.get("Content-Type") or "").lower()
    if "ogg" in ctype or "opus" in ctype:
        return audio  # 模型已直出 ogg
    # 否则按原始音频落盘再转 ogg
    with tempfile.TemporaryDirectory() as d:
        raw = os.path.join(d, "in.audio")
        ogg = os.path.join(d, "a.ogg")
        with open(raw, "wb") as f:
            f.write(audio)
        _to_ogg(raw, ogg)
        with open(ogg, "rb") as f:
            return f.read()


def _mp3_to_ogg(mp3: str, ogg: str):
    """mp3 -> Ogg/Opus：优先 ffmpeg，否则 mpg123 + opusenc。"""
    _to_ogg(mp3, ogg)


def _to_ogg(src: str, ogg: str):
    if shutil.which("ffmpeg"):
        subprocess.run(
            ["ffmpeg", "-y", "-loglevel", "error", "-i", src,
             "-c:a", "libopus", "-b:a", "32k", "-ar", "48000", "-ac", "1", ogg],
            check=True,
        )
        return
    if shutil.which("mpg123") and shutil.which("opusenc"):
        wav = src + ".wav"
        subprocess.run(["mpg123", "-q", "-w", wav, src], check=True)
        subprocess.run(["opusenc", "--quiet", wav, ogg], check=True)
        return
    raise RuntimeError("需要 ffmpeg 或 mpg123+opusenc 做转码")


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        if self.path.rstrip("/") != "/tts":
            self.send_error(404)
            return
        if AUTH_TOKEN and self.headers.get("X-Auth-Token") != AUTH_TOKEN:
            self.send_error(401)
            return
        try:
            n = int(self.headers.get("Content-Length", 0))
            payload = json.loads(self.rfile.read(n) or b"{}")
            text = (payload.get("text") or "").strip()
            if not text:
                self.send_error(400, "empty text")
                return
            voice = payload.get("voice") or DEFAULT_VOICE
            audio = synthesize(text, voice)
        except subprocess.CalledProcessError as e:
            self.send_error(500, f"tts failed: {e.stderr[:200] if e.stderr else e}")
            return
        except Exception as e:  # noqa: BLE001
            self.send_error(500, str(e))
            return
        self.send_response(200)
        self.send_header("Content-Type", "audio/ogg")
        self.send_header("X-Audio-Format", "ogg")
        self.send_header("Content-Length", str(len(audio)))
        self.end_headers()
        self.wfile.write(audio)

    def log_message(self, *_):  # 静默默认访问日志
        pass


if __name__ == "__main__":
    print(f"TTS worker http://{HOST}:{PORT}/tts  backend={BACKEND}  "
          f"voice={DEFAULT_VOICE}  auth={'on' if AUTH_TOKEN else 'off'}")
    ThreadingHTTPServer((HOST, PORT), Handler).serve_forever()
