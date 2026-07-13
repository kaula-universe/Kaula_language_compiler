package sor

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"strings"
)

// ASTProgramAdapter 将 Kaula AST 适配为 SOR 分析器所需的接口
type ASTProgramAdapter struct {
	program *ast.Program
	stmts   []Stmt
}

// NewASTProgramAdapter 从 AST 创建适配器，提取 SOR 语句
func NewASTProgramAdapter(program *ast.Program) *ASTProgramAdapter {
	return &ASTProgramAdapter{
		program: program,
		stmts:   extractStmtsFromAST(program),
	}
}

// GetStmts 返回 SOR 语句列表
func (a *ASTProgramAdapter) GetStmts() []Stmt {
	return a.stmts
}

// GetProgram 返回原始 AST
func (a *ASTProgramAdapter) GetProgram() *ast.Program {
	return a.program
}

// AnalyzeAST 从 Kaula AST 中提取 SOR 相关语句并进行分析
func AnalyzeAST(program *ast.Program) ([]SORError, []string) {
	analyzer := NewSORAnalyzer()
	stmts := extractStmtsFromAST(program)
	errors := analyzer.Analyze(stmts)
	return errors, analyzer.GetExecLog()
}

// AnalyzeFullFromAST 从 Kaula AST 运行完整 SOR 分析流水线
func AnalyzeFullFromAST(program *ast.Program) *FullAnalysisResult {
	adapter := NewASTProgramAdapter(program)
	return AnalyzeFullWithAST(adapter, program)
}

// RegisterStructsFromAST 遍历 AST，将所有结构体和类的字段类型注册到 TypeSizer
// 使 SizeOf 能正确估算自定义结构体大小，从而精确路由到 KMM arena
func RegisterStructsFromAST(program *ast.Program, ts *TypeSizer) {
	if program == nil || ts == nil {
		return
	}
	for _, s := range program.Statements {
		if s == nil {
			continue
		}
		switch stmt := s.(type) {
		case *ast.StructStatement:
			if stmt.Name == "" || len(stmt.Fields) == 0 {
				continue
			}
			fieldTypes := make([]string, 0, len(stmt.Fields))
			for _, f := range stmt.Fields {
				if f != nil {
					fieldTypes = append(fieldTypes, f.Type)
				}
			}
			ts.RegisterStructFields(stmt.Name, fieldTypes)
		case *ast.ClassStatement:
			if stmt.Name == "" || len(stmt.Fields) == 0 {
				continue
			}
			fieldTypes := make([]string, 0, len(stmt.Fields))
			for _, f := range stmt.Fields {
				if f != nil {
					fieldTypes = append(fieldTypes, f.Type)
				}
			}
			ts.RegisterStructFields(stmt.Name, fieldTypes)
		}
	}
}

// extractStmtsFromAST 从 Kaula AST 中提取 SOR 语句
func extractStmtsFromAST(program *ast.Program) []Stmt {
	var stmts []Stmt
	if program == nil {
		return stmts
	}
	for _, s := range program.Statements {
		if s != nil {
			extractStatement(s, &stmts)
		}
	}
	return stmts
}

