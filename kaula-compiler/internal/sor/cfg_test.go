package sor

import (
	"testing"
)

func TestCFGConstruction(t *testing.T) {
	stmts := []Stmt{
		{Kind: StmtLet, VarName: "x", TypeName: "i64"},
		{Kind: StmtYeide, SrcName: "x", VarName: "y"},
		{Kind: StmtRead, VarName: "y"},
	}

	cfg := BuildCFG(stmts)
	if cfg == nil {
		t.Fatal("CFG should not be nil")
	}
	if len(cfg.Blocks) != 1 {
		t.Errorf("Simple linear code should have 1 block, got %d", len(cfg.Blocks))
	}
}
