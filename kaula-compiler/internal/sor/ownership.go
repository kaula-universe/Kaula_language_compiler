package sor

import (
	"fmt"
	"strings"
)

// ============================================================================
// 所有权追踪器
// ============================================================================

// OwnershipTracker 负责追踪所有对象的所有权状态。
// 它是 SOR 验证系统的核心数据结构，维护对象状态、DAG 关系和错误列表。
type OwnershipTracker struct {
	// objects 是所有受追踪对象的映射：key 为对象 ID。
	objects map[string]*SORObject

	// dag 是 release 关系的 DAG 检测器。
	dag *DAGChecker

	// errors 是验证过程中收集的所有错误。
	errors []SORError

	// nextID 是下一个对象 ID 的计数器。
	nextID int

	// currentScope 是当前作用域 ID（用于跨作用域检查）。
	currentScope int

	// scopeStack 是作用域栈，用于管理嵌套作用域。
	scopeStack []int

	// scopeObjects 记录每个作用域中创建的对象 ID，用于作用域结束时的清理检查。
	scopeObjects map[int][]string

	// threadID 是当前线程的 ID（用于多线程安全检查）。
	// 默认为 "main"。
	threadID string

	// threadObjects 记录每个线程拥有的对象（独占所有权的对象）。
	threadObjects map[string][]string
}

// NewOwnershipTracker 创建一个新的所有权追踪器。
func NewOwnershipTracker() *OwnershipTracker {
	tracker := &OwnershipTracker{
		objects:       make(map[string]*SORObject),
		dag:           NewDAGChecker(),
		errors:        make([]SORError, 0),
		nextID:        1,
		currentScope:  0,
		scopeStack:    []int{0},
		scopeObjects:  make(map[int][]string),
		threadID:      "main",
		threadObjects: make(map[string][]string),
	}
	tracker.scopeObjects[0] = make([]string, 0)
	return tracker
}

// ----------------------------------------------------------------------------
// 对象管理
// ----------------------------------------------------------------------------

// NewObject 创建一个新的受所有权追踪的对象。
// 返回对象的唯一 ID。
//
// 参数:
//   - name: 对象名称（变量名）
//   - typeName: 类型名称
//   - isComposite: 是否为复合类型（数组/结构体）
//   - sourceLine: 源码行号
func (t *OwnershipTracker) NewObject(
	name, typeName string,
	isComposite bool,
	sourceLine int,
) string {
	id := fmt.Sprintf("obj_%d", t.nextID)
	t.nextID++

	obj := &SORObject{
		ID:           id,
		Name:         name,
		State:        StateOwned, // 默认独占所有权
		TypeName:     typeName,
		IsComposite:  isComposite,
		Children:     make(map[string]string),
		SourceLine:   sourceLine,
		ScopeID:      t.currentScope,
	}

	t.objects[id] = obj
	t.scopeObjects[t.currentScope] = append(t.scopeObjects[t.currentScope], id)
	t.threadObjects[t.threadID] = append(t.threadObjects[t.threadID], id)

	return id
}

// GetObject 根据 ID 获取对象。
// 如果对象不存在，返回 nil。
func (t *OwnershipTracker) GetObject(id string) *SORObject {
	return t.objects[id]
}

// GetObjectByName 根据名称查找对象（在当前作用域链中）。
// 返回第一个匹配的对象 ID，如果没找到返回空字符串。
func (t *OwnershipTracker) GetObjectByName(name string) string {
	// 从当前作用域开始，沿着作用域栈向外查找
	for i := len(t.scopeStack) - 1; i >= 0; i-- {
		scopeID := t.scopeStack[i]
		for _, objID := range t.scopeObjects[scopeID] {
			if obj := t.objects[objID]; obj != nil && obj.Name == name {
				return objID
			}
		}
	}
	return ""
}

// MarkAsResource 将对象标记为资源类型。
// 资源类型在作用域结束时必须被显式释放或转移所有权，否则报资源泄漏错误。
func (t *OwnershipTracker) MarkAsResource(objID string, resourceKind string) bool {
	obj := t.objects[objID]
	if obj == nil {
		return false
	}
	obj.IsResource = true
	obj.ResourceKind = resourceKind
	return true
}

// ----------------------------------------------------------------------------
// 作用域管理
// ----------------------------------------------------------------------------

