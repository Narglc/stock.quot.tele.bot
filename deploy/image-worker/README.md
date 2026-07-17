# 本地生图 worker（/gen）

本地 GPU 出图，bot 只发提示词收图。适配 **RTX 3060 12GB**（也可其它卡）。

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

## 3. 暴露给 VPS（二选一）

**Tailscale（推荐，掉线自愈、无 Broken pipe）**：
```bash
curl -fsSL https://tailscale.com/install.sh | sh && sudo tailscale up   # 本机 + VPS 各跑一次
tailscale ip -4        # 记下本机 100.x.x.x
```
VPS 的 `bot.env`：`IMAGE_URL=http://<本机100.x>:5001/gen`

**ssh -R（备选）**：
```bash
autossh -M 0 -N -o ServerAliveInterval=30 -o ExitOnForwardFailure=yes \
  -R 0.0.0.0:5001:127.0.0.1:5001 root@<vps>
```
（docker 下还需 sshd `GatewayPorts clientspecified` + compose `extra_hosts: host.docker.internal:host-gateway`，见 ../tts-worker/README.md）

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
