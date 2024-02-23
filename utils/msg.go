package utils

import (
	"math/rand"
	"time"
)

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
	}

	// 返回随机选择的字符串
	return responses[GetRandomIndex(len(responses))]
}

func GetRandomIndex(size int) int {
	// 初始化随机数种子
	rand.Seed(time.Now().UnixNano())

	// 随机选择一个索引
	return rand.Intn(size)
}
