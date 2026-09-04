package stocks

import (
	"math"
	"testing"
)

// 真实响应样本（已转成 UTF-8）。字段下标没有官方文档，用它守住解析位置。
const tencentSample = `v_sh600519="1~贵州茅台~600519~1330.00~1298.88~1295.88~45416~27411~18005~1329.82~1~1329.80~2~1329.70~3~1329.60~1~1329.50~9~1330.00~2~1330.10~1~1330.20~2~1330.30~3~1330.40~1~~20260904150300~31.12~2.40~1338.86~1295.60~1330.00/45416/6021440861~45416~602144~0.36~19.95~~1338.86~1295.60~3.33~16705.56~16705.56~6.47~1429.47~1169.57~1.53~31~1298.81~18.25~19.73~~~0.10~602144.0861~142.9516~11~   A~GP-A";
v_sz000001="51~平安银行~000001~11.89~11.88~11.90~1523165~800000~723165~11.89~100~11.88~200~11.87~300~11.86~100~11.85~900~11.90~100~11.91~200~11.92~300~11.93~100~11.94~200~~20260904150300~0.01~0.08~12.00~11.85~11.89/1523165/1807254404~1523165~180725~0.78~5.12~~12.00~11.85~1.26~2307.25~2307.25~0.62~13.07~10.69~0.55~31~11.88~5.12~5.12~~~0.10~180725.4404~0~11~   A~GP-A";
`

func TestParseTencent(t *testing.T) {
	got := parseTencent(tencentSample)
	if len(got) != 2 {
		t.Fatalf("应解析出 2 条，实得 %d", len(got))
	}

	q := got["600519"]
	checks := []struct {
		name string
		got  float64
		want float64
	}{
		{"现价", q.Price, 1330.00},
		{"昨收", q.PrevClose, 1298.88},
		{"今开", q.Open, 1295.88},
		{"涨跌额", q.Change, 31.12},
		{"涨跌幅", q.ChangePct, 2.40},
		{"最高", q.High, 1338.86},
		{"最低", q.Low, 1295.60},
		{"成交量", q.Volume, 45416},
		{"换手率", q.TurnoverRate, 0.36},
		// 腾讯给的是万元，要转成元才和东财口径一致
		{"成交额(元)", q.Amount, 602144 * 1e4},
	}
	for _, c := range checks {
		if math.Abs(c.got-c.want) > 1e-6 {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if q.Name != "贵州茅台" || q.Code != "600519" {
		t.Errorf("名称/代码解析错误: %q %q", q.Name, q.Code)
	}
	if got["000001"].Name != "平安银行" {
		t.Errorf("第二条解析错误: %+v", got["000001"])
	}
}

func TestParseTencent_Garbage(t *testing.T) {
	// 各种畸形输入都不该 panic，也不该产出半条数据
	for _, in := range []string{"", "\n\n", `v_sh600519="";`, `v_sh600519="1~名字~600519";`, "没有引号"} {
		if got := parseTencent(in); len(got) != 0 {
			t.Errorf("输入 %q 不该解析出数据，实得 %+v", in, got)
		}
	}
}

func TestTencentCode(t *testing.T) {
	cases := map[string]string{
		"600519": "sh600519", "688981": "sh688981", "510300": "sh510300",
		"000001": "sz000001", "300750": "sz300750", "159915": "sz159915",
		"430047": "sz430047",
	}
	for code, want := range cases {
		if got := tencentCode(code); got != want {
			t.Errorf("tencentCode(%q) = %q, want %q", code, got, want)
		}
	}
}

// 腾讯与东财的字段口径必须一致，否则故障转移时数字会跳变。
func TestTencent_SameShapeAsEastMoney(t *testing.T) {
	q := parseTencent(tencentSample)["600519"]
	// 涨跌幅是百分数（2.40 而非 0.024）
	if q.ChangePct < 1 || q.ChangePct > 10 {
		t.Errorf("涨跌幅口径可疑: %v（应是百分数）", q.ChangePct)
	}
	// 成交额是元，茅台单日成交额应在十亿量级
	if q.Amount < 1e9 || q.Amount > 1e11 {
		t.Errorf("成交额口径可疑: %v（应是元）", q.Amount)
	}
	// 换手率是百分数
	if q.TurnoverRate <= 0 || q.TurnoverRate > 100 {
		t.Errorf("换手率口径可疑: %v", q.TurnoverRate)
	}
}
