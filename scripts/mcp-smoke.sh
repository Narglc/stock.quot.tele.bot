#!/usr/bin/env bash
# MCP 冒烟测试：initialize → tools/list → tools/call get_liqmap。
#
# 要在能直连 bot 的地方跑（公网 /mcp 前面有 Google OAuth 门禁，JWT 过不去）：
#   VPS 上：  MCP_URL=http://127.0.0.1:8081/mcp  JWT=$(bot -mktoken <chat_id>)  ./scripts/mcp-smoke.sh
#   compose： docker compose exec bot sh -c 'wget -qO- ...' 不方便，直接在宿主机用 127.0.0.1:8081（若端口已映射）
#             或 docker compose run --rm -e MCP_URL=http://bot:8081/mcp -e JWT=... --entrypoint sh bot -c "$(cat scripts/mcp-smoke.sh)"
#
# 免鉴权部署（MCP_JWT_SECRET 为空）时 JWT 可不设。
set -euo pipefail
MCP_URL="${MCP_URL:-http://127.0.0.1:8081/mcp}"
JWT="${JWT:-}"
SYMBOL="${SYMBOL:-BTC}"

auth=()
[[ -n "$JWT" ]] && auth=(-H "Authorization: Bearer $JWT")
hdr=(-H "Content-Type: application/json" -H "Accept: application/json, text/event-stream")

say() { printf '\n\033[1;36m▸ %s\033[0m\n' "$*"; }
# Streamable HTTP 可能回 SSE（data: 行）也可能回纯 JSON，统一抽出 JSON 部分
body() { sed -n 's/^data: //p' | grep -m1 . || cat; }

say "1. initialize  ($MCP_URL)"
resp="$(curl -sS -D /tmp/mcp-h.txt "${hdr[@]}" "${auth[@]}" "$MCP_URL" -d '{
  "jsonrpc":"2.0","id":1,"method":"initialize",
  "params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}')"
code="$(grep -oE 'HTTP/[0-9.]+ [0-9]+' /tmp/mcp-h.txt | tail -1 | awk '{print $2}')"
sid="$(grep -i '^mcp-session-id:' /tmp/mcp-h.txt | awk '{print $2}' | tr -d '\r')"
echo "HTTP $code   session=${sid:-<无，服务端是无状态模式>}"
echo "$resp" | body | python3 -c 'import sys,json; d=json.loads(sys.stdin.read()); r=d.get("result",{}); print("server:",r.get("serverInfo"),"| protocol:",r.get("protocolVersion")) if "result" in d else print("ERROR:",d.get("error"))'
[[ "$code" == "200" ]] || { echo "initialize 失败，后面不用看了。401 = JWT 不对或没带；404 = 路径不对"; exit 1; }
sess=(); [[ -n "$sid" ]] && sess=(-H "Mcp-Session-Id: $sid")

# 协议要求 initialize 后发一个 initialized 通知（有些服务端不发也行）
curl -sS -o /dev/null "${hdr[@]}" "${auth[@]}" "${sess[@]}" "$MCP_URL" -d '{"jsonrpc":"2.0","method":"notifications/initialized"}' || true

say "2. tools/list"
curl -sS "${hdr[@]}" "${auth[@]}" "${sess[@]}" "$MCP_URL" -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
 | body | python3 -c '
import sys,json; d=json.loads(sys.stdin.read())
if "error" in d: print("ERROR:",d["error"]); sys.exit(1)
tools=[t["name"] for t in d["result"]["tools"]]
print("tools:",tools)
print("get_liqmap:", "✓ 已注册" if "get_liqmap" in tools else "✗ 没有——bot 的环境里 APIFY_TOKEN 是空的（配了才注册）")'

say "3. tools/call get_liqmap symbol=$SYMBOL top_n=3"
echo "（缓存未命中会真的打一次 Apify，约 10 秒，计一次配额）"
curl -sS "${hdr[@]}" "${auth[@]}" "${sess[@]}" "$MCP_URL" -d "{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"tools/call\",\"params\":{\"name\":\"get_liqmap\",\"arguments\":{\"symbol\":\"$SYMBOL\",\"top_n\":3}}}" \
 | body | python3 -c '
import sys,json; d=json.loads(sys.stdin.read())
if "error" in d: print("ERROR:",d["error"]); sys.exit(1)
r=d["result"]
if r.get("isError"): print("tool 返回错误:", r["content"][0]["text"]); sys.exit(1)
p=json.loads(r["content"][0]["text"])
print(f"symbol={p[\"symbol\"]} 现价={p[\"current_price\"]} interval={p[\"interval\"]}")
print("above(空头爆仓区):",[(a["price"],round(a["value_usd"]/1e6,1)) for a in p["above"]],"(M)")
print("below(多头爆仓区):",[(a["price"],round(a["value_usd"]/1e6,1)) for a in p["below"]],"(M)")
print("note:",p["note"][:60],"…")'
say "全部通过"
