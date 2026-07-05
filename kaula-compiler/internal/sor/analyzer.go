package sor

import (
	"fmt"
	"strings"
)

// ============================================================================
// SOR 分析器：符号执行引擎
// ============================================================================

// StmtKind 表示 SOR 语句的种类。
// 这是一个简化的 IR（中间表示），用于模拟源码级别的分析。
type StmtKind int

const (
	// StmtLet 变量声明：let x = value
	StmtLet StmtKind = iota

	// StmtYeide 所有权转移：yeide src -> dst
	StmtYeide

	// StmtRelease 所有权分发：release src -> [a, b, c]
	StmtRelease

	// StmtExtract 子结构提取：extract src[idx] -> elem
	StmtExtract

	// StmtRead 读访问：使用变量的值
	StmtRead

	// StmtWrite 写访问：修改变量
	StmtWrite

	// StmtCall 函数调用：带参数的所有权标注检查
	StmtCall

	// StmtScope 作用域边界：进入/退出代码块
	StmtScopeEnter
	StmtScopeExit

	// StmtThreadSpawn 线程创建：spawn thread { ... }
	StmtThreadSpawn
	StmtThreadEnd

	// StmtComment 注释（用于在输出中显示源码）
	StmtComment

	// StmtUnionRelease 联合域分发：union release src -> [a, b, c]
	// 编译期选举决定 elected writer，其余为 reader
	StmtUnionRelease

	// 控制流结构标记
	StmtLoopEnter    // 循环入口，携带迭代次数信息
	StmtLoopExit     // 循环出口
	StmtBranchEnter  // if 分支入口
	StmtBranchExit   // if/else 分支出口（合并点）
	StmtBranchElse   // else 分支入口（分隔 if body 和 else body）
)

// Stmt 表示一条 SOR 分析语句。
// 它是源码的抽象表示，用于符号执行。
type Stmt struct {
	// Kind 是语句类型。
	Kind StmtKind

	// Line 是源码行号。
	Line int

	// Source 是原始源码文本（用于显示）。
	Source string

	// VarName 是主要变量名（let/yeide/extract 的目标，read/write 的对象等）。
	VarName string

	// TypeName 是类型名（let 语句中使用）。
	TypeName string

	// IsComposite 表示是否为复合类型（let 语句中使用）。
	IsComposite bool

	// ChildPath 是子元素路径（extract 语句中使用，如 "[0]"）。
	ChildPath string

	// SrcName 是源变量名（yeide/release/extract 的源）。
	SrcName string

	// HolderNames 是 release 持有者名称列表。
	HolderNames []string

	// ScopeName 是作用域名称（用于显示）。
	ScopeName string

	// ThreadName 是线程名称。
	ThreadName string

	// FuncName 是函数名（call 语句中使用）。
	FuncName string

	// ArgNames 是函数参数名列表。
	ArgNames []string

	// ArgOwnership 是参数的所有权标注（与 ArgNames 对应）。
	// "owned" 或 "release"。
	ArgOwnership []string

	// Children 是子语句列表（用于作用域/线程等嵌套结构）。
	Children []Stmt

	// IsUnion 标记此 release 是否为 union release（联合域可变共享）
	IsUnion bool

	// LoopIterCount 静态可确定的循环迭代次数（0=未知）
	LoopIterCount int
}

// ============================================================================
// SOR 分析器
// ============================================================================

// SORAnalyzer 是 SOR 编译时验证的主入口。
// 它通过符号执行（模拟执行）来追踪每个变量的所有权状态，
// 并在发现违反 SOR 规则时报告错误。
//
// 分析过程：
//  1. 从函数入口开始，初始状态为空
//  2. 逐条执行语句，更新所有权状态
//  3. 每一步都进行权限检查，发现错误立即记录
//  4. 最终返回所有错误
type SORAnalyzer struct {
	// tracker 是所有权追踪器。
	tracker *OwnershipTracker

	// 语句列表（用于分析）。
	stmts []Stmt

	// 分析结果：所有错误。
	errors []SORError

	// 执行日志（用于调试和展示）。
	execLog []string
}

