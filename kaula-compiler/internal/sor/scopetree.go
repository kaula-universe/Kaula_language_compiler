package sor

// ============================================================================
// Pass 5: 作用域树分析器 — 分析嵌套作用域的空间复用
// ============================================================================

// ScopeType 作用域类型
type ScopeType int

const (
	ScopeSequential ScopeType = iota // 顺序执行块
	ScopeBranch                      // if/else 互斥分支
	ScopeLoop                        // 循环体
	ScopeFunction                    // 函数体
)

// ScopeNode 作用域树节点
type ScopeNode struct {
	Type      ScopeType    // 作用域类型
	Stmts     []Stmt       // 该作用域内的语句
	Children  []*ScopeNode // 子作用域
	Parent    *ScopeNode   // 父作用域
	TotalSize int          // 该作用域内所有对象的总大小
	MaxSize   int          // 该作用域内单个最大对象的大小（用于分支互斥）
}

// ScopeTreeAnalyzer 作用域树分析器
type ScopeTreeAnalyzer struct {
	root *ScopeNode
}

// NewScopeTreeAnalyzer 创建作用域树分析器
func NewScopeTreeAnalyzer() *ScopeTreeAnalyzer {
	return &ScopeTreeAnalyzer{}
}

// BuildScopeTree 从语句列表构建作用域树
func (sta *ScopeTreeAnalyzer) BuildScopeTree(stmts []Stmt) *ScopeNode {
	sta.root = &ScopeNode{
		Type:     ScopeSequential,
		Stmts:    make([]Stmt, 0),
		Children: make([]*ScopeNode, 0),
	}
	sta.buildFromStmts(stmts, sta.root)
	return sta.root
}

// buildFromStmts 递归构建作用域树
func (sta *ScopeTreeAnalyzer) buildFromStmts(stmts []Stmt, parent *ScopeNode) {
	sequentialBlock := &ScopeNode{
		Type:     ScopeSequential,
		Stmts:    make([]Stmt, 0),
		Children: make([]*ScopeNode, 0),
		Parent:   parent,
	}

	i := 0
	for i < len(stmts) {
		stmt := stmts[i]

		switch stmt.Kind {
		case StmtBranchEnter:
			// if/else 分支
			branchNode := &ScopeNode{
				Type:     ScopeBranch,
				Stmts:    make([]Stmt, 0),
				Children: make([]*ScopeNode, 0),
				Parent:   sequentialBlock,
			}

			// 收集 if body
			ifBody := &ScopeNode{
				Type:     ScopeSequential,
				Stmts:    make([]Stmt, 0),
				Children: make([]*ScopeNode, 0),
				Parent:   branchNode,
			}
			branchNode.Children = append(branchNode.Children, ifBody)

			depth := 1
			i++
			// 收集 if body 的语句
			for i < len(stmts) && depth > 0 {
				s := stmts[i]
				switch s.Kind {
				case StmtBranchEnter:
					depth++
					ifBody.Stmts = append(ifBody.Stmts, s)
				case StmtBranchExit:
					depth--
					if depth > 0 {
						ifBody.Stmts = append(ifBody.Stmts, s)
					}
				case StmtBranchElse:
					if depth == 1 {
						// if body 结束，开始 else body
						i++
						elseBody := &ScopeNode{
							Type:     ScopeSequential,
							Stmts:    make([]Stmt, 0),
							Children: make([]*ScopeNode, 0),
							Parent:   branchNode,
						}
						branchNode.Children = append(branchNode.Children, elseBody)
						// 收集 else body 的语句
						elseDepth := 1
						for i < len(stmts) {
							es := stmts[i]
							switch es.Kind {
							case StmtBranchEnter:
								elseDepth++
								elseBody.Stmts = append(elseBody.Stmts, es)
							case StmtBranchExit:
								elseDepth--
								if elseDepth > 0 {
									elseBody.Stmts = append(elseBody.Stmts, es)
								} else {
									// else body 结束
									i++
									goto branchDone
								}
							default:
								elseBody.Stmts = append(elseBody.Stmts, es)
							}
							i++
						}
					} else {
						ifBody.Stmts = append(ifBody.Stmts, s)
					}
				default:
					ifBody.Stmts = append(ifBody.Stmts, s)
				}
				i++
			}
		branchDone:
			sequentialBlock.Children = append(sequentialBlock.Children, branchNode)

		case StmtLoopEnter:
			// 循环体
			loopNode := &ScopeNode{
				Type:     ScopeLoop,
				Stmts:    []Stmt{stmt},
				Children: make([]*ScopeNode, 0),
				Parent:   sequentialBlock,
			}

			// 收集循环体语句
			loopBody := &ScopeNode{
				Type:     ScopeSequential,
				Stmts:    make([]Stmt, 0),
				Children: make([]*ScopeNode, 0),
				Parent:   loopNode,
			}
			loopNode.Children = append(loopNode.Children, loopBody)

			depth := 1
			i++
			for i < len(stmts) && depth > 0 {
				s := stmts[i]
				switch s.Kind {
				case StmtLoopEnter:
					depth++
					loopBody.Stmts = append(loopBody.Stmts, s)
				case StmtLoopExit:
					depth--
					if depth > 0 {
						loopBody.Stmts = append(loopBody.Stmts, s)
					} else {
						loopNode.Stmts = append(loopNode.Stmts, s)
						i++
						goto loopDone
					}
				default:
					loopBody.Stmts = append(loopBody.Stmts, s)
				}
				i++
			}
		loopDone:
			sequentialBlock.Children = append(sequentialBlock.Children, loopNode)

		default:
			sequentialBlock.Stmts = append(sequentialBlock.Stmts, stmt)
		}
		i++
	}

	// 如果 sequential block 有内容，添加到父节点
	if len(sequentialBlock.Stmts) > 0 || len(sequentialBlock.Children) > 0 {
		parent.Children = append(parent.Children, sequentialBlock)
	}
}

