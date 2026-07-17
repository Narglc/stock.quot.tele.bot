# ComfyUI 生图后端（docker）— 对接 bot 的 /gen

用 ComfyUI 出图（比 `../image-worker/serve.py` 功能全：工作流、LoRA、更多模型）。
两个容器：**comfyui**（生图，吃 GPU）+ **adapter**（把 bot 的 `{prompt}` 翻译成 ComfyUI 工作流，返回 PNG）。

```
[本地 GPU] comfyui:8188  ← 生图
             ▲ 工作流 API
           adapter:5001  ← 把 /gen 的 {prompt} 封装成工作流；bot 只跟它打交道
   │  WireGuard/Tailscale（见 ../remote-access.md）
   ▼
[VPS] bot: IMAGE_URL=http://<本机隧道IP>:5001/gen → /gen <描述>
```

> serve.py（diffusers，独立出图）和 ComfyUI 是**二选一**。这里走 ComfyUI，就不用 serve.py。

## 前置
- 宿主装好 **NVIDIA 驱动 + nvidia-container-toolkit**（`docker run --rm --gpus all nvidia/cuda:12.1.0-base nvidia-smi` 能看到卡即可）。
- 下一个 **SDXL 检查点**放到 `./models/checkpoints/`，例如
  `sd_xl_base_1.0.safetensors`（HF: `stabilityai/stable-diffusion-xl-base-1.0`，~6.5GB）。

## 起服务
```bash
cd deploy/comfyui
mkdir -p models/checkpoints models/loras output
# 把 sd_xl_base_1.0.safetensors 放进 models/checkpoints/
docker compose up -d --build
docker compose logs -f comfyui        # 首次构建+加载模型稍久
```
把 `docker-compose.yml` 里 adapter 的 `CKPT` 改成你实际的检查点文件名。

## 自测（在本地）
```bash
curl -X POST http://127.0.0.1:5001/gen -H 'Content-Type: application/json' \
  -d '{"prompt":"a red panda astronaut, watercolor"}' -o t.png && ls -l t.png
```

## 接 bot
本地→VPS 打通后（见 [`../remote-access.md`](../remote-access.md)），VPS `bot.env`：
```
IMAGE_URL=http://<本机隧道IP>:5001/gen      # 如 WireGuard 的 10.8.0.2
# IMAGE_TOKEN=...                            # 若 adapter 设了 IMG_TOKEN
```
重启 bot，发 `/gen a cyberpunk city at night`。

## LoRA
把 LoRA 的 `.safetensors` 放到 `./models/loras/`，然后在 `adapter.py` 的 `workflow()` 里插一个
`LoraLoader` 节点（接在 CheckpointLoader 之后、CLIPTextEncode/KSampler 之前）。或直接用 ComfyUI
网页（开 `ports: ["8188:8188"]`）搭好工作流、"Save (API Format)" 导出，替换 `adapter.py` 的模板。

## FLUX
ComfyUI 原生支持 FLUX（含 GGUF 量化，适合 12GB）。换成 FLUX 工作流即可——建议先在 ComfyUI 网页
用官方 FLUX 模板跑通、导出 API 格式，再替换 adapter 的 `workflow()`。

## ⚠️ 和 ollama 共用 GPU
两者都吃这块卡，12GB 同时驻留可能 OOM。生图时让 ollama 空闲释放（keep_alive），或错峰。
