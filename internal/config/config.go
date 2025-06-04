package config

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"go-p2p/internal/log"
)

// Config 应用程序配置
type Config struct {
	// 服务器配置
	Server struct {
		Port int `json:"port"` // 服务器端口
	} `json:"server"`

	// 客户端配置
	Client struct {
		Port       int    `json:"port"`        // 客户端端口
		ServerAddr string `json:"server_addr"` // 服务器地址
		Username   string `json:"username"`    // 用户名
	} `json:"client"`

	// 日志配置
	Log struct {
		Level  string `json:"level"`  // 日志级别
		Output string `json:"output"` // 日志输出位置
	} `json:"log"`

	// 网络配置
	Network struct {
		ReadTimeout  int `json:"read_timeout"`  // 读取超时（秒）
		WriteTimeout int `json:"write_timeout"` // 写入超时（秒）
		DialTimeout  int `json:"dial_timeout"`  // 连接超时（秒）
	} `json:"network"`
}

var (
	// 单例实例
	instance *Config
	once     sync.Once
	mutex    sync.RWMutex
)

// GetConfig 获取配置实例（单例模式）
func GetConfig() *Config {
	once.Do(func() {
		instance = &Config{}
		// 设置默认值
		instance.Server.Port = 9000
		instance.Client.Port = 0 // 0表示自动选择
		instance.Log.Level = "info"
		instance.Log.Output = "stdout"
		instance.Network.ReadTimeout = 30
		instance.Network.WriteTimeout = 10
		instance.Network.DialTimeout = 5
	})
	return instance
}

// LoadFromFile 从文件加载配置
func LoadFromFile(filePath string) error {
	// 确保目录存在
	dir := filepath.Dir(filePath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// 文件不存在，创建默认配置文件
		return SaveToFile(filePath)
	}

	// 读取文件内容
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return err
	}

	// 解析JSON
	mutex.Lock()
	defer mutex.Unlock()

	config := GetConfig()
	if err := json.Unmarshal(data, config); err != nil {
		return err
	}

	// 应用配置
	applyConfig()

	log.Info("Configuration loaded from %s", filePath)
	return nil
}

// SaveToFile 保存配置到文件
func SaveToFile(filePath string) error {
	mutex.RLock()
	config := GetConfig()
	mutex.RUnlock()

	// 序列化为JSON
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	// 写入文件
	if err := ioutil.WriteFile(filePath, data, 0644); err != nil {
		return err
	}

	log.Info("Configuration saved to %s", filePath)
	return nil
}

// SetServerPort 设置服务器端口
func SetServerPort(port int) {
	mutex.Lock()
	defer mutex.Unlock()

	config := GetConfig()
	config.Server.Port = port
}

// SetClientPort 设置客户端端口
func SetClientPort(port int) {
	mutex.Lock()
	defer mutex.Unlock()

	config := GetConfig()
	config.Client.Port = port
}

// SetServerAddr 设置服务器地址
func SetServerAddr(addr string) {
	mutex.Lock()
	defer mutex.Unlock()

	config := GetConfig()
	config.Client.ServerAddr = addr
}

// SetUsername 设置用户名
func SetUsername(username string) {
	mutex.Lock()
	defer mutex.Unlock()

	config := GetConfig()
	config.Client.Username = username
}

// SetLogLevel 设置日志级别
func SetLogLevel(level string) {
	mutex.Lock()
	defer mutex.Unlock()

	config := GetConfig()
	config.Log.Level = level

	// 应用日志级别
	applyLogLevel()
}

// applyConfig 应用配置
func applyConfig() {
	// 应用日志级别
	applyLogLevel()
}

// applyLogLevel 应用日志级别
func applyLogLevel() {
	config := GetConfig()
	switch config.Log.Level {
	case "debug":
		log.SetLevel(log.DEBUG)
	case "info":
		log.SetLevel(log.INFO)
	case "warn":
		log.SetLevel(log.WARN)
	case "error":
		log.SetLevel(log.ERROR)
	case "fatal":
		log.SetLevel(log.FATAL)
	default:
		log.SetLevel(log.INFO)
	}
}