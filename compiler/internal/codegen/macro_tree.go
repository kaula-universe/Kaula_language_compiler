package codegen

// 本文件把 Tree 约束系统与结构宏桥接起来：
//
//   - **解析**：把前缀的结构签名（#[prefix] 函数上的 `tree(...)` / `struct(...)`
//     注解参数，或 `tree Name { ... }` 声明的树模板）解析成 core.TreeConstraint
//     链，并把每个约束挂到对应的 `$参数` 上；
//   - **映射**：把 AST 节点（ast.Expression / ast.Statement）映射成 core.TreeNodeType，
//     使 core 包不必反向依赖 ast；
//   - **校验**：展开**之前**验证调用点传来的实参结构是否符合声明。约束不满足时
//     给出带位置与期望/实际结构的错误，而不是让展开后的 C 代码在编译期崩掉。
//
// 这一层是"结构宏"区别于"文本宏"的关键：宏不仅替换参数，还声明并检查
// 参数的语法结构（是表达式？是语句块？是字面量？是某个类型的值？）。

import (
	"fmt"
	"strings"

	"kaula/internal/ast"
	"kaula/internal/core"
)

// MacroSignature 前缀模板的结构签名
type MacroSignature struct {
	// Params 形参/参数名 → 结构约束（空约束表示不限）
	Params map[string]*core.TreeConstraint
	// Body 调用体（$body 占位）的结构约束
	Body *core.TreeConstraint
	// Source `tree` 注解的原文（诊断信息用）
	Source string
}

// IsEmpty 签名是否没有任何约束
func (s *MacroSignature) IsEmpty() bool {
	return s == nil || (len(s.Params) == 0 && s.Body == nil)
}

// ConstraintFor 返回参数名对应的约束（无则 nil）
func (s *MacroSignature) ConstraintFor(param string) *core.TreeConstraint {
	if s == nil || s.Params == nil {
		return nil
	}
	return s.Params[param]
}

// macroSignatureFor 取前缀的结构签名。
//
// 来源优先级（与 tree 系统的既有层次一致）：
//  1. `#[prefix] fn Name(...)` 上的 tree 注解参数，例如
//     `#[prefix] fn swap(tree(expression, expression), a: i64, b: i64)`；
//  2. 同文件/导入文件里的 `tree Name { ... }` 声明（prefix/tree 注解），
//     它的根约束作用到 `$body`。
func (sg *StatementGenerator) macroSignatureFor(name string, funcDecl *ast.FunctionStatement, prefixStmt *ast.PrefixStatement) *MacroSignature {
	sig := &MacroSignature{Params: map[string]*core.TreeConstraint{}}
	if funcDecl != nil {
		if decl := treeAnnotationDecl(funcDecl); decl != nil {
			sig.Source = decl.Raw
			applyTreeDecl(sig, decl, funcDecl.Params)
		}
	}
	if tmpl := sg.codegen.findTreeStatement(name); tmpl != nil {
		if sig.Source == "" {
			sig.Source = "tree " + name + " { ... }"
		}
		if sig.Body == nil {
			sig.Body = treeStatementConstraint(tmpl)
		}
	}
	// 形参声明的类型也纳入约束：`#[prefix] fn log(msg: string)` 即
	// `$msg` 必须能作为 string 使用（当前只对字面量做实校验，标识符放行）。
	if funcDecl != nil {
		for i, name := range funcDecl.Params {
			if i >= len(funcDecl.ParamTypes) || funcDecl.ParamTypes[i] == "" {
				continue
			}
			if _, exists := sig.Params[name]; exists {
				continue
			}
			if c := parseTreeTypeString(funcDecl.ParamTypes[i]); c != nil {
				if _, isCtor := macroTypeNameToNodeType(strings.TrimSpace(funcDecl.ParamTypes[i])); isCtor {
					continue
				}
				sig.Params[name] = c
			}
		}
	}
	_ = prefixStmt
	return sig
}

