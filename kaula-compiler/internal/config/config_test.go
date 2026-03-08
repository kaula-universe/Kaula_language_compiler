package config

import (
	"os"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.TemplatePath != "templates" {
		t.Errorf("Expected TemplatePath to be 'templates', got %s", cfg.TemplatePath)
	}
	if cfg.IncludePath != "../std" {
		t.Errorf("Expected IncludePath to be '../std', got %s", cfg.IncludePath)
	}
	if cfg.VOCacheSize != 2048 {
		t.Errorf("Expected VOCacheSize to be 2048, got %d", cfg.VOCacheSize)
	}
	if cfg.QueueSize != 100 {
		t.Errorf("Expected QueueSize to be 100, got %d", cfg.QueueSize)
	}
	if cfg.SpendableSize != 10 {
		t.Errorf("Expected SpendableSize to be 10, got %d", cfg.SpendableSize)
	}
	if cfg.TargetLanguage != "c" {
		t.Errorf("Expected TargetLanguage to be 'c', got %s", cfg.TargetLanguage)
	}
}

func TestLoadConfig(t *testing.T) {
	// 创建临时配置文件
	configContent := `{
		"template_path": "test_templates",
		"include_path": "test_std",
		"vo_cache_size": 4096,
		"queue_size": 200,
		"spendable_size": 20,
		"target_language": "c"
	}`

	err := os.WriteFile("kaula.json", []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}
	defer os.Remove("kaula.json")

	// 加载配置
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// 验证配置 - 路径会被转换为绝对路径，所以我们只需要检查路径的最后部分
	if !strings.HasSuffix(cfg.TemplatePath, "test_templates") {
		t.Errorf("Expected TemplatePath to end with 'test_templates', got %s", cfg.TemplatePath)
	}
	if !strings.HasSuffix(cfg.IncludePath, "test_std") {
		t.Errorf("Expected IncludePath to end with 'test_std', got %s", cfg.IncludePath)
	}
	if cfg.VOCacheSize != 4096 {
		t.Errorf("Expected VOCacheSize to be 4096, got %d", cfg.VOCacheSize)
	}
	if cfg.QueueSize != 200 {
		t.Errorf("Expected QueueSize to be 200, got %d", cfg.QueueSize)
	}
	if cfg.SpendableSize != 20 {
		t.Errorf("Expected SpendableSize to be 20, got %d", cfg.SpendableSize)
	}
	if cfg.TargetLanguage != "c" {
		t.Errorf("Expected TargetLanguage to be 'c', got %s", cfg.TargetLanguage)
	}
}
