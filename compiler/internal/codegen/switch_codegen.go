package codegen

import (
	"strings"

	"kaula/internal/ast"
)

/*
 * generateSwitchStatement 生成跳转表（switch）语句的 C 代码。
 *
 * 该节点由 internal/opt 的「if 链转跳转表」优化产出：一串
 * `if (x == c1) {...} else if (x == c2) {...} ...` 被折叠成单一的
 * C switch 分派点。密集整型区间由 Clang 落成真正的跳转表（jump table），
 * 稀疏区间退化为二分查找/比较链 —— 两种情况下语义都与原 if 链等价。
 *
 * 与 if 分支保持对称的关键点：
 *   - 每个 case/default 分支体各自开一层作用域（变量可见性不变）；
 *   - 分支体的 KMM/offset 作用域包裹与 if 分支完全一致（内存回收行为不变）；
 *   - 分支体末尾不是控制流语句时补 break，防止 case 穿透（C 默认穿透，
 *     而原 if 链是互斥的）；
 *   - 分支间的 promote 状态按**路径敏感交集**合并，与 if/else 的
 *     MergePromotedFromBranches 语义一致（见下方合并逻辑）。
 */
func (sg *StatementGenerator) generateSwitchStatement(stmt *ast.SwitchStatement) string {
	if stmt == nil {
		return ""
	}

	scrutCode := sg.codegen.expressionGenerator.GenerateExpression(stmt.Scrutinee)

	// 路径敏感 promote 快照：每个分支都必须从同一状态出发，
	// 否则分支 N 会看到分支 N-1 的 promote 残留，交集就不再可信。
	promotedSnapshot := sg.codegen.SnapshotPromotedVars()

	var code strings.Builder
	code.WriteString("switch (")
	code.WriteString(scrutCode)
	code.WriteString(") {\n")

	sg.codegen.indent++

	// 各分支新 promote 的变量集合，用于分支合并点求交集
	var branchDiffs []map[string]bool

	for _, c := range stmt.Cases {
		if c == nil {
			continue
		}
		sg.codegen.RestorePromotedVars(promotedSnapshot)

		// case 标签：C 要求整型常量表达式，非常量/浮点标签在上层优化时已被排除。
		for _, v := range c.Values {
			if v == nil {
				continue
			}
			code.WriteString(sg.codegen.indentString())
			code.WriteString("case ")
			code.WriteString(sg.codegen.expressionGenerator.GenerateExpression(v))
			code.WriteString(":\n")
		}
		code.WriteString(sg.codegen.indentString())
		code.WriteString("{\n")
		code.WriteString(sg.generateSwitchBranchBody(c.Body))
		if !lastStmtIsControlFlow(c.Body) {
			// break 与分支体齐平（分支体在 generateSwitchBranchBody 内已 +1 级）
			sg.codegen.indent++
			code.WriteString(sg.codegen.indentString())
			code.WriteString("break;\n")
			sg.codegen.indent--
		}
		code.WriteString(sg.codegen.indentString())
		code.WriteString("}\n")

		branchDiffs = append(branchDiffs, sg.codegen.DiffPromotedVars(promotedSnapshot))
	}

	hasDefault := len(stmt.Default) > 0
	if hasDefault {
		sg.codegen.RestorePromotedVars(promotedSnapshot)
		code.WriteString(sg.codegen.indentString())
		code.WriteString("default:\n")
		code.WriteString(sg.codegen.indentString())
		code.WriteString("{\n")
		code.WriteString(sg.generateSwitchBranchBody(stmt.Default))
		if !lastStmtIsControlFlow(stmt.Default) {
			sg.codegen.indent++
			code.WriteString(sg.codegen.indentString())
			code.WriteString("break;\n")
			sg.codegen.indent--
		}
		code.WriteString(sg.codegen.indentString())
		code.WriteString("}\n")
		branchDiffs = append(branchDiffs, sg.codegen.DiffPromotedVars(promotedSnapshot))
	}

	sg.codegen.indent--

	/*
	 * 合并点：与 generateIfStatement 的 if/else 合并语义对齐。
	 *
	 *   - 有 default（等价于 if 有 else）：只有**所有分支都 promote** 的变量
	 *     才在合并点标记为已 promote；只被部分分支 promote 的保守不标记，
	 *     否则后续代码会跳过对它的回收（漏回收）或重复 promote。
	 *   - 无 default（等价于 if 无 else）：隐式未命中路径没有 promote，
	 *     因此一律不传播，直接恢复到快照。
	 */
	sg.codegen.RestorePromotedVars(promotedSnapshot)
	if hasDefault && len(branchDiffs) > 0 {
		merged := branchDiffs[0]
		for _, d := range branchDiffs[1:] {
			// MergePromotedFromBranches 只做"置位"，不做"清位"，
			// 所以每次合并前必须先恢复到快照，否则交集不会随分支增多而收窄。
			sg.codegen.RestorePromotedVars(promotedSnapshot)
			sg.codegen.MergePromotedFromBranches(merged, d)
			merged = sg.codegen.DiffPromotedVars(promotedSnapshot)
		}
	}

	code.WriteString(sg.codegen.indentString())
	code.WriteString("}\n")
	return code.String()
}

