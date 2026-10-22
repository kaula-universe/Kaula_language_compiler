package core

import (
	"fmt"
	"strings"
	"sync"
)

type TreeAnnotation int

const (
	AnnotationNone TreeAnnotation = iota
	AnnotationPrefix
	AnnotationTree
	AnnotationPrefixTree
	AnnotationRoot
	AnnotationRootTree
)

func (a TreeAnnotation) String() string {
	switch a {
	case AnnotationNone:
		return "none"
	case AnnotationPrefix:
		return "prefix"
	case AnnotationTree:
		return "tree"
	case AnnotationPrefixTree:
		return "prefix,tree"
	case AnnotationRoot:
		return "root"
	case AnnotationRootTree:
		return "root,tree"
	default:
		return "unknown"
	}
}

func ParseAnnotation(s string) TreeAnnotation {
	switch s {
	case "prefix":
		return AnnotationPrefix
	case "tree":
		return AnnotationTree
	case "prefix,tree", "tree,prefix":
		return AnnotationPrefixTree
	case "root":
		return AnnotationRoot
	case "root,tree", "tree,root":
		return AnnotationRootTree
	default:
		return AnnotationNone
	}
}

type TreeNodeType int

const (
	NodeTypeGeneric TreeNodeType = iota
	NodeTypeStatement
	NodeTypeExpression
	NodeTypeFunction
	NodeTypeVariable
	NodeTypeParameter
	NodeTypeBlock
	NodeTypeCondition
	NodeTypeLoop
	NodeTypeReturn
)

func (n TreeNodeType) String() string {
	switch n {
	case NodeTypeGeneric:
		return "generic"
	case NodeTypeStatement:
		return "statement"
	case NodeTypeExpression:
		return "expression"
	case NodeTypeFunction:
		return "function"
	case NodeTypeVariable:
		return "variable"
	case NodeTypeParameter:
		return "parameter"
	case NodeTypeBlock:
		return "block"
	case NodeTypeCondition:
		return "condition"
	case NodeTypeLoop:
		return "loop"
	case NodeTypeReturn:
		return "return"
	default:
		return "unknown"
	}
}

type TreeConstraint struct {
	Required bool
	NodeType TreeNodeType
	Children []*TreeConstraint
	Pattern  string
	// Type 可选的类型约束：参数声明的 Kaula 类型名（如 "i64"、"Point"）。
	// 由结构签名的形参类型解析填入，留空表示不限制类型。
	Type string
	// Name 可选的绑定名约束：约束作用于哪个节点（tree 注解里的 `$p`）。
	Name string
	// Value 可选的**值约束**：tree(mode = "int") 引号字面量。分派时用
	// comptime 求值实参，其字符串结果必须与 Value 相等；空表示不限制值。
	// 这是「值域反哺宏分派」的跨：comptime 算出的字符串/整数值参与规则选择。
	Value string
}

// ---- TreeConstraint ↔ 结构宏桥接 ----
//
// tree 注解（结构签名）是**声明在哪一层嵌套**的约束：形如
//
//	tree(Vec<i64>, i64)
//
// 的第一个实参描述实参 `$body` 那个"结构组"的内部嵌套要求，第二个实参
// 描述 `$body` 本身。core 包不依赖 ast 包（避免 ast→core 的反向依赖），
// 这里只提供纯数据层的归一化与字符串格式化；「约束 ↔ AST 节点类型」的
// 映射在 codegen 侧（macro_tree.go），因为只有那里同时掌握两种类型体系。

// NewStructuralConstraint 构造一条结构约束。pattern 为空表示不限制类型形状。
func NewStructuralConstraint(nodeType TreeNodeType, pattern string) *TreeConstraint {
	return &TreeConstraint{Required: true, NodeType: nodeType, Pattern: pattern}
}

