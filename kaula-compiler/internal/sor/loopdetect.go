package sor

// ============================================================================
// Pass 4: 循环检测器 — 识别循环体内的变量声明，用于循环感知的池容量计算
// ============================================================================

// LoopInfo 描述一个循环的信息
type LoopInfo struct {
	// IterCount 静态可确定的迭代次数（0=未知）
	IterCount int

	// BodyVars 循环体内声明的变量名列表
	BodyVars []string

	// ExternalVars 循环体外但在循环内使用的变量名列表
	ExternalVars []string

	// NestedLoops 嵌套的子循环
	NestedLoops []*LoopInfo
}

// LoopDetector 从 Stmt 列表中识别循环结构
type LoopDetector struct {
	loops []*LoopInfo
}

// NewLoopDetector 创建循环检测器
func NewLoopDetector() *LoopDetector {
	return &LoopDetector{}
}

// DetectLoops 从语句列表中检测所有循环，返回循环信息列表
func (ld *LoopDetector) DetectLoops(stmts []Stmt) []*LoopInfo {
	ld.loops = nil
	ld.scanLoops(stmts)
	return ld.loops
}

// scanLoops 递归扫描语句列表，提取循环标记
func (ld *LoopDetector) scanLoops(stmts []Stmt) {
	depth := 0
	var loopStack []*LoopInfo

	for _, stmt := range stmts {
		switch stmt.Kind {
		case StmtLoopEnter:
			depth++
			newLoop := &LoopInfo{
				IterCount:    stmt.LoopIterCount,
				BodyVars:     make([]string, 0),
				ExternalVars: make([]string, 0),
			}
			// 将新循环添加到栈顶
			loopStack = append(loopStack, newLoop)

			if depth == 1 {
				// 顶层循环
				ld.loops = append(ld.loops, newLoop)
			} else {
				// 嵌套循环，添加到父循环
				if len(loopStack) >= 2 {
					parent := loopStack[len(loopStack)-2]
					parent.NestedLoops = append(parent.NestedLoops, newLoop)
				}
			}

		case StmtLoopExit:
			if depth > 0 {
				depth--
				// 弹出栈顶
				if len(loopStack) > 0 {
					loopStack = loopStack[:len(loopStack)-1]
				}
			}

		case StmtLet:
			// 在循环体内声明的变量
			if depth > 0 && len(loopStack) > 0 {
				currentLoop := loopStack[len(loopStack)-1]
				currentLoop.BodyVars = append(currentLoop.BodyVars, stmt.VarName)
			}

		case StmtRead:
			if depth > 0 && len(loopStack) > 0 {
				currentLoop := loopStack[len(loopStack)-1]
				// 如果读取的变量不在 BodyVars 中，说明是外部变量
				if !containsString(currentLoop.BodyVars, stmt.VarName) {
					currentLoop.ExternalVars = appendUnique(currentLoop.ExternalVars, stmt.VarName)
				}
			}

		case StmtWrite:
			if depth > 0 && len(loopStack) > 0 {
				currentLoop := loopStack[len(loopStack)-1]
				if !containsString(currentLoop.BodyVars, stmt.VarName) {
					currentLoop.ExternalVars = appendUnique(currentLoop.ExternalVars, stmt.VarName)
				}
			}

		case StmtYeide:
			if depth > 0 && len(loopStack) > 0 {
				currentLoop := loopStack[len(loopStack)-1]
				if !containsString(currentLoop.BodyVars, stmt.SrcName) {
					currentLoop.ExternalVars = appendUnique(currentLoop.ExternalVars, stmt.SrcName)
				}
				// yeide 目标也算作循环体变量
				currentLoop.BodyVars = append(currentLoop.BodyVars, stmt.VarName)
			}

		case StmtRelease:
			if depth > 0 && len(loopStack) > 0 {
				currentLoop := loopStack[len(loopStack)-1]
				if !containsString(currentLoop.BodyVars, stmt.SrcName) {
					currentLoop.ExternalVars = appendUnique(currentLoop.ExternalVars, stmt.SrcName)
				}
			}
		}
	}
}

// GetReusableVars 返回循环体内声明且不在外部使用的变量
// 这些变量在 bump pool 中可以按 max(varSize) 而非 sum(varSize) 计算
func (li *LoopInfo) GetReusableVars() []string {
	var reusable []string
	for _, v := range li.BodyVars {
		if !containsString(li.ExternalVars, v) {
			reusable = append(reusable, v)
		}
	}
	return reusable
}

// EstimateLoopPoolContribution 估算循环对池容量的贡献
// 可复用变量按 max(varSize) 计算，不可复用变量按 sum(varSize) 计算
// 如果迭代次数已知，不可复用变量仍按 sum 计算
func (li *LoopInfo) EstimateLoopPoolContribution(sizes map[string]int, tracker *OwnershipTracker) int {
	if tracker == nil {
		return 0
	}

	reusable := li.GetReusableVars()
	reusableSet := make(map[string]bool, len(reusable))
	for _, v := range reusable {
		reusableSet[v] = true
	}

	maxReusable := 0
	sumNonReusable := 0

	// 计算可复用变量的最大值
	for _, varName := range reusable {
		if objID := tracker.GetObjectByName(varName); objID != "" {
			if size, ok := sizes[objID]; ok && size > 0 {
				if size > maxReusable {
					maxReusable = size
				}
			}
		}
	}

	// 计算不可复用变量的总和
	for _, varName := range li.BodyVars {
		if !reusableSet[varName] {
			if objID := tracker.GetObjectByName(varName); objID != "" {
				if size, ok := sizes[objID]; ok && size > 0 {
					sumNonReusable += size
				}
			}
		}
	}

	return maxReusable + sumNonReusable
}

// ============================================================================
// 辅助函数
// ============================================================================

// containsString 检查字符串切片中是否包含指定字符串
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// appendUnique 追加字符串到切片，避免重复
func appendUnique(slice []string, s string) []string {
	if !containsString(slice, s) {
		return append(slice, s)
	}
	return slice
}
