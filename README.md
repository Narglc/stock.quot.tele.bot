# stock.quot.tele.bot
股市行情TG机器人

## 行情来源
> 当前优选雪球
- []()
```bash
# 请求
https://stock.xueqiu.com/v5/stock/realtime/quotec.json?symbol=SH601001,SZ002617
```

### 可选其他行情平台
- [证券宝BaoStock](http://baostock.com/baostock/index.php/%E9%A6%96%E9%A1%B5)
- [tushare](http://tushare.org/)

## 知乎ref
[有哪些免费的获取股票数据的api接口？](https://www.zhihu.com/question/429015656)

## 雪球数据说明
```bash
{
    "symbol": "SZ002617",                   // ID
    "current": 5.4,                         // 现价
    "percent": 2.47,                        // 当前浮动
    "chg": 0.13,                            
    "timestamp": 1708671879000,
    "volume": 30 127788,                     // 总手 30.13w  总量  narglc： 数量*100
    "amount": 1 60575655,                    // 总额 1.61亿  
    "market_capital": 103 84231876,          // 市值 103.8亿
    "float_market_capital": 10213850315,     // 流通值 102.1亿
    "turnover_rate": 1.59,                   // 换手率？
    "amplitude": 3.23,                       // 振幅
    "open": 5.29,
    "last_close": 5.27,
    "high": 5.42,
    "low": 5.25,
    "avg_price": 5.33,
    "trade_volume": null,
    "side": null,
    "is_trade": false,                        // 是否盘中
    "level": 2,
    "trade_session": null,
    "trade_type": null,
    "current_year_percent": -12.9,
    "trade_unique_id": null,
    "type": 11,
    "bid_appl_seq_num": null,
    "offer_appl_seq_num": null,
    "volume_ext": null,
    "traded_amount_ext": null,
    "trade_type_v2": null,
    "yield_to_maturity": null
}
```