// String 以紧凑形式描述约束链（诊断信息用）：
// "expression"，或 "expression<statement, expression>" 这样的树形。
func (c *TreeConstraint) String() string {
	if c == nil {
		return "generic"
	}
	base := c.NodeType.String()
	if c.Pattern != "" {
		base = c.Pattern
	}
	if c.Type != "" {
		base += ":" + c.Type
	}
	if len(c.Children) == 0 {
		return base
	}
	parts := make([]string, 0, len(c.Children))
	for _, ch := range c.Children {
		parts = append(parts, ch.String())
	}
	return base + "<" + strings.Join(parts, ", ") + ">"
}

// MatchesShape 判断约束链的**形状**是否覆盖 target 的形状：
// 逐个 Child 与 target.Children 对齐比较，多余的 target 子节点不被覆盖即失败。
// 类型名（Type）与绑定名（Name）不参与形状匹配，见 codegen 侧的值校验。
func (c *TreeConstraint) MatchesShape(target *TreeConstraint) bool {
	if c == nil {
		return true
	}
	if target == nil {
		return false
	}
	if !matchesTypeCompatible(c.NodeType, c.Pattern, target.NodeType, target.Pattern) {
		return false
	}
	if len(c.Children) > 0 {
		if len(target.Children) < len(c.Children) {
			return false
		}
		for i, ch := range c.Children {
			if !ch.MatchesShape(target.Children[i]) {
				return false
			}
		}
	}
	return true
}

// matchesTypeCompatible 节点类型兼容：generic 通配；否则类型必须相同；
// 双方都带 Pattern 时字符串必须相等（另一方无 Pattern 视为只约束类型）。
func matchesTypeCompatible(wantType TreeNodeType, wantPattern string, gotType TreeNodeType, gotPattern string) bool {
	if wantType == NodeTypeGeneric {
		return true
	}
	if gotType == NodeTypeGeneric {
		// 声明了不全是 generic 的类型时，未解析的结构参数不能被当作满足
		return false
	}
	if wantType != gotType {
		return false
	}
	if wantPattern != "" && gotPattern != "" && wantPattern != gotPattern {
		return false
	}
	return true
}


type TreeNode struct {
	Name       string
	NodeType   TreeNodeType
	Value      interface{}
	Children   []*TreeNode
	Parent     *TreeNode
	Annotation TreeAnnotation
	Constraint *TreeConstraint
	IsRoot     bool
	Mutex      sync.RWMutex
}

func NewTreeNode(name string, nodeType TreeNodeType) *TreeNode {
	return &TreeNode{
		Name:       name,
		NodeType:   nodeType,
		Children:   make([]*TreeNode, 0),
		Annotation: AnnotationNone,
	}
}

func (n *TreeNode) AddChild(child *TreeNode) {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()
	child.Parent = n
	n.Children = append(n.Children, child)
}

func (n *TreeNode) RemoveChild(child *TreeNode) bool {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()
	for i, c := range n.Children {
		if c == child {
			n.Children = append(n.Children[:i], n.Children[i+1:]...)
			child.Parent = nil
			return true
		}
	}
	return false
}

func (n *TreeNode) GetChildren() []*TreeNode {
	n.Mutex.RLock()
	defer n.Mutex.RUnlock()
	children := make([]*TreeNode, len(n.Children))
	copy(children, n.Children)
	return children
}

func (n *TreeNode) SetConstraint(constraint *TreeConstraint) {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()
	n.Constraint = constraint
}

func (n *TreeNode) MatchesConstraint() bool {
	n.Mutex.RLock()
	defer n.Mutex.RUnlock()
	if n.Constraint == nil {
		return true
	}
	return n.matchesConstraintRecursive(n.Constraint)
}

func (n *TreeNode) matchesConstraintRecursive(constraint *TreeConstraint) bool {
	if constraint == nil {
		return true
	}
	if constraint.Required && n.NodeType != constraint.NodeType {
		return false
	}
	if constraint.Pattern != "" && n.Name != constraint.Pattern {
		return false
	}
	if len(constraint.Children) > 0 {
		if len(n.Children) < len(constraint.Children) {
			return false
		}
		for i, childConstraint := range constraint.Children {
			if !n.Children[i].matchesConstraintRecursive(childConstraint) {
				return false
			}
		}
	}
	return true
}

