# Caddy 前置部署指南：MCP 代理 + 静态站 + 其他服务共存 443

本文承接 [`deploy/README.md`](./README.md)（那里讲了 bot + mcp-auth-proxy + Google OAuth 的基础部署）。当你需要在**同一台机、同一个 443 端口**上同时跑「MCP 代理 + 静态网页展示 + 其他服务（如 v2ray）」时，用本文的 Caddy 前置方案。

---

## 1. 为什么要 Caddy 前置

- **443 只能被一个进程绑定**。mcp-auth-proxy 默认自己做 ACME、直接占 443，于是别的服务（静态站、v2ray）就没法再用 443。
- 解决办法：让 **Caddy 当唯一的 443 门面**，按域名 / 路径分流，各后端退到内部 HTTP 端口。一个 Caddy 同时扛：静态站 + MCP 代理 + v2ray。

## 2. 目标架构

```
                          :443 / :80
公网 ───────────────▶  Caddy（唯一门面，统一 TLS/ACME）
                          │
        ┌─────────────────┼───────────────────────────┐
        │ narglc.eu.org/  │ narglc.eu.org/mcp 等        │ 其他域名
        ▼ 静态文件         ▼ 反代                        ▼ 反代
    file_server       mcp-auth-proxy(:8080, HTTP-only)   v2ray ...
                          │ Google OAuth + 注入 JWT
                          ▼
                      gohome bot(:8081, 仅内网)
                          ▼ Telegram
```

## 3. 端口与组件

| 组件 | 监听 | 对公网 |
|---|---|---|
| **Caddy** | 80 + 443 | ✅ 唯一入口 |
| mcp-auth-proxy | 8080（HTTP，`NO_AUTO_TLS`） | ❌ 仅供 Caddy 反代 |
| gohome bot | 8081 | ❌ 仅供 mcp-proxy 反代 |
| redis | 6379 | ❌ 仅内网 |

## 4. 路由方案：二选一

| | 方案 B：同域名按路径分流 | 方案 A：静态站占 apex，MCP 挪子域 |
|---|---|---|
| 静态站 | `narglc.eu.org/`（除保留路径外） | 整个 `narglc.eu.org` |
| MCP | `narglc.eu.org/mcp`（**不变**） | `mcp.narglc.eu.org/mcp` |
| 要不要改 claude.ai / Google | **不用** | 要重配 URL + 回调 + DNS |
| 适合 | 已跑通、不想再折腾 | 想要干净主域给网页 |

推荐 **方案 B**（下面以它为主）。

## 5. 组件配置

### 5.1 mcp-auth-proxy 改为 HTTP-only（`proxy.env` / compose environment）

```ini
EXTERNAL_URL=https://narglc.eu.org   # 保持 https，proxy 据此生成对外 URL
NO_AUTO_TLS=true                     # 关掉自带 ACME/TLS（交给 Caddy）
LISTEN=:8080                         # 只听内部 HTTP 端口
TRUSTED_PROXIES=<见下>               # 信任 Caddy 传的 X-Forwarded-Proto/Host，否则可能重定向死循环
```
> 删掉 `TLS_ACCEPT_TOS`；`TRUSTED_PROXIES` 的值取决于 Caddy 是容器还是宿主（见 §6）。

### 5.2 Caddyfile（方案 B：路径分流 + 静态站 + 访问日志）

```caddyfile
narglc.eu.org {
    # 访问日志：log 是站点级指令，必须放顶层，不能放进 handle 块
    log {
        output file /var/log/caddy/narglc.eu.org/access.log
        # Caddy 文件日志默认就滚动（roll_size 100MiB / roll_keep 10 / roll_keep_for 2160h）
        # 如需自定义，把 roll_* 放进 output file 的子块，且 { 必须与指令同行：
        # output file /path/access.log {
        #     roll_size 100MiB
        #     roll_keep 10
        #     roll_keep_for 2160h
        #     roll_local_time
        # }
    }

    # MCP 代理独占的路径全部转给 mcp-proxy
    @mcp path /mcp /mcp/* /.well-known/* /.idp/* /.auth/* /healthz
    handle @mcp {
        reverse_proxy <UPSTREAM>          # 见 §6：容器名 or 127.0.0.1
    }

    # 其余路径 = 静态网页
    handle {
        root * /usr/share/nginx/html      # 你的静态文件目录
        encode gzip
        file_server                        # 需要目录浏览再加 browse
    }
}
```

