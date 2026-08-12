package sor

import "fmt"

// ============================================================================
// DAG 环检测器
// ============================================================================

// DAGChecker 用于检测 release 关系图中是否存在环。
// release 关系必须构成有向无环图（DAG），以避免循环引用导致的所有权混乱。
//
// 图的含义：
//   - 节点：对象 ID
//   - 有向边 A -> B：表示 A release 给了 B（B 持有 A 的只读引用）
//
// 环的含义：
//   - 如果存在 A -> B -> C -> A 的环，表示 A 间接地持有自己的只读引用，
//     这在 SOR 系统中是非法的，因为它违反了所有权的层次结构。
type DAGChecker struct {
	// adj 是邻接表：key 是源节点 ID，value 是目标节点 ID 列表。
	adj map[string][]string

	// edges 记录所有添加过的边，用于错误报告。
	edges []ReleaseEdge

	// edgeMap 记录边的源行号信息，key 为 "from->to"。
	edgeMap map[string]int
}

// NewDAGChecker 创建一个新的 DAG 环检测器。
func NewDAGChecker() *DAGChecker {
	return &DAGChecker{
		adj:     make(map[string][]string),
		edges:   make([]ReleaseEdge, 0),
		edgeMap: make(map[string]int),
	}
}

// AddEdge 添加一条 release 边到图中。
// from 是被 release 的对象，to 是获得只读访问权的持有者。
// 返回 true 表示添加成功，返回 false 表示边已存在（重复 release）。
func (d *DAGChecker) AddEdge(from, to string, sourceLine int) bool {
	key := from + "->" + to
	if _, exists := d.edgeMap[key]; exists {
		return false // 重复边
	}

	d.edgeMap[key] = sourceLine
	d.adj[from] = append(d.adj[from], to)
	d.edges = append(d.edges, ReleaseEdge{
		From:       from,
		To:         to,
		SourceLine: sourceLine,
	})

	// 确保 to 节点也存在于邻接表中（可能没有出边）
	if _, exists := d.adj[to]; !exists {
		d.adj[to] = d.adj[to] // 保持为 nil/空
	}

	return true
}

// HasCycle 检测图中是否存在环。
// 使用 DFS 三色标记法：
//   - 白色（未访问）：节点尚未被访问
//   - 灰色（访问中）：节点在当前 DFS 路径中
//   - 黑色（已完成）：节点及其所有后代已处理完毕
//
// 如果在 DFS 中遇到灰色节点，则存在环。
func (d *DAGChecker) HasCycle() bool {
	color := make(map[string]int) // 0=白, 1=灰, 2=黑

	// 初始化所有节点为白色
	for node := range d.adj {
		color[node] = 0
	}

	// 对每个未访问的节点启动 DFS
	for node := range d.adj {
		if color[node] == 0 {
			if d.dfsHasCycle(node, color) {
				return true
			}
		}
	}

	return false
}

// dfsHasCycle 是 DFS 环检测的辅助函数。
// 返回 true 表示从 node 出发的子图中存在环。
func (d *DAGChecker) dfsHasCycle(node string, color map[string]int) bool {
	color[node] = 1 // 标记为灰色（访问中）

	for _, neighbor := range d.adj[node] {
		switch color[neighbor] {
		case 0: // 白色：未访问，继续 DFS
			if d.dfsHasCycle(neighbor, color) {
				return true
			}
		case 1: // 灰色：遇到了当前路径中的节点，存在环
			return true
		case 2: // 黑色：已处理完毕，跳过
			// 继续
		}
	}

	color[node] = 2 // 标记为黑色（已完成）
	return false
}

// GetCyclePath 获取图中一个环的路径（用于错误报告）。
// 如果图中没有环，返回 nil。
//
// 返回的路径格式：[A, B, C, A] 表示 A -> B -> C -> A 的环。
func (d *DAGChecker) GetCyclePath() []string {
	color := make(map[string]int)   // 0=白, 1=灰, 2=黑
	path := make([]string, 0)       // 当前 DFS 路径
	pathSet := make(map[string]int) // 节点在 path 中的索引

	// 初始化所有节点为白色
	for node := range d.adj {
		color[node] = 0
	}

	// 对每个未访问的节点启动 DFS
	for node := range d.adj {
		if color[node] == 0 {
			cycle := d.dfsFindCycle(node, color, &path, pathSet)
			if cycle != nil {
				return cycle
			}
		}
	}

	return nil
}

// dfsFindCycle 是寻找环路径的 DFS 辅助函数。
// 返回环路径（包含起点和终点），如果没有环返回 nil。
func (d *DAGChecker) dfsFindCycle(
	node string,
	color map[string]int,
	path *[]string,
	pathSet map[string]int,
) []string {
	color[node] = 1 // 标记为灰色
	*path = append(*path, node)
	pathSet[node] = len(*path) - 1

	for _, neighbor := range d.adj[node] {
		switch color[neighbor] {
		case 0: // 白色：继续 DFS
			cycle := d.dfsFindCycle(neighbor, color, path, pathSet)
			if cycle != nil {
				return cycle
			}
		case 1: // 灰色：找到环！
			// 从 path 中提取环：从 neighbor 的位置到当前节点，再加上 neighbor
			startIdx := pathSet[neighbor]
			cycle := make([]string, len(*path)-startIdx+1)
			copy(cycle, (*path)[startIdx:])
			cycle[len(cycle)-1] = neighbor
			return cycle
		case 2: // 黑色：跳过
			// 继续
		}
	}

	// 回溯
	color[node] = 2 // 标记为黑色
	*path = (*path)[:len(*path)-1]
	delete(pathSet, node)

	return nil
}

// GetAllEdges 返回所有已添加的边。
func (d *DAGChecker) GetAllEdges() []ReleaseEdge {
	return d.edges
}

// GetNodeCount 返回图中的节点数。
func (d *DAGChecker) GetNodeCount() int {
	return len(d.adj)
}

// GetEdgeCount 返回图中的边数。
func (d *DAGChecker) GetEdgeCount() int {
	return len(d.edges)
}

// TopologicalSort 对图进行拓扑排序。
// 如果图中有环，返回 nil 和 false。
// 用于分析 release 关系的层次结构。
func (d *DAGChecker) TopologicalSort() ([]string, bool) {
	// 计算入度
	inDegree := make(map[string]int)
	for node := range d.adj {
		if _, exists := inDegree[node]; !exists {
			inDegree[node] = 0
		}
		for _, neighbor := range d.adj[node] {
			inDegree[neighbor]++
		}
	}

	// 找到所有入度为 0 的节点
	queue := make([]string, 0)
	for node, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, node)
		}
	}

	result := make([]string, 0, len(inDegree))
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		result = append(result, node)

		for _, neighbor := range d.adj[node] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	// 如果结果长度不等于节点数，说明有环
	if len(result) != len(inDegree) {
		return nil, false
	}

	return result, true
}

// String 返回图的可读描述。
func (d *DAGChecker) String() string {
	result := fmt.Sprintf("DAG (%d nodes, %d edges):\n", d.GetNodeCount(), d.GetEdgeCount())
	for _, edge := range d.edges {
		result += fmt.Sprintf("  %s\n", edge.String())
	}
	return result
}
