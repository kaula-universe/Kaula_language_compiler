package codegen

import (
	"fmt"
	"strings"
)

// SORCodeGenAdapter 将 SOR 分析结果适配到 CodeGen 的辅助结构
// 作为 sor 包和 codegen 包之间的桥梁，避免循环导入
type SORCodeGenAdapter struct {
	// 从 FullAnalysisResult 提取的、CodeGen 需要的信息
	VarDecisions  map[string]*SORVarDecision // varName -> 决策
	ScopeDecisions map[int]*SORScopeDecision  // scopeID -> 作用域决策
	IsActive       bool                      // SOR 分析是否启用

	// 作用域 ID 映射：CodeGen scopeName <-> SOR scopeID
	// 建立 CodeGen 的字符串作用域名与 SOR 的整数作用域 ID 之间的双向映射
	scopeNameToID map[string]int // scopeName -> SOR scopeID
	scopeIDToName map[int]string // SOR scopeID -> scopeName
	nextScopeID   int            // 下一个可用的 scopeID

	// 跨函数分析结果
	funcSigs map[string]*SORFuncSig // funcName -> 函数所有权签名
}

// SORFuncSig 函数所有权签名（从 InterProcResult 提取）
type SORFuncSig struct {
	Name         string
	ParamModes   []string // 每个参数的所有权模式: "Owned", "Released", "Unrestricted"
	HasPtrReturn bool     // 返回值是否涉及指针/所有权
}

// SORVarDecision 单个变量的 CodeGen 决策
type SORVarDecision struct {
	VarName    string // 变量名
	ObjID      string // SOR 对象 ID
	CTypeName  string // C 类型名（如 "int64_t*", "MyStruct*"）
	AllocKind  string // "stack", "bmppool", "arena_tiny", "arena_small", "arena_medium"
	DropAction string // "none", "scope_end", "hollow"
	EscapeLevel string // "none", "arg", "return", "cross_scope", "heap"
	SizeBytes  int    // 估算的字节大小
	IsSOR      bool   // 是否由 SOR 追踪
}

// SORScopeDecision 作用域级别的 CodeGen 决策
type SORScopeDecision struct {
	ScopeID      int      // 作用域 ID
	DropVars     []string // 需要在作用域退出时释放的变量
	HollowVars   []string // 需要清理 hollow 状态的变量
	UsesBumpPool bool     // 是否使用 bump pool
	UsesArena    string   // "" 或 "tiny" 或 "small" 或 "medium"
}

