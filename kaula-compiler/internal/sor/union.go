package sor

import (
	"fmt"
	"sort"
	"strings"
)

// ============================================================================
// GetAllEdgesSorted — DAGChecker 补充方法
// ============================================================================

// GetAllEdgesSorted 返回所有已添加的边，按 From + To 字典序排序。
// 用于联合域分析中需要确定性遍历顺序的场景。
// 此方法作为 package sor 的补充定义，与 dag.go 中的 DAGChecker 方法合并。
func (d *DAGChecker) GetAllEdgesSorted() []ReleaseEdge {
	sorted := make([]ReleaseEdge, len(d.edges))
	copy(sorted, d.edges)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].From != sorted[j].From {
			return sorted[i].From < sorted[j].From
		}
		return sorted[i].To < sorted[j].To
	})
	return sorted
}

// ============================================================================
// 联合域 release 编译期分析
// ============================================================================

// UnionReleaseInfo 联合域 release 的编译期分析结果。
// 描述一个对象被 release 后，各持有者之间的选举关系、零拷贝决策和清理顺序。
//
// 核心思想：
//   - 选举在编译期完全确定，运行时无需任何判断分支
//   - 第一个拓扑序中的 holder 是初始 elected writer
//   - 后续 holder 按拓扑序继承选举权（elected → reader）
//   - 联合域 release 默认零拷贝（指针引用），普通 release 对值类型做值拷贝
type UnionReleaseInfo struct {
	// SourceID 被共享的对象 ID
	SourceID string

	// SourceName 被共享的变量名
	SourceName string

	// HolderIDs 持有者 ID 列表（按选举序排列）
	HolderIDs []string

	// HolderNames 持有者名称列表（按选举序排列）
	HolderNames []string

	// IsUnion 是否为 union release（vs 普通 release）
	IsUnion bool

	// Elected 编译期选举结果：每个持有者的写权限范围。
	// elected[i] = true 表示 holders[i] 在其作用域内是 elected writer。
	// 联合域中，只有第一个（拓扑序最早的）holder 是 elected writer。
	Elected []bool

	// ZeroCopy 零拷贝决策：每个持有者是否可以使用零拷贝指针。
	// zeroCopy[i] = true 表示 holders[i] 可以用指针引用（不需值拷贝）。
	ZeroCopy []bool

	// CleanupHolder 清理顺序：拓扑排序后，哪个持有者负责最终清理。
	// -1 表示 source 自身清理。
	CleanupHolder int
}

// String 返回分析结果的可读描述（调试用）。
func (info *UnionReleaseInfo) String() string {
	var b strings.Builder
	tag := "普通 release"
	if info.IsUnion {
		tag = "union release"
	}
	b.WriteString(fmt.Sprintf("=== %s: %s (%s) ===\n", tag, info.SourceName, info.SourceID))
	b.WriteString(fmt.Sprintf("  持有者 (%d):\n", len(info.HolderNames)))
	for i, name := range info.HolderNames {
		electedStr := "reader"
		if info.Elected[i] {
			electedStr = "elected writer"
		}
		zeroStr := "值拷贝"
		if info.ZeroCopy[i] {
			zeroStr = "零拷贝(指针)"
		}
		b.WriteString(fmt.Sprintf("    [%d] %s: %s, %s\n", i, name, electedStr, zeroStr))
	}
	if info.CleanupHolder >= 0 && info.CleanupHolder < len(info.HolderNames) {
		b.WriteString(fmt.Sprintf("  清理持有者: %s\n", info.HolderNames[info.CleanupHolder]))
	} else {
		b.WriteString("  清理: source 自身清理\n")
	}
	return b.String()
}

// ============================================================================
// UnionAnalyzer 联合域编译期分析器
// ============================================================================

// UnionAnalyzer 联合域编译期分析器。
// 从 SOR 分析器的 DAG 和对象状态中提取所有 union/release 信息，
// 生成编译期选举序列和零拷贝决策。
type UnionAnalyzer struct {
	// results 存储所有 release 的分析结果，key = sourceID。
	results map[string]*UnionReleaseInfo
}

// NewUnionAnalyzer 创建一个新的联合域分析器。
func NewUnionAnalyzer() *UnionAnalyzer {
	return &UnionAnalyzer{
		results: make(map[string]*UnionReleaseInfo),
	}
}