// findTreeStatement 查找与前缀同名的 tree 模板（本文件优先，其次导入文件）
func (cg *CodeGenerator) findTreeStatement(name string) *ast.TreeStatement {
	if cg.program != nil {
		if stmt := findTreeInStmts(cg.program.Statements, name); stmt != nil {
			return stmt
		}
	}
	if cg.localTreeStmts != nil {
		if stmt, ok := cg.localTreeStmts[name]; ok {
			return stmt
		}
	}
	return nil
}

func findTreeInStmts(stmts []ast.Statement, name string) *ast.TreeStatement {
	for _, s := range stmts {
		switch n := s.(type) {
		case *ast.TreeStatement:
			if n.Name == name {
				return n
			}
		case *ast.FunctionStatement:
			if r := findTreeInStmts(n.Body, name); r != nil {
				return r
			}
		case *ast.BlockStatement:
			if r := findTreeInStmts(n.Statements, name); r != nil {
				return r
			}
		}
	}
	return nil
}

// treeDecl tree 注解的解析结果
type treeDecl struct {
	Raw     string
	Entries []treeEntry // 按书写顺序：绑定名 → 约束
}

type treeEntry struct {
	Name   string
	Type   string
	Nested []treeEntry
}

// treeAnnotationDecl 从属性列表里取 tree / struct 注解。
//
// 属性实参在解析期以字符串保存（Attribute.Args），形如
// `tree(Vec<i64>, i64)` → ["Vec<i64>", "i64"]；这里只做最外层逗号切分，
// 花括号与尖括号内部的逗号由 splitTopLevel 保护。
//
// 属性实参以字符串保存，嵌套括号（`tree(Vec<i64>, i64)`）已由 parseAttributeArgs
// 正确处理，这里只负责把每一条实参映射成一个结构约束。
func treeAnnotationDecl(fn *ast.FunctionStatement) *treeDecl {
	if fn == nil {
		return nil
	}
	attrs := fn.Attributes
	if len(attrs) == 0 && fn.Annotation == ast.TreeAnnotationPrefixTree {
		// `#[prefix,tree] fn` 的树形态：没有显式参数即为"不限"
		return &treeDecl{Raw: "#[prefix,tree]"}
	}
	for _, a := range attrs {
		if a == nil {
			continue
		}
		if a.Name != "tree" && a.Name != "struct" {
			continue
		}
		items := a.Args
		decl := &treeDecl{Raw: a.Name + "(" + strings.Join(items, ", ") + ")"}
		for i, arg := range items {
			t := strings.TrimSpace(arg)
			if t == "" {
				continue
			}
			name := fmt.Sprintf("#%d", i)
			rest := t
			// 支持 `$name = 结构` 的显式绑定写法
			if eq := strings.Index(t, "="); eq > 0 {
				left := strings.TrimSpace(t[:eq])
				if strings.HasPrefix(left, "$") || isSimpleIdent(left) {
					name = left
					rest = strings.TrimSpace(t[eq+1:])
				}
			}
			decl.Entries = append(decl.Entries, treeEntry{Name: name, Type: rest})
		}
		return decl
	}
	return nil
}

// isSimpleIdent 判断是否为普通标识符（用于 `name = 结构` 的左侧绑定名）
func isSimpleIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// applyTreeDecl 把 tree 注解的条目挂到参数上：
// 第 i 个条目默认对应第 i 个形参；带 `$name =` 前缀时按名字绑定。
func applyTreeDecl(sig *MacroSignature, decl *treeDecl, params []string) {
	for i, entry := range decl.Entries {
		name := strings.TrimPrefix(entry.Name, "$")
		if strings.HasPrefix(entry.Name, "#") {
			if i >= len(params) {
				continue
			}
			name = params[i]
		}
		if name == "" {
			continue
		}
		constraint := parseTreeTypeString(entry.Type)
		if name == "body" {
			sig.Body = constraint
			continue
		}
		sig.Params[name] = constraint
		// `tree(...)` 只有一个实参时，它描述的是**结构组**（$body 的整体形态），
		// 同时作为 Body 约束：这是最常见的写法 `tree(expression)`。
		if len(decl.Entries) == 1 {
			sig.Body = constraint
		}
	}
}

