package sender

// Sender 是「向某平台的某个目标发送一条 markdown 消息」的抽象。
// 新平台（lark 等）实现本接口并通过 Register 注册即可，tool 层无需改动。
type Sender interface {
	// Send 把 markdown 文本发给 recipient（平台内的目标标识，Telegram 为 chat_id 的十进制字符串），
	// 返回平台侧消息 ID。
	Send(md, recipient string) (messageID string, err error)
	// Name 返回平台标识，如 "telegram"。
	Name() string
}

var registry = map[string]Sender{}

// Register 注册一个平台发送器（按 Name 覆盖）。
func Register(s Sender) { registry[s.Name()] = s }

// Get 按平台名取发送器。
func Get(name string) (Sender, bool) {
	s, ok := registry[name]
	return s, ok
}
