package formatter

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"kaula/internal/ast"
	"kaula/internal/errors"
	"kaula/internal/lexer"
	"kaula/internal/parser"
)

type Formatter struct {
	indent    int
	buf       bytes.Buffer
	indentStr string

	// unknown 记录本次格式化中遇到的、formatter **尚未覆盖**的 AST 节点类型。
	// 它是 fail-fast 护栏的依据：与其输出一份丢语义的源码，不如明确失败。
	unknown []string

	// parseErrors / parseErrorMsgs 记录本次解析的**语法**错误。
	// 格式化器必须区分「语法错误」（源码本身有问题，拒绝格式化）与
	//「未覆盖的 AST 节点」（formatter 的能力缺口），二者都不是可降级情形。
	parseErrors    int
	parseErrorMsgs []string
}

func New() *Formatter {
	return &Formatter{
		indentStr: "    ",
	}
}

// FormatSource 格式化一段源码（无文件上下文）。
func (f *Formatter) FormatSource(source string) string {
	return f.formatSource(source, "")
}

// formatSource 是真正干活的格式化入口。
// filePath 仅用于给 parser 提供正确的文件名，使依赖文件位置的检查行为一致；
// 传入空串时回退到占位名。
func (f *Formatter) formatSource(source, filePath string) string {
	if filePath == "" {
		filePath = "<fmt>"
	}

	lex := lexer.NewLexer(source)
	p := parser.NewParser(lex)
	p.SetFile(filePath)
	p.SetSkipMainCheck(true)
	// import 目标的「可解析性」检查依赖运行环境（工作目录 / stdlib / kaula.json），
	// 与源码语法是否正确无关。格式化时必须跳过，否则换个目录执行 fmt
	// 就会把合法的 import 判成错误并拒绝格式化。
	p.SetSkipImportCheck(true)

	program := p.Parse()

	// 注意：必须在 FormatProgram **之后**采集解析错误。
	// FormatProgram 自身会调reset() 清空状态（它是公开 API，需要能独立复用），
	// 若在之前采集，错误计数会被 reset 抹掉。
	out := f.FormatProgram(program)
	f.captureParseErrors(p)

	return out
}

// captureParseErrors 从 parser 与 lexer 的错误收集器中提取**语法**错误。
// 只统计 ErrorSyntax：语义/类型错误与「源码能否被正确格式化」无关，
// 而格式化阶段本来也还没做类型检查。
//
// 必须同时看 lexer：parser 与 lexer 默认各持一个独立收集器，
// 「未闭合块注释」「无法识别的字节」这类词法错误只会进lexer 的收集器。
func (f *Formatter) captureParseErrors(p *parser.Parser) {
	collect := func(ec *errors.ErrorCollector) {
		if ec == nil {
			return
		}
		for _, e := range ec.Errors() {
			if e == nil || e.Type != errors.ErrorSyntax {
				continue
			}
			f.parseErrors++
			f.parseErrorMsgs = append(f.parseErrorMsgs,
				fmt.Sprintf("第 %d 行第 %d 列：%s", e.Line, e.Column, e.Message))
		}
	}
	collect(p.ErrorCollector())
	collect(p.LexerErrorCollector())
}

// HasParseError 报告本次解析是否存在语法错误。
func (f *Formatter) HasParseError() bool {
	return f.parseErrors > 0
}

// ParseErrorCount 返回语法错误数量。
func (f *Formatter) ParseErrorCount() int {
	return f.parseErrors
}

// ParseErrorMessages 返回逐条语法错误信息。
func (f *Formatter) ParseErrorMessages() []string {
	return f.parseErrorMsgs
}

// HasUnknown 报告本次格式化是否遇到过未覆盖的 AST 节点。
func (f *Formatter) HasUnknown() bool {
	return len(f.unknown) > 0
}

// UnknownNodes 返回去重后的未覆盖 AST 节点类型列表。
func (f *Formatter) UnknownNodes() []string {
	return f.unknown
}

// noteUnknown 记录一个未覆盖的 AST 节点类型（去重）。
func (f *Formatter) noteUnknown(what string) {
	for _, u := range f.unknown {
		if u == what {
			return
		}
	}
	f.unknown = append(f.unknown, what)
}

func (f *Formatter) FormatFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return f.formatSource(string(data), path), nil
}

