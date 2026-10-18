package formatter

import (
	"bytes"
	"kaula/internal/ast"
	"kaula/internal/lexer"
	"kaula/internal/parser"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

type Formatter struct {
	indent    int
	buf       bytes.Buffer
	indentStr string
}

func New() *Formatter {
	return &Formatter{
		indentStr: "    ",
	}
}

func (f *Formatter) FormatSource(source string) string {
	f.reset()
	lex := lexer.NewLexer(source)
	p := parser.NewParser(lex)
	p.SetFile("<fmt>")
	p.SetSkipMainCheck(true)
	program := p.Parse()
	return f.FormatProgram(program)
}

func (f *Formatter) FormatFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return f.FormatSource(string(data)), nil
}

func (f *Formatter) FormatFileInPlace(path string) error {
	formatted, err := f.FormatFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(formatted+"\n"), 0644)
}

func (f *Formatter) reset() {
	f.indent = 0
	f.buf.Reset()
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
		f.formatExpression(e.Right)
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
	default:
		f.buf.WriteString(fmt.Sprintf("/* unknown expression: %T */", expr))
	}
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

	if stmt.Generic || stmt.IsExported {
		f.buf.WriteString("export ")
	}

	f.buf.WriteString("fn ")
	f.buf.WriteString(stmt.Name)

	if len(stmt.TypeParams) > 0 {
		f.buf.WriteString("[")
		for i, tp := range stmt.TypeParams {
			if i > 0 {
				f.buf.WriteString(", ")
			}
			if tp != nil {
				f.buf.WriteString(tp.Name)
				if tp.Constraint != "" {
					f.buf.WriteString(": " + tp.Constraint)
				}
			}
		}
		f.buf.WriteString("]")
	}

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
	if stmt.Type != "" {
		f.buf.WriteString(stmt.Type + " ")
	} else if stmt.IsAuto {
		f.buf.WriteString("auto ")
	}
	f.buf.WriteString(stmt.Name)
	if stmt.Nullable {
		f.buf.WriteString("?")
	}
	if stmt.Value != nil {
		f.buf.WriteString(" = ")
		f.formatExpression(stmt.Value)
	}
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

	if len(stmt.TypeParams) > 0 {
		f.buf.WriteString("[")
		for i, tp := range stmt.TypeParams {
			if i > 0 {
				f.buf.WriteString(", ")
			}
			if tp != nil {
				f.buf.WriteString(tp.Name)
			}
		}
		f.buf.WriteString("]")
	}

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
	if len(stmt.TypeParams) > 0 {
		f.buf.WriteString("[")
		for i, tp := range stmt.TypeParams {
			if i > 0 {
				f.buf.WriteString(", ")
			}
			if tp != nil {
				f.buf.WriteString(tp.Name)
			}
		}
		f.buf.WriteString("]")
	}
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
	if len(stmt.TypeParams) > 0 {
		f.buf.WriteString("[")
		for i, tp := range stmt.TypeParams {
			if i > 0 {
				f.buf.WriteString(", ")
			}
			if tp != nil {
				f.buf.WriteString(tp.Name)
				if tp.Constraint != "" {
					f.buf.WriteString(": " + tp.Constraint)
				}
			}
		}
		f.buf.WriteString("]")
	}
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