// NewSORAnalyzer 创建一个新的 SOR 分析器。
func NewSORAnalyzer() *SORAnalyzer {
	return &SORAnalyzer{
		tracker: NewOwnershipTracker(),
		errors:  make([]SORError, 0),
		execLog: make([]string, 0),
	}
}

// Analyze 分析一组语句，返回所有 SOR 错误。
func (a *SORAnalyzer) Analyze(stmts []Stmt) []SORError {
	a.stmts = stmts
	a.errors = make([]SORError, 0)
	a.execLog = make([]string, 0)
	a.tracker = NewOwnershipTracker()

	a.log("=== SOR 分析开始 ===")
	a.executeStmts(stmts)
	a.log("=== SOR 分析结束 ===")

	// 收集所有错误
	a.errors = a.tracker.GetErrors()
	return a.errors
}

// executeStmts 执行一组语句。
func (a *SORAnalyzer) executeStmts(stmts []Stmt) {
	for _, stmt := range stmts {
		a.executeStmt(stmt)
	}
}

// executeStmt 执行单条语句。
func (a *SORAnalyzer) executeStmt(stmt Stmt) {
	a.log(fmt.Sprintf("[line %d] %s", stmt.Line, stmt.Source))

	switch stmt.Kind {
	case StmtLet:
		a.execLet(stmt)
	case StmtYeide:
		a.execYeide(stmt)
	case StmtRelease:
		a.execRelease(stmt)
	case StmtExtract:
		a.execExtract(stmt)
	case StmtRead:
		a.execRead(stmt)
	case StmtWrite:
		a.execWrite(stmt)
	case StmtCall:
		a.execCall(stmt)
	case StmtScopeEnter:
		a.execScopeEnter(stmt)
	case StmtScopeExit:
		a.execScopeExit(stmt)
	case StmtThreadSpawn:
		a.execThreadSpawn(stmt)
	case StmtThreadEnd:
		a.execThreadEnd(stmt)
	case StmtUnionRelease:
		a.execUnionRelease(stmt)
	case StmtComment:
		// 注释，不执行任何操作
		a.log(fmt.Sprintf("  # %s", stmt.Source))
	}
}

// ----------------------------------------------------------------------------
// 语句执行函数
// ----------------------------------------------------------------------------

// execLet 执行变量声明语句。
// let x = Array([1, 2, 3])  -> x 获得独占所有权
func (a *SORAnalyzer) execLet(stmt Stmt) {
	objID := a.tracker.NewObject(stmt.VarName, stmt.TypeName, stmt.IsComposite, stmt.Line)
	a.log(fmt.Sprintf("  -> 创建对象 %s, 状态: %s", stmt.VarName, StateOwned))

	// 如果是复合类型且有子语句（表示初始化子元素），处理子元素
	if stmt.IsComposite && len(stmt.Children) > 0 {
		for _, child := range stmt.Children {
			if child.Kind == StmtLet {
				// 子元素声明
				childPath := child.ChildPath
				if childPath == "" {
					childPath = fmt.Sprintf("[%s]", child.VarName)
				}
				a.tracker.AddChild(objID, childPath, child.TypeName, child.IsComposite, stmt.Line)
				a.log(fmt.Sprintf("  -> 添加子元素 %s%s (%s)", stmt.VarName, childPath, child.TypeName))
			}
		}
	}
}

// execYeide 执行所有权转移语句。
// yeide x -> y  -> x 失效，y 获得所有权
func (a *SORAnalyzer) execYeide(stmt Stmt) {
	srcID := a.tracker.GetObjectByName(stmt.SrcName)
	if srcID == "" {
		a.tracker.addError(SORError{
			Kind:       ErrYeideInvalidSource,
			Message:    fmt.Sprintf("yeide 失败：变量 '%s' 未找到", stmt.SrcName),
			SourceLine: stmt.Line,
		})
		a.log(fmt.Sprintf("  [错误] 源变量 '%s' 未找到", stmt.SrcName))
		return
	}

	// 检查源对象是否可以转移（权限检查）
	if !a.tracker.CheckAccess(srcID, AccessTake, stmt.Line) {
		a.log("  [错误] 源对象无法转移所有权")
		return
	}

	dstID := a.tracker.Yeide(srcID, stmt.VarName, stmt.Line)
	if dstID != "" {
		a.log(fmt.Sprintf("  -> 所有权转移: %s --yeide--> %s", stmt.SrcName, stmt.VarName))
		a.log(fmt.Sprintf("     %s 状态: %s, %s 状态: %s",
			stmt.SrcName, StateMoved, stmt.VarName, StateOwned))
	}
}

