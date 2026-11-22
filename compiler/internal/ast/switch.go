package ast

// SwitchCase 表示跳转表（switch）语句中的一个 case 分支。
//
// Values 是该分支对应的**编译期整型常量**标签。允许并列多个值共享同一段
// 分支体（语义上等价于 if 链里的 `x == a || x == b`）；由 if 链改写产生时
// 通常只有一个值。
//
// 限制：C 的 case 标签必须是整型常量表达式，因此浮点字面量不能作为标签，
// 上层的 if 链改写会对非常量/浮点条件直接放弃改写（见 internal/opt）。
type SwitchCase struct {
	Values []Expression
	Body   []Statement
	Pos    Position
}

// String 实现Node接口
func (c *SwitchCase) String() string { return "SwitchCase" }

// GetPosition 实现Node接口
func (c *SwitchCase) GetPosition() Position { return c.Pos }

// SetPosition 实现Node接口
func (c *SwitchCase) SetPosition(pos Position) { c.Pos = pos }

// SwitchStatement 表示跳转表（switch）语句。
//
// 这是**编译器内部节点**：由 internal/opt 的「if 链转跳转表」优化产出，
// 语法层（parser）不直接产生它，因此不需要解析器支持。保留它是为了让
// codegen 拿到一个显式的、单一的分派点，从而把 N 次顺序比较降为一次
// switch 分派（密集整型区间由 Clang 落成真正的跳转表）。
//
// Scrutinee 为被分派的值；Default 为 else 分支（nil 表示无 default，
// 此时 switch 结束后控制流继续向下，与原 if 链无 else 时完全一致）。
type SwitchStatement struct {
	Scrutinee Expression
	Cases     []*SwitchCase
	Default   []Statement
	Pos       Position
}

// statementNode 实现Statement接口
func (s *SwitchStatement) statementNode() {}

// String 实现Node接口
func (s *SwitchStatement) String() string { return "SwitchStatement" }

// GetPosition 实现Node接口
func (s *SwitchStatement) GetPosition() Position { return s.Pos }

// SetPosition 实现Node接口
func (s *SwitchStatement) SetPosition(pos Position) { s.Pos = pos }

// Traverse 遍历switch节点及其子节点
func (s *SwitchStatement) Traverse(visitor func(Node)) {
	traverseNode(s, visitor)
}

// AddCase 追加一个 case 分支
func (s *SwitchStatement) AddCase(c *SwitchCase) {
	s.Cases = append(s.Cases, c)
}

// HasDefault 检查是否有 default 分支
func (s *SwitchStatement) HasDefault() bool {
	return len(s.Default) > 0
}