// treeStatementConstraint 把 `tree Name { ... }` 语句体转成根约束。
//
// 语句体的每一条语句即一层嵌套要求：用 ast 节点类型映射成 TreeNodeType，
// 多个子句挂到同一个根约束的 Children 上（顺序即结构顺序）。
func treeStatementConstraint(stmt *ast.TreeStatement) *core.TreeConstraint {
	root := &core.TreeConstraint{Required: true, NodeType: core.NodeTypeBlock}
	for _, s := range stmt.Body {
		child := &core.TreeConstraint{Required: true}
		child.NodeType, child.Pattern = astNodeType(s)
		root.Children = append(root.Children, child)
	}
	if len(root.Children) == 0 {
		return &core.TreeConstraint{Required: true, NodeType: core.NodeTypeGeneric}
	}
	return root
}

// ---- 约束字符串解析 ----

// parseTreeTypeString 解析结构类型串：
//
//	expression / statement / block / literal / call / identifier / if / for ...
//	Vec<i64>         → generic，Pattern="Vec"（类型名约束），Children=[ i64 的类型约束 ]
//	block<expression, expression> → block，Children 依次为两个子约束
//
// 空串返回 nil（不限）。
func parseTreeTypeString(s string) *core.TreeConstraint {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	name, args := splitGeneric(s)
	name = strings.TrimSpace(name)
	constraint := &core.TreeConstraint{Required: true}
	// 顺序要紧：**先**查子类表。子类名（identifier/literal/binary/…）大多不在
	// 大类表里，若先查大类表就会被当成"值类型名"，NodeType 落成 generic，
	// 校验时 macroValueHasType 一律放行——tree(binary) 就成了摆设。
	if subtypeNodeType, isSubtype := macroSubtypeToNodeType(name); isSubtype {
		// 结构子类名：NodeType 用大类，Pattern 保留子类名，
		// 多规则分派才能区分同一大类下的不同结构。
		constraint.NodeType = subtypeNodeType
		constraint.Pattern = strings.ToLower(name)
	} else {
		nodeType, isCtor := macroTypeNameToNodeType(name)
		constraint.NodeType = nodeType
		if !isCtor {
			// 不是 AST 节点类别 → 当作类型名约束（如 i64 / Vec）
			constraint.Pattern = name
		}
	}
	for _, arg := range args {
		if child := parseTreeTypeString(arg); child != nil {
			constraint.Children = append(constraint.Children, child)
		}
	}
	return constraint
}

// splitGeneric 按最外层尖括号切分 "Vec<i64, f64>" → ("Vec", ["i64","f64"])。
// 同时兼容 `Vec<i64>` 中嵌套的 `<`。
func splitGeneric(s string) (string, []string) {
	s = strings.TrimSpace(s)
	open := strings.Index(s, "<")
	if open < 0 || !strings.HasSuffix(s, ">") {
		return s, nil
	}
	head := s[:open]
	inner := s[open+1 : len(s)-1]
	var args []string
	depth := 0
	cur := strings.Builder{}
	for _, r := range inner {
		switch r {
		case '<', '{', '(':
			depth++
		case '>', '}', ')':
			depth--
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(cur.String()))
				cur.Reset()
				continue
			}
		}
		cur.WriteRune(r)
	}
	if t := strings.TrimSpace(cur.String()); t != "" {
		args = append(args, t)
	}
	return strings.TrimSpace(head), args
}

