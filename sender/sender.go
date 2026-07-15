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

// VoiceSender 是「发送语音/音频」的可选能力。实现了本接口的 Sender 才支持 send_voice。
// format 决定形态："ogg"(Ogg/Opus)走语音气泡 sendVoice，"mp3" 走音频文件 sendAudio。
type VoiceSender interface {
	SendVoice(audio []byte, format, caption, recipient string) (messageID string, err error)
}

var registry = map[string]Sender{}

// Register 注册一个平台发送器（按 Name 覆盖）。
func Register(s Sender) { registry[s.Name()] = s }

// Get 按平台名取发送器。
func Get(name string) (Sender, bool) {
	s, ok := registry[name]
	return s, ok
}
