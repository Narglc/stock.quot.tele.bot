package utils

import "unicode/utf8"

// Truncate 把 s 截断到最多 n 字节，用于错误日志里限制超大响应体的长度。
// 与直接 s[:n] 不同：会退回到最近的 UTF-8 字符边界，不会在日志里留下半个中文字符；
// 发生截断时追加 "…" 以示省略。
func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	// 从 n 往回退，直到落在一个字符起始字节上。
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