// execRelease 执行所有权分发语句。
// release x -> [a, b, c]  -> x 变为 Released，a/b/c 获得只读访问
func (a *SORAnalyzer) execRelease(stmt Stmt) {
	srcID := a.tracker.GetObjectByName(stmt.SrcName)
	if srcID == "" {
		a.tracker.addError(SORError{
			Kind:       ErrReleaseInvalidSource,
			Message:    fmt.Sprintf("release 失败：变量 '%s' 未找到", stmt.SrcName),
			SourceLine: stmt.Line,
		})
		a.log(fmt.Sprintf("  [错误] 源变量 '%s' 未找到", stmt.SrcName))
		return
	}

	holderIDs := a.tracker.Release(srcID, stmt.HolderNames, stmt.Line)
	if holderIDs != nil {
		a.log(fmt.Sprintf("  -> 所有权分发: %s --release--> [%s]",
			stmt.SrcName, strings.Join(stmt.HolderNames, ", ")))
		a.log(fmt.Sprintf("     %s 状态: %s", stmt.SrcName, StateReleased))
	}
}

// execUnionRelease 执行联合域分发语句。
// union release x -> [a, b, c]  -> x 变为 UnionReleased，编译期选举第一个 holder 为 elected writer
func (a *SORAnalyzer) execUnionRelease(stmt Stmt) {
	srcID := a.tracker.GetObjectByName(stmt.SrcName)
	if srcID == "" {
		a.tracker.addError(SORError{
			Kind:       ErrReleaseInvalidSource,
			Message:    fmt.Sprintf("union release 失败：变量 '%s' 未找到", stmt.SrcName),
			SourceLine: stmt.Line,
		})
		a.log(fmt.Sprintf("  [错误] 源变量 '%s' 未找到", stmt.SrcName))
		return
	}

	holderIDs := a.tracker.Release(srcID, stmt.HolderNames, stmt.Line)
	if holderIDs != nil {
		// 标记源对象为联合域状态
		if srcObj := a.tracker.GetObject(srcID); srcObj != nil {
			srcObj.State = StateUnionReleased
			srcObj.IsUnion = true
		}

		a.log(fmt.Sprintf("  -> 联合域分发: %s --union-release--> [%s]",
			stmt.SrcName, strings.Join(stmt.HolderNames, ", ")))
		a.log(fmt.Sprintf("     %s 状态: %s", stmt.SrcName, StateUnionReleased))
		if len(stmt.HolderNames) > 0 {
			a.log(fmt.Sprintf("     编译期选举: %s (elected writer)", stmt.HolderNames[0]))
			for i := 1; i < len(stmt.HolderNames); i++ {
				a.log(fmt.Sprintf("     编译期只读: %s (reader)", stmt.HolderNames[i]))
			}
		}
	}
}

// execExtract 执行子结构提取语句。
// extract x[2] -> elem  -> x[2] 变 null，elem 获得独占所有权
func (a *SORAnalyzer) execExtract(stmt Stmt) {
	srcID := a.tracker.GetObjectByName(stmt.SrcName)
	if srcID == "" {
		a.tracker.addError(SORError{
			Kind:       ErrExtractFromNonComposite,
			Message:    fmt.Sprintf("extract 失败：变量 '%s' 未找到", stmt.SrcName),
			SourceLine: stmt.Line,
		})
		a.log(fmt.Sprintf("  [错误] 源变量 '%s' 未找到", stmt.SrcName))
		return
	}

	elemID := a.tracker.Extract(srcID, stmt.ChildPath, stmt.VarName, stmt.Line)
	if elemID != "" {
		a.log(fmt.Sprintf("  -> 所有权提取: %s%s --extract--> %s",
			stmt.SrcName, stmt.ChildPath, stmt.VarName))
		a.log(fmt.Sprintf("     %s 状态: %s, %s%s 状态: %s",
			stmt.VarName, StateOwned, stmt.SrcName, stmt.ChildPath, StateHollow))
	}
}