type Tree struct {
	Root       *TreeNode
	Annotation TreeAnnotation
	Name       string
	IsOrphan   bool
	Mutex      sync.RWMutex
}

func NewTree() *Tree {
	return &Tree{
		Root:     NewTreeNode("root", NodeTypeBlock),
		IsOrphan: false,
	}
}

func NewTreeWithName(name string) *Tree {
	return &Tree{
		Root:       NewTreeNode("root", NodeTypeBlock),
		Annotation: AnnotationTree,
		Name:       name,
		IsOrphan:   false,
	}
}

func (t *Tree) SetAnnotation(annotation TreeAnnotation) {
	t.Mutex.Lock()
	defer t.Mutex.Unlock()
	t.Annotation = annotation
	if annotation == AnnotationRoot || annotation == AnnotationRootTree {
		t.Root.IsRoot = true
	}
}

func (t *Tree) GetAnnotation() TreeAnnotation {
	t.Mutex.RLock()
	defer t.Mutex.RUnlock()
	return t.Annotation
}

func (t *Tree) AddNode(parent, child *TreeNode) {
	t.Mutex.Lock()
	defer t.Mutex.Unlock()
	parent.AddChild(child)
}

func (t *Tree) RemoveNode(parent, child *TreeNode) bool {
	t.Mutex.Lock()
	defer t.Mutex.Unlock()
	return parent.RemoveChild(child)
}

func (t *Tree) Traverse(fn func(*TreeNode)) {
	t.Mutex.RLock()
	defer t.Mutex.RUnlock()
	t.traverse(t.Root, fn)
}

func (t *Tree) traverse(node *TreeNode, fn func(*TreeNode)) {
	if node == nil {
		return
	}
	node.Mutex.RLock()
	fn(node)
	children := node.GetChildren()
	node.Mutex.RUnlock()
	for _, child := range children {
		t.traverse(child, fn)
	}
}

func (t *Tree) FindNode(predicate func(*TreeNode) bool) *TreeNode {
	t.Mutex.RLock()
	defer t.Mutex.RUnlock()
	return t.findNode(t.Root, predicate)
}

func (t *Tree) findNode(node *TreeNode, predicate func(*TreeNode) bool) *TreeNode {
	if node == nil {
		return nil
	}
	if predicate(node) {
		return node
	}
	for _, child := range node.GetChildren() {
		if result := t.findNode(child, predicate); result != nil {
			return result
		}
	}
	return nil
}

func (t *Tree) FindNodes(predicate func(*TreeNode) bool) []*TreeNode {
	t.Mutex.RLock()
	defer t.Mutex.RUnlock()
	results := make([]*TreeNode, 0)
	t.findNodes(t.Root, predicate, &results)
	return results
}

func (t *Tree) findNodes(node *TreeNode, predicate func(*TreeNode) bool, results *[]*TreeNode) {
	if node == nil {
		return
	}
	if predicate(node) {
		*results = append(*results, node)
	}
	for _, child := range node.GetChildren() {
		t.findNodes(child, predicate, results)
	}
}

func (t *Tree) Validate(rootTree *Tree) error {
	if rootTree == nil {
		return nil
	}
	if t.Annotation == AnnotationRoot || t.Annotation == AnnotationRootTree {
		return nil
	}
	return t.validateAgainstTree(rootTree.Root)
}

func (t *Tree) validateAgainstTree(rootNode *TreeNode) error {
	if rootNode == nil {
		return nil
	}
	rootConstraint := rootNode.Constraint
	if rootConstraint == nil {
		return nil
	}
	if !t.Root.MatchesConstraint() {
		return fmt.Errorf("tree '%s' does not match root tree structure", t.Name)
	}
	return nil
}

func (t *Tree) MarkOrphan() {
	t.Mutex.Lock()
	defer t.Mutex.Unlock()
	t.IsOrphan = true
}