// NewSORCodeGenAdapter 从分析结果的序列化形式创建 CodeGen 适配器
// input: FullAnalysisResult 的序列化形式（map[string]interface{}）
// 通过 sor.FullAnalysisResultSerialize() 生成，避免 codegen 对 sor 的直接依赖
func NewSORCodeGenAdapter(result map[string]interface{}) *SORCodeGenAdapter {
	adapter := &SORCodeGenAdapter{
		VarDecisions:  make(map[string]*SORVarDecision),
		ScopeDecisions: make(map[int]*SORScopeDecision),
		scopeNameToID: make(map[string]int),
		scopeIDToName: make(map[int]string),
		nextScopeID:   1, // 0 保留给全局作用域
		funcSigs:      make(map[string]*SORFuncSig),
	}
	if result == nil {
		return adapter
	}
	adapter.IsActive = true

	// 从 decisions 提取变量决策
	if decisions, ok := result["decisions"].([]interface{}); ok {
		for _, d := range decisions {
			if dm, ok := d.(map[string]interface{}); ok {
				varName, _ := dm["var_name"].(string)
				objID, _ := dm["obj_id"].(string)
				allocKind, _ := dm["alloc_kind"].(string)
				dropAction, _ := dm["drop_action"].(string)

				decision := &SORVarDecision{
					VarName:    varName,
					ObjID:      objID,
					AllocKind:  allocKind,
					DropAction: dropAction,
					IsSOR:      true,
				}

				// 从逃逸信息补充
				if escape, ok := result["escape"].(map[string]interface{}); ok {
					if objID != "" {
						if level, ok := escape[objID].(string); ok {
							decision.EscapeLevel = level
						}
					}
				}

				// 从大小信息补充
				if sizes, ok := result["sizes"].(map[string]interface{}); ok {
					if objID != "" {
						if sizeVal, ok := sizes[objID]; ok {
							switch v := sizeVal.(type) {
							case int:
								decision.SizeBytes = v
							case int64:
								decision.SizeBytes = int(v)
							case float64:
								decision.SizeBytes = int(v)
							}
						}
					}
				}

				adapter.VarDecisions[varName] = decision

				// 构建作用域决策（按 scope_id 聚合）
				scopeIDStr, _ := dm["scope_id"].(string)
				if scopeIDStr != "" {
					// 简单解析 scope ID
					scopeID := 0
					fmt.Sscanf(scopeIDStr, "%d", &scopeID)

					sd := adapter.ScopeDecisions[scopeID]
					if sd == nil {
						sd = &SORScopeDecision{ScopeID: scopeID}
						adapter.ScopeDecisions[scopeID] = sd
					}

					switch dropAction {
					case "Hollow":
						sd.HollowVars = append(sd.HollowVars, varName)
					case "ScopeEnd":
						sd.DropVars = append(sd.DropVars, varName)
					}

					switch allocKind {
					case "BumpPool":
						sd.UsesBumpPool = true
					case "ArenaTiny":
						sd.UsesArena = "tiny"
					case "ArenaSmall":
						sd.UsesArena = "small"
					case "ArenaMedium":
						sd.UsesArena = "medium"
					}
				}
			}
		}
	}

	// 解析跨函数分析结果
	if funcSigs, ok := result["func_sigs"].(map[string]interface{}); ok {
		for name, sig := range funcSigs {
			if sigMap, ok := sig.(map[string]interface{}); ok {
				fs := &SORFuncSig{Name: name}
				if modes, ok := sigMap["param_modes"].([]interface{}); ok {
					for _, m := range modes {
						if modeStr, ok := m.(string); ok {
							fs.ParamModes = append(fs.ParamModes, modeStr)
						}
					}
				}
				if hasPtr, ok := sigMap["has_ptr_return"].(bool); ok {
					fs.HasPtrReturn = hasPtr
				}
				adapter.funcSigs[name] = fs
			}
		}
	}

	return adapter
}

// GetVarDecision 获取变量的 CodeGen 决策
func (a *SORCodeGenAdapter) GetVarDecision(varName string) *SORVarDecision {
	if a == nil || !a.IsActive {
		return nil
	}
	return a.VarDecisions[varName]
}

// GetScopeDecision 获取作用域的 CodeGen 决策
func (a *SORCodeGenAdapter) GetScopeDecision(scopeID int) *SORScopeDecision {
	if a == nil || !a.IsActive {
		return nil
	}
	return a.ScopeDecisions[scopeID]
}

// GenerateVarDecl 生成变量的 C 声明代码（含 Arena 路由）
// 如果变量有 SOR 决策，将根据分配策略生成带注释的初始化声明
func (a *SORCodeGenAdapter) GenerateVarDecl(cType, varName string, indent string) string {
	if decision := a.GetVarDecision(varName); decision != nil && decision.IsSOR {
		switch decision.AllocKind {
		case "ArenaTiny":
			return fmt.Sprintf("%s%s %s = {0}; /* sor: arena_tiny */\n", indent, cType, varName)
		case "ArenaSmall":
			return fmt.Sprintf("%s%s %s = {0}; /* sor: arena_small */\n", indent, cType, varName)
		case "ArenaMedium":
			return fmt.Sprintf("%s%s %s = {0}; /* sor: arena_medium */\n", indent, cType, varName)
		case "BumpPool":
			return fmt.Sprintf("%s%s %s = {0}; /* sor: bmppool */\n", indent, cType, varName)
		default:
			return fmt.Sprintf("%s%s %s = {0}; /* sor: stack */\n", indent, cType, varName)
		}
	}
	return fmt.Sprintf("%s%s %s;\n", indent, cType, varName)
}

