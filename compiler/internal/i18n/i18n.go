package i18n

import (
	"fmt"
	"sync"
)

// Locale 表示当前语言环境
type Locale string

const (
	LocaleEN Locale = "en"
	LocaleZH Locale = "zh"
)

// MsgKey 消息 key 类型
type MsgKey int

const (
	// 错误类型标签
	MsgSyntaxError MsgKey = iota
	MsgSemanticError
	MsgTypeError
	MsgRuntimeError
	MsgWarning
	MsgUnknownError

	// 摘要统计
	MsgNoErrors
	MsgFoundErrors
	MsgFoundWarnings
	MsgFoundErrorsAndWarnings
	MsgStatus
	MsgSuccess
	MsgFailed
	MsgWarningsCount
	MsgErrorsCount
	MsgOutput

	// Section headers
	MsgCompilationErrorsWarnings
	MsgLexParseErrors
	MsgSemanticAnalysisErrors
	MsgCodegenErrors
	MsgSorOwnershipErrors
	MsgWarningsHeader
	MsgCompilationResults
	MsgFrontendErrorsSkipped
	MsgError

	// Stage headers
	MsgStage1
	MsgStage1Completed
	MsgStage2
	MsgStage2Completed
	MsgStage25
	MsgStage25Completed
	MsgStage3
	MsgStage3aCompleted
	MsgStage3Completed
	MsgGeneratedCode
	MsgTimingBreakdown
	MsgTotalTime

	// 建议
	MsgSuggestion
	MsgSuggestionUnterminatedString
	MsgSuggestionUnexpectedToken
	MsgSuggestionFuncNameExists
	MsgSuggestionPrefixNameExists
	MsgSuggestionObjectType
	MsgSuggestionObjectName
	MsgSuggestionSpendExpression
	MsgSuggestionSpendCall
	MsgSuggestionPrefixName
	MsgSuggestionCheckSyntax

	// 高频错误消息（parser/sema 通用前缀/模板）
	MsgUnexpectedChar
	MsgUnterminatedString
	MsgUnterminatedCharLit
	MsgExpectedGot
	MsgUndefinedVar
	MsgUndefinedFunc
	MsgDuplicateParam
	MsgConstNeedInit
	MsgAutoNeedInit
	MsgAutoDeductionFailed
	MsgImportMissingModule
	MsgImportEmptyPath
	MsgImportNotExist
	MsgDivByZero
	MsgOperatorMismatch
	MsgNullableUnassigned
	MsgReturnTypeMismatch
	MsgFuncNotExported
	MsgModuleNotImported
	MsgTypeMismatch
	MsgIfConditionNotBool
	MsgIntegerOutOfRange
	MsgUnusedFunction
	MsgUnusedVariable
	MsgUnusedParameter
	MsgVarUninitialized
	MsgShouldUseMatch
	MsgVarShadows
	MsgFloatPrecision
	MsgSelfComparison
	MsgVarAssignedNotRead
)

// locale 存储当前语言环境
var (
	currentLocale Locale
	initialized  bool
	once         sync.Once
)

// Init 初始化 i18n 系统，自动检测系统语言
func Init() {
	once.Do(func() {
		locale := detectLocale()
		currentLocale = Locale(locale)
		initialized = true
	})
}

// SetLocale 手动设置语言环境（用于测试或 --lang 参数）
func SetLocale(locale string) {
	switch Locale(locale) {
	case LocaleEN, LocaleZH:
		currentLocale = Locale(locale)
	default:
		currentLocale = LocaleEN
	}
}

// GetLocale 返回当前语言环境
func GetLocale() Locale {
	if currentLocale == "" {
		Init()
	}
	return currentLocale
}

// T 返回当前语言环境下 key 对应的消息，支持 fmt.Sprintf 格式化
func T(key MsgKey, args ...any) string {
	if currentLocale == "" {
		Init()
	}
	msg := getMessage(key, currentLocale)
	if len(args) > 0 {
		return fmt.Sprintf(msg, args...)
	}
	return msg
}

// getMessage 从消息目录获取翻译
func getMessage(key MsgKey, locale Locale) string {
	if catalog, ok := catalogs[locale]; ok {
		if msg, ok := catalog[key]; ok {
			return msg
		}
	}
	// 回退到英文
	if catalog, ok := catalogs[LocaleEN]; ok {
		if msg, ok := catalog[key]; ok {
			return msg
		}
	}
	return fmt.Sprintf("[missing:%d]", int(key))
}

// catalogs 消息目录
var catalogs = map[Locale]map[MsgKey]string{
	LocaleEN: enCatalog,
	LocaleZH: zhCatalog,
}
