package config

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
)

// Config 表示编译器的配置
type Config struct {
	// 基础路径
	BasePath string `json:"base_path"`
	// 模板路径
	TemplatePath string `json:"template_path"`
	// 包含路径
	IncludePath string `json:"include_path"`
	// VO 缓存大小
	VOCacheSize int `json:"vo_cache_size"`
	// 队列大小
	QueueSize int `json:"queue_size"`
	// 可花费组件大小
	SpendableSize int `json:"spendable_size"`
	// 目标语言
	TargetLanguage string `json:"target_language"`
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	// 获取当前工作目录作为默认BasePath
	basePath, _ := os.Getwd()
	return &Config{
		BasePath:        basePath,
		TemplatePath:    "templates",
		IncludePath:     "../std",
		VOCacheSize:     2048,
		QueueSize:       100,
		SpendableSize:   10,
		TargetLanguage:  "c",
	}
}

// LoadConfig 从配置文件和命令行参数加载配置
func LoadConfig() (*Config, error) {
	// 加载默认配置
	config := DefaultConfig()

	// 从配置文件加载
	configFile := "kaula.json"
	if _, err := os.Stat(configFile); err == nil {
		data, err := os.ReadFile(configFile)
		if err == nil {
			if err := json.Unmarshal(data, config); err != nil {
				// 配置文件解析失败，使用默认配置
			}
		}
	}

	// 命令行参数覆盖
	templatePath := flag.String("template", config.TemplatePath, "模板路径")
	includePath := flag.String("include", config.IncludePath, "包含路径")
	voCacheSize := flag.Int("vo-cache", config.VOCacheSize, "VO 缓存大小")
	queueSize := flag.Int("queue", config.QueueSize, "队列大小")
	spendableSize := flag.Int("spendable", config.SpendableSize, "可花费组件大小")
	targetLanguage := flag.String("target", config.TargetLanguage, "目标语言")

	flag.Parse()

	// 更新配置
	config.TemplatePath = *templatePath
	config.IncludePath = *includePath
	config.VOCacheSize = *voCacheSize
	config.QueueSize = *queueSize
	config.SpendableSize = *spendableSize
	config.TargetLanguage = *targetLanguage

	// 确保路径是绝对路径
	if !filepath.IsAbs(config.BasePath) {
		absPath, err := filepath.Abs(config.BasePath)
		if err == nil {
			config.BasePath = absPath
		}
	}

	if !filepath.IsAbs(config.TemplatePath) {
		absPath, err := filepath.Abs(config.TemplatePath)
		if err == nil {
			config.TemplatePath = absPath
		}
	}

	if !filepath.IsAbs(config.IncludePath) {
		absPath, err := filepath.Abs(config.IncludePath)
		if err == nil {
			config.IncludePath = absPath
		}
	}

	return config, nil
}
