package handler

import (
	"fmt"
	"strings"
	"time"

	"github.com/narglc/stock.quot.tele.bot/domain/claims"
	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/stocks"
	tele "gopkg.in/telebot.v3"
)

var claimStore = claims.NewRedisStore()

const claimUsage = `📌 记一条带证伪条件的判断

格式（用 | 分段，手机上比 --flag 好打）：
/claim <判断> | 证伪 <条件> | 到期 <时长> | 依据 <来源>

例：
/claim BTC 会去扫上方 12.4万 的大簇 | 证伪 BTC<118000 | 到期 3d | 依据 清算地图15m

各段说明：
· 判断 —— 必填，你的观点本身
· 证伪 —— 必填，什么情况下算你错了。没有这段会被拒收：不能证伪的判断等于「偏多/偏空」，涨了算对跌了能找补
· 到期 —— 可选，默认 7d。支持 3d / 12h / 2w / 2026-09-10
· 依据 —— 可选，数据来源或推理依据，事后复盘用

证伪条件两种写法：
① 价格条件：BTC<118000、ETH>=3000、600519<1700
   支持 万 / w / k 单位（12.4万 = 124000，118k = 118000）
   这种会被自动检查：每小时一次，一旦触发立刻通知你，判定为错
② 其它任意文字：如「三天内成交量不放大」
   照样能记，只是到期时由你自己结案

注意：
· 到期 ≠ 错。到期只是提醒你来结案，不会自动给结论
· 被证伪优先于到期：条件先触发的直接判错，不等到期
· 6 位数字当 A 股代码，其余当币种符号
· 段前缀中英都认（证伪 / falsify / f），冒号或空格分隔都行

其它命令：
/claims          看还在追踪的
/claims all      看全部（含已结案）
/claims export   导出 jsonl
/resolve <id> correct|wrong|partial [说明]   结案`

// Claim 处理 /claim：记录一条带证伪条件的判断。
func Claim(c tele.Context) error {
	payload := strings.TrimSpace(c.Message().Payload)
	if payload == "" {
		return c.Send(claimUsage)
	}

	now := time.Now().In(beijingLoc)
	cl, err := claims.ParseInput(payload, now)
	if err != nil {
		// 证伪条件是这套东西的全部意义，缺了就不能记——否则又变回"偏多/偏空"那种话。
		return c.Send("记不了 🥲：" + err.Error() + "\n\n" + claimUsage)
	}

	cl.ID = claims.NewID()
	cl.ChatID = c.Chat().ID
	cl.UserID = c.Sender().ID
	cl.UserName = c.Sender().FirstName

	if err := claimStore.Save(cl); err != nil {
		log.Warnf("保存 claim 失败: %v", err)
		return c.Send("保存失败了 🥲：" + err.Error())
	}
	log.Infof("sender:[%d - %s] chat:[%d] /claim %s auto=%v", cl.UserID, cl.UserName, cl.ChatID, cl.ID, cl.Auto())

	var b strings.Builder
	b.WriteString("📌 已记录\n\n")
	b.WriteString(claims.FormatOne(cl, now))
	if cl.Auto() {
		b.WriteString("\n\n每小时自动检查一次，触发证伪会立刻告诉你。")
	} else {
		b.WriteString("\n\n这条没法自动验证，到期时我会来问你结果。")
	}
	return c.Send(b.String())
}

// Claims 处理 /claims：列表 / 全部 / 导出。
func Claims(c tele.Context) error {
	arg := strings.ToLower(strings.TrimSpace(c.Message().Payload))
	now := time.Now().In(beijingLoc)

	all, err := claimStore.All()
	if err != nil {
		log.Warnf("读取 claims 失败: %v", err)
		return c.Send("读取失败了 🥲：" + err.Error())
	}
	mine := claims.ByChat(all, c.Chat().ID)

	switch arg {
	case "export":
		if len(mine) == 0 {
			return c.Send("没有可导出的记录")
		}
		jsonl, eerr := claims.ExportJSONL(mine)
		if eerr != nil {
			return c.Send("导出失败了 🥲：" + eerr.Error())
		}
		doc := &tele.Document{
			File:     tele.FromReader(strings.NewReader(jsonl)),
			FileName: fmt.Sprintf("claims-%s.jsonl", now.Format("20060102")),
			Caption:  fmt.Sprintf("共 %d 条", len(mine)),
		}
		return c.Send(doc)
	case "all":
		return c.Send(claims.FormatList(mine, now, "📋 全部判断记录"))
	default:
		return c.Send(claims.FormatList(claims.Open(mine), now, "📋 追踪中的判断"))
	}
}

// Resolve 处理 /resolve <id> <结论> [说明]：人工结案。
func Resolve(c tele.Context) error {
	fields := strings.Fields(strings.TrimSpace(c.Message().Payload))
	if len(fields) < 2 {
		return c.Send("用法：/resolve <id> correct|wrong|partial [说明]\n例：/resolve A7K3M9 wrong 高估了空头力量")
	}

	id := strings.ToUpper(fields[0])
	outcome, ok := claims.ParseOutcome(fields[1])
	if !ok {
		return c.Send("结论只能是 correct / wrong / partial（或 对 / 错 / 半对）")
	}
	note := strings.Join(fields[2:], " ")

	cl, err := claims.Resolve(claimStore, id, outcome, note, time.Now().In(beijingLoc))
	if err != nil {
		return c.Send("结案失败 🥲：" + err.Error())
	}
	log.Infof("claim %s 已结案: %s", id, outcome)
	return c.Send("✅ 已结案\n\n" + claims.FormatOne(cl, time.Now().In(beijingLoc)))
}

// ClaimPrice 是给自动检查用的取价函数：6 位数字当 A股，其余当 crypto。
//
// 放在 handler 包而不是 claims 包：让 claims 保持对数据源无知，
// 「这个 symbol 该去哪查」是应用层的分派问题。
func ClaimPrice(symbol string) (float64, error) {
	if code, err := stocks.NormalizeStockCode(symbol); err == nil {
		quotes, qerr := stocks.GetStockQuotes([]string{code})
		if qerr != nil {
			return 0, qerr
		}
		if len(quotes) == 0 {
			return 0, fmt.Errorf("没查到 %s 的行情", symbol)
		}
		return quotes[0].Price, nil
	}

	quotes, _, err := stocks.GetCryptoQuotesBySymbols([]string{symbol}, false)
	if err != nil {
		return 0, err
	}
	if len(quotes) == 0 {
		return 0, fmt.Errorf("不认识的标的: %s", symbol)
	}
	return quotes[0].Price, nil
}
