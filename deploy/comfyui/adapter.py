#!/usr/bin/env python3
"""ComfyUI 适配器：把 bot /gen 的简单契约 {prompt} 翻译成 ComfyUI 工作流，
提交 → 轮询 → 取图，返回 PNG。bot 的 IMAGE_URL 指向本服务(:5001/gen)。

为什么要它：ComfyUI 的 API 是"提交工作流 JSON → 拿 prompt_id → 轮询 history → 取图"，
不是"POST prompt 直接回图"。本适配器把这套封装起来，bot 端零改动。

环境变量：
    COMFY_URL   ComfyUI 地址（默认 http://comfyui:8188）
    CKPT        检查点文件名（放在 ComfyUI 的 models/checkpoints/ 下）
    STEPS/WIDTH/HEIGHT/CFG  采样参数
    PORT        监听端口（默认 5001）
    IMG_TOKEN   非空则校验 X-Auth-Token
"""
import json
import os
import time
import urllib.parse
import urllib.request
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

COMFY = os.environ.get("COMFY_URL", "http://comfyui:8188")
CKPT = os.environ.get("CKPT", "sd_xl_base_1.0.safetensors")
STEPS = int(os.environ.get("STEPS", "25"))
W = int(os.environ.get("WIDTH", "1024"))
H = int(os.environ.get("HEIGHT", "1024"))
CFG = float(os.environ.get("CFG", "7.0"))
PORT = int(os.environ.get("PORT", "5001"))
TOKEN = os.environ.get("IMG_TOKEN", "")


def workflow(prompt, negative, seed):
    # ComfyUI 默认 SDXL 文生图工作流（API 格式）。要加 LoRA 就插一个 LoraLoader 节点。
    return {
        "4": {"class_type": "CheckpointLoaderSimple", "inputs": {"ckpt_name": CKPT}},
        "6": {"class_type": "CLIPTextEncode", "inputs": {"text": prompt, "clip": ["4", 1]}},
        "7": {"class_type": "CLIPTextEncode", "inputs": {"text": negative, "clip": ["4", 1]}},
        "5": {"class_type": "EmptyLatentImage", "inputs": {"width": W, "height": H, "batch_size": 1}},
        "3": {"class_type": "KSampler", "inputs": {
            "seed": seed, "steps": STEPS, "cfg": CFG,
            "sampler_name": "euler", "scheduler": "normal", "denoise": 1.0,
            "model": ["4", 0], "positive": ["6", 0], "negative": ["7", 0], "latent_image": ["5", 0]}},
        "8": {"class_type": "VAEDecode", "inputs": {"samples": ["3", 0], "vae": ["4", 2]}},
        "9": {"class_type": "SaveImage", "inputs": {"filename_prefix": "gen", "images": ["8", 0]}},
    }


def _post(url, obj):
    req = urllib.request.Request(url, data=json.dumps(obj).encode(), headers={"Content-Type": "application/json"})
    return json.load(urllib.request.urlopen(req, timeout=30))


def generate(prompt, negative, seed):
    cid = uuid.uuid4().hex
    resp = _post(f"{COMFY}/prompt", {"prompt": workflow(prompt, negative, seed), "client_id": cid})
    pid = resp["prompt_id"]
    for _ in range(600):  # 最多 ~5min
        time.sleep(0.5)
        try:
            hist = json.load(urllib.request.urlopen(f"{COMFY}/history/{pid}", timeout=15))
        except Exception:  # noqa: BLE001
            continue
        if pid not in hist:
            continue
        for node in hist[pid].get("outputs", {}).values():
            for im in node.get("images", []):
                q = urllib.parse.urlencode({
                    "filename": im["filename"],
                    "subfolder": im.get("subfolder", ""),
                    "type": im.get("type", "output"),
                })
                return urllib.request.urlopen(f"{COMFY}/view?{q}", timeout=30).read()
        raise RuntimeError("ComfyUI 完成但无图片输出")
    raise RuntimeError("ComfyUI 超时")


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
            seed = int(p.get("seed", int(time.time() * 1000) % (2 ** 31)))
            data = generate(prompt, p.get("negative", ""), seed)
        except Exception as e:  # noqa: BLE001
            self.send_error(500, str(e)[:200])
            return
        self.send_response(200)
        self.send_header("Content-Type", "image/png")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, *_):
        pass


if __name__ == "__main__":
    print(f"comfy adapter :{PORT} -> {COMFY} ckpt={CKPT}")
    ThreadingHTTPServer(("0.0.0.0", PORT), Handler).serve_forever()
