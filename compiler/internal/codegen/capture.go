package codegen

import (
	"sort"
	"strconv"
	"strings"

	"compiler/internal/ast"
	"compiler/internal/astutil"
	"compiler/internal/symbol"
)

// nestedFuncInfo 嵌套函数的捕获信息（用于生成调用点实参）
type nestedFuncInfo struct {
	// Captures 捕获变量名列表（按下标对应 _cap_N 参数）
	Captures []string
	// Types 捕获变量的 Kaula 类型（按下标对应）
	Types []string
}

// captureArgsFor 返回调用 funcName 时应注入的捕获实参列表（逗号分隔，不含括号）。
// 若 funcName 没有捕获信息（不是嵌套函数 / 无捕获），返回空串。
func (cg *CodeGenerator) captureArgsFor(funcName string) string {
	info, ok := cg.nestedFuncCaptures[funcName]
	if !ok || len(info.Captures) == 0 {
		return ""
	}
	var parts []string
	for _, name := range info.Captures {
		if cg.currentCaptureSet != nil {
			if idx, ok := cg.currentCaptureSet[name]; ok {
				// 当前函数已捕获同名外层变量：直接转发捕获指针
				parts = append(parts, "_cap_"+strconv.Itoa(idx))
				continue
			}
		}
		// 否则取地址（变量/参数/全局均可）
		parts = append(parts, "&"+name)
	}
	return strings.Join(parts, ", ")
}

// detectCaptures 分析嵌套函数 stmt 的引用：返回 stmt 引用的、但属于外层
// 函数作用域（非全局、非本函数参数/局部、非函数类型）的变量名列表（去重、排序）。
// 调用时机：已经 EnterScope 并注册参数之后。
func (cg *CodeGenerator) detectCaptures(stmt *ast.FunctionStatement) []string {
	collected := astutil.CollectIdentifiers(stmt.Body)
	// 本函数参数与局部声明均属遮蔽集
	locals := make(map[string]bool)
	for _, p := range stmt.Params {
		locals[p] = true
	}
	for name := range collected.Locals {
		locals[name] = true
	}

	var names []string
	for name := range collected.Refs {
		if locals[name] {
			continue
		}
		if cg.lookupOuterSymbol(name) != nil {
			names = append(names, name)
		}
	}
	// 稳定顺序：保证多次编译输出一致
	sort.Strings(names)
	return names
}

// lookupOuterSymbol 从当前作用域链向上查找符号；仅返回外层非全局、且非
// 函数/类型符号的变量符号。用于判定嵌套函数捕获目标。
func (cg *CodeGenerator) lookupOuterSymbol(name string) *symbol.Symbol {
	for sc := cg.currentScope; sc != nil; sc = sc.GetParent() {
		if sym := sc.GetLocalSymbol(name); sym != nil {
			if sc.GetScopeName() == "global" {
				return nil
			}
			if sym.Type == "function" || sym.Type == "type" {
				return nil
			}
			return sym
		}
	}
	return nil
}

// preRegisterNestedFuncs 预扫描语句中的嵌套函数定义并注册捕获信息，
// 使调用点在嵌套函数定义语句之前出现时也能正确生成捕获实参。
func (cg *CodeGenerator) preRegisterNestedFuncs(stmts []ast.Statement) {
	for _, s := range stmts {
		if fn, ok := s.(*ast.FunctionStatement); ok {
			if _, exists := cg.nestedFuncCaptures[fn.Name]; exists {
				continue
			}
			captures := cg.detectCaptures(fn)
			if len(captures) == 0 {
				continue
			}
			info := nestedFuncInfo{Captures: captures, Types: make([]string, 0, len(captures))}
			for _, name := range captures {
				sym := cg.lookupOuterSymbol(name)
				if sym != nil {
					info.Types = append(info.Types, sym.Type)
				} else {
					info.Types = append(info.Types, "")
				}
			}
			if cg.nestedFuncCaptures == nil {
				cg.nestedFuncCaptures = make(map[string]nestedFuncInfo)
			}
			cg.nestedFuncCaptures[fn.Name] = info
		}
	}
}