// GetResult 获取指定 sourceID 的联合域分析结果。
func (ua *UnionAnalyzer) GetResult(sourceID string) *UnionReleaseInfo {
	return ua.results[sourceID]
}

// GetResults 返回所有联合域分析结果。
func (ua *UnionAnalyzer) GetResults() map[string]*UnionReleaseInfo {
	return ua.results
}

// ----------------------------------------------------------------------------
// 方法 1: AnalyzeUnionReleases
// ----------------------------------------------------------------------------

// AnalyzeUnionReleases 从 SOR 分析器的 DAG 和对象状态中提取所有 union/release 信息，
// 生成编译期选举序列和零拷贝决策。
//
// 输入：
//   - dag:     DAGChecker，包含所有 release 边的 DAG 结构
//   - tracker: OwnershipTracker，包含所有对象的所有权状态
//   - unionReleases: 外部传入的 union 标记集合，key = sourceID，value = true
//     用于标识哪些 source 的 release 属于联合域（union）类型。
//
// 逻辑流程：
//  1. 遍历所有 ReleaseEdge，按 From（source）分组
//  2. 对每组 release，检查 source 对象状态
//  3. 通过 unionReleases 判断是 union release 还是普通 release
//  4. 使用拓扑排序确定选举序列
//  5. 根据联合域/普通域的规则生成零拷贝决策
func (ua *UnionAnalyzer) AnalyzeUnionReleases(
	dag *DAGChecker,
	tracker *OwnershipTracker,
	unionReleases map[string]bool,
) map[string]*UnionReleaseInfo {
	if dag == nil || tracker == nil {
		return ua.results
	}

	ua.results = make(map[string]*UnionReleaseInfo)

	// 步骤 1：遍历所有边，按 From 分组
	edges := dag.GetAllEdgesSorted()
	groups := make(map[string][]ReleaseEdge) // sourceID -> 边列表
	for _, edge := range edges {
		groups[edge.From] = append(groups[edge.From], edge)
	}

	// 步骤 2-5：对每个 source 进行分析
	for sourceID, edgeGroup := range groups {
		// 获取 source 对象信息
		sourceObj := tracker.GetObject(sourceID)
		if sourceObj == nil {
			continue
		}

		// 构建 holder 信息
		holderIDs := make([]string, 0, len(edgeGroup))
		holderNames := make([]string, 0, len(edgeGroup))
		for _, edge := range edgeGroup {
			holderIDs = append(holderIDs, edge.To)
			// 尝试获取 holder 的变量名
			if holderObj := tracker.GetObject(edge.To); holderObj != nil {
				holderNames = append(holderNames, holderObj.Name)
			} else {
				holderNames = append(holderNames, edge.To)
			}
		}

		// 判断是 union release 还是普通 release
		isUnion := IsUnionRelease(sourceID, unionReleases)

		// 构建基础分析结果
		info := &UnionReleaseInfo{
			SourceID:    sourceID,
			SourceName:  sourceObj.Name,
			HolderIDs:   holderIDs,
			HolderNames: holderNames,
			IsUnion:     isUnion,
			Elected:     make([]bool, len(holderIDs)),
			ZeroCopy:    make([]bool, len(holderIDs)),
			CleanupHolder: -1,
		}

		// 步骤 4：选举序列
		ua.computeElectionSequence(info, dag, tracker)

		// 步骤 5：零拷贝决策
		ua.computeZeroCopyDecision(info, tracker)

		ua.results[sourceID] = info
	}

	return ua.results
}

