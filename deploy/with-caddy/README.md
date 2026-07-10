# Caddy 前置版部署（宿主 / docker 两种）

在同一 443 上共存「MCP 代理 + 静态站」，Caddy 当唯一门面统一 TLS。原理与排错详见上层 [`../caddy-guide.md`](../caddy-guide.md)。

```
公网 :443 ─▶ Caddy ─┬─ /mcp、/.well-known、/.idp、/.auth、/healthz ─▶ mcp-proxy:8080 ─▶ bot:8081
                    └─ 其余路径 ─▶ 静态站(site/)
```

两版共用：`bot.env`、`proxy.env`（从 `*.example` 复制填值）、仓库根的 `deploy/Dockerfile`。
- 签注入 JWT：`docker compose -f <compose文件> run --rm -e MCP_JWT_SECRET=<密钥> bot /app/bot -mktoken <chat_id> | tail -1` → 填进 `proxy.env` 的 `PROXY_BEARER_TOKEN`。
- Google OAuth 回调登记 `https://<域名>/.auth/google/callback`；claude.ai 连接器 URL = `https://<域名>/mcp`。

---

## 方式一：宿主 Caddy（你线上在用的）

Caddy 是系统服务（`/etc/caddy`），compose 只跑 redis+bot+mcp-proxy，mcp-proxy 暴露到 `127.0.0.1:8080`。

```bash
cp bot.env.example bot.env && cp proxy.env.example proxy.env   # 填值（proxy.env 里 TRUSTED_PROXIES=127.0.0.1/32）
docker compose -f docker-compose.host-caddy.yml up -d --build

# 宿主 Caddy 站点配置
sudo cp narglc.eu.org.conf /etc/caddy/sites/narglc.eu.org.conf   # 需 Caddyfile 里有 import sites/*.conf
sudo mkdir -p /usr/share/nginx/html && sudo cp -r site/* /usr/share/nginx/html/   # 静态页
sudo systemctl reload caddy
```

## 方式二：Docker Caddy（Caddy 也进容器）

Caddy 容器独占 80/443，mcp-proxy 不对外暴露端口。

```bash
cp bot.env.example bot.env && cp proxy.env.example proxy.env
# ★ 把 proxy.env 的 TRUSTED_PROXIES 改为 172.16.0.0/12,10.0.0.0/8,192.168.0.0/16
#   （docker-caddy compose 也会用 environment 覆盖成这个）
# 静态页放 ./site/
docker compose -f docker-compose.docker-caddy.yml up -d --build
docker compose -f docker-compose.docker-caddy.yml logs -f caddy   # 看 ACME 签证书
```

---

## 验证

```bash
H=narglc.eu.org
curl -sS -o /dev/null -w "root %{http_code}\n" https://$H/            # 静态站 200
curl -sS -o /dev/null -w "mcp %{http_code}\n" -X POST https://$H/mcp -d '{}'   # 401（OAuth 门禁）
curl -sS https://$H/.well-known/oauth-authorization-server | head -c 80; echo   # OAuth 元数据
```

宿主/容器 Caddy 的 `reverse_proxy` 目标区别、502 排错见 [`../caddy-guide.md`](../caddy-guide.md) §6/§7。
