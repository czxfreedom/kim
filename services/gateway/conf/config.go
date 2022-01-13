package conf

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kelseyhightower/envconfig"
	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/logger"
	"github.com/spf13/viper"
)

// Config Config
type Config struct {
	ServiceID       string   //serverId
	ServiceName     string   `default:"wgateway"` //服务名
	Listen          string   `default:":8000"`    //监听端口
	PublicAddress   string   //公共地址
	PublicPort      int      `default:"8000"` //公共端口
	Tags            []string //目标
	Domain          string   //域名
	ConsulURL       string   //consulUrl
	MonitorPort     int      `default:"8001"` //监控端口
	AppSecret       string   //校验码
	LogLevel        string   `default:"DEBUG"`
	MessageGPool    int      `default:"10000"` //消息池
	ConnectionGPool int      `default:"15000"` //连接池
}

func (c Config) String() string {
	bts, _ := json.Marshal(c)
	return string(bts)
}

// Init InitConfig
func Init(file string) (*Config, error) {
	viper.SetConfigFile(file)
	viper.AddConfigPath(".")
	viper.AddConfigPath("/etc/conf")

	var config Config

	err := envconfig.Process("kim", &config)
	if err != nil {
		return nil, err
	}

	if err := viper.ReadInConfig(); err != nil {
		logger.Warn(err)
	} else {
		if err := viper.Unmarshal(&config); err != nil {
			return nil, err
		}
	}

	if config.ServiceID == "" {
		localIP := kim.GetLocalIP()
		config.ServiceID = fmt.Sprintf("gate_%s", strings.ReplaceAll(localIP, ".", ""))
	}
	if config.PublicAddress == "" {
		config.PublicAddress = kim.GetLocalIP()
	}
	logger.Info(config)
	return &config, nil
}