// computeElectionSequence 使用拓扑排序确定选举序列。
//
// 联合域选举规则：
//   - 第一个拓扑序中的 holder 是初始 elected writer
//   - 如果 holders 之间有依赖边（A->B 表示 A 必须先退出），B 继承选举
//   - 简化实现：按 holders 在代码中的出现顺序 + 拓扑序生成选举序列
//
// 普通域选举规则：
//   - 所有 holder 都是 reader（无 elected writer）
func (ua *UnionAnalyzer) computeElectionSequence(
	info *UnionReleaseInfo,
	dag *DAGChecker,
	tracker *OwnershipTracker,
) {
	if !info.IsUnion || len(info.HolderIDs) == 0 {
		// 普通域：所有 holder 都是 reader，无 elected writer
		return
	}

	// 联合域：使用 DAG 拓扑排序确定选举序
	// 构建仅包含 source 和 holders 的子图
	holderSet := make(map[string]bool)
	for _, id := range info.HolderIDs {
		holderSet[id] = true
	}

	// 获取全局拓扑序
	topoOrder, ok := dag.TopologicalSort()
	if !ok {
		// 存在环（不应发生，DAG 已在分析阶段检查），回退到原始顺序
		// 第一个 holder 作为 elected writer
		if len(info.HolderIDs) > 0 {
			info.Elected[0] = true
		}
		info.CleanupHolder = len(info.HolderIDs) - 1
		return
	}

	// 在拓扑序中找到 holders 的出现位置，按拓扑序重新排列
	type indexedHolder struct {
		id    string
		index int // 在全局拓扑序中的位置
	}
	sortedHolders := make([]indexedHolder, 0, len(info.HolderIDs))
	for _, holderID := range info.HolderIDs {
		pos := -1
		for j, node := range topoOrder {
			if node == holderID {
				pos = j
				break
			}
		}
		sortedHolders = append(sortedHolders, indexedHolder{id: holderID, index: pos})
	}

	// 按拓扑序位置排序
	sort.Slice(sortedHolders, func(i, j int) bool {
		return sortedHolders[i].index < sortedHolders[j].index
	})

	// 重新排列 holderIDs、holderNames 和 elected 数组
	originalElected := info.Elected
	info.Elected = make([]bool, len(info.HolderIDs))
	reorderedIDs := make([]string, len(info.HolderIDs))
	reorderedNames := make([]string, len(info.HolderIDs))

	for i, h := range sortedHolders {
		reorderedIDs[i] = h.id
		if holderObj := tracker.GetObject(h.id); holderObj != nil {
			reorderedNames[i] = holderObj.Name
		} else {
			reorderedNames[i] = h.id
		}
	}

	info.HolderIDs = reorderedIDs
	info.HolderNames = reorderedNames

	// 第一个拓扑序中的 holder 是初始 elected writer
	if len(info.HolderIDs) > 0 {
		info.Elected[0] = true
	}

	// 检查 holders 之间的依赖边，确定选举继承
	// 如果 A -> B（A release 给 B），则 B 在 A 之后获得选举权
	for i := 0; i < len(info.HolderIDs)-1; i++ {
		for j := i + 1; j < len(info.HolderIDs); j++ {
			// 检查是否存在 A -> B 的边
			allEdges := dag.GetAllEdges()
			for _, edge := range allEdges {
				if edge.From == info.HolderIDs[i] && edge.To == info.HolderIDs[j] {
					// i 必须先退出，j 继承选举
					info.Elected[j] = true
					info.Elected[i] = false // i 退出后不再是 elected writer
					break
				}
			}
		}
	}

	// 清理顺序：拓扑序最后一个 holder 负责清理
	if len(info.HolderIDs) > 0 {
		info.CleanupHolder = len(info.HolderIDs) - 1
	}

	// 如果原始选举信息中有任何 elected，保留到新数组
	for i := range info.Elected {
		if i < len(originalElected) {
			info.Elected[i] = info.Elected[i] || originalElected[i]
		}
	}
}

