package core

import (
	"encoding/json"
	"sync"
)

// TreeNode 表示树节点
type TreeNode struct {
	Value    interface{}
	Children []*TreeNode
	Mutex    sync.RWMutex
}

// Tree 表示树
type Tree struct {
	Root *TreeNode
	Mutex sync.RWMutex
}

// NewTree 创建一个新的树
func NewTree() *Tree {
	return &Tree{
		Root: &TreeNode{
			Value:    nil,
			Children: make([]*TreeNode, 0),
		},
	}
}

// NewTreeNode 创建一个新的树节点
func NewTreeNode(value interface{}) *TreeNode {
	return &TreeNode{
		Value:    value,
		Children: make([]*TreeNode, 0),
	}
}

// AddNode 添加节点
func (t *Tree) AddNode(parent, child *TreeNode) {
	t.Mutex.Lock()
	defer t.Mutex.Unlock()
	parent.Mutex.Lock()
	defer parent.Mutex.Unlock()
	parent.Children = append(parent.Children, child)
}

// RemoveNode 移除节点
func (t *Tree) RemoveNode(parent, child *TreeNode) bool {
	t.Mutex.Lock()
	defer t.Mutex.Unlock()
	parent.Mutex.Lock()
	defer parent.Mutex.Unlock()
	for i, c := range parent.Children {
		if c == child {
			parent.Children = append(parent.Children[:i], parent.Children[i+1:]...)
			return true
		}
	}
	return false
}

// Traverse 遍历树
func (t *Tree) Traverse(fn func(*TreeNode)) {
	t.Mutex.RLock()
	defer t.Mutex.RUnlock()
	t.traverse(t.Root, fn)
}

// traverse 递归遍历树
func (t *Tree) traverse(node *TreeNode, fn func(*TreeNode)) {
	if node == nil {
		return
	}
	node.Mutex.RLock()
	fn(node)
	children := make([]*TreeNode, len(node.Children))
	copy(children, node.Children)
	node.Mutex.RUnlock()
	for _, child := range children {
		t.traverse(child, fn)
	}
}

// FindNode 查找节点
func (t *Tree) FindNode(predicate func(*TreeNode) bool) *TreeNode {
	t.Mutex.RLock()
	defer t.Mutex.RUnlock()
	return t.findNode(t.Root, predicate)
}

// findNode 递归查找节点
func (t *Tree) findNode(node *TreeNode, predicate func(*TreeNode) bool) *TreeNode {
	if node == nil {
		return nil
	}
	node.Mutex.RLock()
	match := predicate(node)
	children := make([]*TreeNode, len(node.Children))
	copy(children, node.Children)
	node.Mutex.RUnlock()
	if match {
		return node
	}
	for _, child := range children {
		if result := t.findNode(child, predicate); result != nil {
			return result
		}
	}
	return nil
}

// Serialize 序列化树
func (t *Tree) Serialize() ([]byte, error) {
	t.Mutex.RLock()
	defer t.Mutex.RUnlock()
	return json.Marshal(t)
}

// Deserialize 反序列化树
func (t *Tree) Deserialize(data []byte) error {
	t.Mutex.Lock()
	defer t.Mutex.Unlock()
	return json.Unmarshal(data, t)
}

// GetHeight 获取树的高度
func (t *Tree) GetHeight() int {
	t.Mutex.RLock()
	defer t.Mutex.RUnlock()
	return t.getHeight(t.Root)
}

// getHeight 递归获取树的高度
func (t *Tree) getHeight(node *TreeNode) int {
	if node == nil {
		return 0
	}
	node.Mutex.RLock()
	defer node.Mutex.RUnlock()
	maxHeight := 0
	for _, child := range node.Children {
		height := t.getHeight(child)
		if height > maxHeight {
			maxHeight = height
		}
	}
	return maxHeight + 1
}

// GetSize 获取树的节点数量
func (t *Tree) GetSize() int {
	t.Mutex.RLock()
	defer t.Mutex.RUnlock()
	return t.getSize(t.Root)
}

// getSize 递归获取树的节点数量
func (t *Tree) getSize(node *TreeNode) int {
	if node == nil {
		return 0
	}
	node.Mutex.RLock()
	defer node.Mutex.RUnlock()
	size := 1
	for _, child := range node.Children {
		size += t.getSize(child)
	}
	return size
}
