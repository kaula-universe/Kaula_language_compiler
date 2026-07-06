package sor

import (
	"testing"
)

func TestScopeTreeAnalyzer_SimpleSequence(t *testing.T) {
	stmts := []Stmt{
		LetStmt(1, "a = ...", "a", "int64", false),
		LetStmt(2, "b = ...", "b", "int64", false),
	}

	sta := NewScopeTreeAnalyzer()
	root := sta.BuildScopeTree(stmts)

	if root == nil {
		t.Fatal("root should not be nil")
	}
	if root.Type != ScopeSequential {
		t.Errorf("root.Type = %v, want ScopeSequential", root.Type)
	}
	// Should have at least one child with the stmts
	if len(root.Children) == 0 {
		t.Error("root should have children")
	}
}

func TestScopeTreeAnalyzer_IfBranch(t *testing.T) {
	stmts := []Stmt{
		BranchEnterStmt(1, "if (...)"),
		LetStmt(2, "x = ...", "x", "int64", false),
		BranchElseStmt(3, "else"),
		LetStmt(4, "y = ...", "y", "int64", false),
		BranchExitStmt(5, "endif"),
	}

	sta := NewScopeTreeAnalyzer()
	root := sta.BuildScopeTree(stmts)

	if root == nil {
		t.Fatal("root should not be nil")
	}

	// Find the branch node (may be nested in a sequential block)
	var branchNode *ScopeNode
	var findBranch func(node *ScopeNode) bool
	findBranch = func(node *ScopeNode) bool {
		if node == nil {
			return false
		}
		if node.Type == ScopeBranch {
			branchNode = node
			return true
		}
		for _, child := range node.Children {
			if findBranch(child) {
				return true
			}
		}
		return false
	}

	if !findBranch(root) {
		t.Fatal("Expected to find a ScopeBranch node")
	}

	// Branch should have 2 children (if body and else body)
	if len(branchNode.Children) != 2 {
		t.Errorf("Branch has %d children, want 2", len(branchNode.Children))
	}
}

func TestScopeTreeAnalyzer_Loop(t *testing.T) {
	stmts := []Stmt{
		LoopEnterStmt(1, "while (...)", 0),
		LetStmt(2, "i = ...", "i", "int64", false),
		LoopExitStmt(3, "endwhile"),
	}

	sta := NewScopeTreeAnalyzer()
	root := sta.BuildScopeTree(stmts)

	if root == nil {
		t.Fatal("root should not be nil")
	}

	// Find the loop node (may be nested in a sequential block)
	var loopNode *ScopeNode
	var findLoop func(node *ScopeNode) bool
	findLoop = func(node *ScopeNode) bool {
		if node == nil {
			return false
		}
		if node.Type == ScopeLoop {
			loopNode = node
			return true
		}
		for _, child := range node.Children {
			if findLoop(child) {
				return true
			}
		}
		return false
	}

	if !findLoop(root) {
		t.Fatal("Expected to find a ScopeLoop node")
	}

	// Loop should have at least one child (loop body)
	if len(loopNode.Children) == 0 {
		t.Error("Loop should have children")
	}
}

func TestScopeTreeAnalyzer_EstimatePoolCapacity_BranchExclusion(t *testing.T) {
	// Simulate: if branch creates object A (100 bytes), else branch creates object B (200 bytes)
	// Without optimization: 300 bytes
	// With branch exclusion: 200 bytes (max of branches)

	tracker := NewOwnershipTracker()
	sizes := map[string]int{
		"obj_1": 100, // if branch
		"obj_2": 200, // else branch
	}

	// Create objects
	tracker.NewObject("x", "int64", false, 1) // obj_1
	tracker.NewObject("y", "int64", false, 2) // obj_2

	stmts := []Stmt{
		BranchEnterStmt(1, "if (...)"),
		Stmt{Kind: StmtLet, Line: 2, VarName: "x", TypeName: "int64"},
		BranchElseStmt(3, "else"),
		Stmt{Kind: StmtLet, Line: 4, VarName: "y", TypeName: "int64"},
		BranchExitStmt(5, "endif"),
	}

	sta := NewScopeTreeAnalyzer()
	sta.BuildScopeTree(stmts)

	capacity := sta.EstimatePoolCapacity(sizes, tracker)
	// Branch exclusion: max(100, 200) = 200
	if capacity != 200 {
		t.Errorf("EstimatePoolCapacity = %d, want 200 (branch exclusion)", capacity)
	}
}

func TestScopeTreeAnalyzer_GetMaxBranchSize(t *testing.T) {
	tracker := NewOwnershipTracker()
	sizes := map[string]int{
		"obj_1": 100,
		"obj_2": 200,
	}

	tracker.NewObject("x", "int64", false, 1)
	tracker.NewObject("y", "int64", false, 2)

	stmts := []Stmt{
		BranchEnterStmt(1, "if (...)"),
		Stmt{Kind: StmtLet, Line: 2, VarName: "x", TypeName: "int64"},
		BranchElseStmt(3, "else"),
		Stmt{Kind: StmtLet, Line: 4, VarName: "y", TypeName: "int64"},
		BranchExitStmt(5, "endif"),
	}

	sta := NewScopeTreeAnalyzer()
	sta.BuildScopeTree(stmts)

	maxSize := sta.GetMaxBranchSize(sizes, tracker)
	if maxSize != 200 {
		t.Errorf("GetMaxBranchSize = %d, want 200", maxSize)
	}
}
