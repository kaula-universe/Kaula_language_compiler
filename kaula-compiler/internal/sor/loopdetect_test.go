package sor

import (
	"testing"
)

func TestLoopDetector_SimpleLoop(t *testing.T) {
	stmts := []Stmt{
		LoopEnterStmt(1, "for (...)", 10),
		LetStmt(2, "x = ...", "x", "int64", false),
		ReadStmt(3, "x", "x"),
		LoopExitStmt(4, "endfor"),
	}

	ld := NewLoopDetector()
	loops := ld.DetectLoops(stmts)

	if len(loops) != 1 {
		t.Fatalf("Expected 1 loop, got %d", len(loops))
	}

	loop := loops[0]
	if loop.IterCount != 10 {
		t.Errorf("IterCount = %d, want 10", loop.IterCount)
	}
	if len(loop.BodyVars) != 1 || loop.BodyVars[0] != "x" {
		t.Errorf("BodyVars = %v, want [x]", loop.BodyVars)
	}
}

func TestLoopDetector_NestedLoops(t *testing.T) {
	stmts := []Stmt{
		LoopEnterStmt(1, "for (...)", 5),
		LetStmt(2, "i = ...", "i", "int64", false),
		LoopEnterStmt(3, "for (...)", 0),
		LetStmt(4, "j = ...", "j", "int64", false),
		ReadStmt(5, "i", "i"),
		LoopExitStmt(6, "endfor"),
		LoopExitStmt(7, "endfor"),
	}

	ld := NewLoopDetector()
	loops := ld.DetectLoops(stmts)

	if len(loops) != 1 {
		t.Fatalf("Expected 1 top-level loop, got %d", len(loops))
	}

	loop := loops[0]
	if len(loop.NestedLoops) != 1 {
		t.Fatalf("Expected 1 nested loop, got %d", len(loop.NestedLoops))
	}

	nested := loop.NestedLoops[0]
	if len(nested.BodyVars) != 1 || nested.BodyVars[0] != "j" {
		t.Errorf("Nested BodyVars = %v, want [j]", nested.BodyVars)
	}
}

func TestLoopDetector_GetReusableVars(t *testing.T) {
	loop := &LoopInfo{
		IterCount:     10,
		BodyVars:      []string{"x", "y", "z"},
		ExternalVars:  []string{"y"}, // y is used outside
		NestedLoops:   nil,
	}

	reusable := loop.GetReusableVars()
	// x and z are reusable (not in ExternalVars)
	if len(reusable) != 2 {
		t.Fatalf("Expected 2 reusable vars, got %d", len(reusable))
	}
	if reusable[0] != "x" || reusable[1] != "z" {
		t.Errorf("Reusable = %v, want [x, z]", reusable)
	}
}

func TestLoopDetector_ExternalVars(t *testing.T) {
	stmts := []Stmt{
		LoopEnterStmt(1, "for (...)", 10),
		LetStmt(2, "x = ...", "x", "int64", false),
		ReadStmt(3, "n", "n"), // n is external
		ReadStmt(4, "x", "x"), // x is internal
		LoopExitStmt(5, "endfor"),
	}

	ld := NewLoopDetector()
	loops := ld.DetectLoops(stmts)

	if len(loops) != 1 {
		t.Fatalf("Expected 1 loop, got %d", len(loops))
	}

	loop := loops[0]
	if len(loop.ExternalVars) != 1 || loop.ExternalVars[0] != "n" {
		t.Errorf("ExternalVars = %v, want [n]", loop.ExternalVars)
	}
}

func TestLoopDetector_MultipleLoops(t *testing.T) {
	stmts := []Stmt{
		LoopEnterStmt(1, "while (...)", 0),
		LetStmt(2, "a = ...", "a", "int64", false),
		LoopExitStmt(3, "endwhile"),
		LoopEnterStmt(4, "for (...)", 100),
		LetStmt(5, "b = ...", "b", "int64", false),
		LoopExitStmt(6, "endfor"),
	}

	ld := NewLoopDetector()
	loops := ld.DetectLoops(stmts)

	if len(loops) != 2 {
		t.Fatalf("Expected 2 loops, got %d", len(loops))
	}
}