/*
 * generateSwitchBranchBody 生成 switch 分支体（与 if 分支体完全同构）。
 *
 * 之所以必须与 if 分支同构：优化前这些语句处在 if 分支里，各自带一层
 * KMM/offset 作用域包裹；改写后若少了这层包裹，分支内分配的对象会把
 * 生命周期撑到外层函数作用域，内存峰值行为随之改变。这里逐字复用
 * generateIfStatement 的包裹逻辑来避免这类静默退化。
 */
func (sg *StatementGenerator) generateSwitchBranchBody(body []ast.Statement) string {
	sg.codegen.indent++
	sg.codegen.EnterScope("switch_case")

	// promotedVars 快照必须在生成分支体之前取，否则检测不到分支体内新增的 promote
	promotedSnapshot := sg.codegen.SnapshotPromotedVars()

	useKMM := sg.shouldUseKMMScopeForBody(body)
	useOffset := !useKMM && bodyContainsAllocation(body, sg.codegen.IsFreestanding())

	if useKMM {
		sg.codegen.EnterKMMScope()
	}
	if useOffset {
		sg.codegen.EnterOffsetScope()
	}

	var bodyCode strings.Builder
	for _, bodyStmt := range body {
		bodyCode.WriteString(sg.codegen.indentString())
		bodyCode.WriteString(sg.codegen.generateStatement(bodyStmt))
	}
	// 方案 A：在作用域退出前发射 per-object survivor_free
	// 若末尾是 return，return 中已通过 generateSurvivorFreeForReturn 释放，此处为死代码，跳过
	if !lastStmtIsReturn(body) {
		bodyCode.WriteString(sg.generateSurvivorFreeForScope())
	}

	if useOffset {
		sg.codegen.ExitOffsetScope()
	}
	if useKMM {
		sg.codegen.ExitKMMScope()
	}
	sg.codegen.ExitScope()
	sg.codegen.indent--

	var code strings.Builder
	if useOffset {
		code.WriteString(sg.codegen.indentString() + "kmm_v4_mark_t _sw_scope_start = kmm_v4_mark_save();\n")
		code.WriteString(bodyCode.String())
		code.WriteString(sg.codegen.indentString() + "kmm_v4_mark_restore(_sw_scope_start);\n")
	} else if useKMM {
		code.WriteString(sg.codegen.indentString() + "kmm_survivor_checkpoint_t __sw_surv_cp = kmm_v4_survivor_checkpoint();\n")
		code.WriteString(sg.codegen.indentString() + sg.codegen.KMMStart(""))
		code.WriteString(bodyCode.String())
		code.WriteString(sg.codegen.indentString() + sg.codegen.KMMEnd())
		if len(sg.codegen.DiffPromotedVars(promotedSnapshot)) == 0 {
			code.WriteString(sg.codegen.indentString() + "kmm_v4_survivor_rewind(__sw_surv_cp); /* survivor: scope reclaim */\n")
		}
	} else {
		code.WriteString(bodyCode.String())
	}
	return code.String()
}
