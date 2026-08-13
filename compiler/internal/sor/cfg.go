package sor

// EdgeKind 控制流边类型
type EdgeKind int

const (
	EdgeNormal EdgeKind = iota // 普通顺序执行
	EdgeBranch                 // if/else 分支边
	EdgeLoop                   // 循环回边
	EdgeMerge                  // 分支合并边
)

// BasicBlock represents a sequence of statements with no branches
type BasicBlock struct {
	ID        int
	Stmts     []Stmt
	Succs     []*BasicBlock
	Preds     []*BasicBlock
	EdgeKinds []EdgeKind // 与 Succs 对应的边类型
}

// CFG represents the control flow graph
type CFG struct {
	Blocks []*BasicBlock
	Entry  *BasicBlock
	Exit   *BasicBlock
}

// BuildCFG constructs a CFG from a statement list
// 识别 StmtLoopEnter/Exit 和 StmtBranchEnter/Else/Exit 标记，
// 将语句拆分为多个 BasicBlock 并建立正确的控制流边。
func BuildCFG(stmts []Stmt) *CFG {
	if len(stmts) == 0 {
		block := &BasicBlock{ID: 0}
		return &CFG{Blocks: []*BasicBlock{block}, Entry: block, Exit: block}
	}

	cfg := &CFG{}
	var blocks []*BasicBlock
	nextID := 0

	newBlock := func() *BasicBlock {
		b := &BasicBlock{ID: nextID}
		nextID++
		blocks = append(blocks, b)
		return b
	}

	addEdge := func(from, to *BasicBlock, kind EdgeKind) {
		from.Succs = append(from.Succs, to)
		from.EdgeKinds = append(from.EdgeKinds, kind)
		to.Preds = append(to.Preds, from)
	}

	// 扫描语句列表，按控制流标记拆分为基本块
	currentBlock := newBlock()
	cfg.Entry = currentBlock

	// 用于处理嵌套控制流的栈
	type contextKind int
	const (
		ctxLoop contextKind = iota
		ctxBranch
	)
	type ctxInfo struct {
		kind      contextKind
		headerBlk *BasicBlock // 循环头块 / 分支入口块
		exitBlk   *BasicBlock // 退出块（待创建）
		elseBlk   *BasicBlock // else 分支块（仅分支用）
	}
	var ctxStack []ctxInfo

	for _, stmt := range stmts {
		switch stmt.Kind {
		case StmtLoopEnter:
			// 结束当前块，创建循环头块
			loopHeader := newBlock()
			loopHeader.Stmts = append(loopHeader.Stmts, stmt)
			addEdge(currentBlock, loopHeader, EdgeNormal)
			ctxStack = append(ctxStack, ctxInfo{kind: ctxLoop, headerBlk: loopHeader})
			currentBlock = newBlock()
			addEdge(loopHeader, currentBlock, EdgeLoop)

		case StmtLoopExit:
			// 结束当前块，添加回边和退出块
			currentBlock.Stmts = append(currentBlock.Stmts, stmt)
			if len(ctxStack) > 0 {
				ctx := ctxStack[len(ctxStack)-1]
				ctxStack = ctxStack[:len(ctxStack)-1]
				// 回边：当前块 → 循环头
				addEdge(currentBlock, ctx.headerBlk, EdgeLoop)
			}
			exitBlock := newBlock()
			addEdge(currentBlock, exitBlock, EdgeNormal)
			currentBlock = exitBlock

		case StmtBranchEnter:
			// 结束当前块，创建分支入口块
			branchHeader := newBlock()
			branchHeader.Stmts = append(branchHeader.Stmts, stmt)
			addEdge(currentBlock, branchHeader, EdgeNormal)
			ctxStack = append(ctxStack, ctxInfo{kind: ctxBranch, headerBlk: branchHeader})
			// 创建 if body 块
			ifBody := newBlock()
			addEdge(branchHeader, ifBody, EdgeBranch)
			currentBlock = ifBody

		case StmtBranchElse:
			// 结束 if body 块，创建 else body 块
			if len(ctxStack) > 0 {
				ctx := ctxStack[len(ctxStack)-1]
				elseBody := newBlock()
				addEdge(ctx.headerBlk, elseBody, EdgeBranch)
				ctx.elseBlk = currentBlock // 记录 if body 用于后续合并
				currentBlock = elseBody
			}

		case StmtBranchExit:
			// 结束当前块，创建合并块
			currentBlock.Stmts = append(currentBlock.Stmts, stmt)
			mergeBlock := newBlock()
			addEdge(currentBlock, mergeBlock, EdgeMerge)
			// 如果有 else 分支，也需要连接到合并块
			if len(ctxStack) > 0 {
				ctx := ctxStack[len(ctxStack)-1]
				ctxStack = ctxStack[:len(ctxStack)-1]
				if ctx.elseBlk != nil {
					addEdge(ctx.elseBlk, mergeBlock, EdgeMerge)
				}
			}
			currentBlock = mergeBlock

		default:
			currentBlock.Stmts = append(currentBlock.Stmts, stmt)
		}
	}

	cfg.Exit = currentBlock
	cfg.Blocks = blocks
	return cfg
}

