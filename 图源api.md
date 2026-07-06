# 随机图源 API 清单（已验证）

> 验证日期：2026-07-06　验证方式：`curl` 实测 + 公共 DNS（8.8.8.8 / 1.1.1.1 / 223.5.5.5）交叉解析。
>
> ⚠️ 环境说明：验证在一台海外网络主机上进行，部分「域名解析正常但本机不可达」的接口，
> 很可能只是本测试机的地域/网络限制，需在**实际部署机**复验后再决定取舍（见「待复验」区）。

## 图例
- ✅ 可用：实测 HTTP 200 且返回预期内容
- ❌ 失效：全球公共 DNS 无解析 / 服务已下线 / 证书失效，默认客户端不可用
- ⚠️ 待复验：域名解析正常，但本测试环境不可达（超时 / 连接拒绝 / TLS 报错）

---

## ✅ 可用

### lolicon（二次元 setu，**生产在用**）
```
POST https://api.lolicon.app/setu/v2
Content-Type: application/json
{"r18": 1, "size": ["regular", "original"]}
```
响应（节选）：
```json
{"error":"","data":[{
  "pid":96223744,"title":"...","author":"...",
  "width":1412,"height":1987,"ext":"jpg",
  "urls":{
    "regular":"https://i.pixiv.re/img-master/.../96223744_p1_master1200.jpg",
    "original":"https://i.pixiv.re/img-original/.../96223744_p1.jpg"
  }
}]}
```
- `size` 支持 `mini/thumb/small/regular/original`。**优先用 `regular`（长边≤1200）**，避免 `original` 原图过大导致 Telegram 发送失败。代码 `domain/randompic/lolicon.go` 已按此实现。

### nekos.best（二次元 neko/waifu，无鉴权，**推荐新增**）
```
GET https://nekos.best/api/v2/neko      # 也有 /waifu /kitsune 等分类
```
响应：
```json
{"results":[{
  "artist_name":"...","source_url":"https://www.pixiv.net/...",
  "url":"https://nekos.best/api/v2/neko/xxxx.png",
  "dimensions":{"width":1241,"height":1752}
}]}
```
- 图片尺寸适中（多为千级像素），适合直接按图片发送。SFW。

### The Cat API / The Dog API（猫狗图，无鉴权）
```
GET https://api.thecatapi.com/v1/images/search
GET https://api.thedogapi.com/v1/images/search
```
响应：`[{"id":"...","url":"https://.../xxx.jpg","width":3000,"height":4000}]`
- 注意 `width/height` 可能很大（实测有 3000×4000），发送前建议走「过大兜底」逻辑。

### Lorem Picsum（占位图，无鉴权）
```
GET https://picsum.photos/800/600        # 302 跳转到具体图片
```
- 尺寸可控，最稳，可作为兜底默认图来源。

---

## ❌ 失效（不要再用）

| 源 | 端点 | 结论 |
|---|---|---|
| **lolimi 全系** | `https://api.lolimi.cn/API/*`（meinv / xjj / meizi / dmt / yuan） | `api.lolimi.cn` 在 8.8.8.8 / 1.1.1.1 / 223.5.5.5 **均无 A 记录**。主域 `lolimi.cn` 已迁移到 EdgeOne CDN，但 `api.` 子域不复存在。**⚠️ 生产 `getRandomPicSrc()` 仍在随机选 `"lolimi"`，实际已坏，需移除或替换。** |
| randomgirl | `https://jiejie.uk/taotu/random.php` | 全球公共 DNS 均无解析，域名已失效 |
| edgecats（猫猫动图） | `http://edgecats.net/` | HTTP 404（19B 文本），接口已下线 |
| mwm.moe（二次元壁纸） | `https://t.mwm.moe/fj/` 等 | 服务在线（忽略证书时返回 302），但 **TLS 证书已过期**，Go `http` 客户端默认会拒绝连接，不可直接用 |

---

## ⚠️ 待复验（部署机上再测）

| 源 | 端点 | 本环境现象 | 说明 |
|---|---|---|---|
| anosu（pixiv setu） | `POST https://image.anosu.top/pixiv/json` `{"r18":true}` | 连接超时 | 解析到阿里云上海 IP（47.102.156.132），疑似 CN 境内托管、跨境不通；若部署在国内大概率可用 |
| **sex.nyan.xyz**（**生产在用**） | `GET https://sex.nyan.xyz/api/v2/?r18=true` | TLS `unrecognized name` | 解析正常（AWS Global Accelerator）。`domain/randompic/sexnyan.go` 在用，务必在部署机确认是否仍可用 |
| dog.ceo（狗狗图） | `GET https://dog.ceo/api/breeds/image/random` | 443 连接失败 | 解析正常，疑似本环境网络问题 |
| shibe.online（柴犬图） | `GET https://shibe.online/api/shibes?count=1&urls=true&httpsUrls=true` | 空响应 | 解析正常，疑似本环境网络问题 |

---

## 与代码的关联（已按验证结果落地）
- `domain/randompic/` 现有实现：`lolicon`(✅) / `sexnyan`(⚠️ 待复验) / `nekos`(✅ 新增，nekos.best)。
  - 已删除失效的 `lolimi.go`。
- `handler/handleApi.go: getRandomPicSrc()` 随机名单已更新为 `["lolicon","sexnyan","nekos"]`。
- `Wakeup` 发送前会 `fetchSendablePhoto()` 预校验：**体积 > 10MB 或 宽+高 > 10000 的图直接跳过换下一张**
  （最多重试 3 次，都不合适则回落默认表情），从根本上避免「图图太大」发送失败。
- 新增图源的接入方式见 `domain/randompic/common.go`（接口 + `init()` 自注册到 `AllRandomPicSrv`）。

---

## 附：原始参考链接（未逐一验证）
- lolicon 说明：https://api.lolicon.app/#/setu
- anosu 说明：https://docs.anosu.top/intro/address.html
- 图源合集参考：https://blog.jixiaob.cn/?post=93
