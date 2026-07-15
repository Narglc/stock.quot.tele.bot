package tts

import (
	"context"
	"strings"
	"testing"
)

func TestToOggOpusNoop(t *testing.T) {
	// 已是 ogg：不应转码，原样返回同一份数据。
	in := &Audio{Data: []byte("fake-ogg-bytes"), Format: "ogg"}
	out := ToOggOpus(context.Background(), in)
	if out != in {
		t.Fatalf("ogg 输入应原样返回（无转码）")
	}
	// nil 安全
	if ToOggOpus(context.Background(), nil) != nil {
		t.Fatalf("nil 输入应返回 nil")
	}
}

func TestSplitText(t *testing.T) {
	// 不超限：原样单段
	if got := SplitText("你好世界。", 100); len(got) != 1 || got[0] != "你好世界。" {
		t.Fatalf("短文本应单段返回, got %v", got)
	}
	// 空文本
	if got := SplitText("   ", 100); got != nil {
		t.Fatalf("空文本应返回 nil, got %v", got)
	}
	// 超限：按句子切分，每段不超过 maxRunes
	text := "第一句话在这里。第二句话也不短哦！第三句问句呢？第四句结束。"
	chunks := SplitText(text, 12)
	if len(chunks) < 2 {
		t.Fatalf("超长文本应被切分, got %d 段: %v", len(chunks), chunks)
	}
	for _, c := range chunks {
		if len([]rune(c)) > 18 { // 允许句子边界略超，但不应远超
			t.Fatalf("分段过长: %q (%d runes)", c, len([]rune(c)))
		}
	}
	// 拼回去应包含全部内容（去标点顺序保持）
	joined := strings.Join(chunks, "")
	for _, kw := range []string{"第一句", "第二句", "第三句", "第四句"} {
		if !strings.Contains(joined, kw) {
			t.Fatalf("切分后丢内容: 缺 %q", kw)
		}
	}
}

func TestHardSplitLongSentence(t *testing.T) {
	// 无标点的超长句子应被硬切
	long := strings.Repeat("字", 50)
	chunks := SplitText(long, 10)
	if len(chunks) != 5 {
		t.Fatalf("50字/每段10 应切成5段, got %d: %v", len(chunks), chunks)
	}
}

func TestBuildSSMLEscaping(t *testing.T) {
	ssml := buildSSML("zh-CN-XiaoxiaoNeural", "涨幅 <5% & 备注")
	if strings.Contains(ssml, "<5%") || !strings.Contains(ssml, "&lt;5% &amp; 备注") {
		t.Fatalf("SSML 未正确转义 &<: %s", ssml)
	}
	if !strings.Contains(ssml, "zh-CN-XiaoxiaoNeural") {
		t.Fatalf("SSML 缺少 voice: %s", ssml)
	}
}

func TestRegistry(t *testing.T) {
	Register(NewAzure("k", "eastasia", ""))
	s, ok := Get("azure")
	if !ok || s.Name() != "azure" {
		t.Fatalf("注册/获取 azure 失败: ok=%v", ok)
	}
}