// GetLoopBlocks 返回循环内的基本块集合
// 通过检测 EdgeLoop 类型的回边来识别循环体
func (cfg *CFG) GetLoopBlocks() [][]*BasicBlock {
	var loops [][]*BasicBlock
	for _, block := range cfg.Blocks {
		for i, succ := range block.Succs {
			if i < len(block.EdgeKinds) && block.EdgeKinds[i] == EdgeLoop {
				// block → succ 是回边，succ 是循环头
				// 收集从 succ 到 block 之间的所有块作为循环体
				loopBody := collectLoopBody(succ, block, cfg)
				if len(loopBody) > 0 {
					loops = append(loops, loopBody)
				}
			}
		}
	}
	return loops
}

// collectLoopBody 收集循环头到回边源之间的所有基本块
func collectLoopBody(header, latch *BasicBlock, cfg *CFG) []*BasicBlock {
	visited := make(map[int]bool)
	var body []*BasicBlock
	var dfs func(b *BasicBlock)
	dfs = func(b *BasicBlock) {
		if visited[b.ID] || b == header && len(body) > 0 {
			return
		}
		visited[b.ID] = true
		body = append(body, b)
		for _, succ := range b.Succs {
			// 不越过循环头
			if succ == header && b != latch {
				continue
			}
			dfs(succ)
		}
	}
	// 从 header 的后继开始 DFS
	for _, succ := range header.Succs {
		if succ != header {
			dfs(succ)
		}
	}
	return body
}

// GetBranchPairs 返回 if/else 互斥分支对
// 每个元素是 [ifBodyBlocks, elseBodyBlocks]
func (cfg *CFG) GetBranchPairs() [][2][]*BasicBlock {
	var pairs [][2][]*BasicBlock
	for _, block := range cfg.Blocks {
		branchSuccs := 0
		for i, kind := range block.EdgeKinds {
			if kind == EdgeBranch {
				_ = i
				branchSuccs++
			}
		}
		if branchSuccs >= 2 {
			// 这是一个分支头，收集各分支的块
			var branchBodies [][]*BasicBlock
			for i, succ := range block.Succs {
				if i < len(block.EdgeKinds) && block.EdgeKinds[i] == EdgeBranch {
					body := collectBranchBody(succ, block, cfg)
					branchBodies = append(branchBodies, body)
				}
			}
			if len(branchBodies) >= 2 {
				pairs = append(pairs, [2][]*BasicBlock{branchBodies[0], branchBodies[1]})
			}
		}
	}
	return pairs
}

// collectBranchBody 收集分支头到合并点之间的基本块
func collectBranchBody(entry, header *BasicBlock, cfg *CFG) []*BasicBlock {
	visited := make(map[int]bool)
	var body []*BasicBlock
	var dfs func(b *BasicBlock)
	dfs = func(b *BasicBlock) {
		if visited[b.ID] {
			return
		}
		visited[b.ID] = true
		// 停止在合并块（有 EdgeMerge 入边的块）
		for _, pred := range b.Preds {
			if pred != entry && pred != header {
				// 可能是合并块，检查是否有 EdgeMerge 边
				for i, succ := range pred.Succs {
					if succ == b && i < len(pred.EdgeKinds) && pred.EdgeKinds[i] == EdgeMerge {
						return
					}
				}
			}
		}
		body = append(body, b)
		for _, succ := range b.Succs {
			dfs(succ)
		}
	}
	dfs(entry)
	return body
}
