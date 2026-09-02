#!/usr/bin/env bash
# 一键部署 liqmap-web 到 Cloudflare Workers。幂等：重复跑只做缺的步骤。
#
# 前置：一个 Cloudflare API Token（无 GUI 服务器上 `wrangler login` 的 OAuth 回调打不回来）。
#   1. 打开 https://dash.cloudflare.com/profile/api-tokens → Create Token
#   2. 选模板 "Edit Cloudflare Workers"，不用改权限，Continue → Create
#   3. 把它写进仓库根的 deploy/bot.env：  CLOUDFLARE_API_TOKEN=xxx
#      （那文件已在 .gitignore，本来就是放密钥的地方）
#   若账号下有多个 account，再加一行 CLOUDFLARE_ACCOUNT_ID=xxx（dash 首页右侧能看到）
#
# 然后：  ./scripts/deploy.sh
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT="$(cd ../.. && pwd)"
ENV_FILE="$ROOT/deploy/bot.env"

say() { printf '\n\033[1;36m▸ %s\033[0m\n' "$*"; }
die() { printf '\n\033[1;31m✗ %s\033[0m\n' "$*" >&2; exit 1; }

# ---- 0. 凭证 ----
if [[ -z "${CLOUDFLARE_API_TOKEN:-}" && -f "$ENV_FILE" ]]; then
  # 只导出这两个变量，不把 bot.env 里其它密钥带进环境
  eval "$(grep -E '^(CLOUDFLARE_API_TOKEN|CLOUDFLARE_ACCOUNT_ID)=' "$ENV_FILE" | sed 's/^/export /')"
fi
[[ -n "${CLOUDFLARE_API_TOKEN:-}" ]] || die "缺 CLOUDFLARE_API_TOKEN：写进 $ENV_FILE 或 export 后再跑（见脚本头部注释）"

say "检查凭证"
# 先用 curl 打 /user/tokens/verify：这是校验 token 本身的端点，任何合法 token 都返回 200。
# 只调一次、不重试——Cloudflare 对连续认证失败会锁 IP（code 10502），锁了要等。
verify="$(curl -s -m 15 -w '\n%{http_code}' -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
  https://api.cloudflare.com/client/v4/user/tokens/verify)"
code="${verify##*$'\n'}"
case "$code" in
  200) echo "token 有效" ;;
  401) die "token 被 Cloudflare 拒绝（401）：这串字符不是合法的 API Token。重新创建并完整复制'创建成功'页面上显示的那一串（只显示一次），不是 Token ID" ;;
  403) die "token 合法但权限不够（403）：请用 'Edit Cloudflare Workers' 模板重建" ;;
  429) die "认证被临时限流（429/10502）：之前失败次数太多，等 10~15 分钟再试" ;;
  *)   die "校验失败（HTTP $code）：$(echo "$verify" | head -c 300)" ;;
esac
npx wrangler whoami 2>&1 | grep -E 'Account Name|Account ID' || true

# ---- 1. KV namespace ----
if grep -q 'REPLACE_WITH_YOUR_KV_NAMESPACE_ID' wrangler.toml; then
  say "创建 KV namespace LIQMAP"
  out="$(npx wrangler kv namespace create LIQMAP 2>&1)"
  echo "$out" | grep -vE '^\s*$' | tail -6
  id="$(echo "$out" | grep -oE 'id = "[0-9a-f]{32}"' | head -1 | grep -oE '[0-9a-f]{32}')"
  [[ -n "$id" ]] || die "没从输出里解析到 namespace id，手动把它填进 wrangler.toml 再跑"
  sed -i "s/REPLACE_WITH_YOUR_KV_NAMESPACE_ID/$id/" wrangler.toml
  echo "已写入 wrangler.toml: id = \"$id\""
else
  say "KV namespace 已配置，跳过"
fi

# ---- 2. PUSH_TOKEN secret ----
# 存在性检查：secret list 里有没有 PUSH_TOKEN。首次部署前 Worker 不存在，list 会失败，那就直接设。
if npx wrangler secret list 2>/dev/null | grep -q '"PUSH_TOKEN"'; then
  say "PUSH_TOKEN 已设置，跳过（要换：npx wrangler secret put PUSH_TOKEN）"
else
  say "设置 PUSH_TOKEN（bot 侧 LIQWEB_TOKEN 要用同一个值）"
  if [[ -n "${PUSH_TOKEN:-}" ]]; then
    printf '%s' "$PUSH_TOKEN" | npx wrangler secret put PUSH_TOKEN
  else
    gen="$(head -c 24 /dev/urandom | base64 | tr -d '/+=' | head -c 32)"
    echo "未提供 PUSH_TOKEN，生成一个随机值："
    echo
    echo "    $gen"
    echo
    echo "把它同时写进 bot 侧：LIQWEB_TOKEN=$gen"
    printf '%s' "$gen" | npx wrangler secret put PUSH_TOKEN
  fi
fi

# ---- 3. 部署 ----
say "部署"
npx wrangler deploy 2>&1 | grep -vE '^\s*$' | tail -8

say "完成"
cat <<TXT
下一步，bot 侧配两个变量（config.yaml 的 liqweb 段，或环境变量）：
  LIQWEB_URL=https://liqmap-web.<你的子域>.workers.dev   ← 上面 deploy 输出里那个地址，不带结尾斜杠
  LIQWEB_TOKEN=<上面设置的 PUSH_TOKEN>
重启 bot 后，在 Telegram 发一次 /liqmap BTC，快照就会推上来。
TXT