### 5.3 docker-compose.yml（Caddy 为 compose 容器时）

```yaml
services:
  redis:
    image: redis:7-alpine
    restart: unless-stopped
    volumes: [redis-data:/data]

  bot:
    build: { context: .., dockerfile: deploy/Dockerfile }
    restart: unless-stopped
    env_file: [./bot.env]
    environment:
      RDB_URL: redis://redis:6379
      MCP_ADDR: 0.0.0.0:8081       # 仅内网
    depends_on: [redis]

  mcp-proxy:
    image: ghcr.io/sigbit/mcp-auth-proxy:latest
    restart: unless-stopped
    env_file: [./proxy.env]
    environment:
      DATA_PATH: /data
      NO_AUTO_TLS: "true"
      LISTEN: ":8080"
      TRUSTED_PROXIES: "172.16.0.0/12,10.0.0.0/8,192.168.0.0/16"
    command: ["--", "http://bot:8081"]   # ★ 上游填 bot 根地址，不带 /mcp（见 §7 坑2）
    volumes: [proxy-data:/data]
    depends_on: [bot]
    # ★ 不要 ports：80/443 归 caddy，8080 仅内网

  caddy:
    image: caddy:2
    restart: unless-stopped
    ports: ["80:80", "443:443"]     # ★ 80/443 归 caddy
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - ./sites:/etc/caddy/sites:ro
      - ./site:/usr/share/nginx/html:ro   # 静态网页
      - caddy-data:/data
      - caddy-config:/config
      - caddy-logs:/var/log/caddy
    depends_on: [mcp-proxy]

volumes:
  redis-data:
  proxy-data:
  caddy-data:
  caddy-config:
  caddy-logs:
```

## 6. 关键：Caddy 是「容器」还是「宿主服务」——决定 reverse_proxy 目标

这是最容易踩的 502 根源。

| Caddy 形态 | mcp-proxy 暴露方式 | Caddyfile 上游 | TRUSTED_PROXIES |
|---|---|---|---|
| **compose 容器**（在同一 compose 文件） | 不 publish 端口，走内网 | `reverse_proxy mcp-proxy:8080` | docker 私网段 |
| **宿主服务**（系统装的 Caddy，配置在 `/etc/caddy/…`） | 需 `ports: "127.0.0.1:8080:8080"` 暴露到宿主回环 | `reverse_proxy 127.0.0.1:8080` | `127.0.0.1/32` |

- **宿主 Caddy 解析不了容器名 `mcp-proxy`**，且若 mcp-proxy 没 publish 端口，宿主根本够不到它 → 502。
- 判断你是哪种：`docker compose ps` 里**有没有 `caddy` 容器**。没有 → 宿主服务。

宿主 Caddy 的 mcp-proxy 片段：
```yaml
  mcp-proxy:
    ports:
      - "127.0.0.1:8080:8080"   # 只对宿主回环，不对公网
    environment:
      LISTEN: ":8080"
      NO_AUTO_TLS: "true"
      TRUSTED_PROXIES: "127.0.0.1/32"
```

## 7. 常见坑与排错（本次部署实际踩过的）

**坑1 · Google 报 `redirect_uri_mismatch`**
回调地址没登记。到 Google Cloud Console 的 OAuth 客户端，把 `https://<域名>/.auth/google/callback` 原样加进「已授权的重定向 URI」（`https`、无结尾斜杠、类型必须是 Web application）。

