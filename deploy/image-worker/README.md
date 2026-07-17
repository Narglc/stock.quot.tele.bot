# 本地生图 worker（/gen）

本地 GPU 出图，bot 只发提示词收图。适配 **RTX 3060 12GB**（也可其它卡）。

> **`serve.py` 是完整独立的生图器（用 diffusers 直接出图），不是转发、不需要 ComfyUI。**
> HTTP 进 → diffusers 生成 → PNG 出。**ComfyUI 是"另一条路"**（功能更全的替代后端），
> 用它就不用 serve.py，改让 bot 调 ComfyUI 的 API。简单 `/gen` 用 `serve.py` 就够。

```
[本地 GPU] serve.py :5001  ← diffusers(SDXL / FLUX) 生成 PNG
   │  Tailscale（推荐）或 ssh -R
   ▼
[VPS] bot: /gen <描述> → POST → 拿图 → sendPhoto
```

## 1. 装依赖（建议 venv）

```bash
python3 -m venv venv && . venv/bin/activate
pip install torch --index-url https://download.pytorch.org/whl/cu121   # 按你的 CUDA 选
pip install diffusers transformers accelerate safetensors sentencepiece peft
```

## 2. 起 worker

```bash
# SDXL：12G 友好、LoRA 生态最全（首次会从 HF 下模型，约 7GB）
MODEL=stabilityai/stable-diffusion-xl-base-1.0 python3 serve.py
# 或 FLUX.1-schnell：Apache 可商用、4 步快
MODEL=black-forest-labs/FLUX.1-schnell STEPS=4 python3 serve.py
```

自测：
```bash
curl -X POST http://127.0.0.1:5001/gen -H 'Content-Type: application/json' \
  -d '{"prompt":"a cyberpunk city at night, neon, rain"}' -o t.png && ls -l t.png
```

## 3. 暴露给 VPS

三种方式（**裸 WireGuard 推荐** / Tailscale / ssh -R）详见 [`../remote-access.md`](../remote-access.md)。
打通后 VPS 的 `bot.env` 填本机隧道 IP，例如 WireGuard：`IMAGE_URL=http://10.8.0.2:5001/gen`。

## 4. bot 用法
配好 `IMAGE_URL` 重启 bot，发 `/gen a red panda astronaut, watercolor` 即可。

## LoRA（风格/角色定制，SDXL 生态最全）
```bash
MODEL=stabilityai/stable-diffusion-xl-base-1.0 \
LORA=<HF仓库或本地 .safetensors 路径> LORA_WEIGHT=0.8 python3 serve.py
```
用你自己的图训 LoRA（3060 12G 可行）后，把路径传进来即可。

## RTX 3060 12GB 选型 & 注意
| 模型 | 跑法 | 特点 |
|---|---|---|
| SDXL | 原生 fp16 | 快、LoRA 海量 |
| FLUX.1-schnell | fp16 + cpu offload，`STEPS=4` | Apache 可商用、快、画质强 |
| FLUX.1-dev | 需 GGUF/量化（建议走 ComfyUI），或 offload 慢跑 | 画质更顶、非商用 |

> ⚠️ **和 ollama 共用这块 GPU**：两个模型同时驻留 12G 可能不够。生图时让 ollama 空闲释放，或用更小量化，或错峰。`serve.py` 已开 `enable_model_cpu_offload` + VAE 分块尽量省显存。