// macroTypeNames 结构类型名 → TreeNodeType 的关键字表。
// 与 `tree` 系统既有的 NodeType* 常量一一对应，避免两套叫法。
//
// 「大类名」（generic/statement/expression/function/variable/parameter/block/
// condition/loop/return）只决定 NodeType，不设 Pattern；「子类名」
// （identifier/literal/call/...）在 astNodeType 里表现为 Pattern，需要保留
// 以区分同一 NodeType 下的不同结构（例如 identifier 与 literal 都是
// NodeTypeExpression，但多规则分派必须能分开它们）。
var macroTypeNames = map[string]core.TreeNodeType{
	"generic":      core.NodeTypeGeneric,
	"any":          core.NodeTypeGeneric,
	"statement":    core.NodeTypeStatement,
	"stmt":         core.NodeTypeStatement,
	"expression":   core.NodeTypeExpression,
	"expr":         core.NodeTypeExpression,
	"function":     core.NodeTypeFunction,
	"fn":           core.NodeTypeFunction,
	"variable":     core.NodeTypeVariable,
	"var":          core.NodeTypeVariable,
	"parameter":    core.NodeTypeParameter,
	"block":        core.NodeTypeBlock,
	"condition":    core.NodeTypeCondition,
	"loop":         core.NodeTypeLoop,
	"return":       core.NodeTypeReturn,
	"literal":      core.NodeTypeExpression,
	"call":         core.NodeTypeExpression,
	"identifier":   core.NodeTypeExpression,
	"assign":       core.NodeTypeExpression,
	"declaration":  core.NodeTypeVariable,
	"transition":   core.NodeTypeGeneric,
	"states":       core.NodeTypeGeneric,
	"transitionIn": core.NodeTypeGeneric,
}

// macroSubtypeNames 结构「子类名」集合：这些名字映射到某个大类 NodeType，
// 但本身携带更具体的结构信息（与 astNodeType 返回的 Pattern 一致）。展开时
// 必须把它们写进 TreeConstraint.Pattern，否则 tree(identifier) 与 tree(literal)
// 都会退化成 tree(expression)，多规则按结构分派（Task #1）无从区分。
//
// 值是该子类对应的大类 NodeType，与 macroTypeNames 保持一致；同时补齐
// astNodeType 能产出、但宏用户可能书写的其余子类（binary/unary/member/…）。
var macroSubtypeNames = map[string]core.TreeNodeType{
	"identifier":   core.NodeTypeExpression,
	"literal":      core.NodeTypeExpression,
	"call":         core.NodeTypeExpression,
	"assign":       core.NodeTypeExpression,
	"binary":       core.NodeTypeExpression,
	"unary":        core.NodeTypeExpression,
	"member":       core.NodeTypeExpression,
	"index":        core.NodeTypeExpression,
	"prefix_call":  core.NodeTypeExpression,
	"lambda":       core.NodeTypeFunction,
	"match":        core.NodeTypeExpression,
	"conditional":  core.NodeTypeCondition,
	"declaration":  core.NodeTypeVariable,
	"if":           core.NodeTypeCondition,
	"while":        core.NodeTypeLoop,
	"for":          core.NodeTypeLoop,
}

// macroTypeNameToNodeType 结构类型名 → 节点类别，"非类别名"（i64/Vec…）→ false
func macroTypeNameToNodeType(name string) (core.TreeNodeType, bool) {
	if t, ok := macroTypeNames[name]; ok {
		return t, true
	}
	if t, ok := macroTypeNames[strings.ToLower(name)]; ok {
		return t, true
	}
	return core.NodeTypeGeneric, false
}

// macroSubtypeToNodeType 结构子类名 → 大类 NodeType；非子类名返回 false。
func macroSubtypeToNodeType(name string) (core.TreeNodeType, bool) {
	if t, ok := macroSubtypeNames[name]; ok {
		return t, true
	}
	if t, ok := macroSubtypeNames[strings.ToLower(name)]; ok {
		return t, true
	}
	return core.NodeTypeGeneric, false
}

// ASTNodeType 把 AST 节点映射为结构类型（TreeNodeType + 子类形状 Pattern）。
// 供宏展开期的结构约束校验与诊断输出使用。node 为 nil 时返回 generic。
func ASTNodeType(node ast.Node) (core.TreeNodeType, string) {
	if node == nil {
		return core.NodeTypeGeneric, ""
	}
	return astNodeType(node)
}