// EstimatePoolCapacity 估算作用域树的池容量需求
// 分支互斥取最大值，顺序块支持空间复用
func (sta *ScopeTreeAnalyzer) EstimatePoolCapacity(sizes map[string]int, tracker *OwnershipTracker) int {
	if sta.root == nil {
		return 0
	}
	return sta.estimateNode(sta.root, sizes, tracker)
}

// estimateNode 递归估算节点的池容量
func (sta *ScopeTreeAnalyzer) estimateNode(node *ScopeNode, sizes map[string]int, tracker *OwnershipTracker) int {
	if node == nil {
		return 0
	}

	// 计算当前节点语句中对象的大小
	nodeSize := 0
	for _, stmt := range node.Stmts {
		if stmt.Kind == StmtLet {
			if objID := tracker.GetObjectByName(stmt.VarName); objID != "" {
				if size, ok := sizes[objID]; ok && size > 0 {
					nodeSize += size
				} else {
					nodeSize += 256 // 保守默认值
				}
			}
		}
	}

	// 递归计算子节点
	childrenSize := 0
	for _, child := range node.Children {
		childSize := sta.estimateNode(child, sizes, tracker)
		switch node.Type {
		case ScopeBranch:
			// 分支互斥：取最大值
			if childSize > childrenSize {
				childrenSize = childSize
			}
		case ScopeLoop:
			// 循环体：只需计算一次（迭代间复用）
			childrenSize += childSize
		case ScopeSequential:
			// 顺序块：累加（暂时不优化空间复用，需要活跃性分析支持）
			childrenSize += childSize
		default:
			childrenSize += childSize
		}
	}

	return nodeSize + childrenSize
}

// GetMaxBranchSize 获取分支互斥后的最大分支大小
func (sta *ScopeTreeAnalyzer) GetMaxBranchSize(sizes map[string]int, tracker *OwnershipTracker) int {
	if sta.root == nil {
		return 0
	}
	return sta.findMaxBranch(sta.root, sizes, tracker)
}

// findMaxBranch 递归查找最大分支大小
func (sta *ScopeTreeAnalyzer) findMaxBranch(node *ScopeNode, sizes map[string]int, tracker *OwnershipTracker) int {
	if node == nil {
		return 0
	}

	maxSize := 0
	for _, child := range node.Children {
		if child.Type == ScopeBranch {
			for _, branch := range child.Children {
				size := sta.estimateNode(branch, sizes, tracker)
				if size > maxSize {
					maxSize = size
				}
			}
		} else {
			size := sta.findMaxBranch(child, sizes, tracker)
			if size > maxSize {
				maxSize = size
			}
		}
	}
	return maxSize
}