// GenerateScopeCleanup 生成作用域退出时的 SOR 清理代码
// 在 KMM_V4_SCOPE_END 之前调用，处理 hollow 清理和 scope_end 释放
func (a *SORCodeGenAdapter) GenerateScopeCleanup(scopeID int, indent string) string {
	if !a.IsActive {
		return ""
	}

	decision := a.GetScopeDecision(scopeID)
	if decision == nil {
		return ""
	}

	var b strings.Builder
	for _, varName := range decision.HollowVars {
		b.WriteString(fmt.Sprintf("%s/* sor: hollow cleanup %s */\n", indent, varName))
	}
	for _, varName := range decision.DropVars {
		b.WriteString(fmt.Sprintf("%s/* sor: drop %s (scope_end) */\n", indent, varName))
	}
	return b.String()
}

// NeedsKMMScope 判断指定作用域是否需要 KMM scope
// 基于 SOR 的精确分析结果，只有当作用域内有 BumpPool/Arena 分配的变量时才返回 true
func (a *SORCodeGenAdapter) NeedsKMMScope(scopeID int) bool {
	if !a.IsActive {
		return false
	}
	sd := a.GetScopeDecision(scopeID)
	if sd == nil {
		return false
	}
	return sd.UsesBumpPool || sd.UsesArena != ""
}

// NeedsKMMScopeByVars 判断一组变量是否需要 KMM scope
// 如果其中任何一个变量需要 BumpPool/Arena 分配且需要作用域回收，则返回 true
// 活跃性分析驱动的优化：
// - DropNone：已 yield/extract，所有权已转移，不需要 KMM 回收
// - DropHollow：hollow 状态，所有权已转移只剩外壳，不需要 KMM 回收
// - DropScopeEnd：作用域结束时需要回收，需要 KMM
func (a *SORCodeGenAdapter) NeedsKMMScopeByVars(varNames []string) bool {
	if !a.IsActive {
		return false
	}
	for _, name := range varNames {
		if d := a.GetVarDecision(name); d != nil {
			// 活跃性分析：DropNone 和 DropHollow 的变量不需要 KMM 回收
			if d.DropAction == "None" || d.DropAction == "" || d.DropAction == "Hollow" {
				continue
			}
			switch d.AllocKind {
			case "BumpPool", "ArenaTiny", "ArenaSmall", "ArenaMedium":
				return true
			}
		}
	}
	return false
}

// GenerateSmartVarAlloc 生成智能内存分配代码
// 根据 SOR 的大小路由和逃逸分析结果，选择最合适的分配策略
func (a *SORCodeGenAdapter) GenerateSmartVarAlloc(cType, varName string, indent string) string {
	if !a.IsActive {
		return fmt.Sprintf("%s%s %s;\n", indent, cType, varName)
	}

	decision := a.GetVarDecision(varName)
	if decision == nil || !decision.IsSOR {
		return fmt.Sprintf("%s%s %s;\n", indent, cType, varName)
	}

	isPointer := strings.HasSuffix(cType, "*")
	if !isPointer {
		return fmt.Sprintf("%s%s %s = {0}; /* sor: %s */\n", indent, cType, varName, decision.AllocKind)
	}

	baseType := strings.TrimRight(cType, "*")
	switch decision.AllocKind {
	case "BumpPool", "ArenaTiny", "ArenaSmall", "ArenaMedium":
		return fmt.Sprintf("%s%s %s = (%s)kmm_v4_alloc_auto(sizeof(%s)); /* sor: %s */\n",
			indent, cType, varName, cType, baseType, decision.AllocKind)
	default:
		return fmt.Sprintf("%s%s %s = {0}; /* sor: stack */\n", indent, cType, varName)
	}
}

