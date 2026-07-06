package sor

// ============================================================================
// Pass 6: 轻量级 Points-To 分析 — 识别 release holder 的共享目标
// ============================================================================

// PointsToSet 存储对象的指向关系
// key = holder ID, value = 指向的 source ID（共享同一数据）
type PointsToSet struct {
	// holders 持有者 -> 源对象的映射
	// release src -> [holder_a, holder_b] 意味着 holder_a 和 holder_b 都指向 src
	holders map[string]string // holderID -> sourceID

	// sources 源对象 -> 持有者的映射
	sources map[string][]string // sourceID -> []holderID
}

// NewPointsToSet 创建指向集
func NewPointsToSet() *PointsToSet {
	return &PointsToSet{
		holders: make(map[string]string),
		sources: make(map[string][]string),
	}
}

// AddRelation 添加指向关系：holder 指向 source
func (pts *PointsToSet) AddRelation(holderID, sourceID string) {
	pts.holders[holderID] = sourceID
	pts.sources[sourceID] = append(pts.sources[sourceID], holderID)
}

// GetSharedSource 获取 holder 共享的源对象
// 如果 holder 是 release 产生的持有者，返回源对象 ID
// 如果不是持有者，返回空字符串
func (pts *PointsToSet) GetSharedSource(holderID string) string {
	return pts.holders[holderID]
}

// GetSharedHolders 获取共享同一源对象的所有持有者
func (pts *PointsToSet) GetSharedHolders(sourceID string) []string {
	return pts.sources[sourceID]
}

// IsHolder 判断对象是否为 release 持有者
func (pts *PointsToSet) IsHolder(holderID string) bool {
	_, ok := pts.holders[holderID]
	return ok
}

// GetSharedSourceSize 获取 holder 共享的源对象大小
// 如果 holder 指向 source，返回 source 的大小（因为共享同一数据）
func (pts *PointsToSet) GetSharedSourceSize(holderID string, sizes map[string]int) int {
	sourceID := pts.GetSharedSource(holderID)
	if sourceID == "" {
		return 0
	}
	if size, ok := sizes[sourceID]; ok {
		return size
	}
	return 0
}

// BuildPointsToSet 从 OwnershipTracker 构建指向关系
// 分析 release 操作产生的 holder 共享关系
func BuildPointsToSet(tracker *OwnershipTracker) *PointsToSet {
	pts := NewPointsToSet()

	if tracker == nil {
		return pts
	}

	// 遍历所有对象，找到 release 持有者
	allObjs := tracker.GetAllObjects()
	for _, obj := range allObjs {
		if obj == nil {
			continue
		}
		// 如果对象是 released 状态且有 release 持有者
		if obj.State == StateReleased && len(obj.ReleaseHolders) > 0 {
			for _, holderID := range obj.ReleaseHolders {
				pts.AddRelation(holderID, obj.ID)
			}
		}
	}

	return pts
}

// EstimatePoolAdjustment 基于指向关系估算池容量调整
// 释放 holder 的大小（因为与 source 共享同一数据）
func (pts *PointsToSet) EstimatePoolAdjustment(sizes map[string]int) int {
	adjusted := 0

	// 对每个 source，只计算一次大小
	// 持有者不额外占用内存（指针引用）
	visitedSources := make(map[string]bool)
	for holderID, sourceID := range pts.holders {
		if visitedSources[sourceID] {
			// source 已经计算过，holder 不需要额外空间
			// 但 holder 的大小不应该被重复计算
			if holderSize, ok := sizes[holderID]; ok {
				adjusted -= holderSize // 减去 holder 的重复计算
			}
			continue
		}
		visitedSources[sourceID] = true
	}

	return adjusted
}

// FormatPointsToSummary 格式化指向分析结果
func FormatPointsToSummary(pts *PointsToSet) string {
	if pts == nil || len(pts.holders) == 0 {
		return "(no points-to results)"
	}

	result := "=== Points-To Analysis ===\n"
	for holderID, sourceID := range pts.holders {
		result += "  " + holderID + " -> " + sourceID + "\n"
	}
	return result
}
