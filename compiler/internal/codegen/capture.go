package codegen

import (
	"sort"
	"strconv"
	"strings"

	"kaula/internal/ast"
	"kaula/internal/astutil"
	"kaula/internal/symbol"
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

// detectCaptures 分析嵌套函数 stmt 的引用：
//   - captures: stmt 直接引用的、属于外层函数作用域的变量名（去重、排序）
//   - calls: stmt 体内调用的、同在本层作用域的嵌套函数名（用于传递闭包计算）
//
// 调用时机：已经 EnterScope 并注册参数之后。
// nestedFuncNames 为当前层已知的嵌套函数名集合（用于区分函数调用与变量引用）。
func (cg *CodeGenerator) detectCaptures(stmt *ast.FunctionStatement, nestedFuncNames map[string]bool) ([]string, []string) {
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
	var calls []string
	for name := range collected.Refs {
		if locals[name] {
			continue
		}
		// 判断是否是嵌套函数调用：
		// 1. 在当前层嵌套函数名集合中（同层尚未注册的）
		// 2. 或已注册到 nestedFuncCaptures（同层或外层已处理的嵌套函数）
		isNestedCall := false
		if nestedFuncNames != nil && nestedFuncNames[name] {
			isNestedCall = true
		} else if cg.nestedFuncCaptures != nil {
			if _, ok := cg.nestedFuncCaptures[name]; ok {
				isNestedCall = true
			}
		}
		if isNestedCall {
			calls = append(calls, name)
			continue
		}
		if cg.lookupOuterSymbol(name) != nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	sort.Strings(calls)
	return names, calls
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

// collectNestedFuncNames 扫描语句列表，收集所有嵌套函数定义的名称。
func collectNestedFuncNames(stmts []ast.Statement) map[string]bool {
	names := make(map[string]bool)
	for _, s := range stmts {
		if fn, ok := s.(*ast.FunctionStatement); ok {
			names[fn.Name] = true
		}
	}
	return names
}

// expandCaptureClosure 计算捕获的传递闭包：
// 若 A 调用 B，则 A 的捕获 = A 的直接捕获 ∪ B 的捕获（迭代至不动点）。
// directCaptures 和 directCalls 以函数名为 key。
func expandCaptureClosure(directCaptures map[string][]string, directCalls map[string][]string) map[string][]string {
	result := make(map[string][]string, len(directCaptures))
	for k, v := range directCaptures {
		cp := make([]string, len(v))
		copy(cp, v)
		result[k] = cp
	}
	changed := true
	for changed {
		changed = false
		for name := range result {
			calls := directCalls[name]
			for _, called := range calls {
				if calledCaps, ok := result[called]; ok {
					for _, cap := range calledCaps {
						if !containsStr(result[name], cap) {
							result[name] = append(result[name], cap)
							changed = true
						}
					}
				}
			}
		}
	}
	for k := range result {
		sort.Strings(result[k])
	}
	return result
}

// buildNestedFuncInfo 根据捕获变量名列表构建 nestedFuncInfo（含类型查询）。
func (cg *CodeGenerator) buildNestedFuncInfo(captures []string) nestedFuncInfo {
	info := nestedFuncInfo{Captures: captures, Types: make([]string, 0, len(captures))}
	for _, name := range captures {
		sym := cg.lookupOuterSymbol(name)
		if sym != nil {
			info.Types = append(info.Types, sym.Type)
		} else {
			info.Types = append(info.Types, "")
		}
	}
	return info
}

// preRegisterNestedFuncs 预扫描语句中的嵌套函数定义并注册捕获信息，
// 使调用点在嵌套函数定义语句之前出现时也能正确生成捕获实参。
// 捕获列表包含传递闭包：A 调用 B 时，A 自动捕获 B 所需的外层变量，
// 以便在 A 体内调用 B 时转发捕获指针。
func (cg *CodeGenerator) preRegisterNestedFuncs(stmts []ast.Statement) {
	nestedFuncNames := collectNestedFuncNames(stmts)

	directCaptures := make(map[string][]string)
	directCalls := make(map[string][]string)
	for _, s := range stmts {
		if fn, ok := s.(*ast.FunctionStatement); ok {
			if _, exists := cg.nestedFuncCaptures[fn.Name]; exists {
				continue
			}
			captures, calls := cg.detectCaptures(fn, nestedFuncNames)
			directCaptures[fn.Name] = captures
			directCalls[fn.Name] = calls
		}
	}

	closure := expandCaptureClosure(directCaptures, directCalls)

	for name, captures := range closure {
		if len(captures) == 0 {
			continue
		}
		if cg.nestedFuncCaptures == nil {
			cg.nestedFuncCaptures = make(map[string]nestedFuncInfo)
		}
		cg.nestedFuncCaptures[name] = cg.buildNestedFuncInfo(captures)
	}
}

// containsStr 判断字符串切片是否包含指定字符串。
func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
