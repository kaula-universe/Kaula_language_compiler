package stdlib

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// abiTagRe 匹配 llvm-cxxfilt 反修饰输出里的 ABI 标签（[abi:v160004]）
var abiTagRe = regexp.MustCompile(`\[[^\]]*\]`)

// CppBridgeResult 描述一次 C++ 头文件 extern "C" 桥接生成的结果
type CppBridgeResult struct {
	Header      string // 相对库目录的 C 兼容头文件（如 imgui/imgui_kbridge.h）
	Source      string // 相对库目录的 .cpp 桥接源文件
	LibName     string
	BridgeFuncs map[string]Function // 桥接后的 C 函数表（key=导出名，直接写入 JSON functions）
}

// cPrimitiveTypes 判断裸类型是否为 C 可表达的基础类型
var cPrimitiveTypes = map[string]bool{
	"char": true, "signed char": true, "unsigned char": true,
	"short": true, "unsigned short": true,
	"int": true, "signed int": true, "unsigned int": true,
	"long": true, "signed long": true, "unsigned long": true,
	"long long": true, "unsigned long long": true,
	"float": true, "double": true, "long double": true,
	"bool": true, "int8_t": true, "uint8_t": true,
	"int16_t": true, "uint16_t": true, "int32_t": true, "uint32_t": true,
	"int64_t": true, "uint64_t": true, "size_t": true, "ssize_t": true,
	"intptr_t": true, "uintptr_t": true, "wchar_t": true, "char16_t": true,
}

// bridgeEligible 判断函数是否适合生成桥接（全参数可表达且无变参）
func bridgeEligible(fn *Function) bool {
	if fn.VarArgs {
		return false
	}
	if fn.Return != "" && !cppTypeCompatible(fn.Return) {
		if fn.Return != "void" {
			return false
		}
	}
	for _, a := range fn.Args {
		if !cppTypeCompatible(a) {
			return false
		}
	}
	return true
}

// demangleQualifiedName 从 C++ 符号反修饰中提取限定函数名（形如 ImGui::Text）
// 解析失败返回空。
// 优先处理 MSVC 风格（?addInt@demo@@YAHHH@Z），回退 llvm-cxxfilt 的 Itanium 反修饰。
func demangleQualifiedName(mangled string) string {
	if mangled == "" {
		return ""
	}
	if strings.HasPrefix(mangled, "?") {
		if q := undecorateMSVC(mangled); q != "" {
			return q
		}
	}
	dem := Demangle(mangled)
	if dem == "" {
		return ""
	}
	// 去掉参数列表：以第一个 '(' 为界（函数名内不含 '('，除非函数指针参数，但那些已被过滤）
	if idx := strings.Index(dem, "("); idx >= 0 {
		dem = dem[:idx]
	}
	dem = strings.TrimSpace(dem)
	if dem == "" {
		return ""
	}
	// 去掉 ABI 标签（Itanium 的 [abi:v160004] 等），否则会生成非法的 C 标识符
	dem = abiTagRe.ReplaceAllString(dem, "")
	dem = strings.TrimSpace(dem)
	if dem == "" {
		return ""
	}
	return dem
}

// undecorateMSVC 解析 MSVC 风格的函数修饰名（限制在无模板的普通自由函数）：
//
//	全局：     ?name@@Y...@Z
//	命名空间： ?name@ns1@ns2@@Y...@Z
//
// 解析成功返回 "ns::name"，无法解析返回空。
func undecorateMSVC(mangled string) string {
	if !strings.HasPrefix(mangled, "?") {
		return ""
	}
	body := mangled[1:]
	at := strings.Index(body, "@")
	if at < 0 {
		return ""
	}
	base := body[:at]
	if base == "" || strings.ContainsAny(base, "$.<>") {
		return "" // 模板/匿名/操作符不处理
	}
	rest := body[at+1:]
	// 定位首个 "@@"（连接块前面），若没有则认为全局函数（rest 直接以 @@ 开头）
	doubleIdx := strings.Index(rest, "@@")
	var segment string
	if doubleIdx >= 0 {
		segment = rest[:doubleIdx]
	} else {
		segment = rest
	}
	// segment 是 "ns1@ns2"，按 @ 分割，顺序从内向外，需要反转
	var parts []string
	if segment != "" {
		for _, p := range strings.Split(segment, "@") {
			if p != "" {
				parts = append(parts, p)
			}
		}
	}
	if len(parts) == 0 {
		return base // 全局函数
	}
	// 反转：parts=[ns2, ns1] -> namespace 顺序 ns1::ns2
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, "::") + "::" + base
}

