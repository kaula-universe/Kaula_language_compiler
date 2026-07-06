package sor

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"strings"
)

// ============================================================================
// Pass 2.5e: 跨函数（inter-procedural）所有权传播分析
// ============================================================================

// OwnershipMode 参数/返回值的所有权模式
type OwnershipMode int

const (
	ModeOwned        OwnershipMode = iota // 消费所有权（move in）
	ModeReleased                          // 只读借用
	ModeUnrestricted                      // 无限制（值语义拷贝）
)

func (m OwnershipMode) String() string {
	switch m {
	case ModeOwned:
		return "Owned"
	case ModeReleased:
		return "Released"
	case ModeUnrestricted:
		return "Unrestricted"
	default:
		return fmt.Sprintf("Unknown(%d)", int(m))
	}
}

// ParamOwnership 参数所有权信息
type ParamOwnership struct {
	Name  string
	Type  string
	Mode  OwnershipMode
}

// ReturnOwnership 返回值所有权信息
type ReturnOwnership struct {
	Type string
	Mode OwnershipMode
}

// FuncOwnershipSig 函数所有权签名
type FuncOwnershipSig struct {
	Name    string
	Params  []ParamOwnership
	Returns []ReturnOwnership
}

// CallGraphEdge 调用图边
type CallGraphEdge struct {
	Caller string
	Callee string
	Line   int
}

// OwnershipTransfer 所有权转移记录
type OwnershipTransfer struct {
	CallSite string
	VarName  string
	Direction string // "in" 或 "out"
	Mode     OwnershipMode
}

// InterProcResult 跨函数分析结果
type InterProcResult struct {
	FuncSigs      map[string]*FuncOwnershipSig
	CallGraph     []CallGraphEdge
	TransferPoints map[string][]OwnershipTransfer
}

// InterProcAnalyzer 跨函数分析器
type InterProcAnalyzer struct {
	sigs      map[string]*FuncOwnershipSig
	callGraph []CallGraphEdge
	transfers map[string][]OwnershipTransfer
}

func NewInterProcAnalyzer() *InterProcAnalyzer {
	return &InterProcAnalyzer{
		sigs:      make(map[string]*FuncOwnershipSig),
		transfers: make(map[string][]OwnershipTransfer),
	}
}

// AnalyzeInterProc 执行跨函数所有权传播分析
// astProgram 非 nil 时，从 AST 构建函数所有权签名
func (ipa *InterProcAnalyzer) AnalyzeInterProc(stmts []Stmt, tracker *OwnershipTracker, astProgram *ast.Program) *InterProcResult {
	// 第零遍：从 AST 构建函数所有权签名
	if astProgram != nil {
		ipa.buildFuncSigsFromAST(astProgram)
	}

	// 第一遍：构建调用图
	// 从 AST 中提取函数体内的调用，确定实际的 caller
	if astProgram != nil {
		ipa.buildCallGraphFromAST(astProgram)
	} else {
		// 回退到简化模型：所有调用者为 "main"
		for _, stmt := range stmts {
			if stmt.Kind == StmtCall {
				if stmt.FuncName != "" {
					ipa.callGraph = append(ipa.callGraph, CallGraphEdge{
						Caller: "main",
						Callee: stmt.FuncName,
						Line:   stmt.Line,
					})
				}
			}
		}
	}

	// 第二遍：分析所有权转移
	for _, stmt := range stmts {
		if stmt.Kind == StmtCall {
			callSite := fmt.Sprintf("main:%d", stmt.Line)
			for i, arg := range stmt.ArgNames {
				mode := ModeOwned // 保守默认
				if i < len(stmt.ArgOwnership) {
					switch stmt.ArgOwnership[i] {
					case "release":
						mode = ModeReleased
					case "owned":
						mode = ModeOwned
					default:
						mode = ModeUnrestricted
					}
				}
				ipa.transfers[callSite] = append(ipa.transfers[callSite], OwnershipTransfer{
					CallSite:  callSite,
					VarName:   arg,
					Direction: "in",
					Mode:      mode,
				})
			}
		}
	}

	return &InterProcResult{
		FuncSigs:       ipa.sigs,
		CallGraph:      ipa.callGraph,
		TransferPoints: ipa.transfers,
	}
}

// buildCallGraphFromAST 从 AST 构建完整的调用图
// 识别每个函数体内的函数调用，建立 caller -> callee 边
func (ipa *InterProcAnalyzer) buildCallGraphFromAST(program *ast.Program) {
	if program == nil {
		return
	}

	for _, stmt := range program.Statements {
		if fn, ok := stmt.(*ast.FunctionStatement); ok {
			// 遍历函数体，找到所有函数调用
			ipa.scanFuncBodyForCalls(fn.Name, fn.Body)
		}
	}
}