// EnterScope 进入一个新的作用域。
func (t *OwnershipTracker) EnterScope() {
	newScope := t.currentScope + 1
	t.scopeStack = append(t.scopeStack, newScope)
	t.currentScope = newScope
	t.scopeObjects[newScope] = make([]string, 0)
}

// ExitScope 退出当前作用域。
// 检查该作用域中创建的独占所有权对象是否都已正确处理（yeide 或释放）。
// 注意：release 状态的对象在作用域结束时自动失效。
// 资源类型（IsResource=true）如果在作用域结束时仍为 Owned 状态，报资源泄漏错误。
func (t *OwnershipTracker) ExitScope(sourceLine int) {
	if len(t.scopeStack) <= 1 {
		return // 不能退出全局作用域
	}

	scopeID := t.currentScope

	// 检查当前作用域中的对象
	for _, objID := range t.scopeObjects[scopeID] {
		obj := t.objects[objID]
		if obj == nil {
			continue
		}

		// 如果对象仍是 Owned 状态
		if obj.State == StateOwned {
			// 如果是资源类型，报资源泄漏错误
			if obj.IsResource {
				t.addError(SORError{
					Kind:       ErrResourceLeak,
					Message:    fmt.Sprintf("资源 '%s'（类型: %s, 种类: %s）在作用域结束时仍被持有，可能导致资源泄漏", obj.Name, obj.TypeName, obj.ResourceKind),
					SourceLine: sourceLine,
					ObjectID:   objID,
					Details:    "请在作用域结束前释放资源（调用释放函数）或转移所有权（yeide）",
				})
			}
			// 标记为 Moved（作用域结束，对象销毁）
			obj.State = StateMoved
		}
	}

	// 弹出作用域栈
	t.scopeStack = t.scopeStack[:len(t.scopeStack)-1]
	t.currentScope = t.scopeStack[len(t.scopeStack)-1]
}

// GetCurrentScope 返回当前作用域 ID。
func (t *OwnershipTracker) GetCurrentScope() int {
	return t.currentScope
}

// ----------------------------------------------------------------------------
// 线程管理
// ----------------------------------------------------------------------------

// SetThread 设置当前线程 ID（用于多线程安全检查）。
func (t *OwnershipTracker) SetThread(threadID string) {
	t.threadID = threadID
	if _, exists := t.threadObjects[threadID]; !exists {
		t.threadObjects[threadID] = make([]string, 0)
	}
}

// GetThread 返回当前线程 ID。
func (t *OwnershipTracker) GetThread() string {
	return t.threadID
}

// ----------------------------------------------------------------------------
// 三大原语：yeide
// ----------------------------------------------------------------------------

// Yeide 执行所有权转移：将 src 的所有权转移给 dst。
// 转移后，src 的状态变为 Moved（已转移，失效），
// dst 获得独占所有权（Owned 状态）。
//
// 这是 SOR 的核心原语之一：显式所有权转移。
//
// 参数:
//   - srcID: 源对象 ID（失去所有权）
//   - dstName: 目标对象名称（获得所有权的新变量）
//   - sourceLine: 源码行号
//
// 返回: 目标对象的 ID，如果转移失败返回空字符串。
func (t *OwnershipTracker) Yeide(srcID, dstName string, sourceLine int) string {
	src := t.objects[srcID]
	if src == nil {
		t.addError(SORError{
			Kind:       ErrYeideInvalidSource,
			Message:    fmt.Sprintf("yeide 失败：源对象 '%s' 不存在", srcID),
			SourceLine: sourceLine,
			ObjectID:   srcID,
		})
		return ""
	}

	// 检查源对象是否可以转移（必须是 Owned 状态）
	if src.State != StateOwned {
		t.addError(SORError{
			Kind:       ErrYeideInvalidSource,
			Message:    fmt.Sprintf("yeide 失败：源对象 '%s' 处于 %s 状态，无法转移所有权", src.Name, src.State),
			SourceLine: sourceLine,
			ObjectID:   srcID,
			Details:    "只有处于 Owned(独占) 状态的对象才能进行 yeide 操作",
		})
		return ""
	}

	// 创建目标对象（获得所有权）
	dstID := t.NewObject(dstName, src.TypeName, src.IsComposite, sourceLine)
	dst := t.objects[dstID]

	// 复制子元素信息（如果是复合类型）
	if src.IsComposite {
		for k, v := range src.Children {
			dst.Children[k] = v
		}
	}

	// 复制资源属性
	if src.IsResource {
		dst.IsResource = true
		dst.ResourceKind = src.ResourceKind
	}

	// 源对象标记为已转移
	src.State = StateMoved

	return dstID
}