// validCIdentifier 判断字符串是否为合法的 C 标识符（导出名必须可用）
func validCIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' ||
			(i > 0 && c >= '0' && c <= '9')
		if !ok {
			return false
		}
	}
	return true
}

// nsFromQualified 从限定名中提取命名空间前缀（不含函数名；全局函数返回 ""）
func nsFromQualified(qualified string) string {
	if idx := strings.LastIndex(qualified, "::"); idx >= 0 {
		ns := qualified[:idx]
		if ns == "std" || strings.HasPrefix(ns, "(") {
			return ""
		}
		return ns
	}
	return ""
}

// bridgeExportName 生成桥接导出名（确定性、可预测）：
// 规则：kbridge_<原始函数名>。
func bridgeExportName(name string) string {
	return "kbridge_" + name
}

// GenCppBridge 为需要 C++ 模式才能解析的头文件生成 extern "C" 桥接。
// 一个库可能同时有多个 C++ 头（如 imgui 的 imgui.h 与 imgui_internal.h），
// headerRefs 收齐全部引用，cppFuncs 为合并后的完整函数表（含 extern "C" 函数）。
// 返回的 BridgeFuncs 可作为 JSON functions 字段写入。
//
// 通用化（不限定"基础类型参数"子集）：
//   - 引用参数 T& → T*，stub 内 *p 还原；引用返回 T& → T*
//   - 未知/模板类型指针 → void*，stub 内 (T*) 还原
//   - 简单类型指针（float*、const char*）原样导出；其余指针/引用导出为 void*，stub 内 (T*) 还原
//   - 同名重载保留"最友好"的一个签名（其余丢弃），不再整族跳过
func GenCppBridge(pkgDir, libName string, headerRefs []string, cppFuncs map[string]Function) (*CppBridgeResult, error) {
	type cand struct {
		key        string
		fn         Function
		qualified  string
		argInfos   []*bridgeArgInfo
		retIsRef   bool
		retVoid    bool
		retConv    string
		score      int
	}
	// 导出名 -> 调用解析（C++ 调用表达式）
	// 分类原则：
	//  - 原生 extern "C" 函数（mangledName==name 或未记录 mangled）：原样导出，无需桥接体
	//  - C++ 函数（命名空间内或全局）：统一加 kbridge_ 前缀导出，避免与平台/
	//    标准库 C 符号冲突（如 windows.h 的 GetVersion）
	exportDecls := make(map[string]Function) // 导出名 -> 签名（写入 JSON）
	cppCalls := make(map[string]string)      // 导出名 -> C++ 调用表达式（仅需桥接体的函数）
	argInfoMap := make(map[string][]*bridgeArgInfo)
	retIsRefMap := make(map[string]bool)
	retVoidMap := make(map[string]bool)
	var callableNames []string // 无需桥接的直接名
	sortable := make([]string, 0, len(cppFuncs))
	for n := range cppFuncs {
		sortable = append(sortable, n)
	}
	sort.Strings(sortable)

	// 重载处理：按基础名分组，每族只留"最友好"（转换最少、参数最少的）签名
	termWinners := make(map[string]*cand)

	for _, n := range sortable {
		fn := cppFuncs[n]
		if fn.MangledName == "" || fn.MangledName == n {
			// extern "C" 或 C 风格函数：直接可用
			callableNames = append(callableNames, n)
			exportDecls[n] = fn
			continue
		}
		// C++ 函数：尝试桥接
		if !bridgeEligible(&fn) {
			continue
		}
		qualified := demangleQualifiedName(fn.MangledName)
		if os.Getenv("KDEBUG") != "" && strings.Contains(qualified, "GetVersion") {
			fmt.Printf("KDEBUG: qualified=%q eligible=%v varargs=%v args=%v ret=%q retOK=%v\n",
				qualified, bridgeEligible(&fn), fn.VarArgs, fn.Args, fn.Return,
				cppTypeCompatible(fn.Return))
		}
		if qualified == "" {
			continue
		}
		term := qualified
		if idx := strings.LastIndex(qualified, "::"); idx >= 0 {
			term = qualified[idx+2:]
		}
		if term == "" || !validCIdentifier(term) {
			// operator 重载（operator!=、operator[] 等）与含非法字符的名字无法导出
			continue
		}
		// 逐参数构造导出类型与调用转换；任一参数不可表达则放弃整个函数
		argsOk := true
		var argInfos []*bridgeArgInfo
		score := 0
		for i, a := range fn.Args {
			info := buildBridgeArg(a, fmt.Sprintf("p%d", i))
			if info == nil {
				argsOk = false
				break
			}
			argInfos = append(argInfos, info)
			switch {
			case info.CallExpr == fmt.Sprintf("p%d", i) && !strings.ContainsAny(info.RealType, "&"):
				score += 2 // 零转换最友好
			case strings.HasSuffix(strings.TrimSpace(info.RealType), "*"):
				score += 1
			}
			if info.CType == "void*" {
				score -= 1
			}
		}
		if !argsOk {
			continue
		}
		// 返回类型转换
		retConv, retBase, retVoid := cppExportType(fn.Return)
		if fn.Return != "" && fn.Return != "void" && retConv == "" {
			continue
		}
		retIsRef := strings.HasSuffix(strings.TrimSpace(fn.Return), "&") ||
			strings.HasSuffix(strings.TrimSpace(fn.Return), "&&")
if retIsRef {
			// 引用返回：base 为基础类型才导出其指针（如 float *，stub return &(...)），
			// 否则 void* 兜底（C 侧无名，stub (void*)&(...)）
			if cPrimitiveTypes[stripCV(retBase)] {
				retConv = strings.TrimSpace(retBase) + " *"
				retVoid = false
			} else {
				retConv = "void*"
				retVoid = true
			}
		}

		cur := termWinners[term]
		if cur == nil || score > cur.score ||
			(score == cur.score && len(argInfos) < len(cur.argInfos)) {
			termWinners[term] = &cand{
				key: n, fn: fn, qualified: qualified,
				argInfos: argInfos, retIsRef: retIsRef, retVoid: retVoid, retConv: retConv,
				score: score,
			}
		}
	}

	for term, c := range termWinners {
		ns := nsFromQualified(c.qualified)
		// 所有 C++ 函数一律加 kbridge_ 前缀导出：
		// 导出名落在全局 C 命名空间，若用原名会与平台头冲突
		export := bridgeExportName(term)
		if _, exists := exportDecls[export]; exists {
			continue // 同名 extern "C" 函数冲突时，跳过 C++ 版本
		}
		call := c.qualified
		if ns != "" {
			call = "::" + c.qualified
		}
		bFn := c.fn
		bFn.MangledName = "" // 导出的 C 符号不再关联 C++ 修饰名
		bFn.Args = make([]string, len(c.argInfos))
		for i, info := range c.argInfos {
			bFn.Args[i] = info.CType
		}
		bFn.Return = c.retConv
		exportDecls[export] = bFn
		cppCalls[export] = call
		argInfoMap[export] = c.argInfos
		retIsRefMap[export] = c.retIsRef
		retVoidMap[export] = c.retVoid
	}

	if len(exportDecls) == 0 {
		return nil, fmt.Errorf("no bridgeable C++ functions for %s", libName)
	}

	hName := libName + "_kbridge.h"
	cpp := libName + "_kbridge.cpp"

	// ------ 生成 C 兼容桥接头 ------
	var hb strings.Builder
	guard := strings.ToUpper(libName) + "_KBRIDGE_H"
	hb.WriteString("/* auto-generated extern \"C\" bridge header for ")
	hb.WriteString(libName)
	hb.WriteString(" — do not edit */\n#ifndef ")
	hb.WriteString(guard)
	hb.WriteString("\n#define ")
	hb.WriteString(guard)
	hb.WriteString("\n\n#ifdef __cplusplus\nextern \"C\" {\n#endif\n\n")
	// 不透明类型前置声明：C 侧只有声明（实际定义由 C++ 头提供）
	keys := make([]string, 0, len(exportDecls))
	for k := range exportDecls {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fn := exportDecls[k]
		hb.WriteString("extern ")
		hb.WriteString(declareSignature(k, fn))
		hb.WriteString(";\n")
	}
	hb.WriteString("\n#ifdef __cplusplus\n}\n#endif\n\n#endif /* ")
	hb.WriteString(guard)
	hb.WriteString(" */\n")

	headerPath := filepath.Join(pkgDir, hName)
	if err := os.WriteFile(headerPath, []byte(hb.String()), 0644); err != nil {
		return nil, fmt.Errorf("write bridge header: %w", err)
	}

	// ------- 生成桥接实现 (.cpp) -------
	var cb strings.Builder
	cb.WriteString("/* Automated extern \"C\" bridge implementation for ")
	cb.WriteString(libName)
	cb.WriteString(" — generated by Kaula compiler */\n")
	for _, ref := range headerRefs {
		cb.WriteString("#include \"")
		cb.WriteString(ref)
		cb.WriteString("\"\n")
	}
	cb.WriteString("#include \"")
	cb.WriteString(hName)
	cb.WriteString("\"\n\n")

	cb.WriteString("extern \"C\" {\n")
	for _, k := range keys {
		fn := exportDecls[k]
		callName, ok := cppCalls[k]
		if !ok {
			continue // extern "C" 函数无需桥接体
		}
		infos := argInfoMap[k]
		paramDefs := make([]string, len(infos))
		argUses := make([]string, len(infos))
		for i, info := range infos {
			pi := fmt.Sprintf("p%d", i)
			paramDefs[i] = info.CType + " " + pi
			argUses[i] = info.CallExpr
		}
		cb.WriteString(exportSignatureWithParams(k, fn, paramDefs))
		cb.WriteString("{\n    ")
		if retIsRefMap[k] {
			// 引用返回：导出为指针，取地址即可
			if retVoidMap[k] {
				cb.WriteString("return (void*)&(")
			} else {
				cb.WriteString("return &(")
			}
			cb.WriteString(callName)
		} else if retVoidMap[k] {
			cb.WriteString("return (void*)(")
			cb.WriteString(callName)
		} else if fn.Return != "" && fn.Return != "void" {
			cb.WriteString("return ")
			cb.WriteString(callName)
		} else {
			cb.WriteString(callName)
		}
		if len(argUses) > 0 {
			cb.WriteString("(")
			cb.WriteString(strings.Join(argUses, ", "))
			cb.WriteString(")")
		} else {
			cb.WriteString("()")
		}
		if retIsRefMap[k] || retVoidMap[k] {
			cb.WriteString(")")
		}
		cb.WriteString(";\n}\n")
	}
	cb.WriteString("}\n")

	cppPath := filepath.Join(pkgDir, cpp)
	if err := os.WriteFile(cppPath, []byte(cb.String()), 0644); err != nil {
		return nil, fmt.Errorf("write bridge source: %w", err)
	}

	// 语法校验：bridge.cpp 必须可编译通过，否则回退
	if err := verifyBridgeCompiles(pkgDir, libName, cppPath); err != nil {
		os.Remove(headerPath)
		os.Remove(cppPath)
		return nil, fmt.Errorf("bridge verification failed: %w", err)
	}

	return &CppBridgeResult{
		Header:      libName + "/" + hName,
		Source:      libName + "/" + cpp,
		LibName:     libName,
		BridgeFuncs: exportDecls,
	}, nil
}