func extractStatement(s ast.Statement, stmts *[]Stmt) {
	if s == nil {
		return
	}

	// 使用 nil-safe 的类型断言
	switch stmt := s.(type) {
	case nil:
		return

	case *ast.VariableDeclaration:
		if stmt == nil {
			return
		}
		isComposite := false
		typeName := ""
		if stmt.Type != "" {
			typeName = stmt.Type
			isComposite = strings.HasPrefix(typeName, "[]") || isCompositeType(typeName)
		}
		// 检查初始值表达式是否包含索引访问（用于 extract 追踪）
		srcName := stmt.Name
		if stmt.Value != nil {
			if _, ok := stmt.Value.(*ast.ArrayLiteral); ok {
				isComposite = true
			}
		}
		_ = typeName
		*stmts = append(*stmts, LetStmt(stmt.Pos.Line, stmt.Name+" = ...", srcName, stmt.Type, isComposite))

	case *ast.YieldStatement:
		if stmt == nil {
			return
		}
		srcName := getExprName(stmt.Source)
		*stmts = append(*stmts, YieldStmt(stmt.Pos.Line,
			"yield "+srcName+" -> "+stmt.Target, srcName, stmt.Target))

	case *ast.ReleaseStatement:
		if stmt == nil {
			return
		}
		srcName := getExprName(stmt.Source)
		*stmts = append(*stmts, ReleaseStmt(stmt.Pos.Line,
			"release "+srcName, srcName, stmt.Holders...))

	case *ast.ExtractStatement:
		if stmt == nil {
			return
		}
		// 从 Source 表达式中提取真正的对象名和索引
		srcName := ""
		childPath := "[0]"
		// parsePrimaryExpressionIterative 会将 data[2] 解析为 IndexExpression
		if idxExpr, ok := stmt.Source.(*ast.IndexExpression); ok {
			srcName = getExprName(idxExpr.Object)
			if idxExpr.Index != nil {
				if idxLit, ok := idxExpr.Index.(*ast.IntegerLiteral); ok {
					childPath = fmt.Sprintf("[%d]", idxLit.Value)
				} else if ident, ok := idxExpr.Index.(*ast.Identifier); ok {
					childPath = "[" + ident.Name + "]"
				}
			}
		} else {
			srcName = getExprName(stmt.Source)
			if stmt.Index != nil {
				if idxLit, ok := stmt.Index.(*ast.IntegerLiteral); ok {
					childPath = fmt.Sprintf("[%d]", idxLit.Value)
				} else if ident, ok := stmt.Index.(*ast.Identifier); ok {
					childPath = "[" + ident.Name + "]"
				}
			}
		}
		*stmts = append(*stmts, ExtractStmt(stmt.Pos.Line,
			"extract "+srcName+childPath+" -> "+stmt.Target, srcName, childPath, stmt.Target))

	case *ast.ExpressionStatement:
		extractExprStmt(stmt, stmts)

	case *ast.IfStatement:
		if stmt == nil {
			return
		}
		// 插入分支入口标记
		*stmts = append(*stmts, BranchEnterStmt(stmt.Pos.Line, "if (...)"))
		for _, bodyStmt := range stmt.Body {
			extractStatement(bodyStmt, stmts)
		}
		if len(stmt.Else) > 0 {
			// 插入 else 标记
			*stmts = append(*stmts, BranchElseStmt(stmt.Pos.Line, "else"))
			for _, elseStmt := range stmt.Else {
				extractStatement(elseStmt, stmts)
			}
		}
		// 插入分支出口标记
		*stmts = append(*stmts, BranchExitStmt(stmt.Pos.Line, "endif"))

	case *ast.WhileStatement:
		if stmt == nil {
			return
		}
		// 插入循环入口标记（迭代次数未知，传 0）
		*stmts = append(*stmts, LoopEnterStmt(stmt.Pos.Line, "while (...)", 0))
		for _, bodyStmt := range stmt.Body {
			extractStatement(bodyStmt, stmts)
		}
		// 插入循环出口标记
		*stmts = append(*stmts, LoopExitStmt(stmt.Pos.Line, "endwhile"))

	case *ast.ForStatement:
		if stmt == nil {
			return
		}
		if stmt.Init != nil {
			extractStatement(stmt.Init, stmts)
		}
		// 尝试静态推断迭代次数
		iterCount := estimateForLoopIterCount(stmt)
		// 插入循环入口标记
		*stmts = append(*stmts, LoopEnterStmt(stmt.Pos.Line, "for (...)", iterCount))
		for _, bodyStmt := range stmt.Body {
			extractStatement(bodyStmt, stmts)
		}
		// 插入循环出口标记
		*stmts = append(*stmts, LoopExitStmt(stmt.Pos.Line, "endfor"))

	case *ast.FunctionStatement:
		if stmt == nil {
			return
		}
		for _, bodyStmt := range stmt.Body {
			extractStatement(bodyStmt, stmts)
		}

	case *ast.PrefixStatement:
		if stmt == nil {
			return
		}
		for _, bodyStmt := range stmt.Body {
			extractStatement(bodyStmt, stmts)
		}

	default:
		// 未知语句类型，安全跳过
		return
	}
}

func extractExprStmt(stmt *ast.ExpressionStatement, stmts *[]Stmt) {
	if stmt == nil || stmt.Expression == nil {
		return
	}
	switch expr := stmt.Expression.(type) {
	case nil:
		return
	case *ast.CallExpression:
		extractCallExpr(expr, stmts)
	case *ast.BinaryExpression:
		extractBinaryExpr(expr, stmts)
	case *ast.Identifier:
		*stmts = append(*stmts, ReadStmt(stmt.Pos.Line, expr.Name, expr.Name))
	case *ast.IndexExpression:
		if expr != nil && expr.Object != nil {
			if ident, ok := expr.Object.(*ast.Identifier); ok {
				*stmts = append(*stmts, ReadStmt(stmt.Pos.Line, ident.Name+"[...]", ident.Name))
			}
		}
	}
}

