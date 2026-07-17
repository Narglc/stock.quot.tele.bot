# ComfyUI（从源码构建）。CUDA 由 pytorch 基础镜像带；如驱动较老可换低版本 cuda tag。
FROM pytorch/pytorch:2.4.0-cuda12.1-cudnn9-runtime

RUN apt-get update && apt-get install -y --no-install-recommends git \
 && rm -rf /var/lib/apt/lists/*

RUN git clone --depth 1 https://github.com/comfyanonymous/ComfyUI /app
WORKDIR /app
RUN pip install --no-cache-dir -r requirements.txt

EXPOSE 8188
# --listen 0.0.0.0 让容器外（adapter）可访问；模型/输出走挂载卷
CMD ["python", "main.py", "--listen", "0.0.0.0", "--port", "8188"]