// GetScopeAllocSummary 获取作用域的分配策略摘要
func (a *SORCodeGenAdapter) GetScopeAllocSummary(scopeID int) string {
	if !a.IsActive {
		return ""
	}
	sd := a.GetScopeDecision(scopeID)
	if sd == nil {
		return ""
	}
	var parts []string
	if sd.UsesBumpPool {
		parts = append(parts, "bump_pool")
	}
	if sd.UsesArena != "" {
		parts = append(parts, "arena_"+sd.UsesArena)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

// String 返回适配器的摘要信息（用于调试）
func (a *SORCodeGenAdapter) String() string {
	if a == nil || !a.IsActive {
		return "SORCodeGenAdapter(inactive)"
	}
	return fmt.Sprintf("SORCodeGenAdapter(active: %d vars, %d scopes, %d funcs)",
		len(a.VarDecisions), len(a.ScopeDecisions), len(a.funcSigs))
}

// ============================================================================
// 作用域 ID 映射：建立 CodeGen scopeName <-> SOR scopeID 的双向映射
// ============================================================================

// RegisterScope 注册 CodeGen 作用域名并返回对应的 SOR scopeID
// 如果已注册则返回已有 ID，否则分配新 ID
func (a *SORCodeGenAdapter) RegisterScope(scopeName string) int {
	if id, ok := a.scopeNameToID[scopeName]; ok {
		return id
	}
	id := a.nextScopeID
	a.nextScopeID++
	a.scopeNameToID[scopeName] = id
	a.scopeIDToName[id] = scopeName
	return id
}

// GetScopeIDByName 通过 CodeGen 作用域名获取 SOR scopeID
func (a *SORCodeGenAdapter) GetScopeIDByName(scopeName string) int {
	if id, ok := a.scopeNameToID[scopeName]; ok {
		return id
	}
	return 0
}

// GetScopeNameByID 通过 SOR scopeID 获取 CodeGen 作用域名
func (a *SORCodeGenAdapter) GetScopeNameByID(scopeID int) string {
	return a.scopeIDToName[scopeID]
}

// NeedsKMMScopeByScopeName 通过 CodeGen 作用域名判断是否需要 KMM scope
// 统一使用 SOR scopeID 查询 ScopeDecisions，实现精确的作用域级别判断
func (a *SORCodeGenAdapter) NeedsKMMScopeByScopeName(scopeName string) bool {
	if !a.IsActive {
		return false
	}
	scopeID := a.GetScopeIDByName(scopeName)
	if scopeID == 0 {
		return false
	}
	return a.NeedsKMMScope(scopeID)
}

// ============================================================================
// 跨函数分析驱动 KMM 优化
// ============================================================================

// IsPureFunction 判断函数是否为纯函数（不涉及指针所有权传递）
// 纯函数不需要 KMM scope，因为所有参数都是值语义拷贝或只读借用
func (a *SORCodeGenAdapter) IsPureFunction(funcName string) bool {
	if !a.IsActive {
		return false
	}
	sig, ok := a.funcSigs[funcName]
	if !ok {
		return false // 无签名信息，保守处理
	}
	// 有指针返回值的函数不是纯函数
	if sig.HasPtrReturn {
		return false
	}
	// 检查参数：如果有 Owned 模式的参数，说明所有权转移发生，不是纯函数
	for _, mode := range sig.ParamModes {
		if mode == "Owned" {
			return false
		}
	}
	return true
}

// GetFuncSig 获取函数所有权签名
func (a *SORCodeGenAdapter) GetFuncSig(funcName string) *SORFuncSig {
	if !a.IsActive {
		return nil
	}
	return a.funcSigs[funcName]
}
