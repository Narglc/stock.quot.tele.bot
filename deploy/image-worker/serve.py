#!/usr/bin/env python3
"""本地生图 worker —— 接收 {prompt}，用 GPU(diffusers) 生成图片，返回 PNG。

架构：重活（模型 + GPU）在本地这台机器；经 Tailscale（推荐）或 ssh -R 暴露到 VPS，
bot 的 /gen 命令 POST 提示词过来拿图。和 TTS worker 一个模式。

依赖（建议 venv）：
    python3 -m venv venv && . venv/bin/activate
    pip install torch --index-url https://download.pytorch.org/whl/cu121
    pip install diffusers transformers accelerate safetensors sentencepiece peft

运行（SDXL，RTX 3060 12G 友好、LoRA 生态最全）：
    MODEL=stabilityai/stable-diffusion-xl-base-1.0 python3 serve.py
或 FLUX.1-schnell（Apache 可商用、4 步快）：
    MODEL=black-forest-labs/FLUX.1-schnell STEPS=4 python3 serve.py

VPS 端 bot 配置：
    IMAGE_URL=http://<本机 Tailscale IP 100.x>:5001/gen  [IMAGE_TOKEN=...]

环境变量：
    IMG_HOST/IMG_PORT   监听（默认 0.0.0.0:5001）
    MODEL               HF 模型 id 或本地路径（默认 SDXL）
    STEPS/WIDTH/HEIGHT  默认步数/尺寸（SDXL 25/1024；FLUX schnell 用 STEPS=4）
    LORA / LORA_WEIGHT  可选：LoRA 仓库或 .safetensors 路径 + 权重（默认 0.8）
    IMG_TOKEN           非空则校验请求头 X-Auth-Token（与 bot 的 IMAGE_TOKEN 一致）
"""
import io
import json
import os

import torch
from diffusers import AutoPipelineForText2Image
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

HOST = os.environ.get("IMG_HOST", "0.0.0.0")
PORT = int(os.environ.get("IMG_PORT", "5001"))
MODEL = os.environ.get("MODEL", "stabilityai/stable-diffusion-xl-base-1.0")
TOKEN = os.environ.get("IMG_TOKEN", "")
DEF_STEPS = int(os.environ.get("STEPS", "25"))
DEF_W = int(os.environ.get("WIDTH", "1024"))
DEF_H = int(os.environ.get("HEIGHT", "1024"))
LORA = os.environ.get("LORA", "")
LORA_W = float(os.environ.get("LORA_WEIGHT", "0.8"))

print(f"loading {MODEL} …")
pipe = AutoPipelineForText2Image.from_pretrained(MODEL, torch_dtype=torch.float16)
if LORA:
    pipe.load_lora_weights(LORA)
    try:
        pipe.set_adapters(pipe.get_active_adapters(), [LORA_W])
    except Exception:  # noqa: BLE001
        pass
    print(f"LoRA loaded: {LORA} @ {LORA_W}")
# 12GB 友好：把不常驻的子模块卸到内存 + VAE 分块，避免 OOM（尤其和 ollama 共用显卡时）。
pipe.enable_model_cpu_offload()
try:
    pipe.enable_vae_tiling()
except Exception:  # noqa: BLE001
    pass


def generate(prompt, negative, steps, w, h):
    out = pipe(
        prompt=prompt,
        negative_prompt=(negative or None),
        num_inference_steps=steps,
        width=w,
        height=h,
    )
    buf = io.BytesIO()
    out.images[0].save(buf, "PNG")
    return buf.getvalue()


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        if self.path.rstrip("/") != "/gen":
            self.send_error(404)
            return
        if TOKEN and self.headers.get("X-Auth-Token") != TOKEN:
            self.send_error(401)
            return
        try:
            n = int(self.headers.get("Content-Length", 0))
            p = json.loads(self.rfile.read(n) or b"{}")
            prompt = (p.get("prompt") or "").strip()
            if not prompt:
                self.send_error(400, "empty prompt")
                return
            data = generate(
                prompt, p.get("negative", ""),
                int(p.get("steps", DEF_STEPS)),
                int(p.get("width", DEF_W)), int(p.get("height", DEF_H)),
            )
        except Exception as e:  # noqa: BLE001
            self.send_error(500, str(e)[:200])
            return
        self.send_response(200)
        self.send_header("Content-Type", "image/png")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, *_):  # 静默默认访问日志
        pass


if __name__ == "__main__":
    print(f"image worker on http://{HOST}:{PORT}/gen  model={MODEL}")
    ThreadingHTTPServer((HOST, PORT), Handler).serve_forever()
