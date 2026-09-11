package i18n

import (
	"os"
	"runtime"
	"strings"
)

// detectLocale 检测系统语言环境，返回 locale 字符串（"zh" 或 "en"）
func detectLocale() string {
	// 优先级: LANGUAGE > LC_ALL > LC_MESSAGES > LANG
	// 支持格式: zh_CN.UTF-8, zh, en_US.UTF-8, en 等
	envKeys := []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"}
	for _, key := range envKeys {
		val := os.Getenv(key)
		if val == "" {
			continue
		}
		// LANGUAGE 可能包含多语言列表，取第一个
		if idx := strings.IndexByte(val, ':'); idx >= 0 {
			val = val[:idx]
		}
		locale := parseLocale(val)
		if locale != "" {
			return locale
		}
	}

	// Windows 特殊检测
	if runtime.GOOS == "windows" {
		return detectWindowsLocale()
	}

	return "en"
}

// parseLocale 从 locale 字符串中提取语言代码
// "zh_CN.UTF-8" → "zh", "en_US.UTF-8" → "en", "C" → "en"
func parseLocale(val string) string {
	if val == "" || val == "C" || val == "POSIX" {
		return "en"
	}
	// 去掉编码后缀 (.UTF-8, .utf8, .GBK 等)
	if idx := strings.IndexByte(val, '.'); idx >= 0 {
		val = val[:idx]
	}
	// 取语言部分（下划线前）
	if idx := strings.IndexByte(val, '_'); idx >= 0 {
		val = val[:idx]
	}
	if idx := strings.IndexByte(val, '-'); idx >= 0 {
		val = val[:idx]
	}
	val = strings.ToLower(strings.TrimSpace(val))
	switch val {
	case "zh", "cmn":
		return "zh"
	case "en", "en_US", "en_GB":
		return "en"
	default:
		// 如果是空或者未知，返回空字符串让调用者跳过
		if val == "" {
			return ""
		}
		// 对于其他语言，目前回退到英文
		return "en"
	}
}

// detectWindowsLocale 在 Windows 上检测系统语言
func detectWindowsLocale() string {
	// 方法 1: 检查.windir 目录名是否包含中文路径
	// 方法 2: 检查 powershell Get-Culture（太慢，不用）
	// 方法 3: 检查系统 UI 语言环境变量
	uiLang := os.Getenv("USERPREFERENCEPAGE")
	if uiLang != "" {
		if strings.HasPrefix(uiLang, "0x0804") || strings.HasPrefix(uiLang, "0x0404") {
			return "zh"
		}
		return "en"
	}

	// 方法 4: 检查系统目录名称特征
	windir := os.Getenv("windir")
	if windir != "" {
		// Windows 目录名通常是英文，但 Program Files 可能本地化
		// 不可靠，跳过
	}

	// 回退到英文
	return "en"
}