// ----------------------------------------------------------------------------
// 三大原语：release
// ----------------------------------------------------------------------------

// Release 将对象的只读访问权分发给多个持有者。
// 源对象的状态从 Owned 变为 Released，
// 所有持有者共享只读访问权限。
//
// release 关系必须构成 DAG（有向无环图），不允许循环引用。
//
// 这是 SOR 的核心原语之二：所有权分发。
//
// 参数:
//   - srcID: 源对象 ID（被 release 的对象）
//   - holderNames: 获得只读访问权的持有者名称列表
//   - sourceLine: 源码行号
//
// 返回: 持有者对象的 ID 列表，如果失败返回 nil。
func (t *OwnershipTracker) Release(srcID string, holderNames []string, sourceLine int) []string {
	src := t.objects[srcID]
	if src == nil {
		t.addError(SORError{
			Kind:       ErrReleaseInvalidSource,
			Message:    fmt.Sprintf("release 失败：源对象 '%s' 不存在", srcID),
			SourceLine: sourceLine,
			ObjectID:   srcID,
		})
		return nil
	}

	// 检查源对象状态：必须是 Owned 或 Released
	if src.State != StateOwned && src.State != StateReleased {
		t.addError(SORError{
			Kind:       ErrReleaseInvalidSource,
			Message:    fmt.Sprintf("release 失败：源对象 '%s' 处于 %s 状态，无法 release", src.Name, src.State),
			SourceLine: sourceLine,
			ObjectID:   srcID,
			Details:    "只有处于 Owned(独占) 或 Released(分发) 状态的对象才能进行 release",
		})
		return nil
	}

	holderIDs := make([]string, 0, len(holderNames))

	for _, holderName := range holderNames {
		var holderID string
		var holder *SORObject

		// 检查是否已存在同名变量
		existingID := t.GetObjectByName(holderName)
		if existingID != "" {
			// 复用现有对象：将其添加为 release 持有者
			// （用于构建 release 关系图，支持环检测）
			holderID = existingID
			holder = t.objects[holderID]
			// 如果原来是 Owned，现在变为 Released（获得另一个对象的只读引用）
			// 注意：这简化了模型——实际语义中一个变量不能同时拥有两个对象
			// 这里为了 DAG 演示，我们允许将已有变量作为 release 目标
			if holder.State == StateOwned {
				holder.State = StateReleased
			}
		} else {
			// 创建新的持有者对象（只读视图）
			holderID = t.NewObject(holderName, src.TypeName, src.IsComposite, sourceLine)
			holder = t.objects[holderID]
			holder.State = StateReleased // 持有者也是 Released 状态（只读）

			// 复制子元素信息（只读视图也可以访问子元素）
			if src.IsComposite {
				for k, v := range src.Children {
					holder.Children[k] = v
				}
			}
		}

		// 添加 release 边到 DAG
		added := t.dag.AddEdge(srcID, holderID, sourceLine)
		if !added {
			t.addError(SORError{
				Kind:       ErrDoubleRelease,
				Message:    fmt.Sprintf("release 失败：'%s' 已经是 '%s' 的 release 持有者", holderName, src.Name),
				SourceLine: sourceLine,
				ObjectID:   srcID,
			})
			continue
		}

		// 记录持有者
		src.ReleaseHolders = append(src.ReleaseHolders, holderID)
		holderIDs = append(holderIDs, holderID)
	}

	// 源对象也变为 Released 状态（如果之前是 Owned）
	src.State = StateReleased

	// 检查 DAG 是否有环
	if t.dag.HasCycle() {
		cycle := t.dag.GetCyclePath()
		cycleDesc := t.formatCyclePath(cycle)
		t.addError(SORError{
			Kind:       ErrCycleDetected,
			Message:    fmt.Sprintf("release 关系中检测到环，违反 DAG 约束"),
			SourceLine: sourceLine,
			ObjectID:   srcID,
			Details:    fmt.Sprintf("环路径: %s", cycleDesc),
		})
	}

	return holderIDs
}

