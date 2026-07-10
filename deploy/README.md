# 云端部署：gohome bot + MCP（经 mcp-auth-proxy 接入 claude.ai 网页版）

目标：让 **claude.ai 网页版**（只认 OAuth）能连上我们**静态 JWT 鉴权**的 MCP bot。方案是在 bot 前面放一个 [`sigbit/mcp-auth-proxy`](https://github.com/sigbit/mcp-auth-proxy)：它对 claude.ai 讲标准 OAuth2.1 + DCR，用户登录委托给 Google；校验通过后把我们的 JWT 注入 `Authorization` 转发给 bot——**bot 的鉴权/路由代码一行不改**。

```
claude.ai web ──OAuth(Google)+DCR──▶ mcp-auth-proxy  (:443, ACME TLS)
                                      │ 验证是你的 Google 账号
                                      │ 注入 Authorization: Bearer <你的JWT>
                                      ▼
                                    gohome bot  127.0.0.1:8081/mcp
                                      │ 校验 JWT(HS256) → sub=chat_id
                                      ▼ 发 Telegram
```

> 只用 Claude Code / API 的话**不需要本方案**：直接 `.mcp.json` 配 `headers: Authorization Bearer` 即可（见项目根 README）。本目录仅为「要接 claude.ai 网页版」而设。
>
> 若要在**同一 443 端口**上同时跑「MCP + 静态网页 + 其他服务（如 v2ray）」，用 Caddy 前置方案，见 [`caddy-guide.md`](./caddy-guide.md)（含路由分流、宿主/容器 Caddy 区别、502 等排错）。

---

## 方式 A：Docker Compose（推荐，对系统侵入最小）

只需 Docker + Compose 插件。三个容器：`redis` + `bot`（内网 8081，不对宿主/公网开放）+ `mcp-proxy`（对外 80/443、自动 ACME）。

```bash
cd deploy
cp bot.env.example bot.env
cp proxy.env.example proxy.env
```
- 编辑 `bot.env`：填 `TOKEN` / `MCP_TARGET_CHAT_ID` / `MCP_JWT_SECRET`
  （`RDB_URL`、`MCP_ADDR` 由 compose 固定注入，`bot.env` 里的值会被忽略）
- 编辑 `proxy.env`：填 `EXTERNAL_URL` / `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` / `GOOGLE_ALLOWED_USERS`
  （`DATA_PATH` 由 compose 固定为 `/data`；`PROXY_BEARER_TOKEN` 见下）

**① 配 Google OAuth**：同下方[方式 B 第 3 步](#3-配-google-oauth)，回调填 `https://<你的域名>/.auth/google/callback`。

**② 签发注入用 JWT**（填进 `proxy.env` 的 `PROXY_BEARER_TOKEN`）：
```bash
docker compose run --rm -e MCP_JWT_SECRET=<与bot.env相同> bot \
  /app/bot -mktoken <你的chat_id> | tail -1
```

**③ DNS/防火墙**：域名 A 记录指向本机，放通入站 80 / 443。

**④ 启动**：
```bash
docker compose up -d --build
docker compose logs -f mcp-proxy      # 看 ACME 签证书、OAuth
```

自检：
```bash
docker compose ps
curl -s https://<你的域名>/.well-known/oauth-authorization-server | head
```

**⑤ 接入 claude.ai**：同下方[第 7 步](#7-接入-claudeai-网页版)，连接器 URL = `https://<你的域名>/mcp`。

> 更新 bot 代码：`docker compose up -d --build bot`。

### 数据与日志（运行时目录，已 gitignore）

首次 `up` 会在 compose 同级自动创建：

| 目录 | 内容 | 说明 |
|---|---|---|
| `./data/redis/` | redis 数据（AOF `appendonly*` + `dump.rdb`） | bind mount，注册群等落这里，重启不丢；可直接备份 |
| `./logs/` | bot 日志（lumberjack 滚动：`tele.bog.log` + 切割备份） | `tail -f logs/tele.bog.log` 查看 |

`mcp-proxy` 的证书 / OAuth 动态注册状态仍在命名卷 `proxy-data`（`docker compose down` 不加 `-v` 不会丢）。备份 redis 直接拷 `./data/redis/` 即可。

---

## 方式 B：systemd（裸机）

### 目录与账号

```bash
sudo useradd -r -s /usr/sbin/nologin gohome
sudo mkdir -p /opt/gohome/config /etc/gohome /var/lib/gohome/mcp-proxy
sudo chown -R gohome:gohome /opt/gohome /var/lib/gohome
```

## 1. 放置二进制与配置

```bash
# bot：在项目根构建（需 Go ≥ 1.25.5）
go build -o bot main.go
sudo cp bot /opt/gohome/bot
sudo cp config/config.yaml /opt/gohome/config/config.yaml

# mcp-auth-proxy：从 release 下载对应平台二进制
#   https://github.com/sigbit/mcp-auth-proxy/releases
sudo cp mcp-auth-proxy /opt/gohome/mcp-auth-proxy
sudo chmod +x /opt/gohome/bot /opt/gohome/mcp-auth-proxy
```

## 2. 填环境变量

```bash
sudo cp deploy/bot.env.example   /etc/gohome/bot.env
sudo cp deploy/proxy.env.example /etc/gohome/proxy.env
sudo chmod 600 /etc/gohome/*.env && sudo chown gohome:gohome /etc/gohome/*.env
# 编辑填入：bot.env 的 TOKEN/RDB_URL/MCP_TARGET_CHAT_ID/MCP_JWT_SECRET
#          proxy.env 的 EXTERNAL_URL/GOOGLE_*（PROXY_BEARER_TOKEN 见第 4 步）
```

密钥生成：`openssl rand -hex 32`（`MCP_JWT_SECRET`）。

## 3. 配 Google OAuth

Google Cloud Console → APIs & Services → Credentials → Create OAuth client ID →
类型选 **Web application** →
**Authorized redirect URI** 填（务必和 `EXTERNAL_URL` 一致）：

```
https://mcp.example.com/.auth/google/callback
```

拿到 Client ID / Secret 填进 `proxy.env` 的 `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET`；
`GOOGLE_ALLOWED_USERS` 填你自己的 Gmail，限定只有你能登录。

## 4. 签发注入用的 JWT

代理转发给 bot 时要带一个合法 JWT（sub=你的 chat_id）。用 bot 的 `-mktoken` 签（密钥用 `bot.env` 里同一个 `MCP_JWT_SECRET`）：

```bash
MCP_JWT_SECRET=<与bot.env相同> /opt/gohome/bot -mktoken <你的chat_id> | tail -1
```

把输出的 token 填进 `proxy.env` 的 `PROXY_BEARER_TOKEN`。

## 5. DNS 与防火墙

- `mcp.example.com` 的 A 记录指向本机公网 IP。
- 放通入站 **80 / 443**（80 供 ACME HTTP-01 验证，443 对外服务）。

## 6. 启动

```bash
sudo cp deploy/gohome-bot.service      /etc/systemd/system/
sudo cp deploy/gohome-mcp-proxy.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now gohome-bot gohome-mcp-proxy
# 看日志
journalctl -u gohome-bot -f
journalctl -u gohome-mcp-proxy -f
```

自检：
```bash
# bot 只在回环
ss -ltnp | grep 8081        # 127.0.0.1:8081
# 代理的 OAuth 元数据可达（claude.ai 靠它发现）
curl -s https://mcp.example.com/.well-known/oauth-authorization-server | head
```

## 7. 接入 claude.ai 网页版

Settings → Connectors →「+」→ **Add custom connector** →
- 名称：随意
- URL：`https://mcp.example.com/mcp`

保存后点连接，会跳 Google 登录（用 `GOOGLE_ALLOWED_USERS` 里的账号），授权完成即接入。之后就能让 Claude 调 `send_message` 给你的 Telegram 发消息。

> **上游地址填 bot 根地址 `http://127.0.0.1:8081`（docker 内为 `http://bot:8081`），不要带 `/mcp`**：
> 代理会把外部请求路径原样拼到上游后面，外部 `/mcp` + 上游根 → `http://bot:8081/mcp`（正确）；
> 若上游写成 `.../mcp`，会变成 `/mcp/mcp` → bot 返回 404，claude.ai 报「authorized 但找不到 MCP server」。

## 安全要点

- bot 始终绑 `127.0.0.1`，公网只暴露代理（TLS + Google 登录 + `GOOGLE_ALLOWED_USERS` 白名单）。
- `MCP_JWT_SECRET`、`PROXY_BEARER_TOKEN`、Google Secret 只放 `/etc/gohome/*.env`（`chmod 600`），勿进仓库。
- 改 `MCP_JWT_SECRET` 后要重新 `-mktoken` 更新 `PROXY_BEARER_TOKEN`。
