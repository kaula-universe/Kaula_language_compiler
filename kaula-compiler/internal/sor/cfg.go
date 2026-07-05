package sor

// BasicBlock represents a sequence of statements with no branches
type BasicBlock struct {
	ID    int
	Stmts []Stmt
	Succs []*BasicBlock
	Preds []*BasicBlock
}

// CFG represents the control flow graph
type CFG struct {
	Blocks []*BasicBlock
	Entry  *BasicBlock
	Exit   *BasicBlock
}

// BuildCFG constructs a CFG from a statement list
func BuildCFG(stmts []Stmt) *CFG {
	block := &BasicBlock{
		ID:    0,
		Stmts: stmts,
	}
	return &CFG{
		Blocks: []*BasicBlock{block},
		Entry:  block,
		Exit:   block,
	}
}