// computeZeroCopyDecision 根据分析结果计算零拷贝决策。
//
// 决策规则：
//   - 联合域 release：默认零拷贝（指针引用），因为所有 holder 共享同一数据
//   - 普通 release：
//     - 如果 holder 的作用域 ⊆ source 的作用域 → 零拷贝（指针引用）
//     - 如果 holder 是指针类型 → 零拷贝
//     - 如果 holder 可能比 source 活得久 → 值拷贝
//     - 简化：所有 union release 默认零拷贝（指针），普通 release 对值类型做值拷贝
func (ua *UnionAnalyzer) computeZeroCopyDecision(
	info *UnionReleaseInfo,
	tracker *OwnershipTracker,
) {
	sourceObj := tracker.GetObject(info.SourceID)
	if sourceObj == nil {
		return
	}

	for i, holderID := range info.HolderIDs {
		holderObj := tracker.GetObject(holderID)
		if holderObj == nil {
			// 对象不存在，默认零拷贝
			info.ZeroCopy[i] = true
			continue
		}

		if info.IsUnion {
			// 联合域 release：默认零拷贝
			info.ZeroCopy[i] = true
		} else {
			// 普通 release 的零拷贝决策
			// 规则 1：holder 的作用域 ⊆ source 的作用域 → 零拷贝
			if holderObj.ScopeID <= sourceObj.ScopeID {
				info.ZeroCopy[i] = true
				continue
			}

			// 规则 2：holder 是指针类型 → 零拷贝
			if strings.HasSuffix(holderObj.TypeName, "*") ||
				strings.HasPrefix(holderObj.TypeName, "[]") {
				info.ZeroCopy[i] = true
				continue
			}

			// 规则 3：holder 可能比 source 活得久 → 值拷贝
			if holderObj.ScopeID > sourceObj.ScopeID {
				info.ZeroCopy[i] = false
				continue
			}

			// 默认：零拷贝
			info.ZeroCopy[i] = true
		}
	}
}

// ----------------------------------------------------------------------------
// 方法 2: GenerateElectionCode
// ----------------------------------------------------------------------------

// GenerateElectionCode 为 union release 生成编译期选举的 C 代码。
// 由于选举在编译期确定，运行时只需按序生成代码，无任何判断分支。
//
// 生成的代码模式：
//
//	// union release data -> [scope_a, scope_b, scope_c]
//	// 编译期选举: scope_a 是初始写者
//	int64_t* scope_a_ref = &data;        // scope_a: elected writer (零拷贝)
//	const int64_t* scope_b_ref = &data;   // scope_b: reader (零拷贝)
//	const int64_t* scope_c_ref = &data;   // scope_c: reader (零拷贝)
func GenerateElectionCode(info *UnionReleaseInfo, sourceCType string, indent string) string {
	if info == nil || len(info.HolderNames) == 0 {
		return ""
	}

	var b strings.Builder

	// 注释头：union release 概览
	holderList := strings.Join(info.HolderNames, ", ")
	b.WriteString(fmt.Sprintf("%s// union release %s -> [%s]\n",
		indent, info.SourceName, holderList))

	// 编译期选举信息
	electedWriter := ""
	for i, name := range info.HolderNames {
		if i < len(info.Elected) && info.Elected[i] {
			electedWriter = name
			break
		}
	}
	if electedWriter != "" {
		b.WriteString(fmt.Sprintf("%s// 编译期选举: %s 是初始写者\n",
			indent, electedWriter))
	}

	// 为每个 holder 生成代码
	// 去除 sourceCType 中可能的指针后缀，统一处理
	baseType := strings.TrimSuffix(sourceCType, "*")
	for i, name := range info.HolderNames {
		isZeroCopy := i < len(info.ZeroCopy) && info.ZeroCopy[i]
		isElected := i < len(info.Elected) && info.Elected[i]

		if isElected {
			// elected writer：可写指针
			if isZeroCopy {
				b.WriteString(fmt.Sprintf("%s%s* %s_ref = &%s;        // %s: elected writer (零拷贝)\n",
					indent, baseType, name, info.SourceName, name))
			} else {
				b.WriteString(fmt.Sprintf("%s%s %s = %s;        // %s: elected writer (值拷贝)\n",
					indent, baseType, name, info.SourceName, name))
			}
		} else {
			// reader：只读指针
			if isZeroCopy {
				b.WriteString(fmt.Sprintf("%sconst %s* %s_ref = &%s;   // %s: reader (零拷贝)\n",
					indent, baseType, name, info.SourceName, name))
			} else {
				b.WriteString(fmt.Sprintf("%sconst %s %s = %s;   // %s: reader (值拷贝)\n",
					indent, baseType, name, info.SourceName, name))
			}
		}
	}

	return b.String()
}

// ----------------------------------------------------------------------------
// 方法 3: GenerateZeroCopyReleaseCode
// ----------------------------------------------------------------------------

