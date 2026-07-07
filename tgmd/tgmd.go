package tgmd

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// gm 复用同一个解析器实例（含 GFM：删除线/表格/autolink）。
var gm = goldmark.New(goldmark.WithExtensions(extension.GFM))

// Convert 把标准 markdown 转成 Telegram 支持的 HTML 子集。
// 不支持的结构（标题/列表/表格）降级为粗体/项目符号/纯文本。
func Convert(md string) (string, error) {
	source := []byte(md)
	doc := gm.Parser().Parse(text.NewReader(source))

	var b strings.Builder
	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		switch node := n.(type) {
		case *ast.Heading:
			if entering {
				b.WriteString("<b>")
			} else {
				b.WriteString("</b>\n\n")
			}
		case *ast.Paragraph:
			if !entering {
				b.WriteString("\n\n")
			}
		case *ast.Blockquote:
			if entering {
				b.WriteString("<blockquote>")
			} else {
				b.WriteString("</blockquote>\n\n")
			}
		case *ast.List:
			if !entering {
				b.WriteString("\n")
			}
		case *ast.ListItem:
			if entering {
				b.WriteString("• ")
			} else {
				b.WriteString("\n")
			}
		case *ast.Emphasis:
			tag := "i"
			if node.Level == 2 {
				tag = "b"
			}
			if entering {
				b.WriteString("<" + tag + ">")
			} else {
				b.WriteString("</" + tag + ">")
			}
		case *extast.Strikethrough:
			if entering {
				b.WriteString("<s>")
			} else {
				b.WriteString("</s>")
			}
		case *ast.CodeSpan:
			if entering {
				b.WriteString("<code>")
			} else {
				b.WriteString("</code>")
			}
		case *ast.Link:
			if entering {
				b.WriteString(`<a href="`)
				b.WriteString(escapeAttr(string(node.Destination)))
				b.WriteString(`">`)
			} else {
				b.WriteString("</a>")
			}
		case *ast.AutoLink:
			if entering {
				url := string(node.URL(source))
				b.WriteString(`<a href="`)
				b.WriteString(escapeAttr(url))
				b.WriteString(`">`)
				b.WriteString(escape(url))
				b.WriteString("</a>")
				return ast.WalkSkipChildren, nil
			}
		case *ast.FencedCodeBlock:
			if entering {
				b.WriteString("<pre>")
				writeLines(&b, source, node)
				b.WriteString("</pre>\n\n")
				return ast.WalkSkipChildren, nil
			}
		case *ast.CodeBlock:
			if entering {
				b.WriteString("<pre>")
				writeLines(&b, source, node)
				b.WriteString("</pre>\n\n")
				return ast.WalkSkipChildren, nil
			}
		case *ast.ThematicBreak:
			if entering {
				b.WriteString("———\n\n")
			}
		case *ast.Text:
			if entering {
				b.WriteString(escape(string(node.Segment.Value(source))))
				if node.SoftLineBreak() || node.HardLineBreak() {
					b.WriteString("\n")
				}
			}
		case *ast.String:
			if entering {
				b.WriteString(escape(string(node.Value)))
			}
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(b.String()), nil
}

// liner 抽出 FencedCodeBlock/CodeBlock 共有的 Lines() 方法。
type liner interface{ Lines() *text.Segments }

func writeLines(b *strings.Builder, source []byte, n ast.Node) {
	l, ok := n.(liner)
	if !ok {
		return
	}
	lines := l.Lines()
	var sb strings.Builder
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		sb.Write(seg.Value(source))
	}
	b.WriteString(escape(strings.TrimRight(sb.String(), "\n")))
}

func escape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func escapeAttr(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}
