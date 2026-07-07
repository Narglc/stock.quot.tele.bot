package tgmd

import "testing"

func TestConvert(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bold", "**bold**", "<b>bold</b>"},
		{"italic", "*italic*", "<i>italic</i>"},
		{"strike", "~~gone~~", "<s>gone</s>"},
		{"inline_code", "`code`", "<code>code</code>"},
		{"heading", "# Title", "<b>Title</b>"},
		{"link", "[go](https://go.dev)", `<a href="https://go.dev">go</a>`},
		{"escape", "1 < 2 & 3 > 0", "1 &lt; 2 &amp; 3 &gt; 0"},
		{"list", "- a\n- b", "• a\n• b"},
		{"code_block", "```\nfoo\nbar\n```", "<pre>foo\nbar</pre>"},
		{"mixed", "**a** and `b`", "<b>a</b> and <code>b</code>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Convert(c.in)
			if err != nil {
				t.Fatalf("Convert(%q) err: %v", c.in, err)
			}
			if got != c.want {
				t.Errorf("Convert(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