// scanFuncBodyForCalls 扫描函数体中的函数调用
func (ipa *InterProcAnalyzer) scanFuncBodyForCalls(callerName string, body []ast.Statement) {
	for _, stmt := range body {
		if stmt == nil {
			continue
		}
		switch s := stmt.(type) {
		case *ast.ExpressionStatement:
			if s.Expression != nil {
				if callExpr, ok := s.Expression.(*ast.CallExpression); ok {
					calleeName := extractFuncName(callExpr.Function)
					if calleeName != "" {
						ipa.callGraph = append(ipa.callGraph, CallGraphEdge{
							Caller: callerName,
							Callee: calleeName,
							Line:   s.Pos.Line,
						})
					}
				}
			}
		case *ast.FunctionStatement:
			// 嵌套函数（如果有）
			if s != nil {
				ipa.scanFuncBodyForCalls(callerName, s.Body)
			}
		case *ast.IfStatement:
			if s != nil {
				ipa.scanFuncBodyForCalls(callerName, s.Body)
				ipa.scanFuncBodyForCalls(callerName, s.Else)
			}
		case *ast.WhileStatement:
			if s != nil {
				ipa.scanFuncBodyForCalls(callerName, s.Body)
			}
		case *ast.ForStatement:
			if s != nil {
				ipa.scanFuncBodyForCalls(callerName, s.Body)
			}
		case *ast.BlockStatement:
			if s != nil {
				ipa.scanFuncBodyForCalls(callerName, s.Statements)
			}
		case *ast.ReturnStatement:
			if s != nil && s.Value != nil {
				if callExpr, ok := s.Value.(*ast.CallExpression); ok {
					calleeName := extractFuncName(callExpr.Function)
					if calleeName != "" {
						ipa.callGraph = append(ipa.callGraph, CallGraphEdge{
							Caller: callerName,
							Callee: calleeName,
							Line:   s.Pos.Line,
						})
					}
				}
			}
		}
	}
}

// buildFuncSigsFromAST 从 AST 构建函数所有权签名
// 策略：
//   - 指针类型参数: ModeOwned（保守处理，可能有所有权转移）
//   - 值类型参数: ModeUnrestricted（值语义拷贝，无所有权转移）
//   - 指针返回类型: HasPtrReturn = true
func (ipa *InterProcAnalyzer) buildFuncSigsFromAST(program *ast.Program) {
	if program == nil {
		return
	}
	for _, stmt := range program.Statements {
		if fn, ok := stmt.(*ast.FunctionStatement); ok {
			sig := &FuncOwnershipSig{Name: fn.Name}
			// 构建参数所有权
			for i, paramName := range fn.Params {
				paramType := ""
				if i < len(fn.ParamTypes) {
					paramType = fn.ParamTypes[i]
				}
				mode := ModeUnrestricted
				if strings.Contains(paramType, "*") {
					mode = ModeOwned
				}
				sig.Params = append(sig.Params, ParamOwnership{
					Name: paramName,
					Type: paramType,
					Mode: mode,
				})
			}
			// 构建返回值所有权
			if fn.ReturnType != "" && fn.ReturnType != "void" {
				retMode := ModeUnrestricted
				if strings.Contains(fn.ReturnType, "*") {
					retMode = ModeOwned
				}
				sig.Returns = append(sig.Returns, ReturnOwnership{
					Type: fn.ReturnType,
					Mode: retMode,
				})
			}
			ipa.sigs[fn.Name] = sig
		}
	}
}

// ApplyInterProcToDecisions 将跨函数分析结果应用到内存决策
func (ipa *InterProcAnalyzer) ApplyInterProcToDecisions(decisions []*MemoryDecision, result *InterProcResult) {
	if result == nil {
		return
	}
	for _, d := range decisions {
		for _, transfers := range result.TransferPoints {
			for _, t := range transfers {
				if t.VarName == d.VarName && t.Direction == "in" && t.Mode == ModeOwned {
					d.DropAction = DropNone
					d.FinalState = StateMoved
				}
			}
		}
	}
}

// GetFuncSig 获取函数签名
func (ipa *InterProcAnalyzer) GetFuncSig(name string) *FuncOwnershipSig {
	return ipa.sigs[name]
}

// GetCallGraph 获取调用图
func (ipa *InterProcAnalyzer) GetCallGraph() []CallGraphEdge {
	return ipa.callGraph
}

// GetTransfers 获取所有权转移记录
func (ipa *InterProcAnalyzer) GetTransfers() map[string][]OwnershipTransfer {
	return ipa.transfers
}

// FormatInterProcSummary 格式化跨函数分析结果
func FormatInterProcSummary(result *InterProcResult) string {
	if result == nil {
		return "(no interproc results)"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== InterProc Analysis ===\n"))
	b.WriteString(fmt.Sprintf("  Functions: %d\n", len(result.FuncSigs)))
	b.WriteString(fmt.Sprintf("  Call edges: %d\n", len(result.CallGraph)))
	b.WriteString(fmt.Sprintf("  Transfer points: %d\n", len(result.TransferPoints)))
	for _, edge := range result.CallGraph {
		b.WriteString(fmt.Sprintf("  call: %s -> %s (line %d)\n", edge.Caller, edge.Callee, edge.Line))
	}
	return b.String()
}
