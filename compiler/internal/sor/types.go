// Package sor 实现了 Sub-structural Ownership (SOR) 编译时验证器原型。
// SOR 是一种纯编译时验证的安全范式，零运行时开销。
//
// 三大核心原语:
//   - yield:   显式所有权转移，原持有者变为无效
//   - extract: 从复合对象中提取子对象所有权，原位置留下 null 空洞
//   - release: 所有权分发，多个持有者共享只读访问，必须构成 DAG
package sor

import "fmt"

// ============================================================================
// 所有权状态枚举
// ============================================================================

// OwnershipState 表示对象的所有权状态。
type OwnershipState int

const (
	// StateOwned 表示独占所有权状态：唯一持有者，可读写。
	StateOwned OwnershipState = iota

	// StateReleased 表示分发状态：多个持有者共享只读访问，必须构成 DAG。
	StateReleased

	// StateExtracted 表示已提取状态：原对象中该位置已被提取，留下 null。
	// （用于复合对象的子元素追踪）
	StateExtracted

	// StateMoved 表示已转移状态：yield 后原持有者失效。
	StateMoved

	// StateHollow 表示空洞状态：extract 后原位置为 null，需要空安全检查。
	// （用于访问 extract 后位置时的空安全检查）
	StateHollow

	// StateUnionReleased 表示联合域分发状态：多个持有者共享可变访问，
	// 编译期选举决定谁是 elected writer，其余为 reader。
	StateUnionReleased
)

// String 返回所有权状态的可读字符串。
func (s OwnershipState) String() string {
	switch s {
	case StateOwned:
		return "Owned(独占)"
	case StateReleased:
		return "Released(分发只读)"
	case StateExtracted:
		return "Extracted(已提取)"
	case StateMoved:
		return "Moved(已转移)"
	case StateHollow:
		return "Hollow(空洞/null)"
	case StateUnionReleased:
		return "UnionReleased(联合域分发)"
	default:
		return fmt.Sprintf("Unknown(%d)", int(s))
	}
}

// ============================================================================
// SOR 对象：所有权追踪的基本单元
// ============================================================================

// SORObject 表示一个受所有权系统追踪的对象。
// 每个对象在编译时都有唯一的 ID 和对应的所有权状态。
type SORObject struct {
	// ID 是对象的唯一标识符（编译时符号）。
	ID string

	// Name 是对象的可读名称（变量名）。
	Name string

	// State 是对象当前的所有权状态。
	State OwnershipState

	// TypeName 是对象的类型名称（如 "[]int", "StructFoo"）。
	TypeName string

	// IsComposite 表示对象是否为复合类型（数组/结构体），
	// 复合类型可以进行 extract 操作。
	IsComposite bool

	// Children 记录复合对象的子元素所有权状态。
	// key 为子元素路径（如 "[0]", ".field"），value 为子对象 ID。
	Children map[string]string

	// ReleaseHolders 记录在 release 状态下，所有共享只读访问的持有者列表。
	// 只有当 State == StateReleased 时有效。
	ReleaseHolders []string

	// SourceLine 记录该对象创建时的源码行号（用于报错）。
	SourceLine int

	// ScopeID 记录该对象所在的作用域 ID（用于跨作用域检查）。
	ScopeID int

	// IsUnion 表示该对象是否通过 union release 分发。
	// 为 true 时，编译期选举决定 elected writer。
	IsUnion bool

	// IsResource 表示该对象是否是资源类型（需要显式释放）。
	// 资源类型在作用域结束时必须被释放或转移所有权，否则报资源泄漏错误。
	IsResource bool

	// ResourceKind 表示资源种类（如 "file", "socket", "lock", "memory" 等）。
	// 用于错误信息和资源特定的处理逻辑。
	ResourceKind string
}

// ============================================================================
// Release 边：用于 DAG 环检测
// ============================================================================

// ReleaseEdge 表示一条 release 关系边：from 向 to 分发了只读访问权限。
// 即 to 持有 from 的只读引用。
type ReleaseEdge struct {
	// From 是原始对象的 ID（被 release 的对象）。
	From string

	// To 是获得只读访问权的持有者 ID。
	To string

	// SourceLine 记录该 release 操作的源码行号（用于报错）。
	SourceLine int
}

// String 返回边的可读描述。
func (e ReleaseEdge) String() string {
	return fmt.Sprintf("%s --release--> %s (line %d)", e.From, e.To, e.SourceLine)
}

// ============================================================================
// SOR 编译错误
// ============================================================================

// ErrorKind 表示 SOR 错误的种类。
type ErrorKind int