// GenerateZeroCopyReleaseCode 为普通 release 生成零拷贝 C 代码。
//
// 当分析确定可以零拷贝时：
//
//	// release data -> [reader_a, reader_b]
//	const int64_t* reader_a = &data;  // 零拷贝只读引用
//	const int64_t* reader_b = &data;  // 零拷贝只读引用
//
// 不能零拷贝时（值类型，holder 作用域更大）：
//
//	// release data -> [reader_a, reader_b]
//	int64_t reader_a = data;  // 值拷贝
//	int64_t reader_b = data;  // 值拷贝
func GenerateZeroCopyReleaseCode(info *UnionReleaseInfo, sourceCType string, indent string) string {
	if info == nil || len(info.HolderNames) == 0 {
		return ""
	}

	var b strings.Builder

	// 注释头
	holderList := strings.Join(info.HolderNames, ", ")
	b.WriteString(fmt.Sprintf("%s// release %s -> [%s]\n",
		indent, info.SourceName, holderList))

	// 去除 sourceCType 中可能的指针后缀
	baseType := strings.TrimSuffix(sourceCType, "*")

	for i, name := range info.HolderNames {
		isZeroCopy := i < len(info.ZeroCopy) && info.ZeroCopy[i]

		if isZeroCopy {
			// 零拷贝：只读指针引用
			b.WriteString(fmt.Sprintf("%sconst %s* %s = &%s;  // 零拷贝只读引用\n",
				indent, baseType, name, info.SourceName))
		} else {
			// 值拷贝
			b.WriteString(fmt.Sprintf("%s%s %s = %s;  // 值拷贝\n",
				indent, baseType, name, info.SourceName))
		}
	}

	return b.String()
}

// ----------------------------------------------------------------------------
// 方法 4: GenerateCleanupOrder
// ----------------------------------------------------------------------------

// GenerateCleanupOrder 根据拓扑排序生成清理代码。
// 编译期已确定清理顺序，无需运行时遍历。
//
// 输出格式：
//
//	/* cleanup order: scope_a -> scope_b -> scope_c -> source */
//
// 如果 CleanupHolder >= 0，最后一个 holder 负责最终清理（释放 source 的引用计数）。
// 如果 CleanupHolder == -1，source 自身在其作用域结束时清理。
func GenerateCleanupOrder(info *UnionReleaseInfo, indent string) string {
	if info == nil || len(info.HolderNames) == 0 {
		return ""
	}

	var b strings.Builder

	// 构建清理顺序链
	parts := make([]string, 0, len(info.HolderNames)+1)
	for _, name := range info.HolderNames {
		parts = append(parts, name)
	}
	if info.CleanupHolder < 0 {
		parts = append(parts, info.SourceName)
	} else if info.CleanupHolder < len(info.HolderNames) {
		// 由最后一个 holder 负责清理 source
		parts = append(parts, info.SourceName+" (由 "+info.HolderNames[info.CleanupHolder]+" 清理)")
	}

	orderStr := strings.Join(parts, " -> ")
	b.WriteString(fmt.Sprintf("%s/* cleanup order: %s */\n", indent, orderStr))

	// 生成清理操作代码
	if info.CleanupHolder >= 0 && info.CleanupHolder < len(info.HolderNames) {
		cleanerName := info.HolderNames[info.CleanupHolder]
		b.WriteString(fmt.Sprintf("%s/* %s: 最后一个 holder，负责清理 %s 的引用 */\n",
			indent, cleanerName, info.SourceName))
	} else {
		b.WriteString(fmt.Sprintf("%s/* %s: 自身作用域结束时清理 */\n",
			indent, info.SourceName))
	}

	return b.String()
}

// ============================================================================
// 辅助函数
// ============================================================================

// IsUnionRelease 判断一个 release 是否为 union release。
// 通过检查 sourceID 是否存在于外部传入的 unionSet 中。
//
// 由于 SORObject 没有 IsUnion 字段，用外部 map 传入以避免修改现有类型定义。
// 调用方应在编译前端解析时收集 union 标记信息，作为 map 传入。
//
// 参数：
//   - sourceID:    被 release 的对象 ID
//   - unionSet:    union 标记集合，key = sourceID，value = true 表示该对象是 union release
//
// 返回 true 表示该 release 属于联合域（union）类型。
func IsUnionRelease(sourceID string, unionSet map[string]bool) bool {
	if unionSet == nil {
		return false
	}
	return unionSet[sourceID]
}
