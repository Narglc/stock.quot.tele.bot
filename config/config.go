package config

import (
	"fmt"
	"os"

	logger "github.com/narglc/stock.quot.tele.bot/pkg/logger"

	"github.com/spf13/viper"
)

type Config struct {
	// 日志
	LoggerConfig logger.LoggerConfig `json:"logger" mapstructure:"logger"`
}

// 返回err
func InitConfig(configPath string) (*Config, bool) {
	var conf Config
	_, err := os.Stat(configPath)
	if err != nil {
		return nil, false
	}

	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	err = viper.ReadInConfig()
	if err != nil {
		fmt.Printf("config file %s read failed. %v\n", configPath, err)
		return nil, false
	}
	err = viper.GetViper().Unmarshal(&conf)
	if err != nil {
		fmt.Printf("config file %s loaded failed. %v\n", configPath, err)
		return nil, false
	}

	fmt.Printf("config %s %+v load ok!\n", configPath, conf)
	return &conf, true
}