const (
	// ErrUseAfterMove 使用了已转移（yield）的变量。
	ErrUseAfterMove ErrorKind = iota

	// ErrUseAfterExtract 使用了已提取（extract）的位置（null 安全违规）。
	ErrUseAfterExtract

	// ErrCycleDetected release 关系中检测到环，违反 DAG 约束。
	ErrCycleDetected

	// ErrWriteOnReleased 对已 release 的对象进行写操作（只读状态）。
	ErrWriteOnReleased

	// ErrExtractFromNonComposite 对非复合对象执行 extract。
	ErrExtractFromNonComposite

	// ErrYieldInvalidSource yield 源对象无效（已转移/已提取）。
	ErrYieldInvalidSource

	// ErrReleaseInvalidSource release 源对象无效。
	ErrReleaseInvalidSource

	// ErrCrossScopeYield 跨作用域 yield 违规（内层作用域变量不能 yield 给外层）。
	ErrCrossScopeYield

	// ErrCrossThreadYield 跨线程所有权传递未显式 yield。
	ErrCrossThreadYield

	// ErrThreadWriteOnReleased 多线程中对 release 对象进行写操作。
	ErrThreadWriteOnReleased

	// ErrNullDereference 空解引用：访问了 extract 后的空洞位置。
	ErrNullDereference

	// ErrDoubleRelease 重复 release 同一对象。
	ErrDoubleRelease

	// ErrResourceLeak 资源泄漏：作用域结束时资源仍处于 Owned 状态，未被释放或转移。
	ErrResourceLeak

	// ErrExternShallowPromote 外部逃逸源浅提升警告：
	// extern（opaque C 函数）返回的值被移入 SOR 管理域（yield/release/extract），
	// 但 promote 仅浅拷贝外壳，内部指向外部分配的指针不受 SOR 追踪。
	// 作为警告（而非硬错误）随 FullAnalysisResult.Warnings 上报。
	ErrExternShallowPromote
)

// SORError 表示 SOR 编译时验证发现的错误。
type SORError struct {
	// Kind 是错误种类。
	Kind ErrorKind

	// Message 是人类可读的错误描述。
	Message string

	// SourceLine 是错误发生的源码行号。
	SourceLine int

	// ObjectID 是涉及的对象 ID（可选）。
	ObjectID string

	// Details 是额外的错误详情（如环路径、建议修复方式等）。
	Details string
}

// Error 实现 error 接口，返回格式化的错误信息。
func (e SORError) Error() string {
	header := fmt.Sprintf("[SOR Error | %s] line %d", e.Kind.String(), e.SourceLine)
	if e.ObjectID != "" {
		header += fmt.Sprintf(" (对象: %s)", e.ObjectID)
	}
	msg := fmt.Sprintf("%s\n  %s", header, e.Message)
	if e.Details != "" {
		msg += fmt.Sprintf("\n  详情: %s", e.Details)
	}
	return msg
}

// String 返回错误种类的可读名称。
func (k ErrorKind) String() string {
	switch k {
	case ErrUseAfterMove:
		return "Use-After-Move"
	case ErrUseAfterExtract:
		return "Use-After-Extract"
	case ErrCycleDetected:
		return "Cycle-Detected"
	case ErrWriteOnReleased:
		return "Write-On-Released"
	case ErrExtractFromNonComposite:
		return "Extract-From-NonComposite"
	case ErrYieldInvalidSource:
		return "Yield-Invalid-Source"
	case ErrReleaseInvalidSource:
		return "Release-Invalid-Source"
	case ErrCrossScopeYield:
		return "Cross-Scope-Yield"
	case ErrCrossThreadYield:
		return "Cross-Thread-Yield"
	case ErrThreadWriteOnReleased:
		return "Thread-Write-On-Released"
	case ErrNullDereference:
		return "Null-Dereference"
	case ErrDoubleRelease:
		return "Double-Release"
	case ErrResourceLeak:
		return "Resource-Leak"
	case ErrExternShallowPromote:
		return "Extern-Shallow-Promote"
	default:
		return fmt.Sprintf("Unknown(%d)", int(k))
	}
}

// ============================================================================
// 访问操作类型：用于权限检查
// ============================================================================

// AccessType 表示对对象的访问类型。
type AccessType int

const (
	// AccessRead 表示读访问。
	AccessRead AccessType = iota

	// AccessWrite 表示写访问。
	AccessWrite

	// AccessTake 表示取走所有权（yield 源）。
	AccessTake
)

// String 返回访问类型的可读名称。
func (a AccessType) String() string {
	switch a {
	case AccessRead:
		return "Read"
	case AccessWrite:
		return "Write"
	case AccessTake:
		return "Take(Ownership)"
	default:
		return "Unknown"
	}
}
