package stdlib

import (
	"fmt"
	"strings"
)

// cppTypeCompatible 判断桥接函数的类型是否可被 C 侧表达
func cppTypeCompatible(t string) bool {
	conv, _, _ := cppExportType(t)
	return conv != ""
}

// bridgeArgInfo 记录一个参数如何从 C 侧类型转换为 C++ 调用实参
type bridgeArgInfo struct {
	CType    string // C 导出签名里的类型（写入 JSON/头文件）
	CallExpr string // 调用表达式（p0 -> *p0 / ((T*)p0) / p0）
	RealType string // C++ 原始类型字符串（用于还原）
}

// stripCV 去掉前导 const/volatile 修饰
func stripCV(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "const ")
	s = strings.TrimPrefix(s, "volatile ")
	return strings.TrimSpace(s)
}

// cppExportType 把 C++ 类型转成 C 侧可命名、Kaula 可调用的导出类型。
// 通用化规则（取代"仅基础类型/基础类型指针"的硬限制）：
//   - T& / T&&        -> T* 或 void*（stub 内 *p 还原；未知类型 cast 后解引用）
//   - 基础类型指针     -> 原样保留（float*、const char*、size_t* ...）
//   - 其它指针/引用    -> void*（C 侧不命名，stub 内 (T*) 还原，避开头文件 typedef 冲突）
//   - 含 ::、<> 的指针 -> void*
//   - 非基础值类型     -> 不可导出
func cppExportType(t string) (conv string, base string, voidStar bool) {
	t = strings.TrimSpace(t)
	if t == "" {
		return "", "", false
	}
	if strings.ContainsAny(t, "()[]=") {
		return "", "", false
	}
	ref := strings.HasSuffix(t, "&&") || strings.HasSuffix(t, "&")
	if ref {
		if strings.HasSuffix(t, "&&") {
			t = strings.TrimSpace(strings.TrimSuffix(t, "&&"))
		} else {
			t = strings.TrimSpace(strings.TrimSuffix(t, "&"))
		}
	}
	isPtr := ref || strings.HasSuffix(t, "*")
	base = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(t), "*"))
	base = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(base), " const"))
	baseCV := stripCV(base)

	if !isPtr {
		if cPrimitiveTypes[baseCV] || baseCV == "void" {
			return t, baseCV, false
		}
		return "", base, false
	}
	// 指针/引用：base 必须是单纯标识符；模板/匿名命名空间映射 void*
	if strings.ContainsAny(base, "<>()") {
		return "void*", base, true
	}
	if strings.Contains(baseCV, "::") {
		return "void*", base, true
	}
	if ref {
		// 引用 -> 指针导出（保留 const 语义）
		if cPrimitiveTypes[baseCV] {
			return base + " *", base, false
		}
		return "void*", base, true
	}
	// 只原生导出编译器熟知的基础类型指针（float*、const char*、size_t* 等）；
	// 其它类型 C 侧不命名，统一 void* 导出 + stub 内 (T*) 还原，
	// 避免与真实头文件里的 typedef/匿名结构体冲突（FILE、ImGuiWindow 等）
	if cPrimitiveTypes[baseCV] {
		return t, base, false
	}
	return "void*", base, true
}

// buildBridgeArg 为参数构造导出类型与调用转换表达式（pN 为 C 参数名）
func buildBridgeArg(argType string, p string) *bridgeArgInfo {
	conv, base, voidStar := cppExportType(argType)
	if conv == "" {
		return nil
	}
	info := &bridgeArgInfo{CType: conv, CallExpr: p, RealType: argType}
	isRef := strings.HasSuffix(strings.TrimSpace(argType), "&") ||
		strings.HasSuffix(strings.TrimSpace(argType), "&&")
	if voidStar {
		// 无法在头文件里命名：void* 导出，stub 内 (T*) 还原（引用还需再解引用）
		if isRef {
			info.CallExpr = fmt.Sprintf("(*((%s*)%s))", base, p)
		} else {
			info.CallExpr = fmt.Sprintf("((%s*)%s)", base, p)
		}
		return info
	}
	if isRef {
		// 引用实参：传给 C++ 需解引用
		info.CallExpr = fmt.Sprintf("*%s", p)
	}
	return info
}