// execRead 执行读访问语句。
func (a *SORAnalyzer) execRead(stmt Stmt) {
	objID := a.tracker.GetObjectByName(stmt.VarName)
	if objID == "" {
		a.tracker.addError(SORError{
			Kind:       ErrUseAfterMove,
			Message:    fmt.Sprintf("读访问失败：变量 '%s' 未找到", stmt.VarName),
			SourceLine: stmt.Line,
		})
		a.log(fmt.Sprintf("  [错误] 变量 '%s' 未找到", stmt.VarName))
		return
	}

	if a.tracker.CheckAccess(objID, AccessRead, stmt.Line) {
		a.log(fmt.Sprintf("  -> 读访问 %s: 允许 (状态: %s)", stmt.VarName, a.tracker.GetObject(objID).State))
	} else {
		a.log(fmt.Sprintf("  [错误] 读访问 %s: 拒绝", stmt.VarName))
	}
}

// execWrite 执行写访问语句。
func (a *SORAnalyzer) execWrite(stmt Stmt) {
	objID := a.tracker.GetObjectByName(stmt.VarName)
	if objID == "" {
		a.tracker.addError(SORError{
			Kind:       ErrUseAfterMove,
			Message:    fmt.Sprintf("写访问失败：变量 '%s' 未找到", stmt.VarName),
			SourceLine: stmt.Line,
		})
		a.log(fmt.Sprintf("  [错误] 变量 '%s' 未找到", stmt.VarName))
		return
	}

	if a.tracker.CheckAccess(objID, AccessWrite, stmt.Line) {
		a.log(fmt.Sprintf("  -> 写访问 %s: 允许 (状态: %s)", stmt.VarName, a.tracker.GetObject(objID).State))
	} else {
		a.log(fmt.Sprintf("  [错误] 写访问 %s: 拒绝", stmt.VarName))
	}
}

// execCall 执行函数调用语句，检查参数所有权匹配。
func (a *SORAnalyzer) execCall(stmt Stmt) {
	a.log(fmt.Sprintf("  -> 函数调用: %s(%s)", stmt.FuncName, strings.Join(stmt.ArgNames, ", ")))

	for i, argName := range stmt.ArgNames {
		argID := a.tracker.GetObjectByName(argName)
		if argID == "" {
			// 参数不是 SOR 追踪的对象（可能是字面量、非所有权变量等），跳过
			continue
		}

		argObj := a.tracker.GetObject(argID)
		expected := ""
		if i < len(stmt.ArgOwnership) {
			expected = stmt.ArgOwnership[i]
		}

		// 无论函数是否有所有权标注，都检查参数是否可用（不能是 Moved/Hollow）
		if argObj.State == StateMoved {
			a.tracker.addError(SORError{
				Kind:       ErrUseAfterMove,
				Message:    fmt.Sprintf("use-after-move：'%s' 已通过 yeide 转移所有权，不能再使用", argName),
				SourceLine: stmt.Line,
				Details:    "对象已通过 yeide 转移给其他变量",
			})
			a.log(fmt.Sprintf("     [错误] 参数 %s: use-after-move", argName))
			continue
		}
		if argObj.State == StateHollow {
			a.tracker.addError(SORError{
				Kind:       ErrNullDereference,
				Message:    fmt.Sprintf("null-safety：'%s' 已被 extract，为 null", argName),
				SourceLine: stmt.Line,
				Details:    "对象已被 extract 提取，原位置为 null",
			})
			a.log(fmt.Sprintf("     [错误] 参数 %s: null-safety violation", argName))
			continue
		}

		switch expected {
		case "owned":
			// 函数需要独占所有权：检查参数是否为 Owned
			if argObj.State != StateOwned {
				a.tracker.addError(SORError{
					Kind:       ErrYeideInvalidSource,
					Message:    fmt.Sprintf("参数不匹配：函数 '%s' 第 %d 个参数需要 owned 所有权，但 '%s' 处于 %s 状态",
						stmt.FuncName, i+1, argName, argObj.State),
					SourceLine: stmt.Line,
					ObjectID:   argID,
					Details:    "owned 参数要求独占所有权，需要传递 Owned 状态的对象",
				})
				a.log(fmt.Sprintf("     [错误] 参数 %s: 需要 owned, 实际 %s", argName, argObj.State))
			} else {
				// 模拟函数消耗所有权（如果是 owned 参数）
				argObj.State = StateMoved
				a.log(fmt.Sprintf("     参数 %s: owned -> moved (函数消耗所有权)", argName))
			}
		case "release":
			// 函数需要只读访问：Owned 或 Released 都可以
			if argObj.State != StateOwned && argObj.State != StateReleased {
				a.tracker.addError(SORError{
					Kind:       ErrReleaseInvalidSource,
					Message:    fmt.Sprintf("参数不匹配：函数 '%s' 第 %d 个参数需要 release 只读访问，但 '%s' 处于 %s 状态",
						stmt.FuncName, i+1, argName, argObj.State),
					SourceLine: stmt.Line,
					ObjectID:   argID,
				})
				a.log(fmt.Sprintf("     [错误] 参数 %s: 需要 release, 实际 %s", argName, argObj.State))
			} else {
				a.log(fmt.Sprintf("     参数 %s: %s (只读访问, 兼容)", argName, argObj.State))
			}
		default:
			a.log(fmt.Sprintf("     参数 %s: 未知标注 '%s'", argName, expected))
		}
	}
}