// WriteFileInPlace 直接把格式化结果写入磁盘（不追加换行，由调用方决定）。
func (f *Formatter) WriteFileInPlace(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

// FormatFileInPlace 就地格式化文件。
// 遇到语法错误或未覆盖的 AST 节点时**拒绝落盘**——
// 此时返回的 string 已是不可信的产物，写回会造成不可逆的源码损坏。
func (f *Formatter) FormatFileInPlace(path string) error {
	formatted, err := f.FormatFile(path)
	if err != nil {
		return err
	}
	if f.HasParseError() {
		return fmt.Errorf("%s: 解析失败，已跳过写入（%d 个语法错误）：%s",
			path, f.parseErrors, strings.Join(f.parseErrorMsgs, "; "))
	}
	if f.HasUnknown() {
		return fmt.Errorf("%s: 存在未覆盖的 AST 节点，已跳过写入：%s",
			path, strings.Join(f.unknown, ", "))
	}
	return f.WriteFileInPlace(path, formatted+"\n")
}

func (f *Formatter) reset() {
	f.indent = 0
	f.buf.Reset()
	f.unknown = nil
	f.parseErrors = 0
	f.parseErrorMsgs = nil
}

func (f *Formatter) Reset() {
	f.reset()
}

func (f *Formatter) FormatProgram(program *ast.Program) string {
	f.reset()

	if len(program.Statements) == 0 {
		return f.buf.String()
	}

	importEnd := 0
	for importEnd < len(program.Statements) {
		if _, ok := program.Statements[importEnd].(*ast.ImportStatement); ok {
			importEnd++
		} else {
			break
		}
	}

	if importEnd > 0 {
		type importEntry struct {
			stmt *ast.ImportStatement
			key  string
		}
		stdImports := []importEntry{}
		otherImports := []importEntry{}
		for i := 0; i < importEnd; i++ {
			imp := program.Statements[i].(*ast.ImportStatement)
			entry := imp.Module
			if entry == "" {
				entry = imp.Path
			}
			if strings.HasPrefix(entry, "std.") {
				stdImports = append(stdImports, importEntry{stmt: imp, key: entry})
			} else {
				otherImports = append(otherImports, importEntry{stmt: imp, key: entry})
			}
		}
		sort.SliceStable(stdImports, func(a, b int) bool { return stdImports[a].key < stdImports[b].key })
		sort.SliceStable(otherImports, func(a, b int) bool { return otherImports[a].key < otherImports[b].key })

		writeImport := func(imp *ast.ImportStatement) {
			f.formatImportStatement(imp)
		}
		first := true
		for _, ie := range stdImports {
			if !first {
				f.buf.WriteString("\n")
			}
			writeImport(ie.stmt)
			first = false
		}
		for _, ie := range otherImports {
			if !first {
				f.buf.WriteString("\n\n")
			}
			writeImport(ie.stmt)
			first = false
		}

		if importEnd < len(program.Statements) {
			f.buf.WriteString("\n\n")
		}
	}

	for i := importEnd; i < len(program.Statements); i++ {
		if i > importEnd {
			f.buf.WriteString("\n\n")
		}
		f.formatStatement(program.Statements[i])
	}
	return f.buf.String()
}

func (f *Formatter) formatStatement(stmt ast.Statement) {
	if stmt == nil {
		return
	}

	switch s := stmt.(type) {
	case *ast.FunctionStatement:
		f.formatFunctionStatement(s)
	case *ast.IfStatement:
		f.formatIfStatement(s)
	case *ast.WhileStatement:
		f.formatWhileStatement(s)
	case *ast.ForStatement:
		f.formatForStatement(s)
	case *ast.ReturnStatement:
		f.formatReturnStatement(s)
	case *ast.ExpressionStatement:
		f.formatExpressionStatement(s)
	case *ast.BlockStatement:
		f.formatBlockStatement(s)
	case *ast.VariableDeclaration:
		f.formatVariableDeclaration(s)
	case *ast.SpendStatement:
		f.formatSpendStatement(s)
	case *ast.PrefixStatement:
		f.formatPrefixStatement(s)
	case *ast.TreeStatement:
		f.formatTreeStatement(s)
	case *ast.ObjectStatement:
		f.formatObjectStatement(s)
	case *ast.ClassStatement:
		f.formatClassStatement(s)
	case *ast.InterfaceStatement:
		f.formatInterfaceStatement(s)
	case *ast.StructStatement:
		f.formatStructStatement(s)
	case *ast.TypeAliasStatement:
		f.formatTypeAliasStatement(s)
	case *ast.ImportStatement:
		f.formatImportStatement(s)
	case *ast.ExportStatement:
		f.formatExportStatement(s)
	case *ast.NonLocalStatement:
		f.formatNonLocalStatement(s)
	case *ast.CallStatement:
		f.formatCallStatementStmt(s)
	case *ast.MethodStatement:
		f.formatMethodStatement(s)
	case *ast.ConstructorStatement:
		f.formatConstructorStatement(s)
	case *ast.BreakStatement:
		f.buf.WriteString("break")
	case *ast.ContinueStatement:
		f.buf.WriteString("continue")
	case *ast.ForInStatement:
		f.formatForInStatement(s)
	case *ast.YieldStatement:
		f.formatYieldStatement(s)
	case *ast.ReleaseStatement:
		f.formatReleaseStatement(s)
	case *ast.ExtractStatement:
		f.formatExtractStatement(s)
	case *ast.ExternStatement:
		f.formatExternStatement(s)
	case *ast.EnumStatement:
		f.formatEnumStatement(s)
	case *ast.PackageStatement:
		f.buf.WriteString("package " + s.Name)
	default:
		f.buf.WriteString(fmt.Sprintf("/* unknown statement: %T */", stmt))
	}
}

func (f *Formatter) formatExpression(expr ast.Expression) {
	if expr == nil {
		return
	}

	switch e := expr.(type) {
	case *ast.BinaryExpression:
		f.formatBinaryExpression(e)
	case *ast.CallExpression:
		f.formatCallExpression(e)
	case *ast.Identifier:
		f.buf.WriteString(e.Name)
	case *ast.IntegerLiteral:
		f.buf.WriteString(strconv.FormatUint(e.Value, 10))
	case *ast.FloatLiteral:
		s := strconv.FormatFloat(e.Value, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eE") {
			s += ".0"
		}
		f.buf.WriteString(s)
	case *ast.StringLiteral:
		f.buf.WriteString(`"` + e.Value + `"`)
	case *ast.BooleanLiteral:
		if e.Value {
			f.buf.WriteString("true")
		} else {
			f.buf.WriteString("false")
		}
	case *ast.IndexExpression:
		f.formatExpression(e.Object)
		f.buf.WriteString("[")
		f.formatExpression(e.Index)
		f.buf.WriteString("]")
	case *ast.MemberAccessExpression:
		f.formatExpression(e.Object)
		f.buf.WriteString(".")
		f.buf.WriteString(e.Member)
	case *ast.UnaryExpression:
		f.buf.WriteString(e.Operator)
		// 前缀一元运算符的操作数若本身是复合表达式，必须补括号。
		// 否则 `!(5 < 3)` 会被写成 `!5 < 3`——解析成 `(!5) < 3`，
		// 语义从「5不小于3」变成「-5 小于 3」，恒为真。
		f.formatUnaryOperand(e.Right)
	case *ast.PrefixCallExpression:
		f.formatPrefixCallExpression(e)
	case *ast.TypeCastExpression:
		f.buf.WriteString("as<" + e.TargetType + ">(")
		f.formatExpression(e.Expression)
		f.buf.WriteString(")")
	case *ast.CharLiteral:
		f.buf.WriteString("'" + e.Value + "'")
	case *ast.StructLiteral:
		f.formatStructLiteral(e)
	case *ast.ObjectLiteral:
		f.formatObjectLiteral(e)
	case *ast.LambdaExpression:
		f.formatLambdaExpression(e)
	case *ast.SizeOfExpression:
		f.buf.WriteString("sizeof(" + e.TargetType + ")")
	case *ast.AlignOfExpression:
		f.buf.WriteString("alignof(" + e.TargetType + ")")
	case *ast.OffsetOfExpression:
		f.buf.WriteString("offsetof(" + e.TargetType + ", " + e.FieldName + ")")
	case *ast.ComptimeExpression:
		f.buf.WriteString("comptime ")
		f.formatExpression(e.Inner)
	case *ast.TypeNameExpression:
		f.buf.WriteString("type_name(" + e.TargetType + ")")
	case *ast.FieldCountExpression:
		f.buf.WriteString("field_count(" + e.TargetType + ")")
	case *ast.FieldNameExpression:
		f.buf.WriteString("field_name(" + e.TargetType + ", ")
		f.formatExpression(e.Index)
		f.buf.WriteString(")")
	case *ast.FieldTypeExpression:
		f.buf.WriteString("field_type(" + e.TargetType + ", ")
		f.formatExpression(e.Index)
		f.buf.WriteString(")")
	case *ast.TypeKindExpression:
		f.buf.WriteString("type_kind(" + e.TargetType + ")")
	case *ast.MatchExpression:
		f.formatMatchExpression(e)
	case *ast.AttributeExpression:
		f.formatAttributeExpression(e)
	case *ast.SpawnExpression:
		f.formatSpawnExpression(e)
	case *ast.AwaitExpression:
		f.formatAwaitExpression(e)
	case *ast.ArrayLiteral:
		f.formatArrayLiteral(e)
	case *ast.ConditionalExpression:
		f.formatConditionalExpression(e)
	case *ast.ParenExpression:
		f.buf.WriteString("(")
		f.formatExpression(e.Inner)
		f.buf.WriteString(")")
	case *ast.MemberExpression:
		f.formatExpression(e.Object)
		f.buf.WriteString(".")
		f.buf.WriteString(e.Member)
	case *ast.LiteralExpression:
		f.formatLiteralExpressionValue(e)
	default:
		// 未覆盖的节点类型：记录下来并输出占位。
		// 上层护栏会据此 fail-fast，避免把损坏的源码写回磁盘。
		f.noteUnknown(fmt.Sprintf("%T", expr))
		f.buf.WriteString(fmt.Sprintf("/* unknown expression: %T */", expr))
	}
}

// formatSpawnExpression 格式化并发提交表达式。
// 语法：spawn func(args...)；parser 要求目标是裸函数名，实参可选。
func (f *Formatter) formatSpawnExpression(e *ast.SpawnExpression) {
	if e == nil {
		return
	}
	f.buf.WriteString("spawn ")
	f.formatExpression(e.Function)
	f.buf.WriteString("(")
	for i, arg := range e.Args {
		if i > 0 {
			f.buf.WriteString(", ")
		}
		f.formatExpression(arg)
	}
	f.buf.WriteString(")")
}

// formatAwaitExpression 格式化 await 表达式。
//
// 采用**后缀**形式 `handle.await`：这是 parser 构造 AwaitExpression 的唯一路径
// （后缀链，见 parseIterativeExpression 中的 TOKEN_AWAIT 分支），
// 也是仓库中现存代码的实际写法（如 `i64 r = hs[i].await`）。
// 若输出前缀 `await h`，在 `hs[i]` 这类下标句柄上会丢语句。
func (f *Formatter) formatAwaitExpression(e *ast.AwaitExpression) {
	if e == nil {
		return
	}
	f.formatExpression(e.Handle)
	f.buf.WriteString(".await")
}

// formatArrayLiteral 格式化数组字面量 [a, b, c]
func (f *Formatter) formatArrayLiteral(e *ast.ArrayLiteral) {
	if e == nil {
		return
	}
	f.buf.WriteString("[")
	for i, el := range e.Elements {
		if i > 0 {
			f.buf.WriteString(", ")
		}
		f.formatExpression(el)
	}
	f.buf.WriteString("]")
}

// formatConditionalExpression 格式化条件表达式 cond ? a : b
func (f *Formatter) formatConditionalExpression(e *ast.ConditionalExpression) {
	if e == nil {
		return
	}
	f.formatExpression(e.Condition)
	f.buf.WriteString(" ? ")
	f.formatExpression(e.TrueExpr)
	f.buf.WriteString(" : ")
	f.formatExpression(e.FalseExpr)
}

// formatLiteralExpressionValue 按 Kind 分派字面量的具体写法。
func (f *Formatter) formatLiteralExpressionValue(e *ast.LiteralExpression) {
	if e == nil {
		return
	}
	switch strings.ToLower(e.Kind) {
	case "int", "i64", "integer":
		f.buf.WriteString(formatIntegerLiteral(e.Value))
	case "float", "f64", "double":
		f.buf.WriteString(formatFloatLiteral(e.Value))
	case "string", "str":
		f.buf.WriteString(formatStringLiteral(e.Value))
	case "bool", "boolean":
		f.buf.WriteString(formatBoolLiteral(e.Value))
	case "char":
		f.buf.WriteString(formatCharLiteral(e.Value))
	default:
		f.buf.WriteString(formatFallbackLiteral(e.Value))
	}
}

// writeTypeParams 输出泛型类型参数列表。
//
// 必须用尖括号 <T>：这是 Kaula 真实的泛型语法（fn<T>(...)）。
// 方括号 [T] 会被 parser 解析成「表达式下标」，导致泛型语义丢失、
// 格式化后的源码编译报错。
func (f *Formatter) writeTypeParams(tps []*ast.TypeParameter) {
	if len(tps) == 0 {
		return
	}
	f.buf.WriteString("<")
	for i, tp := range tps {
		if i > 0 {
			f.buf.WriteString(", ")
		}
		if tp == nil {
			continue
		}
		f.buf.WriteString(tp.Name)
		if tp.Constraint != "" {
			f.buf.WriteString(": " + tp.Constraint)
		}
	}
	f.buf.WriteString(">")
}

func (f *Formatter) formatFunctionStatement(stmt *ast.FunctionStatement) {
	if len(stmt.Attributes) > 0 {
		for _, attr := range stmt.Attributes {
			if attr == nil {
				continue
			}
			f.buf.WriteString("#[" + attr.Name)
			if len(attr.Args) > 0 {
				f.buf.WriteString("(")
				f.buf.WriteString(strings.Join(attr.Args, ", "))
				f.buf.WriteString(")")
			}
			f.buf.WriteString("]\n")
			f.writeIndent()
		}
	} else if stmt.Annotation != ast.TreeAnnotationNone {
		var annotationStr string
		switch stmt.Annotation {
		case ast.TreeAnnotationPrefix:
			annotationStr = "prefix"
		case ast.TreeAnnotationTree:
			annotationStr = "tree"
		case ast.TreeAnnotationPrefixTree:
			annotationStr = "prefix,tree"
		case ast.TreeAnnotationRoot:
			annotationStr = "root"
		case ast.TreeAnnotationRootTree:
			annotationStr = "root,tree"
		default:
			annotationStr = ""
		}
		if annotationStr != "" {
			f.buf.WriteString("#[" + annotationStr + "]\n")
			f.writeIndent()
		}
	}

	// 修饰符输出顺序：conc → export / pub
	//
	// 1. conc 必须在最前（conc fn 是并发函数标记）
	// 2. export 与 pub 互斥：parser 对 `export fn` 会**同时**置 IsExported 与
	//    IsPublic（export 语义包含 pub），因此这里必须用 else if，
	//    否则会输出 parser 不接受的 `pub export fn`
	// 3. 绝不能用 stmt.Generic 参与导出判定：它的含义是「是否泛型函数」。
	//    早期实现写成 `stmt.Generic || stmt.IsExported`，
	//    结果给每一个泛型函数都加上 export 并改变其可见性
	if stmt.IsConcurrent {
		f.buf.WriteString("conc ")
	}
	if stmt.IsExported {
		f.buf.WriteString("export ")
	} else if stmt.IsPublic {
		f.buf.WriteString("pub ")
	}

	f.buf.WriteString("fn ")
	f.buf.WriteString(stmt.Name)

	f.writeTypeParams(stmt.TypeParams)

	f.buf.WriteString("(")
	for i, param := range stmt.Params {
		if i > 0 {
			f.buf.WriteString(", ")
		}
		f.buf.WriteString(param)
		if i < len(stmt.ParamTypes) && stmt.ParamTypes[i] != "" {
			f.buf.WriteString(": " + stmt.ParamTypes[i])
		}
	}
	f.buf.WriteString(")")

	if stmt.ReturnType != "" {
		f.buf.WriteString(" " + stmt.ReturnType)
	}

	if len(stmt.Body) > 0 {
		f.buf.WriteString(" {\n")
		f.indent++
		for _, bodyStmt := range stmt.Body {
			f.writeIndent()
			f.formatStatement(bodyStmt)
			f.buf.WriteString("\n")
		}
		f.indent--
		f.writeIndent()
		f.buf.WriteString("}")
	} else {
		f.buf.WriteString(" {}")
	}
}

func (f *Formatter) formatIfStatement(stmt *ast.IfStatement) {
	f.buf.WriteString("if (")
	f.formatExpression(stmt.Condition)
	f.buf.WriteString(")")

	if len(stmt.Body) > 0 {
		f.buf.WriteString(" {\n")
		f.indent++
		for _, bodyStmt := range stmt.Body {
			f.writeIndent()
			f.formatStatement(bodyStmt)
			f.buf.WriteString("\n")
		}
		f.indent--
		f.writeIndent()
		f.buf.WriteString("}")
	}

	if len(stmt.Else) > 0 {
		f.buf.WriteString(" else ")
		if len(stmt.Else) == 1 {
			if _, isIf := stmt.Else[0].(*ast.IfStatement); isIf {
				f.formatStatement(stmt.Else[0])
			} else {
				f.buf.WriteString("{\n")
				f.indent++
				for _, elseStmt := range stmt.Else {
					f.writeIndent()
					f.formatStatement(elseStmt)
					f.buf.WriteString("\n")
				}
				f.indent--
				f.writeIndent()
				f.buf.WriteString("}")
			}
		} else {
			f.buf.WriteString("{\n")
			f.indent++
			for _, elseStmt := range stmt.Else {
				f.writeIndent()
				f.formatStatement(elseStmt)
				f.buf.WriteString("\n")
			}
			f.indent--
			f.writeIndent()
			f.buf.WriteString("}")
		}
	}
}

func (f *Formatter) formatWhileStatement(stmt *ast.WhileStatement) {
	f.buf.WriteString("while (")
	f.formatExpression(stmt.Condition)
	f.buf.WriteString(")")

	if len(stmt.Body) > 0 {
		f.buf.WriteString(" {\n")
		f.indent++
		for _, bodyStmt := range stmt.Body {
			f.writeIndent()
			f.formatStatement(bodyStmt)
			f.buf.WriteString("\n")
		}
		f.indent--
		f.writeIndent()
		f.buf.WriteString("}")
	}
}

func (f *Formatter) formatForStatement(stmt *ast.ForStatement) {
	f.buf.WriteString("for (")
	if stmt.Init != nil {
		f.formatStatement(stmt.Init)
		f.buf.WriteString("; ")
	} else {
		f.buf.WriteString("; ")
	}

	if stmt.Condition != nil {
		f.formatExpression(stmt.Condition)
	}
	f.buf.WriteString("; ")

	if stmt.Update != nil {
		f.formatStatement(stmt.Update)
	}
	f.buf.WriteString(")")

	if len(stmt.Body) > 0 {
		f.buf.WriteString(" {\n")
		f.indent++
		for _, bodyStmt := range stmt.Body {
			f.writeIndent()
			f.formatStatement(bodyStmt)
			f.buf.WriteString("\n")
		}
		f.indent--
		f.writeIndent()
		f.buf.WriteString("}")
	}
}

func (f *Formatter) formatVariableDeclaration(stmt *ast.VariableDeclaration) {
	if stmt.IsExported {
		f.buf.WriteString("export ")
	} else if stmt.IsPublic {
		f.buf.WriteString("pub ")
	}
	if stmt.IsStatic {
		f.buf.WriteString("static ")
	}
	if stmt.IsConst {
		f.buf.WriteString("const ")
	}
	baseType := declTypeString(stmt)
	if baseType != "" {
		f.buf.WriteString(baseType + " ")
	} else if stmt.IsAuto && !stmt.IsTryAssign {
		// := 赋值的类型由右侧 Ok 变体推断，书写形式里没有 auto 前缀
		f.buf.WriteString("auto ")
	}
	f.buf.WriteString(stmt.Name)
	f.buf.WriteString(declTypeSuffix(stmt.Type, baseType))
	if stmt.Nullable {
		f.buf.WriteString("?")
	}
	// := 错误传播赋值。
	// parser 对 `x := f()` 同时置 IsAuto 与 IsTryAssign，
	// 若按 IsAuto 输出 `auto x = ...` 就丢掉了 := 语义
	//（类型推断来源不同，且右侧 Result enum 的错误传播行为会消失）。
	if stmt.IsTryAssign {
		f.buf.WriteString(" := ")
		f.formatExpression(stmt.Value)
	} else if stmt.Value != nil {
		f.buf.WriteString(" = ")
		f.formatExpression(stmt.Value)
	}
	// 同语句内逗号分隔的后续声明符：i64 a = 1, b = 2
	for _, more := range stmt.More {
		f.buf.WriteString(", ")
		f.buf.WriteString(more.Name)
		f.buf.WriteString(declTypeSuffix(more.Type, baseType))
		if more.Nullable {
			f.buf.WriteString("?")
		}
		if more.Value != nil {
			f.buf.WriteString(" = ")
			f.formatExpression(more.Value)
		}
	}
}

// declTypeString 返回声明语句书写用的类型名。
// 多变量声明共享 BaseType（各声明符自身的后缀另行拼接），单变量声明用 Type。
func declTypeString(stmt *ast.VariableDeclaration) string {
	if len(stmt.More) > 0 && stmt.BaseType != "" {
		return stmt.BaseType
	}
	return stmt.Type
}

// declTypeSuffix 返回声明符完整类型相对共享类型书写之后的后缀（"*" 或 "[N]"）。
// 例：full="i64*" base="i64" → "*"；full="[4]i64" base="i64" → "[4]"。
func declTypeSuffix(full, base string) string {
	if base == "" || full == base {
		return ""
	}
	if strings.HasPrefix(full, base) {
		return full[len(base):]
	}
	// 数组后缀形式：[4]i64 相对 i64 书写为 name[4]
	if strings.HasPrefix(full, "[") {
		if i := strings.IndexByte(full, ']'); i > 0 && full[i+1:] == base {
			return full[:i+1]
		}
	}
	return ""
}

func (f *Formatter) formatReturnStatement(stmt *ast.ReturnStatement) {
	f.buf.WriteString("return")
	if stmt.Value != nil {
		f.buf.WriteString(" ")
		f.formatExpression(stmt.Value)
	}
}

func (f *Formatter) formatExpressionStatement(stmt *ast.ExpressionStatement) {
	f.formatExpression(stmt.Expression)
}

func (f *Formatter) formatBlockStatement(stmt *ast.BlockStatement) {
	f.buf.WriteString("{\n")
	f.indent++
	for _, bodyStmt := range stmt.Statements {
		f.writeIndent()
		f.formatStatement(bodyStmt)
		f.buf.WriteString("\n")
	}
	f.indent--
	f.writeIndent()
	f.buf.WriteString("}")
}

func (f *Formatter) formatSpendStatement(stmt *ast.SpendStatement) {
	f.buf.WriteString("spend ")
	if stmt.Target != nil {
		f.formatExpression(stmt.Target)
	}

	if len(stmt.Calls) > 0 {
		f.buf.WriteString(" {\n")
		f.indent++
		for _, call := range stmt.Calls {
			f.writeIndent()
			f.buf.WriteString("call ")
			if call.Index != nil {
				f.formatExpression(call.Index)
			}
			if len(call.Body) > 0 {
				f.buf.WriteString(" {\n")
				f.indent++
				for _, bodyStmt := range call.Body {
					f.writeIndent()
					f.formatStatement(bodyStmt)
					f.buf.WriteString("\n")
				}
				f.indent--
				f.writeIndent()
				f.buf.WriteString("}")
			}
			f.buf.WriteString("\n")
		}
		f.indent--
		f.writeIndent()
		f.buf.WriteString("}")
	}
}

func (f *Formatter) formatPrefixStatement(stmt *ast.PrefixStatement) {
	f.buf.WriteString("prefix ")
	f.buf.WriteString(stmt.Name)

	if len(stmt.Body) > 0 {
		f.buf.WriteString(" {\n")
		f.indent++
		for _, bodyStmt := range stmt.Body {
			f.writeIndent()
			f.formatStatement(bodyStmt)
			f.buf.WriteString("\n")
		}
		f.indent--
		f.writeIndent()
		f.buf.WriteString("}")
	}
}

func (f *Formatter) formatTreeStatement(stmt *ast.TreeStatement) {
	f.buf.WriteString("tree ")
	if stmt.Root != nil {
		f.formatExpression(stmt.Root)
	}
}

func (f *Formatter) formatObjectStatement(stmt *ast.ObjectStatement) {
	f.buf.WriteString("object ")
	if stmt.Type != "" {
		f.buf.WriteString(stmt.Type + " ")
	}
	f.buf.WriteString(stmt.Name)

	if len(stmt.Fields) > 0 || stmt.Value != nil {
		f.buf.WriteString(" {\n")
		f.indent++
		for _, field := range stmt.Fields {
			f.writeIndent()
			f.formatExpression(field)
			f.buf.WriteString("\n")
		}
		if stmt.Value != nil {
			f.writeIndent()
			f.buf.WriteString("value = ")
			f.formatExpression(stmt.Value)
			f.buf.WriteString("\n")
		}
		f.indent--
		f.writeIndent()
		f.buf.WriteString("}")
	}
}

func (f *Formatter) formatClassStatement(stmt *ast.ClassStatement) {
	if stmt.Generic {
		f.buf.WriteString("export ")
	}
	f.buf.WriteString("class ")
	f.buf.WriteString(stmt.Name)

	f.writeTypeParams(stmt.TypeParams)

	if len(stmt.Implements) > 0 {
		f.buf.WriteString(" implements ")
		for i, imp := range stmt.Implements {
			if i > 0 {
				f.buf.WriteString(", ")
			}
			f.buf.WriteString(imp)
		}
	}

	hasBody := len(stmt.Fields) > 0 || len(stmt.Methods) > 0 || len(stmt.Constructors) > 0
	if hasBody {
		f.buf.WriteString(" {\n")
		f.indent++
		for _, field := range stmt.Fields {
			f.writeIndent()
			f.buf.WriteString(field.Name)
			if field.Type != "" {
				f.buf.WriteString(": " + field.Type)
			}
			f.buf.WriteString("\n")
		}
		for _, method := range stmt.Methods {
			f.writeIndent()
			f.formatMethodStatement(method)
			f.buf.WriteString("\n")
		}
		for _, ctor := range stmt.Constructors {
			f.writeIndent()
			f.formatConstructorStatement(ctor)
			f.buf.WriteString("\n")
		}
		f.indent--
		f.writeIndent()
		f.buf.WriteString("}")
	} else {
		f.buf.WriteString(" {}")
	}
}

func (f *Formatter) formatInterfaceStatement(stmt *ast.InterfaceStatement) {
	f.buf.WriteString("interface ")
	f.buf.WriteString(stmt.Name)

	if len(stmt.Methods) > 0 {
		f.buf.WriteString(" {\n")
		f.indent++
		for _, method := range stmt.Methods {
			f.writeIndent()
			f.buf.WriteString("fn ")
			f.buf.WriteString(method.Name)
			f.buf.WriteString("(")
			for i, param := range method.Params {
				if i > 0 {
					f.buf.WriteString(", ")
				}
				f.buf.WriteString(param.Name)
				if param.Type != "" {
					f.buf.WriteString(": " + param.Type)
				}
			}
			f.buf.WriteString(")")
			if method.ReturnType != "" {
				f.buf.WriteString(" -> " + method.ReturnType)
			}
			f.buf.WriteString("\n")
		}
		f.indent--
		f.writeIndent()
		f.buf.WriteString("}")
	} else {
		f.buf.WriteString(" {}")
	}
}

func (f *Formatter) formatStructStatement(stmt *ast.StructStatement) {
	f.buf.WriteString("struct ")
	f.buf.WriteString(stmt.Name)

	if len(stmt.Fields) > 0 {
		f.buf.WriteString(" {\n")
		f.indent++
		for _, field := range stmt.Fields {
			f.writeIndent()
			f.buf.WriteString(field.Name)
			if field.Type != "" {
				f.buf.WriteString(": " + field.Type)
			}
			f.buf.WriteString("\n")
		}
		f.indent--
		f.writeIndent()
		f.buf.WriteString("}")
	} else {
		f.buf.WriteString(" {}")
	}
}

func (f *Formatter) formatTypeAliasStatement(stmt *ast.TypeAliasStatement) {
	f.buf.WriteString("type ")
	if stmt.Generic {
		f.buf.WriteString("export ")
	}
	f.buf.WriteString(stmt.Name)
	f.writeTypeParams(stmt.TypeParams)
	if stmt.UnderlyingType != "" {
		f.buf.WriteString(" = " + stmt.UnderlyingType)
	}
}

func (f *Formatter) formatImportStatement(stmt *ast.ImportStatement) {
	if stmt.Path != "" {
		f.buf.WriteString(`import "` + stmt.Path + `"`)
		return
	}
	f.buf.WriteString("import " + stmt.Module)
}

func (f *Formatter) formatExportStatement(stmt *ast.ExportStatement) {
	f.buf.WriteString("export " + stmt.Name)
}

func (f *Formatter) formatNonLocalStatement(stmt *ast.NonLocalStatement) {
	f.buf.WriteString(stmt.Type + " " + stmt.Name)
	if stmt.Value != nil {
		f.buf.WriteString(" = ")
		f.formatExpression(stmt.Value)
	}
}

func (f *Formatter) formatCallStatementStmt(stmt *ast.CallStatement) {
	f.buf.WriteString("call")
	if stmt.Target != nil {
		f.buf.WriteString(" ")
		f.formatExpression(stmt.Target)
	}
}

func (f *Formatter) formatMethodStatement(stmt *ast.MethodStatement) {
	f.buf.WriteString("fn ")
	f.buf.WriteString(stmt.Name)
	f.buf.WriteString("(")
	for i, param := range stmt.Params {
		if i > 0 {
			f.buf.WriteString(", ")
		}
		if param.Type != "" {
			f.buf.WriteString(param.Type)
			f.buf.WriteString(" ")
		}
		f.buf.WriteString(param.Name)
	}
	f.buf.WriteString(")")
	if stmt.ReturnType != "" {
		f.buf.WriteString(" " + stmt.ReturnType)
	}
	if len(stmt.Body) > 0 {
		f.buf.WriteString(" {\n")
		f.indent++
		for _, bodyStmt := range stmt.Body {
			f.writeIndent()
			f.formatStatement(bodyStmt)
			f.buf.WriteString("\n")
		}
		f.indent--
		f.writeIndent()
		f.buf.WriteString("}")
	}
}

func (f *Formatter) formatConstructorStatement(stmt *ast.ConstructorStatement) {
	f.buf.WriteString("constructor(")
	for i, param := range stmt.Params {
		if i > 0 {
			f.buf.WriteString(", ")
		}
		if param.Type != "" {
			f.buf.WriteString(param.Type)
			f.buf.WriteString(" ")
		}
		f.buf.WriteString(param.Name)
	}
	f.buf.WriteString(")")
	if len(stmt.Body) > 0 {
		f.buf.WriteString(" {\n")
		f.indent++
		for _, bodyStmt := range stmt.Body {
			f.writeIndent()
			f.formatStatement(bodyStmt)
			f.buf.WriteString("\n")
		}
		f.indent--
		f.writeIndent()
		f.buf.WriteString("}")
	}
}

func (f *Formatter) formatForInStatement(stmt *ast.ForInStatement) {
	f.buf.WriteString("for ")
	if stmt.Variable != nil {
		f.buf.WriteString(stmt.Variable.Name)
	}
	f.buf.WriteString(" in ")
	f.formatExpression(stmt.Iterable)
	if len(stmt.Body) > 0 {
		f.buf.WriteString(" {\n")
		f.indent++
		for _, bodyStmt := range stmt.Body {
			f.writeIndent()
			f.formatStatement(bodyStmt)
			f.buf.WriteString("\n")
		}
		f.indent--
		f.writeIndent()
		f.buf.WriteString("}")
	}
}

func (f *Formatter) formatYieldStatement(stmt *ast.YieldStatement) {
	f.buf.WriteString("yield ")
	f.formatExpression(stmt.Source)
	if stmt.Target != "" {
		f.buf.WriteString(" -> " + stmt.Target)
	}
}

func (f *Formatter) formatReleaseStatement(stmt *ast.ReleaseStatement) {
	f.buf.WriteString("release ")
	f.formatExpression(stmt.Source)
	f.buf.WriteString(" -> [")
	for i, h := range stmt.Holders {
		if i > 0 {
			f.buf.WriteString(", ")
		}
		f.buf.WriteString(h)
	}
	f.buf.WriteString("]")
}

func (f *Formatter) formatExtractStatement(stmt *ast.ExtractStatement) {
	f.buf.WriteString("extract ")
	f.formatExpression(stmt.Source)
	if stmt.Index != nil {
		f.buf.WriteString("[")
		f.formatExpression(stmt.Index)
		f.buf.WriteString("]")
	}
	if stmt.Target != "" {
		f.buf.WriteString(" -> " + stmt.Target)
	}
}

func (f *Formatter) formatExternStatement(stmt *ast.ExternStatement) {
	f.buf.WriteString("extern ")
	if stmt.IsFunction {
		f.buf.WriteString("fn " + stmt.Name + "(")
		for i, param := range stmt.Params {
			if i > 0 {
				f.buf.WriteString(", ")
			}
			f.buf.WriteString(param)
			if i < len(stmt.ParamTypes) && stmt.ParamTypes[i] != "" {
				f.buf.WriteString(": " + stmt.ParamTypes[i])
			}
		}
		f.buf.WriteString(")")
		if stmt.ReturnType != "" && stmt.ReturnType != "void" {
			f.buf.WriteString(" -> " + stmt.ReturnType)
		}
	} else {
		f.buf.WriteString(stmt.Name + ": " + stmt.Type)
	}
}

func (f *Formatter) formatEnumStatement(stmt *ast.EnumStatement) {
	f.buf.WriteString("enum ")
	f.buf.WriteString(stmt.Name)
	f.writeTypeParams(stmt.TypeParams)
	f.buf.WriteString(" {")
	if len(stmt.Variants) > 0 {
		f.buf.WriteString("\n")
		f.indent++
		for _, v := range stmt.Variants {
			f.writeIndent()
			f.buf.WriteString(v.Name)
			if len(v.FieldTypes) > 0 {
				f.buf.WriteString("(")
				for i, ft := range v.FieldTypes {
					if i > 0 {
						f.buf.WriteString(", ")
					}
					f.buf.WriteString(ft)
				}
				f.buf.WriteString(")")
			}
			f.buf.WriteString("\n")
		}
		f.indent--
		f.writeIndent()
	}
	f.buf.WriteString("}")
}

func (f *Formatter) formatStructLiteral(expr *ast.StructLiteral) {
	f.buf.WriteString("{ ")
	for i, field := range expr.Fields {
		if i > 0 {
			f.buf.WriteString(", ")
		}
		f.buf.WriteString("." + field.Name)
		if field.Value != nil {
			f.buf.WriteString(" = ")
			f.formatExpression(field.Value)
		}
	}
	f.buf.WriteString(" }")
}

func (f *Formatter) formatObjectLiteral(expr *ast.ObjectLiteral) {
	f.buf.WriteString("object {")
	for i, field := range expr.Fields {
		if i > 0 {
			f.buf.WriteString(", ")
		}
		f.buf.WriteString(" " + field.Name + ": ")
		f.formatExpression(field.Value)
	}
	if len(expr.Fields) > 0 {
		f.buf.WriteString(" ")
	}
	f.buf.WriteString("}")
}

func (f *Formatter) formatLambdaExpression(expr *ast.LambdaExpression) {
	f.buf.WriteString("fn(")
	for i, param := range expr.Params {
		if i > 0 {
			f.buf.WriteString(", ")
		}
		f.buf.WriteString(param)
		if i < len(expr.ParamTypes) && expr.ParamTypes[i] != "" && expr.ParamTypes[i] != "auto" {
			f.buf.WriteString(": " + expr.ParamTypes[i])
		}
	}
	f.buf.WriteString(")")
	if expr.ReturnType != "" {
		f.buf.WriteString(" -> " + expr.ReturnType)
	}
	if len(expr.Body) > 0 {
		f.buf.WriteString(" {\n")
		f.indent++
		for _, bodyStmt := range expr.Body {
			f.writeIndent()
			f.formatStatement(bodyStmt)
			f.buf.WriteString("\n")
		}
		f.indent--
		f.writeIndent()
		f.buf.WriteString("}")
	} else {
		f.buf.WriteString(" {}")
	}
}

func (f *Formatter) formatMatchExpression(expr *ast.MatchExpression) {
	f.buf.WriteString("match(")
	f.formatExpression(expr.Target)
	f.buf.WriteString(") {\n")
	f.indent++
	for _, arm := range expr.Arms {
		f.writeIndent()
		f.formatMatchPattern(arm.Pattern)
		f.buf.WriteString(" => ")
		if len(arm.Body) > 0 {
			if len(arm.Body) == 1 {
				f.formatStatement(arm.Body[0])
			} else {
				f.buf.WriteString("{\n")
				f.indent++
				for _, bodyStmt := range arm.Body {
					f.writeIndent()
					f.formatStatement(bodyStmt)
					f.buf.WriteString("\n")
				}
				f.indent--
				f.writeIndent()
				f.buf.WriteString("}")
			}
		}
		f.buf.WriteString("\n")
	}
	f.indent--
	f.writeIndent()
	f.buf.WriteString("}")
}

func (f *Formatter) formatMatchPattern(p *ast.MatchPattern) {
	if p == nil {
		f.buf.WriteString("_")
		return
	}
	switch p.Kind {
	case ast.PatternWildcard:
		f.buf.WriteString("_")
	case ast.PatternVariant:
		f.buf.WriteString(p.VariantName)
		if len(p.Bindings) > 0 {
			f.buf.WriteString("(")
			for i, b := range p.Bindings {
				if i > 0 {
					f.buf.WriteString(", ")
				}
				f.buf.WriteString(b)
			}
			f.buf.WriteString(")")
		}
	case ast.PatternInteger:
		f.buf.WriteString(strconv.FormatInt(p.IntValue, 10))
	case ast.PatternString:
		f.buf.WriteString(`"` + p.StrValue + `"`)
	case ast.PatternVariable:
		f.buf.WriteString(strings.Join(p.Bindings, ", "))
	case ast.PatternBoolean:
		f.buf.WriteString(p.VariantName)
	default:
		f.buf.WriteString("_")
	}
}

func (f *Formatter) formatAttributeExpression(expr *ast.AttributeExpression) {
	if expr.Attr == nil {
		f.buf.WriteString("#[]")
		return
	}
	f.buf.WriteString("#[" + expr.Attr.Name)
	if len(expr.Attr.Args) > 0 {
		f.buf.WriteString("(")
		f.buf.WriteString(strings.Join(expr.Attr.Args, ", "))
		f.buf.WriteString(")")
	}
	f.buf.WriteString("]")
}

func (f *Formatter) formatPrefixCallExpression(expr *ast.PrefixCallExpression) {
	f.buf.WriteString("@")
	f.buf.WriteString(expr.Name)
	if len(expr.Params) > 0 {
		f.buf.WriteString("(")
		first := true
		for name, val := range expr.Params {
			if !first {
				f.buf.WriteString(", ")
			}
			f.buf.WriteString(name + " = ")
			f.formatExpression(val)
			first = false
		}
		f.buf.WriteString(")")
	}
	if len(expr.Body) > 0 {
		f.buf.WriteString(" {\n")
		f.indent++
		for _, bodyStmt := range expr.Body {
			f.writeIndent()
			f.formatStatement(bodyStmt)
			f.buf.WriteString("\n")
		}
		f.indent--
		f.writeIndent()
		f.buf.WriteString("}")
	}
}

func (f *Formatter) formatBinaryExpression(expr *ast.BinaryExpression) {
	f.formatBinaryOperand(expr.Left, expr.Operator, true)
	f.buf.WriteString(" " + expr.Operator + " ")
	f.formatBinaryOperand(expr.Right, expr.Operator, false)
}

func binOpPrec(op string) int {
	switch op {
	case "=":
		return 1
	case "||":
		return 2
	case "&&":
		return 3
	case "==", "!=", "^", "|":
		return 4
	case "<", ">", "<=", ">=", "<<", ">>", "&":
		return 5
	case "+", "-":
		return 6
	case "*", "/", "%":
		return 7
	}
	return 0
}

func (f *Formatter) formatBinaryOperand(expr ast.Expression, parentOp string, isLeft bool) {
	if bin, ok := expr.(*ast.BinaryExpression); ok {
		childPrec := binOpPrec(bin.Operator)
		parentPrec := binOpPrec(parentOp)
		needParen := childPrec < parentPrec
		if !isLeft && childPrec == parentPrec {
			needParen = true
		}
		if needParen {
			f.buf.WriteString("(")
			f.formatExpression(expr)
			f.buf.WriteString(")")
			return
		}
	}
	f.formatExpression(expr)
}

func (f *Formatter) formatCallExpression(expr *ast.CallExpression) {
	f.formatExpression(expr.Function)
	f.buf.WriteString("(")
	for i, arg := range expr.Args {
		if i > 0 {
			f.buf.WriteString(", ")
		}
		f.formatExpression(arg)
	}
	f.buf.WriteString(")")
}

func (f *Formatter) writeIndent() {
	for i := 0; i < f.indent; i++ {
		f.buf.WriteString(f.indentStr)
	}
}

// formatIntegerLiteral 把任意整数型 interface{} 渲染成源码文本。
func formatIntegerLiteral(v interface{}) string {
	switch n := v.(type) {
	case int:
		return strconv.Itoa(n)
	case int8:
		return strconv.FormatInt(int64(n), 10)
	case int16:
		return strconv.FormatInt(int64(n), 10)
	case int32:
		return strconv.FormatInt(int64(n), 10)
	case int64:
		return strconv.FormatInt(n, 10)
	case uint:
		return strconv.FormatUint(uint64(n), 10)
	case uint8:
		return strconv.FormatUint(uint64(n), 10)
	case uint16:
		return strconv.FormatUint(uint64(n), 10)
	case uint32:
		return strconv.FormatUint(uint64(n), 10)
	case uint64:
		return strconv.FormatUint(n, 10)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// formatFloatLiteral 渲染浮点字面量。整数值补 ".0"，避免被当成整数类型。
func formatFloatLiteral(v interface{}) string {
	var s string
	switch n := v.(type) {
	case float32:
		s = strconv.FormatFloat(float64(n), 'g', -1, 32)
	case float64:
		s = strconv.FormatFloat(n, 'g', -1, 64)
	default:
		s = fmt.Sprintf("%v", v)
	}
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// formatStringLiteral 渲染字符串字面量。
func formatStringLiteral(v interface{}) string {
	if s, ok := v.(string); ok {
		return `"` + s + `"`
	}
	return `"` + fmt.Sprintf("%v", v) + `"`
}

// formatBoolLiteral 渲染布尔字面量。
func formatBoolLiteral(v interface{}) string {
	if b, ok := v.(bool); ok {
		if b {
			return "true"
		}
		return "false"
	}
	return fmt.Sprintf("%v", v)
}

// formatCharLiteral 渲染字符字面量。
func formatCharLiteral(v interface{}) string {
	if s, ok := v.(string); ok {
		return "'" + s + "'"
	}
	return "'" + fmt.Sprintf("%v", v) + "'"
}

// formatFallbackLiteral 是所有已知 Kind 都不匹配时的兜底渲染。
func formatFallbackLiteral(v interface{}) string {
	if v == nil {
		return "nil"
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// formatUnaryOperand 输出一元前缀运算符的操作数。
//
// 一元运算符（! - ~）的优先级高于所有二元运算符，
// 因此操作数只要是「复合表达式」就必须加括号：
//
//	!(5 < 3)   -> (!5) < 3   语义从 true 变成 false
//	-(a + b)   -> -a + b     语义完全改变
//
// 「复合」指二元、一元、条件、await/spawn 等一切非原子表达式。
func (f *Formatter) formatUnaryOperand(expr ast.Expression) {
	if isAtomicExpr(expr) {
		f.formatExpression(expr)
		return
	}
	f.buf.WriteString("(")
	f.formatExpression(expr)
	f.buf.WriteString(")")
}

// isAtomicExpr 判断表达式是否是「不需要括号即可安全嵌入更大表达式」的原子形式。
func isAtomicExpr(expr ast.Expression) bool {
	switch e := expr.(type) {
	case nil:
		return true
	case *ast.Identifier,
		*ast.IntegerLiteral,
		*ast.FloatLiteral,
		*ast.StringLiteral,
		*ast.BooleanLiteral,
		*ast.CharLiteral,
		*ast.LiteralExpression:
		return true
	case *ast.CallExpression:
		// 函数调用的调用者部分已经自带括号，成员访问链也是原子的
		return isAtomicExpr(e.Function)
	case *ast.MemberExpression:
		return isAtomicExpr(e.Object)
	case *ast.MemberAccessExpression:
		return isAtomicExpr(e.Object)
	case *ast.IndexExpression:
		return isAtomicExpr(e.Object)
	case *ast.ParenExpression:
		// 源文件里已经带括号，原样保留即可
		return true
	default:
		_ = e
		return false
	}
}
