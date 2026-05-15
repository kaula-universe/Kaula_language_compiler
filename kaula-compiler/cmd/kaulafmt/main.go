package main

import (
	"bytes"
	"fmt"
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/lexer"
	"kaula-compiler/internal/parser"
	"os"
	"path/filepath"
	"strconv"
)

type Formatter struct {
	indent    int
	buf       bytes.Buffer
	indentStr string
}

func NewFormatter() *Formatter {
	return &Formatter{
		indentStr: "    ",
	}
}

func (f *Formatter) FormatProgram(program *ast.Program) string {
	for i, stmt := range program.Statements {
		if i > 0 {
			f.buf.WriteString("\n\n")
		}
		f.formatStatement(stmt)
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
	case *ast.VOStatement:
		f.formatVOStatement(s)
	case *ast.SpendStatement:
		f.formatSpendStatement(s)
	case *ast.TaskStatement:
		f.formatTaskStatement(s)
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
	case *ast.SwitchStatement:
		f.formatSwitchStatement(s)
	case *ast.ExportStatement:
		f.formatExportStatement(s)
	case *ast.NonLocalStatement:
		f.formatNonLocalStatement(s)
	case *ast.CaseStatement:
		f.formatCaseStatement(s)
	case *ast.CallStatement:
		f.formatCallStatementStmt(s)
	case *ast.MethodStatement:
		f.formatMethodStatement(s)
	case *ast.ConstructorStatement:
		f.formatConstructorStatement(s)
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
		f.buf.WriteString(strconv.FormatInt(e.Value, 10))
	case *ast.FloatLiteral:
		f.buf.WriteString(strconv.FormatFloat(e.Value, 'g', -1, 64))
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
	case *ast.PrefixCallExpression:
		f.buf.WriteString("$")
		f.buf.WriteString(e.Name)
		f.buf.WriteString("{...}")
	case *ast.TypeCastExpression:
		f.buf.WriteString("(" + e.TargetType + ")(")
		f.formatExpression(e.Expression)
		f.buf.WriteString(")")
	default:
		f.buf.WriteString(fmt.Sprintf("/* unknown expression: %T */", expr))
	}
}

func (f *Formatter) formatFunctionStatement(stmt *ast.FunctionStatement) {
	if stmt.Annotation != ast.TreeAnnotationNone {
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

	if stmt.Generic {
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
	}
	f.buf.WriteString(")")

	if stmt.ReturnType != "" {
		f.buf.WriteString(" -> " + stmt.ReturnType)
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
	f.buf.WriteString("if ")
	f.formatExpression(stmt.Condition)

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
	f.buf.WriteString("while ")
	f.formatExpression(stmt.Condition)

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
	f.buf.WriteString("for ")
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

func (f *Formatter) formatVOStatement(stmt *ast.VOStatement) {
	f.buf.WriteString("vo")
	
	if stmt.Value != nil {
		f.buf.WriteString(" {\n")
		f.indent++
		f.writeIndent()
		f.formatExpression(stmt.Value)
		f.buf.WriteString("\n")
		f.indent--
		f.writeIndent()
		f.buf.WriteString("}")
	}
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

func (f *Formatter) formatTaskStatement(stmt *ast.TaskStatement) {
	f.buf.WriteString("task")
	if stmt.Func != nil {
		f.buf.WriteString(" ")
		f.formatExpression(stmt.Func)
	}
	if stmt.Arg != nil {
		f.buf.WriteString(" ")
		f.formatExpression(stmt.Arg)
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
	f.buf.WriteString("import " + `"` + stmt.Module + `"`)
}

func (f *Formatter) formatSwitchStatement(stmt *ast.SwitchStatement) {
	f.buf.WriteString("switch ")
	f.formatExpression(stmt.Expression)

	if len(stmt.Cases) > 0 {
		f.buf.WriteString(" {\n")
		f.indent++
		for _, caseStmt := range stmt.Cases {
			f.writeIndent()
			if caseStmt.Value != nil {
				f.buf.WriteString("case ")
				f.formatExpression(caseStmt.Value)
				f.buf.WriteString(":\n")
			} else {
				f.buf.WriteString("default:\n")
			}
			f.indent++
			for _, bodyStmt := range caseStmt.Body {
				f.writeIndent()
				f.formatStatement(bodyStmt)
				f.buf.WriteString("\n")
			}
			f.indent--
		}
		f.indent--
		f.writeIndent()
		f.buf.WriteString("}")
	}
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

func (f *Formatter) formatCaseStatement(stmt *ast.CaseStatement) {
	if stmt.Value != nil {
		f.buf.WriteString("case ")
		f.formatExpression(stmt.Value)
		f.buf.WriteString(":\n")
	} else {
		f.buf.WriteString("default:\n")
	}
	f.indent++
	for _, bodyStmt := range stmt.Body {
		f.writeIndent()
		f.formatStatement(bodyStmt)
		f.buf.WriteString("\n")
	}
	f.indent--
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
		f.buf.WriteString(param.Name)
		if param.Type != "" {
			f.buf.WriteString(": " + param.Type)
		}
	}
	f.buf.WriteString(")")
	if stmt.ReturnType != "" {
		f.buf.WriteString(" -> " + stmt.ReturnType)
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
		f.buf.WriteString(param.Name)
		if param.Type != "" {
			f.buf.WriteString(": " + param.Type)
		}
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

func (f *Formatter) formatBinaryExpression(expr *ast.BinaryExpression) {
	f.formatExpression(expr.Left)
	f.buf.WriteString(" " + expr.Operator + " ")
	f.formatExpression(expr.Right)
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

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <input file.kl>\n", os.Args[0])
		fmt.Printf("       %s -w <input file.kl> (write back to file)\n", os.Args[0])
		os.Exit(1)
	}

	writeBack := false
	inputFile := ""

	for _, arg := range os.Args[1:] {
		switch arg {
		case "-w":
			writeBack = true
		default:
			if len(arg) > 0 && arg[0] != '-' {
				inputFile = arg
			}
		}
	}

	if inputFile == "" {
		fmt.Printf("Error: No input file specified\n")
		os.Exit(1)
	}

	if len(inputFile) < 4 || inputFile[len(inputFile)-3:] != ".kl" {
		fmt.Printf("Error: Input file must have .kl extension\n")
		os.Exit(1)
	}

	data, err := os.ReadFile(inputFile)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}
	source := string(data)

	lex := lexer.NewLexer(source)
	p := parser.NewParser(lex)
	p.SetFile(inputFile)
	program := p.Parse()

	if p.HasErrors() {
		fmt.Printf("Error: Failed to parse %s\n", filepath.Base(inputFile))
		os.Exit(1)
	}

	formatter := NewFormatter()
	formatted := formatter.FormatProgram(program)

	if writeBack {
		err := os.WriteFile(inputFile, []byte(formatted+"\n"), 0644)
		if err != nil {
			fmt.Printf("Error writing file: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Formatted %s\n", inputFile)
	} else {
		fmt.Println(formatted)
	}
}