func astNodeType(node ast.Node) (core.TreeNodeType, string) {
	switch n := node.(type) {
	case ast.Expression:
		switch n.(type) {
		case *ast.Identifier:
			return core.NodeTypeExpression, "identifier"
		case *ast.IntegerLiteral, *ast.FloatLiteral, *ast.StringLiteral, *ast.CharLiteral, *ast.BooleanLiteral:
			return core.NodeTypeExpression, "literal"
		case *ast.CallExpression, *ast.SpawnExpression:
			return core.NodeTypeExpression, "call"
		case *ast.BinaryExpression:
			return core.NodeTypeExpression, "binary"
		case *ast.UnaryExpression:
			return core.NodeTypeExpression, "unary"
		case *ast.MemberAccessExpression:
			return core.NodeTypeExpression, "member"
		case *ast.IndexExpression:
			return core.NodeTypeExpression, "index"
		case *ast.PrefixCallExpression:
			return core.NodeTypeExpression, "prefix_call"
		case *ast.LambdaExpression:
			return core.NodeTypeFunction, "lambda"
		case *ast.MatchExpression:
			return core.NodeTypeExpression, "match"
		case *ast.ConditionalExpression:
			return core.NodeTypeCondition, "conditional"
		default:
			return core.NodeTypeExpression, "expression"
		}
	case ast.Statement:
		switch n.(type) {
		case *ast.BlockStatement:
			return core.NodeTypeBlock, "block"
		case *ast.VariableDeclaration:
			return core.NodeTypeVariable, "declaration"
		case *ast.FunctionStatement:
			return core.NodeTypeFunction, "function"
		case *ast.IfStatement:
			return core.NodeTypeCondition, "if"
		case *ast.WhileStatement:
			return core.NodeTypeLoop, "while"
		case *ast.ForStatement, *ast.ForInStatement:
			return core.NodeTypeLoop, "for"
		case *ast.ReturnStatement:
			return core.NodeTypeReturn, "return"
		default:
			return core.NodeTypeStatement, "statement"
		}
	default:
		return core.NodeTypeGeneric, ""
	}
}

// ---- 展开期结构校验 ----

// validateMacroArguments 在展开之前校验每个实参的结构约束。
// 返回错误信息列表（空表示全部满足）。
func (sg *StatementGenerator) validateMacroArguments(exp *macroExpansion, bodyCount int) []string {
	if exp == nil || exp.Sig.IsEmpty() {
		return nil
	}
	var errs []string
	names := make([]string, 0, len(exp.Params))
	for name := range exp.Params {
		names = append(names, name)
	}
	sortStrings(names)
	for _, name := range names {
		constraint := exp.Sig.ConstraintFor(name)
		if constraint == nil {
			continue
		}
		value := exp.Params[name]
		if value == nil {
			continue
		}
		if err := checkMacroArgument(exp.Name, name, value, constraint); err != "" {
			errs = append(errs, err)
		}
	}
	if exp.Sig.Body != nil && bodyCount > 0 {
		// 调用体以"语句组"的整体形态参与校验：body 约束描述这一组语句的形态。
		// 约束是 statement 时，组内每条都必须是语句（语句永远是语句，通过）；
		// 约束是 expression 时，组内每条都必须是表达式语句。
		for _, s := range exp.BodyStmts {
			if err := checkMacroStatement(exp.Name, s, exp.Sig.Body); err != "" {
				errs = append(errs, err)
				break
			}
		}
	}
	return errs
}

// checkMacroStatement 按表达式约束校验一条语句（调用体成员）
func checkMacroStatement(macro string, s ast.Statement, constraint *core.TreeConstraint) string {
	switch constraint.NodeType {
	case core.NodeTypeExpression:
		if es, ok := s.(*ast.ExpressionStatement); ok && es.Expression != nil {
			return checkMacroArgument(macro, "body", es.Expression, constraint)
		}
		return fmt.Sprintf("前缀 '%s' 的调用体结构不匹配：期望 %s，实际是语句 %s",
			macro, constraint.String(), astutilStatementKind(s))
	case core.NodeTypeBlock:
		if b, ok := s.(*ast.BlockStatement); ok {
			for _, inner := range b.Statements {
				if err := checkMacroStatement(macro, inner, constraint); err != "" {
					return err
				}
			}
		}
		return ""
	default:
		return ""
	}
}