func (t *Tree) IsOrphanTree() bool {
	t.Mutex.RLock()
	defer t.Mutex.RUnlock()
	return t.IsOrphan
}

type TreeManager struct {
	trees       map[string]*Tree
	rootTree    *Tree
	prefixTrees map[string]*Tree
	Mutex       sync.RWMutex
}

func NewTreeManager() *TreeManager {
	return &TreeManager{
		trees:       make(map[string]*Tree),
		prefixTrees: make(map[string]*Tree),
	}
}

func (tm *TreeManager) RegisterTree(tree *Tree) error {
	tm.Mutex.Lock()
	defer tm.Mutex.Unlock()
	if tree == nil {
		return fmt.Errorf("cannot register nil tree")
	}
	if tree.Name == "" {
		return fmt.Errorf("tree must have a name")
	}
	annotation := tree.GetAnnotation()
	if annotation == AnnotationRoot || annotation == AnnotationRootTree {
		if tm.rootTree != nil {
			return fmt.Errorf("root tree already exists: %s", tm.rootTree.Name)
		}
		tm.rootTree = tree
	}
	if annotation == AnnotationPrefixTree || annotation == AnnotationPrefix {
		tm.prefixTrees[tree.Name] = tree
	}
	tm.trees[tree.Name] = tree
	return nil
}

func (tm *TreeManager) GetTree(name string) *Tree {
	tm.Mutex.RLock()
	defer tm.Mutex.RUnlock()
	return tm.trees[name]
}

func (tm *TreeManager) GetPrefixTree(name string) *Tree {
	tm.Mutex.RLock()
	defer tm.Mutex.RUnlock()
	return tm.prefixTrees[name]
}

func (tm *TreeManager) GetRootTree() *Tree {
	tm.Mutex.RLock()
	defer tm.Mutex.RUnlock()
	return tm.rootTree
}

func (tm *TreeManager) GetAllTrees() []*Tree {
	tm.Mutex.RLock()
	defer tm.Mutex.RUnlock()
	trees := make([]*Tree, 0, len(tm.trees))
	for _, tree := range tm.trees {
		trees = append(trees, tree)
	}
	return trees
}

func (tm *TreeManager) ValidateAllTrees() []error {
	tm.Mutex.RLock()
	defer tm.Mutex.RUnlock()
	errors := make([]error, 0)
	rootTree := tm.rootTree
	for _, tree := range tm.trees {
		if tree == rootTree {
			continue
		}
		if tree.GetAnnotation() != AnnotationPrefix && tree.GetAnnotation() != AnnotationPrefixTree {
			if err := tree.Validate(rootTree); err != nil {
				errors = append(errors, err)
				tree.MarkOrphan()
			}
		}
	}
	return errors
}

func (tm *TreeManager) FindOrphanTrees() []*Tree {
	tm.Mutex.RLock()
	defer tm.Mutex.RUnlock()
	orphans := make([]*Tree, 0)
	for _, tree := range tm.trees {
		if tree.IsOrphanTree() {
			orphans = append(orphans, tree)
		}
	}
	return orphans
}

func (tm *TreeManager) ApplyTree(target interface{}, tree *Tree) error {
	if tree == nil {
		return fmt.Errorf("tree is nil")
	}
	switch tree.GetAnnotation() {
	case AnnotationPrefix, AnnotationPrefixTree:
		return fmt.Errorf("prefix tree cannot be applied directly")
	case AnnotationRoot, AnnotationRootTree:
		return fmt.Errorf("root tree cannot be applied directly")
	default:
		return tm.applyTreeRecursive(target, tree.Root)
	}
}

func (tm *TreeManager) applyTreeRecursive(target interface{}, node *TreeNode) error {
	if node == nil {
		return nil
	}
	for _, child := range node.GetChildren() {
		if err := tm.applyTreeRecursive(target, child); err != nil {
			return err
		}
	}
	return nil
}