// formatCyclePath 将环路径格式化为可读字符串。
func (t *OwnershipTracker) formatCyclePath(cycle []string) string {
	parts := make([]string, 0, len(cycle))
	for _, id := range cycle {
		if obj := t.objects[id]; obj != nil {
			parts = append(parts, obj.Name)
		} else {
			parts = append(parts, id)
		}
	}
	return strings.Join(parts, " -> ")
}

// ----------------------------------------------------------------------------
// 三大原语：extract
// ----------------------------------------------------------------------------

// Extract 从复合对象中提取子对象的所有权。
// 提取后，子对象获得独占所有权（Owned），
// 原对象中的对应位置变为 Hollow（空洞/null）。
//
// 这是 SOR 的核心原语之三：子结构所有权提取。
//
// 参数:
//   - srcID: 源复合对象 ID
//   - childPath: 子元素路径（如 "[0]", ".field"）
//   - elemName: 提取后元素的名称
//   - sourceLine: 源码行号
//
// 返回: 提取出的子对象 ID，如果失败返回空字符串。
func (t *OwnershipTracker) Extract(srcID, childPath, elemName string, sourceLine int) string {
	src := t.objects[srcID]
	if src == nil {
		t.addError(SORError{
			Kind:       ErrExtractFromNonComposite,
			Message:    fmt.Sprintf("extract 失败：源对象 '%s' 不存在", srcID),
			SourceLine: sourceLine,
			ObjectID:   srcID,
		})
		return ""
	}

	// 检查源对象是否为复合类型（SOR 范式下 extract 是纯所有权语义操作，
	// 不受限于物理类型——类型检查由 Stage 2 语义分析负责）
	// 如果需要严格的类型检查，取消注释下面的代码：
	/*
	if !src.IsComposite {
		t.addError(SORError{
			Kind:       ErrExtractFromNonComposite,
			Message:    fmt.Sprintf("extract 失败：'%s' 不是复合类型（%s），无法提取子元素", src.Name, src.TypeName),
			SourceLine: sourceLine,
			ObjectID:   srcID,
			Details:    "只有数组、结构体等复合类型才能进行 extract 操作",
		})
		return ""
	}
	*/

	// 检查源对象状态：Owned 或 Released 都可以 extract
	// 但 extract 行为不同：
	//   - 从 Owned 中 extract：源位置变 Hollow，提取出的是 Owned
	//   - 从 Released 中 extract：需要特殊处理（暂时不支持或提取出只读）
	if src.State != StateOwned {
		t.addError(SORError{
			Kind:       ErrExtractFromNonComposite,
			Message:    fmt.Sprintf("extract 失败：'%s' 处于 %s 状态，无法提取独占所有权", src.Name, src.State),
			SourceLine: sourceLine,
			ObjectID:   srcID,
			Details:    "只有处于 Owned(独占) 状态的复合对象才能 extract 出独占所有权的子元素",
		})
		return ""
	}

	// 检查子元素是否存在，不存在则自动创建（extract 语义允许从对象中分离出子元素）
	childID, exists := src.Children[childPath]
	if !exists {
		// 自动创建子元素对象
		childID = fmt.Sprintf("%s%s", srcID, childPath)
		t.objects[childID] = &SORObject{
			ID:          childID,
			Name:        src.Name + childPath,
			State:       StateOwned,
			TypeName:    "extracted",
			ScopeID:     src.ScopeID,
			IsComposite: false,
			Children:    make(map[string]string),
		}
		src.Children[childPath] = childID
	}

	childObj := t.objects[childID]
	if childObj == nil {
		t.addError(SORError{
			Kind:       ErrNullDereference,
			Message:    fmt.Sprintf("extract 失败：'%s%s' 对应的子对象已销毁", src.Name, childPath),
			SourceLine: sourceLine,
			ObjectID:   srcID,
		})
		return ""
	}

	// 检查子元素是否已被提取
	if childObj.State == StateExtracted || childObj.State == StateMoved || childObj.State == StateHollow {
		t.addError(SORError{
			Kind:       ErrUseAfterExtract,
			Message:    fmt.Sprintf("extract 失败：'%s%s' 已被提取，当前为 null", src.Name, childPath),
			SourceLine: sourceLine,
			ObjectID:   childID,
			Details:    "同一位置不能被 extract 多次",
		})
		return ""
	}

	// 子元素标记为已提取（从父对象中"取出"）
	childObj.State = StateExtracted

	// 创建新的独立对象，获得独占所有权
	elemID := t.NewObject(elemName, childObj.TypeName, childObj.IsComposite, sourceLine)
	elemObj := t.objects[elemID]
	elemObj.State = StateOwned

	// 复制子元素信息（如果被提取的元素本身也是复合类型）
	if childObj.IsComposite {
		for k, v := range childObj.Children {
			elemObj.Children[k] = v
		}
	}

	// 源对象中该位置变为 Hollow（空洞）
	// 我们用一个特殊的 "hollow" 对象表示
	hollowID := fmt.Sprintf("hollow_%s_%s", srcID, childPath)
	hollowObj := &SORObject{
		ID:          hollowID,
		Name:        src.Name + childPath + "(null)",
		State:       StateHollow,
		TypeName:    childObj.TypeName,
		IsComposite: childObj.IsComposite,
		Children:    make(map[string]string),
		SourceLine:  sourceLine,
		ScopeID:     src.ScopeID,
	}
	t.objects[hollowID] = hollowObj
	src.Children[childPath] = hollowID

	return elemID
}