// execScopeEnter 进入作用域。
func (a *SORAnalyzer) execScopeEnter(stmt Stmt) {
	a.tracker.EnterScope()
	a.log(fmt.Sprintf("  -> 进入作用域 '%s' (scope %d)", stmt.ScopeName, a.tracker.GetCurrentScope()))

	// 执行作用域内的子语句
	if len(stmt.Children) > 0 {
		a.executeStmts(stmt.Children)
	}
}

// execScopeExit 退出作用域。
func (a *SORAnalyzer) execScopeExit(stmt Stmt) {
	a.log(fmt.Sprintf("  -> 退出作用域 '%s' (scope %d)", stmt.ScopeName, a.tracker.GetCurrentScope()))
	a.tracker.ExitScope(stmt.Line)
	a.log(fmt.Sprintf("     返回作用域 %d", a.tracker.GetCurrentScope()))
}

// execThreadSpawn 创建新线程并执行其中的语句。
// 在 SOR 中，跨线程传递所有权必须显式 yeide，
// release 对象跨线程只读。
func (a *SORAnalyzer) execThreadSpawn(stmt Stmt) {
	a.log(fmt.Sprintf("  -> 创建线程 '%s'", stmt.ThreadName))

	// 保存当前线程
	prevThread := a.tracker.GetThread()

	// 切换到新线程
	a.tracker.SetThread(stmt.ThreadName)

	// 执行线程内的语句
	if len(stmt.Children) > 0 {
		a.executeStmts(stmt.Children)
	}

	// 恢复原线程
	a.tracker.SetThread(prevThread)
	a.log(fmt.Sprintf("  -> 线程 '%s' 结束", stmt.ThreadName))
}

// execThreadEnd 结束当前线程。
func (a *SORAnalyzer) execThreadEnd(stmt Stmt) {
	a.log(fmt.Sprintf("  -> 线程 '%s' 结束", stmt.ThreadName))
}

// ----------------------------------------------------------------------------
// 日志与结果
// ----------------------------------------------------------------------------

// log 添加一条执行日志。
func (a *SORAnalyzer) log(msg string) {
	a.execLog = append(a.execLog, msg)
}

// GetExecLog 返回执行日志。
func (a *SORAnalyzer) GetExecLog() []string {
	return a.execLog
}

// GetTracker 返回所有权追踪器（用于调试）。
func (a *SORAnalyzer) GetTracker() *OwnershipTracker {
	return a.tracker
}

// ============================================================================
// 辅助函数：构建测试用例语句
// ============================================================================

// LetStmt 创建一个变量声明语句。
func LetStmt(line int, source, name, typeName string, isComposite bool, children ...Stmt) Stmt {
	return Stmt{
		Kind:        StmtLet,
		Line:        line,
		Source:      source,
		VarName:     name,
		TypeName:    typeName,
		IsComposite: isComposite,
		Children:    children,
	}
}

