package config

import (
	"fmt"
	"os"

	logger "github.com/narglc/stock.quot.tele.bot/pkg/logger"

	"github.com/spf13/viper"
)

type TelegramConfig struct {
	Token string `mapstructure:"token"`
}

type RedisConfig struct {
	URL string `mapstructure:"url"`
}

type MCPConfig struct {
	Addr         string `mapstructure:"addr"`
	TargetChatID int64  `mapstructure:"target_chat_id"`
	// JWTSecret 为空时 MCP 不启用鉴权（仅回环访问）；非空时要求请求带合法 JWT Bearer。
	JWTSecret string `mapstructure:"jwt_secret"`
}

// TTSConfig 文本转语音配置。Provider 空=不启用语音；"azure" / "edge" 可切换。
// 敏感项（azure.key）留空由环境变量覆盖。
type TTSConfig struct {
	Provider string `mapstructure:"provider"`
	Azure    struct {
		Key    string `mapstructure:"key"`
		Region string `mapstructure:"region"`
		Voice  string `mapstructure:"voice"`
	} `mapstructure:"azure"`
	Edge struct {
		Voice string `mapstructure:"voice"`
	} `mapstructure:"edge"`
	// Http：把合成委托给外部 HTTP TTS 服务（如本地 worker 经 ssh -R 映射到回环）。
	Http struct {
		URL   string `mapstructure:"url"`
		Voice string `mapstructure:"voice"`
		Token string `mapstructure:"token"`
	} `mapstructure:"http"`
}

// ApifyConfig：清算图（CoinAnk 热力图）数据源的 Apify token。留空则 /liqmap 不可用。
type ApifyConfig struct {
	Token string `mapstructure:"token"`
}

// ImageConfig：/gen 生图 worker 的 HTTP 端点（本地 GPU worker，经 Tailscale/隧道暴露）。
type ImageConfig struct {
	URL   string `mapstructure:"url"`
	Token string `mapstructure:"token"`
}

type Config struct {
	Telegram TelegramConfig `mapstructure:"telegram"`
	Redis    RedisConfig    `mapstructure:"redis"`
	MCP      MCPConfig      `mapstructure:"mcp"`
	TTS      TTSConfig      `mapstructure:"tts"`
	Apify    ApifyConfig    `mapstructure:"apify"`
	Image    ImageConfig    `mapstructure:"image"`
	// 日志
	LoggerConfig logger.LoggerConfig `json:"logger" mapstructure:"logger"`
}

// InitConfig 以配置文件为主、环境变量可覆盖的方式加载配置。
// 敏感项（token/redis url）在 yaml 留空，由 TOKEN/RDB_URL 环境变量提供。
func InitConfig(configPath string) (*Config, bool) {
	if _, err := os.Stat(configPath); err != nil {
		return nil, false
	}

	viper.Reset()
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	viper.SetDefault("mcp.addr", "127.0.0.1:8081")

	viper.AutomaticEnv()
	// 显式绑定，兼容现有 TOKEN / RDB_URL 环境变量命名。
	_ = viper.BindEnv("telegram.token", "TOKEN")
	_ = viper.BindEnv("redis.url", "RDB_URL")
	_ = viper.BindEnv("mcp.target_chat_id", "MCP_TARGET_CHAT_ID")
	_ = viper.BindEnv("mcp.addr", "MCP_ADDR")
	_ = viper.BindEnv("mcp.jwt_secret", "MCP_JWT_SECRET")
	_ = viper.BindEnv("tts.provider", "TTS_PROVIDER")
	_ = viper.BindEnv("tts.azure.key", "AZURE_TTS_KEY")
	_ = viper.BindEnv("tts.azure.region", "AZURE_TTS_REGION")
	_ = viper.BindEnv("tts.azure.voice", "AZURE_TTS_VOICE")
	_ = viper.BindEnv("tts.edge.voice", "EDGE_TTS_VOICE")
	_ = viper.BindEnv("tts.http.url", "TTS_HTTP_URL")
	_ = viper.BindEnv("tts.http.voice", "TTS_HTTP_VOICE")
	_ = viper.BindEnv("tts.http.token", "TTS_HTTP_TOKEN")
	_ = viper.BindEnv("apify.token", "APIFY_TOKEN")
	_ = viper.BindEnv("image.url", "IMAGE_URL")
	_ = viper.BindEnv("image.token", "IMAGE_TOKEN")

	if err := viper.ReadInConfig(); err != nil {
		fmt.Printf("config file %s read failed. %v\n", configPath, err)
		return nil, false
	}

	var conf Config
	if err := viper.Unmarshal(&conf); err != nil {
		fmt.Printf("config file %s loaded failed. %v\n", configPath, err)
		return nil, false
	}

	fmt.Printf("config %s loaded ok! mcp.addr=%s target=%d\n", configPath, conf.MCP.Addr, conf.MCP.TargetChatID)
	return &conf, true
}