// ----------------------------------------------------------------------------
// 权限检查
// ----------------------------------------------------------------------------

// CanRead 检查对象是否可以被读。
// 可以读的状态：Owned、Released。
func (t *OwnershipTracker) CanRead(objID string) bool {
	obj := t.objects[objID]
	if obj == nil {
		return false
	}
	return obj.State == StateOwned || obj.State == StateReleased
}

// CanWrite 检查对象是否可以被写。
// 可以写的状态：只有 Owned。
func (t *OwnershipTracker) CanWrite(objID string) bool {
	obj := t.objects[objID]
	if obj == nil {
		return false
	}
	return obj.State == StateOwned
}

// CanYeide 检查对象是否可以作为 yeide 的源。
// 只有 Owned 状态可以转移所有权。
func (t *OwnershipTracker) CanYeide(objID string) bool {
	obj := t.objects[objID]
	if obj == nil {
		return false
	}
	return obj.State == StateOwned
}

// CheckAccess 检查对对象的访问是否合法。
// 如果不合法，会记录一条错误。
//
// 参数:
//   - objID: 对象 ID
//   - access: 访问类型（读/写/取走）
//   - sourceLine: 源码行号
//
// 返回: true 表示访问合法，false 表示非法。
func (t *OwnershipTracker) CheckAccess(objID string, access AccessType, sourceLine int) bool {
	obj := t.objects[objID]
	if obj == nil {
		t.addError(SORError{
			Kind:       ErrUseAfterMove,
			Message:    fmt.Sprintf("访问失败：对象 '%s' 不存在或已被销毁", objID),
			SourceLine: sourceLine,
			ObjectID:   objID,
		})
		return false
	}

	// 检查 use-after-move
	if obj.State == StateMoved {
		t.addError(SORError{
			Kind:       ErrUseAfterMove,
			Message:    fmt.Sprintf("use-after-move：'%s' 已通过 yeide 转移所有权，不能再使用", obj.Name),
			SourceLine: sourceLine,
			ObjectID:   objID,
			Details:    fmt.Sprintf("对象在第 %d 行被 yeide 转移", obj.SourceLine),
		})
		return false
	}

	// 检查 use-after-extract / null dereference
	if obj.State == StateHollow || obj.State == StateExtracted {
		t.addError(SORError{
			Kind:       ErrNullDereference,
			Message:    fmt.Sprintf("null dereference：'%s' 已被 extract，当前为 null", obj.Name),
			SourceLine: sourceLine,
			ObjectID:   objID,
			Details:    "访问 extract 后的位置会导致空指针解引用，需要先进行空检查",
		})
		return false
	}

	// 检查写权限
	if access == AccessWrite && obj.State == StateReleased {
		t.addError(SORError{
			Kind:       ErrWriteOnReleased,
			Message:    fmt.Sprintf("write-on-released：'%s' 处于 release 只读状态，不能写入", obj.Name),
			SourceLine: sourceLine,
			ObjectID:   objID,
			Details:    "release 状态的对象只能读。要修改需先 extract 出独占所有权",
		})
		return false
	}

	// 检查取走所有权权限
	if access == AccessTake && obj.State != StateOwned {
		t.addError(SORError{
			Kind:       ErrYeideInvalidSource,
			Message:    fmt.Sprintf("无法取走所有权：'%s' 处于 %s 状态", obj.Name, obj.State),
			SourceLine: sourceLine,
			ObjectID:   objID,
		})
		return false
	}

	return true
}

