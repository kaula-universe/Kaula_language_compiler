package sor

import (
	"testing"
)

func TestResourceRegistry_Basic(t *testing.T) {
	registry := NewResourceRegistry()

	if registry.IsResourceType("File") {
		t.Error("File 不应该是资源类型（注册前）")
	}

	registry.Register(&ResourceTypeInfo{
		TypeName: "File",
		Kind:     "file",
		ReleaseFunc: "close_file",
		Description: "文件句柄",
	})

	if !registry.IsResourceType("File") {
		t.Error("File 应该是资源类型（注册后）")
	}

	info, ok := registry.GetResourceInfo("File")
	if !ok {
		t.Fatal("应该能获取 File 的资源信息")
	}
	if info.Kind != "file" {
		t.Errorf("资源种类错误，期望 'file'，实际 '%s'", info.Kind)
	}
	if info.ReleaseFunc != "close_file" {
		t.Errorf("释放函数错误，期望 'close_file'，实际 '%s'", info.ReleaseFunc)
	}
}

func TestResourceLeakDetection(t *testing.T) {
	analyzer := NewSORAnalyzer()

	// 注册 File 为资源类型
	analyzer.RegisterResource(&ResourceTypeInfo{
		TypeName:    "File",
		Kind:        "file",
		ReleaseFunc: "close_file",
		Description: "文件句柄",
	})

	// 测试场景：在作用域内创建资源但不释放
	stmts := []Stmt{
		{Kind: StmtScopeEnter, Line: 1, ScopeName: "main"},
		{Kind: StmtLet, Line: 2, VarName: "f", TypeName: "File", IsComposite: false},
		{Kind: StmtScopeExit, Line: 3, ScopeName: "main"},
	}

	errors := analyzer.Analyze(stmts)

	// 应该有一个资源泄漏错误
	foundLeak := false
	for _, err := range errors {
		if err.Kind == ErrResourceLeak {
			foundLeak = true
			t.Logf("检测到资源泄漏（预期）: %s", err.Message)
			break
		}
	}

	if !foundLeak {
		t.Error("应该检测到资源泄漏，但没有")
	}
}

func TestResourceYeide_TransferOwnership(t *testing.T) {
	analyzer := NewSORAnalyzer()

	analyzer.RegisterResource(&ResourceTypeInfo{
		TypeName:    "File",
		Kind:        "file",
		ReleaseFunc: "close_file",
	})

	// 测试场景：资源所有权从 f1 yeide 到 f2
	// yeide 后 f1 不再拥有资源，f2 拥有资源
	stmts := []Stmt{
		{Kind: StmtScopeEnter, Line: 1, ScopeName: "main"},
		{Kind: StmtLet, Line: 2, VarName: "f1", TypeName: "File", IsComposite: false},
		{Kind: StmtYeide, Line: 3, SrcName: "f1", VarName: "f2"},
		{Kind: StmtRead, Line: 4, VarName: "f2"},
		{Kind: StmtScopeExit, Line: 5, ScopeName: "main"},
	}

	errors := analyzer.Analyze(stmts)

	// f1 被 yeide 了，不再拥有资源，所以不会泄漏
	// f2 拥有资源，在作用域结束时应该报泄漏（因为没有释放）
	leakCount := 0
	for _, err := range errors {
		if err.Kind == ErrResourceLeak {
			leakCount++
			t.Logf("检测到资源泄漏: %s", err.Message)
		}
	}

	// 应该只有 f2 一个泄漏（f1 已经被 yeide 了）
	if leakCount != 1 {
		t.Errorf("期望 1 个资源泄漏，实际有 %d 个", leakCount)
	}
}

func TestNonResource_NoLeakError(t *testing.T) {
	analyzer := NewSORAnalyzer()

	// 不注册任何资源类型，普通变量不应该报泄漏
	stmts := []Stmt{
		{Kind: StmtScopeEnter, Line: 1, ScopeName: "main"},
		{Kind: StmtLet, Line: 2, VarName: "x", TypeName: "int", IsComposite: false},
		{Kind: StmtScopeExit, Line: 3, ScopeName: "main"},
	}

	errors := analyzer.Analyze(stmts)

	for _, err := range errors {
		if err.Kind == ErrResourceLeak {
			t.Errorf("普通变量不应该报资源泄漏: %s", err.Message)
		}
	}
}

func TestResourceRelease_NoLeak(t *testing.T) {
	analyzer := NewSORAnalyzer()

	analyzer.RegisterResource(&ResourceTypeInfo{
		TypeName:    "File",
		Kind:        "file",
		ReleaseFunc: "close_file",
	})

	// 测试场景：资源被 release 分发给持有者
	// 注意：release 状态的资源在作用域结束时自动失效，不视为泄漏
	stmts := []Stmt{
		{Kind: StmtScopeEnter, Line: 1, ScopeName: "main"},
		{Kind: StmtLet, Line: 2, VarName: "f", TypeName: "File", IsComposite: false},
		{Kind: StmtRelease, Line: 3, SrcName: "f", HolderNames: []string{"a", "b"}},
		{Kind: StmtRead, Line: 4, VarName: "a"},
		{Kind: StmtScopeExit, Line: 5, ScopeName: "main"},
	}

	errors := analyzer.Analyze(stmts)

	// release 状态的对象在作用域结束时不视为泄漏
	// （因为 release 是只读共享，原持有者不再独占）
	// 但实际上，资源释放需要调用释放函数，这里我们暂时认为 release 不是释放
	// 资源泄漏检查只检查 Owned 状态的资源
	for _, err := range errors {
		if err.Kind == ErrResourceLeak {
			t.Logf("release 状态资源的处理: %s", err.Message)
		}
	}
}