// astutilStatementKind 语句的可读类别名
func astutilStatementKind(s ast.Statement) string {
	t, pattern := astNodeType(s)
	if pattern == "" {
		return t.String()
	}
	return pattern
}

// checkMacroArgument 校验单个实参：节点类别 → 类型名 → 嵌套形状，逐层给出可读错误。
func checkMacroArgument(macro, param string, value ast.Expression, constraint *core.TreeConstraint) string {
	nodeType, pattern := astNodeType(value)
	if constraint.NodeType != core.NodeTypeGeneric && nodeType != constraint.NodeType &&
		!(constraint.NodeType == core.NodeTypeExpression && nodeType == core.NodeTypeCondition) {
		// conditional 属于表达式族，显式声明 expression 时不应被拒
		return fmt.Sprintf("前缀 '%s' 的参数 '$%s' 结构不匹配：期望 %s，实际是 %s",
			macro, param, constraint.NodeType.String(), describeMacroArg(value))
	}
	if constraint.Pattern != "" {
		// Pattern 既可能是"结构子类"（literal/call/binary/...），也可能是"值类型名"（i64/Point）
		//
		// 判子类必须查 macroSubtypeNames：它是「子类名」的权威集合，比
		// macroTypeNames 更全（binary/unary/member/index/prefix_call/lambda/
		// match/conditional/if/while/for 都在其中）。查错表会把这些子类名
		// 误当成"值类型名"，走 macroValueHasType 一律放行——于是
		// tree(binary) 会接受任意实参，多规则分派就形同虚设。
		if subclass, ok := macroSubtypeToNodeType(constraint.Pattern); ok && subclass == constraint.NodeType {
			if pattern != "" && pattern != constraint.Pattern {
				return fmt.Sprintf("前缀 '%s' 的参数 '$%s' 结构不匹配：期望 %s，实际是 %s",
					macro, param, constraint.Pattern, describeMacroArg(value))
			}
		} else if !macroValueHasType(value, constraint.Pattern) {
			return fmt.Sprintf("前缀 '%s' 的参数 '$%s' 类型不匹配：期望 %s，实际是 %s",
				macro, param, constraint.Pattern, describeMacroArg(value))
		}
	}
	return ""
}

// macroValueHasType 判断实参值是否可以满足类型名约束。
// 类型推导不确定时返回 true（宁可不报，也不误报）。
func macroValueHasType(value ast.Expression, typeName string) bool {
	if typeName == "" {
		return true
	}
	switch v := value.(type) {
	case *ast.IntegerLiteral:
		return isIntegerTypeName(typeName)
	case *ast.FloatLiteral:
		return isFloatTypeName(typeName)
	case *ast.StringLiteral:
		return typeName == "string"
	case *ast.BooleanLiteral:
		return typeName == "bool"
	case *ast.Identifier:
		// 标识符的静态类型由上下文决定；展开期不做类型推断，放行
		_ = v
		return true
	default:
		return true
	}
}

func isFloatTypeName(t string) bool {
	switch t {
	case "f32", "f64", "float", "float32", "float64", "double":
		return true
	}
	return false
}

// describeMacroArg 以用户可读的方式描述实参的结构（错误信息用）
func describeMacroArg(value ast.Expression) string {
	nodeType, pattern := astNodeType(value)
	if pattern == "" {
		return nodeType.String()
	}
	return pattern + "（" + nodeType.String() + "）"
}

func sortStrings(in []string) {
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j] < in[j-1]; j-- {
			in[j], in[j-1] = in[j-1], in[j]
		}
	}
}