// CheckUseAfterMove 专门检查 use-after-move 错误。
// 这是 SOR 最重要的安全保证之一。
//
// 返回: true 表示存在 use-after-move，false 表示没有。
func (t *OwnershipTracker) CheckUseAfterMove(objID string, sourceLine int) bool {
	obj := t.objects[objID]
	if obj == nil {
		return false
	}

	if obj.State == StateMoved {
		t.addError(SORError{
			Kind:       ErrUseAfterMove,
			Message:    fmt.Sprintf("use-after-move：'%s' 已通过 yeide 转移所有权，不能再使用", obj.Name),
			SourceLine: sourceLine,
			ObjectID:   objID,
			Details:    fmt.Sprintf("对象在第 %d 行被 yeide 转移", obj.SourceLine),
		})
		return true
	}

	return false
}

// ----------------------------------------------------------------------------
// 错误管理
// ----------------------------------------------------------------------------

// addError 添加一条 SOR 错误。
func (t *OwnershipTracker) addError(err SORError) {
	t.errors = append(t.errors, err)
}

// GetErrors 返回所有收集到的错误。
func (t *OwnershipTracker) GetErrors() []SORError {
	return t.errors
}

// HasErrors 检查是否有错误。
func (t *OwnershipTracker) HasErrors() bool {
	return len(t.errors) > 0
}

// ClearErrors 清空所有错误。
func (t *OwnershipTracker) ClearErrors() {
	t.errors = make([]SORError, 0)
}

// ----------------------------------------------------------------------------
// 子元素管理（用于复合类型）
// ----------------------------------------------------------------------------

// AddChild 为复合对象添加子元素。
// 用于初始化复合对象时构建子元素结构。
func (t *OwnershipTracker) AddChild(parentID, childPath, childType string, childIsComposite bool, sourceLine int) string {
	parent := t.objects[parentID]
	if parent == nil || !parent.IsComposite {
		return ""
	}

	childID := t.NewObject(parent.Name+childPath, childType, childIsComposite, sourceLine)
	parent.Children[childPath] = childID
	return childID
}

// GetChild 获取复合对象的子元素 ID。
func (t *OwnershipTracker) GetChild(parentID, childPath string) string {
	parent := t.objects[parentID]
	if parent == nil {
		return ""
	}
	return parent.Children[childPath]
}

// ----------------------------------------------------------------------------
// 调试与输出
// ----------------------------------------------------------------------------

// DumpState 输出当前所有权状态（调试用）。
func (t *OwnershipTracker) DumpState() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("=== 所有权状态 (共 %d 个对象, %d 个错误) ===\n",
		len(t.objects), len(t.errors)))
	sb.WriteString(fmt.Sprintf("当前作用域: %d, 当前线程: %s\n", t.currentScope, t.threadID))

	for id, obj := range t.objects {
		sb.WriteString(fmt.Sprintf("  %s (%s): %s\n", obj.Name, id, obj.State))
		if obj.IsComposite && len(obj.Children) > 0 {
			sb.WriteString("    子元素:\n")
			for path, childID := range obj.Children {
				if child := t.objects[childID]; child != nil {
					sb.WriteString(fmt.Sprintf("      %s -> %s (%s)\n", path, child.Name, child.State))
				}
			}
		}
		if obj.State == StateReleased && len(obj.ReleaseHolders) > 0 {
			sb.WriteString("    Release 持有者:\n")
			for _, h := range obj.ReleaseHolders {
				if holder := t.objects[h]; holder != nil {
					sb.WriteString(fmt.Sprintf("      - %s\n", holder.Name))
				}
			}
		}
	}

	if len(t.errors) > 0 {
		sb.WriteString("\n=== 错误列表 ===\n")
		for i, err := range t.errors {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, err.Error()))
		}
	}

	return sb.String()
}

// GetDAG 返回 DAG 检测器（用于分析）。
func (t *OwnershipTracker) GetDAG() *DAGChecker {
	return t.dag
}

// GetObjectCount 返回对象总数。
func (t *OwnershipTracker) GetObjectCount() int {
	return len(t.objects)
}
