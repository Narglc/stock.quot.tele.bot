package utils

import "math/rand"

// GetRandomResponse 从预定义的字符串切片中随机选择一个字符串并返回
func GetRandomResponse() string {
	responses := []string{
		"不要调戏为湿啦！",
		"有完没完了？",
		"没想到你是这样的人！渣男！",
		"马楼，别玩了！",
		"叼毛，好好工作！",
		"大师我一生只做一件事",
		"Goodbye!",
		"杰哥不要啦！",
	}

	// 返回随机选择的字符串
	return responses[GetRandomIndex(len(responses))]
}

func GetRandomIndex(size int) int {
	// 不需要手动播种：Go 1.20 起全局 rand 自动随机播种，rand.Seed 已废弃。
	// 且「每次调用重新播种」本身是反模式——同一纳秒内的两次调用会得到相同结果。
	return rand.Intn(size)
}