// YeideStmt 创建一个 yeide 语句。
func YeideStmt(line int, source, src, dst string) Stmt {
	return Stmt{
		Kind:    StmtYeide,
		Line:    line,
		Source:  source,
		SrcName: src,
		VarName: dst,
	}
}

// ReleaseStmt 创建一个 release 语句。
func ReleaseStmt(line int, source, src string, holders ...string) Stmt {
	return Stmt{
		Kind:        StmtRelease,
		Line:        line,
		Source:      source,
		SrcName:     src,
		HolderNames: holders,
	}
}

// UnionReleaseStmt 创建一个联合域分发语句。
func UnionReleaseStmt(line int, source, src string, holders ...string) Stmt {
	return Stmt{
		Kind:        StmtUnionRelease,
		Line:        line,
		Source:      source,
		SrcName:     src,
		HolderNames: holders,
		IsUnion:     true,
	}
}

// ExtractStmt 创建一个 extract 语句。
func ExtractStmt(line int, source, src, childPath, elemName string) Stmt {
	return Stmt{
		Kind:      StmtExtract,
		Line:      line,
		Source:    source,
		SrcName:   src,
		ChildPath: childPath,
		VarName:   elemName,
	}
}

// ReadStmt 创建一个读访问语句。
func ReadStmt(line int, source, varName string) Stmt {
	return Stmt{
		Kind:    StmtRead,
		Line:    line,
		Source:  source,
		VarName: varName,
	}
}

// WriteStmt 创建一个写访问语句。
func WriteStmt(line int, source, varName string) Stmt {
	return Stmt{
		Kind:    StmtWrite,
		Line:    line,
		Source:  source,
		VarName: varName,
	}
}

// CallStmt 创建一个函数调用语句。
func CallStmt(line int, source, funcName string, argNames []string, argOwnership []string) Stmt {
	return Stmt{
		Kind:          StmtCall,
		Line:          line,
		Source:        source,
		FuncName:      funcName,
		ArgNames:      argNames,
		ArgOwnership:  argOwnership,
	}
}

// ScopeStmt 创建一个作用域语句（包含 enter 和 exit）。
func ScopeStmt(name string, children ...Stmt) []Stmt {
	return []Stmt{
		{
			Kind:      StmtScopeEnter,
			Line:      0,
			Source:    "{ // " + name,
			ScopeName: name,
			Children:  children,
		},
		{
			Kind:      StmtScopeExit,
			Line:      0,
			Source:    "} // " + name,
			ScopeName: name,
		},
	}
}

// ThreadStmt 创建一个线程语句。
func ThreadStmt(name string, children ...Stmt) Stmt {
	return Stmt{
		Kind:       StmtThreadSpawn,
		Line:       0,
		Source:     "spawn " + name + " { ... }",
		ThreadName: name,
		Children:   children,
	}
}

// CommentStmt 创建一个注释语句。
func CommentStmt(source string) Stmt {
	return Stmt{
		Kind:   StmtComment,
		Line:   0,
		Source: source,
	}
}

// LoopEnterStmt 创建循环入口语句
func LoopEnterStmt(line int, source string, iterCount int) Stmt {
	return Stmt{Kind: StmtLoopEnter, Line: line, Source: source, LoopIterCount: iterCount}
}

// LoopExitStmt 创建循环出口语句
func LoopExitStmt(line int, source string) Stmt {
	return Stmt{Kind: StmtLoopExit, Line: line, Source: source}
}

// BranchEnterStmt 创建分支入口语句
func BranchEnterStmt(line int, source string) Stmt {
	return Stmt{Kind: StmtBranchEnter, Line: line, Source: source}
}

// BranchElseStmt 创建 else 分支入口语句
func BranchElseStmt(line int, source string) Stmt {
	return Stmt{Kind: StmtBranchElse, Line: line, Source: source}
}

// BranchExitStmt 创建分支出口语句
func BranchExitStmt(line int, source string) Stmt {
	return Stmt{Kind: StmtBranchExit, Line: line, Source: source}
}