**坑2 · claude.ai 报 "authorized but no MCP server found at the URL"**
OAuth 成功但连不到 MCP。根因：mcp-auth-proxy 会把外部请求路径**拼到上游地址后面**。上游若写成 `http://bot:8081/mcp`，外部 `/mcp` → `/mcp/mcp` → bot 404。**上游要填 bot 根地址 `http://bot:8081`**，外部 `/mcp` 才正确落到 bot 的 `/mcp`。

**坑3 · Caddy 启动报 `'log' is not an ordered HTTP handler`**
`log` 是站点级指令，不能放进 `handle {}` 块，挪到站点块顶层。

**坑4 · Caddy 报 `unrecognized subdirective: roll_size`**
`roll_size`/`roll_keep`/`roll_keep_for`/`roll_local_time` 是 `output file` 的子指令，不是 `log` 的。必须放进 `output file … { }` 子块里，且 `{` 与 `output file <path>` 同行。Caddy 默认已按 100MiB/10/2160h 滚动，多数情况可直接省略这些。

**坑5 · 走代理的路径全 502，静态站正常**
Caddy 够不到 mcp-proxy。按 §6 区分容器/宿主：
- `no such host` / `lookup mcp-proxy` → 名字解析不了（宿主 Caddy 用了容器名，或不在同网络）。
- `connection refused` → 够到了但 mcp-proxy 没在该端口监听（`LISTEN` 没生效，改后需 `docker compose up -d --force-recreate mcp-proxy`）。

**坑6 · 另起的服务（如 6277）不会自动走 Caddy**
Caddy 只处理到达它 80/443 的流量。别的端口是直连、绕过 Caddy。要让它走 Caddy，就别对公网开该端口，改由 Caddy `reverse_proxy` 到它（子域名或路径）。6277 是 MCP Inspector 默认代理端口，切勿对公网暴露。

## 8. 验证命令清单

外部探测（把 `H` 换成你的域名）：
```bash
H=narglc.eu.org
curl -sS -o /dev/null -w "root %{http_code} %{content_type}\n" https://$H/                       # 静态站，期望 200 text/html
curl -sS -o /dev/null -w "healthz %{http_code}\n"            https://$H/healthz                    # 期望 200
curl -sS https://$H/.well-known/oauth-authorization-server | head -c 120; echo                     # 期望 OAuth 元数据 JSON
curl -sS -o /dev/null -w "mcp %{http_code}\n" -X POST https://$H/mcp -d '{}'                        # 期望 401（鉴权门禁）
curl -sS -w "\ndcr %{http_code}\n" -X POST https://$H/.idp/register -H 'Content-Type: application/json' \
  -d '{"client_name":"probe","redirect_uris":["https://claude.ai/api/mcp/auth_callback"],"grant_types":["authorization_code"],"response_types":["code"],"token_endpoint_auth_method":"none"}'   # 期望 201 + client_id
```

从 Caddy 内部直拨上游（区分「监听没起」还是「网络不通」）：
```bash
# caddy 是容器：
docker compose exec caddy wget -qO- http://mcp-proxy:8080/healthz; echo
# caddy 是宿主：
curl -sS http://127.0.0.1:8080/healthz; echo        # 需 mcp-proxy 已 publish 到回环
```

日志：
```bash
docker compose logs --tail=40 mcp-proxy     # 监听端口 / 崩溃原因
docker compose logs --tail=20 caddy         # 容器 Caddy 的 502 dial 错误
sudo journalctl -u caddy --no-pager | tail  # 宿主 Caddy
```

## 9. 接入 claude.ai / 签发 JWT

不变，见 [`deploy/README.md`](./README.md)：Google OAuth 配置、`-mktoken` 签发注入 JWT、claude.ai 添加自定义连接器（方案 B 下连接器 URL 仍是 `https://narglc.eu.org/mcp`）。
