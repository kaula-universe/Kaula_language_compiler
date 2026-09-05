package i18n

import (
	"fmt"
	"strings"
)

// messageMapping 定义消息模式到翻译 key 的映射
// 键: 原始消息的子串匹配模式（英文或中文）
// 值: 翻译 key + 参数提取函数
type messageMapping struct {
	// substr 是消息中必须包含的子串
	substr string
	// key 是对应的翻译 key
	key MsgKey
	// extractArgs 从原始消息中提取参数
	extractArgs func(string) []any
}

// messageMappings 消息翻译映射表
// 按优先级排列，先匹配先生效
var messageMappings = []messageMapping{
	// 除零错误
	{substr: "除数不能为 0", key: MsgDivByZero, extractArgs: func(msg string) []any {
		if i := strings.Index(msg, "运算符 '"); i >= 0 {
			j := strings.Index(msg[i+5:], "'")
			if j >= 0 {
				return []any{msg[i+5 : i+5+j]}
			}
		}
		return []any{"?"}
	}},
	{substr: "divisor of operator", key: MsgDivByZero, extractArgs: func(msg string) []any {
		if i := strings.Index(msg, "operator '"); i >= 0 {
			j := strings.Index(msg[i+10:], "'")
			if j >= 0 {
				return []any{msg[i+10 : i+10+j]}
			}
		}
		return []any{"?"}
	}},

	// 未定义变量
	{substr: "未定义变量: ", key: MsgUndefinedVar, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},
	{substr: "undefined variable: ", key: MsgUndefinedVar, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},

	// 未定义函数
	{substr: "未定义函数: ", key: MsgUndefinedFunc, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},
	{substr: "undefined function: ", key: MsgUndefinedFunc, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},

	// 未使用的函数
	{substr: "未使用的函数: ", key: MsgUnusedFunction, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},
	{substr: "unused function: ", key: MsgUnusedFunction, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},

	// 未使用的变量
	{substr: "未使用的变量: ", key: MsgUnusedVariable, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},
	{substr: "unused variable: ", key: MsgUnusedVariable, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},

	// 未使用的参数
	{substr: "未使用的参数: ", key: MsgUnusedParameter, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},
	{substr: "unused parameter: ", key: MsgUnusedParameter, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},

	// const 声明必须有初始值
	{substr: "const 声明", key: MsgConstNeedInit, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},
	{substr: "const declaration", key: MsgConstNeedInit, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},

	// auto 声明必须有初始值
	{substr: "auto 声明必须有初始值", key: MsgAutoNeedInit, extractArgs: func(msg string) []any {
		return nil
	}},
	{substr: "auto declaration must have", key: MsgAutoNeedInit, extractArgs: func(msg string) []any {
		return nil
	}},

	// 无法推导类型
	{substr: "无法推导", key: MsgAutoDeductionFailed, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},
	{substr: "cannot deduce type", key: MsgAutoDeductionFailed, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},

	// 导入模块不存在
	{substr: "导入的模块不存在", key: MsgImportMissingModule, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},
	{substr: "imported module does not exist", key: MsgImportMissingModule, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},

	// import 路径为空
	{substr: "import 路径为空", key: MsgImportEmptyPath, extractArgs: func(msg string) []any {
		return nil
	}},
	{substr: "import path is empty", key: MsgImportEmptyPath, extractArgs: func(msg string) []any {
		return nil
	}},

	// 导入的文件或库不存在
	{substr: "导入的文件或库不存在", key: MsgImportNotExist, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},
	{substr: "imported file or library does not exist", key: MsgImportNotExist, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},

	// 函数未导出
	{substr: "未通过 pub 导出", key: MsgFuncNotExported, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.Index(msg[start+1:], "'")
		if start >= 0 && end >= 0 {
			return []any{msg[start+1 : start+1+end]}
		}
		return []any{msg}
	}},
	{substr: "is not exported with 'pub'", key: MsgFuncNotExported, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.Index(msg[start+1:], "'")
		if start >= 0 && end >= 0 {
			return []any{msg[start+1 : start+1+end]}
		}
		return []any{msg}
	}},

	// 模块未导入
	{substr: "未导入, 请添加", key: MsgModuleNotImported, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.Index(msg[start+1:], "'")
		if start >= 0 && end >= 0 {
			return []any{msg[start+1 : start+1+end]}
		}
		return []any{msg}
	}},
	{substr: "not imported, add", key: MsgModuleNotImported, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.Index(msg[start+1:], "'")
		if start >= 0 && end >= 0 {
			return []any{msg[start+1 : start+1+end]}
		}
		return []any{msg}
	}},

	// 变量声明未初始化
	{substr: "声明未初始化", key: MsgVarUninitialized, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},
	{substr: "declared but not initialized", key: MsgVarUninitialized, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},

	// 建议改用 match
	{substr: "建议改用 match", key: MsgShouldUseMatch, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},
	{substr: "consider using match", key: MsgShouldUseMatch, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},

	// 变量遮蔽
	{substr: "遮蔽了外层", key: MsgVarShadows, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},
	{substr: "shadows an outer", key: MsgVarShadows, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},

	// 变量赋值但未读取
	{substr: "被赋值但从未被读取", key: MsgVarAssignedNotRead, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},
	{substr: "is assigned but never read", key: MsgVarAssignedNotRead, extractArgs: func(msg string) []any {
		start := strings.Index(msg, "'")
		end := strings.LastIndex(msg, "'")
		if start >= 0 && end > start {
			return []any{msg[start+1 : end]}
		}
		return []any{msg}
	}},

	// 运算符不兼容
	{substr: "运算符 '", key: MsgOperatorMismatch, extractArgs: func(msg string) []any {
		// 提取三个类型参数
		var args []any
		rest := msg
		for i := 0; i < 3; i++ {
			start := strings.Index(rest, "'")
			if start < 0 {
				args = append(args, "?")
				continue
			}
			end := strings.Index(rest[start+1:], "'")
			if end < 0 {
				args = append(args, "?")
				continue
			}
			args = append(args, rest[start+1:start+1+end])
			rest = rest[start+1+end+1:]
		}
		return args
	}},
	{substr: "cannot be applied to types", key: MsgOperatorMismatch, extractArgs: func(msg string) []any {
		var args []any
		rest := msg
		for i := 0; i < 3; i++ {
			start := strings.Index(rest, "'")
			if start < 0 {
				args = append(args, "?")
				continue
			}
			end := strings.Index(rest[start+1:], "'")
			if end < 0 {
				args = append(args, "?")
				continue
			}
			args = append(args, rest[start+1:start+1+end])
			rest = rest[start+1+end+1:]
		}
		return args
	}},
}

// TranslateMessage 尝试翻译错误消息
// 如果消息匹配已知模式且当前语言为中文/英文，则返回翻译后的消息
// 如果不匹配或已是目标语言，返回原始消息
func TranslateMessage(msg string) string {
	if !initialized {
		return msg
	}
	locale := GetLocale()
	// 如果当前是英文，且消息是中文，翻译为英文
	// 如果当前是中文，且消息是英文，翻译为中文
	// 简单判断：包含中文字符视为中文消息
	msgIsChinese := containsChinese(msg)

	for _, m := range messageMappings {
		if strings.Contains(msg, m.substr) {
			args := m.extractArgs(msg)
			result := T(m.key, args...)

			// 根据当前 locale 和消息语言决定是否翻译
			if locale == LocaleZH && !msgIsChinese {
				return result
			}
			if locale == LocaleEN && msgIsChinese {
				return result
			}
			// 如果已经是目标语言，返回原始消息
			break
		}
	}
	return msg
}

// containsChinese 检查字符串是否包含中文字符
func containsChinese(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

// FormatMessage 格式化消息（支持参数替换）
func FormatMessage(key MsgKey, args ...any) string {
	return fmt.Sprintf(T(key), args...)
}