func extractCallExpr(expr *ast.CallExpression, stmts *[]Stmt) {
	if expr == nil || expr.Function == nil {
		return
	}
	funcName := ""
	switch fn := expr.Function.(type) {
	case *ast.Identifier:
		funcName = fn.Name
	case *ast.MemberAccessExpression:
		if fn != nil {
			funcName = fn.Member
		}
	}

	// 检查是否是 SOR 原语调用
	switch funcName {
	case "yield":
		if len(expr.Args) >= 2 {
			if src, ok := expr.Args[0].(*ast.Identifier); ok {
				if dst, ok := expr.Args[1].(*ast.Identifier); ok {
					*stmts = append(*stmts, YieldStmt(expr.Pos.Line, "yield "+src.Name+" -> "+dst.Name, src.Name, dst.Name))
				}
			}
		}
	case "release":
		if len(expr.Args) >= 2 {
			if src, ok := expr.Args[0].(*ast.Identifier); ok {
				var holders []string
				if arr, ok := expr.Args[1].(*ast.ArrayLiteral); ok {
					for _, elem := range arr.Elements {
						if ident, ok := elem.(*ast.Identifier); ok {
							holders = append(holders, ident.Name)
						}
					}
				}
				*stmts = append(*stmts, ReleaseStmt(expr.Pos.Line, "release "+src.Name, src.Name, holders...))
			}
		}
	case "union":
		// union release src -> [holders...]
		if len(expr.Args) >= 3 {
			if src, ok := expr.Args[1].(*ast.Identifier); ok {
				var holders []string
				if arr, ok := expr.Args[2].(*ast.ArrayLiteral); ok {
					for _, elem := range arr.Elements {
						if ident, ok := elem.(*ast.Identifier); ok {
							holders = append(holders, ident.Name)
						}
					}
				}
				if len(holders) > 0 {
					*stmts = append(*stmts, UnionReleaseStmt(expr.Pos.Line, "union release "+src.Name, src.Name, holders...))
				}
			}
		}
	case "extract":
		if len(expr.Args) >= 3 {
			if src, ok := expr.Args[0].(*ast.Identifier); ok {
				childPath := "[0]"
				if idx, ok := expr.Args[1].(*ast.IntegerLiteral); ok {
					childPath = fmt.Sprintf("[%d]", idx.Value)
				}
				if dst, ok := expr.Args[2].(*ast.Identifier); ok {
					*stmts = append(*stmts, ExtractStmt(expr.Pos.Line, "extract "+src.Name+childPath+" -> "+dst.Name, src.Name, childPath, dst.Name))
				}
			}
		}
	default:
		// 普通函数调用
		if funcName != "" && len(expr.Args) > 0 {
			var argNames []string
			for _, arg := range expr.Args {
				if ident, ok := arg.(*ast.Identifier); ok {
					argNames = append(argNames, ident.Name)
				}
			}
			if len(argNames) > 0 {
				*stmts = append(*stmts, CallStmt(expr.Pos.Line, funcName+"(...)", funcName, argNames, nil))
			}
		}
	}
}

func extractBinaryExpr(expr *ast.BinaryExpression, stmts *[]Stmt) {
	if expr == nil {
		return
	}
	if expr.Operator == "=" || expr.Operator == "ASSIGN" {
		if left, ok := expr.Left.(*ast.Identifier); ok {
			*stmts = append(*stmts, WriteStmt(expr.Pos.Line, left.Name+" = ...", left.Name))
		}
	}
}

func isCompositeType(typeName string) bool {
	compositeTypes := map[string]bool{
		"Array": true, "[]": true,
	}
	return compositeTypes[typeName] || strings.HasPrefix(typeName, "[]")
}

// estimateForLoopIterCount 尝试从 ForStatement 的 Condition 静态推断迭代次数
// 支持模式：for i := 0; i < N; i++  其中 N 为整数字面量
// 返回 0 表示无法确定
func estimateForLoopIterCount(stmt *ast.ForStatement) int {
	if stmt == nil || stmt.Condition == nil {
		return 0
	}
	// 期望 Condition 是 i < N 的比较表达式
	binExpr, ok := stmt.Condition.(*ast.BinaryExpression)
	if !ok {
		return 0
	}
	// 操作符必须是 < 或 <=
	if binExpr.Operator != "<" && binExpr.Operator != "LT" &&
		binExpr.Operator != "<=" && binExpr.Operator != "LE" {
		return 0
	}
	// 右侧应为整数字面量
	if rightLit, ok := binExpr.Right.(*ast.IntegerLiteral); ok {
		if rightLit.Value > 0 {
			n := int(rightLit.Value)
			// 如果是 <=，迭代次数为 N+1
			if binExpr.Operator == "<=" || binExpr.Operator == "LE" {
				n++
			}
			return n
		}
	}
	return 0
}

// getExprName 从表达式 AST 节点提取可读名称
func getExprName(expr ast.Expression) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		if e != nil {
			return e.Name
		}
	case *ast.IndexExpression:
		if e != nil && e.Object != nil {
			if obj, ok := e.Object.(*ast.Identifier); ok {
				if e.Index != nil {
					if idx, ok := e.Index.(*ast.IntegerLiteral); ok {
						return fmt.Sprintf("%s[%d]", obj.Name, idx.Value)
					}
				}
				return obj.Name + "[?]"
			}
		}
	case *ast.MemberAccessExpression:
		if e != nil {
			return e.Member
		}
	}
	return "expr"
}