// verifyBridgeCompiles 用 clang -fsyntax-only -x c++ 校验桥接源
func verifyBridgeCompiles(pkgDir, libName, cppPath string) error {
	clangPath, err := findClangPath()
	if err != nil {
		return err
	}
	args := []string{"-x", "c++", "-std=c++11", "-fsyntax-only",
		"-I", libPathOf(pkgDir), cppPath}
	cmd := exec.Command(clangPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v\n%s", err, strings.TrimRight(string(out), "\n"))
	}
	return nil
}

// libPathOf 返回库目录（pkgDir 的父目录），桥接 include 需要从那里解析 "lib/x.h"
func libPathOf(pkgDir string) string {
	dir := filepath.Dir(pkgDir)
	if strings.HasSuffix(dir, "..") {
		return ".."
	}
	return dir
}

// declareSignature 生成 C 声明行： ret exportName(args)
func declareSignature(export string, fn Function) string {
	if fn.Return == "" || fn.Return == "void" {
		return "void " + export + "(" + strings.Join(fn.Args, ", ") + ")"
	}
	return fn.Return + " " + export + "(" + strings.Join(fn.Args, ", ") + ")"
}

// exportSignatureWithParams 生成实现签名（带形参名）
func exportSignatureWithParams(export string, fn Function, paramDefs []string) string {
	if fn.Return == "" || fn.Return == "void" {
		return "void " + export + "(" + strings.Join(paramDefs, ", ") + ")"
	}
	return fn.Return + " " + export + "(" + strings.Join(paramDefs, ", ") + ")"
}