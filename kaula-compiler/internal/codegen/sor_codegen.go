package codegen

import (
	"fmt"
	"strings"
)

// SORCodeGenAdapter 将 SOR 分析结果适配到 CodeGen 的辅助结构
// 作为 sor 包和 codegen 包之间的桥梁，避免循环导入
type SORCodeGenAdapter struct {
	// 从 FullAnalysisResult 提取的、CodeGen 需要的信息
	VarDecisions   map[string]*SORVarDecision // varName -> 决策
	ScopeDecisions map[int]*SORScopeDecision  // scopeID -> 作用域决策
	IsActive       bool                       // SOR 分析是否启用

	// 修复 #18：直接使用 SOR 的 scopeID，不再自建映射
	// codegen scopeName -> SOR scopeID（从 decision 的 scope_id 提取）
	scopeNameToID map[string]int

	// 修复 #16：liveness 数据，用于 scope 拆分优化
	lastUses map[string]int // varName -> 最后使用行号
}

// SORVarDecision 单个变量的 CodeGen 决策
type SORVarDecision struct {
	VarName     string // 变量名
	ObjID       string // SOR 对象 ID
	CTypeName   string // C 类型名（如 "int64_t*", "MyStruct*"）
	AllocKind   string // "stack", "bmppool"（arena 已收敛到 bmppool）
	AllocKindID int    // 修复 #23：int 值，0=Stack, 1=BumpPool, 2-4=Arena(收敛到BumpPool)
	DropAction  string // "none", "scope_end", "hollow"
	DropActionID int   // 修复 #23：int 值，0=None, 1=ScopeEnd, 2=Hollow
	EscapeLevel string // "none", "arg", "return", "cross_scope", "heap"
	SizeBytes   int    // 估算的字节大小
	IsSOR       bool   // 是否由 SOR 追踪
	ScopeID     int    // 修复 #18：直接存储 SOR scopeID
}

// SORScopeDecision 作用域级别的 CodeGen 决策
type SORScopeDecision struct {
	ScopeID      int    // 作用域 ID
	UsesBumpPool bool   // 是否使用 bump pool
}

// NewSORCodeGenAdapter 从分析结果的序列化形式创建 CodeGen 适配器
// input: FullAnalysisResult 的序列化形式（map[string]interface{}）
// 通过 sor.FullAnalysisResultSerialize() 生成，避免 codegen 对 sor 的直接依赖
func NewSORCodeGenAdapter(result map[string]interface{}) *SORCodeGenAdapter {
	adapter := &SORCodeGenAdapter{
		VarDecisions:   make(map[string]*SORVarDecision),
		ScopeDecisions: make(map[int]*SORScopeDecision),
		scopeNameToID:  make(map[string]int),
		lastUses:       make(map[string]int),
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

				// 修复 #23：优先使用 int 值，回退到字符串解析
				allocKindID := 0
				if id, ok := dm["alloc_kind_id"].(int); ok {
					allocKindID = id
				} else if id, ok := dm["alloc_kind_id"].(float64); ok {
					allocKindID = int(id)
				}
				dropActionID := 0
				if id, ok := dm["drop_action_id"].(int); ok {
					dropActionID = id
				} else if id, ok := dm["drop_action_id"].(float64); ok {
					dropActionID = int(id)
				}

				// 修复 #18：优先使用 int scopeID
				scopeID := 0
				if id, ok := dm["scope_id_int"].(int); ok {
					scopeID = id
				} else if id, ok := dm["scope_id_int"].(float64); ok {
					scopeID = int(id)
				} else {
					scopeIDStr, _ := dm["scope_id"].(string)
					if scopeIDStr != "" {
						fmt.Sscanf(scopeIDStr, "%d", &scopeID)
					}
				}

				// 修复 #15：Arena 分级收敛到 BumpPool
				// ArenaTiny(2)/ArenaSmall(3)/ArenaMedium(4) 全部映射到 BumpPool
				normalizedKind := allocKind
				switch allocKind {
				case "ArenaTiny", "ArenaSmall", "ArenaMedium":
					normalizedKind = "BumpPool"
				}
				// 基于 int 的收敛
				if allocKindID >= 2 && allocKindID <= 4 {
					normalizedKind = "BumpPool"
					allocKindID = 1 // BumpPool
				}

				decision := &SORVarDecision{
					VarName:      varName,
					ObjID:        objID,
					AllocKind:    normalizedKind,
					AllocKindID:  allocKindID,
					DropAction:   dropAction,
					DropActionID: dropActionID,
					IsSOR:        true,
					ScopeID:      scopeID,
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

				// 构建作用域决策（按 SOR scopeID 聚合）
				sd := adapter.ScopeDecisions[scopeID]
				if sd == nil {
					sd = &SORScopeDecision{ScopeID: scopeID}
					adapter.ScopeDecisions[scopeID] = sd
				}

				// 修复 #15：收敛后只判断 BumpPool
				if normalizedKind == "BumpPool" {
					sd.UsesBumpPool = true
				}
			}
		}
	}

	// 修复 #16：解析 liveness 数据
	if liveness, ok := result["liveness"].([]interface{}); ok {
		for _, l := range liveness {
			if lm, ok := l.(map[string]interface{}); ok {
				varName, _ := lm["var_name"].(string)
				lastLine := 0
				if line, ok := lm["last_use_line"].(int); ok {
					lastLine = line
				} else if line, ok := lm["last_use_line"].(float64); ok {
					lastLine = int(line)
				}
				if varName != "" && lastLine > 0 {
					adapter.lastUses[varName] = lastLine
				}
			}
		}
	}

	// 修复 #17：funcSigs 未消费，不再解析

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
// 修复 #18：直接用 SOR scopeID 查询
func (a *SORCodeGenAdapter) GetScopeDecision(scopeID int) *SORScopeDecision {
	if a == nil || !a.IsActive {
		return nil
	}
	return a.ScopeDecisions[scopeID]
}

// GetLastUseLine 获取变量的最后使用行号（修复 #16）
func (a *SORCodeGenAdapter) GetLastUseLine(varName string) int {
	if a == nil || !a.IsActive {
		return 0
	}
	return a.lastUses[varName]
}

// NeedsKMMScopeByVars 判断一组变量是否需要 KMM scope
// 如果其中任何一个变量需要 BumpPool 分配且需要作用域回收，则返回 true
// 活跃性分析驱动的优化：
// - DropNone(0)：已 yield/extract，所有权已转移，不需要 KMM 回收
// - DropHollow(2)：hollow 状态，所有权已转移只剩外壳，不需要 KMM 回收
// - DropScopeEnd(1)：作用域结束时需要回收，需要 KMM
func (a *SORCodeGenAdapter) NeedsKMMScopeByVars(varNames []string) bool {
	if !a.IsActive {
		return false
	}
	for _, name := range varNames {
		if d := a.GetVarDecision(name); d != nil {
			// 修复 #23：优先用 int 判断，回退到字符串
			// DropNone(0) 和 DropHollow(2) 不需要 KMM 回收
			if d.DropActionID == 0 || d.DropActionID == 2 {
				continue
			}
			// 字符串回退（兼容旧序列化）
			if d.DropAction == "None" || d.DropAction == "" || d.DropAction == "Hollow" {
				continue
			}
			// 修复 #15：收敛后只判断 BumpPool
			if d.AllocKind == "BumpPool" || d.AllocKindID == 1 {
				return true
			}
		}
	}
	return false
}

// GenerateSmartVarAlloc 生成智能内存分配代码
// 根据 SOR 的大小路由和逃逸分析结果，选择最合适的分配策略
// 修复 #15：Arena 分级已收敛到 BumpPool，统一生成 kmm_v4_alloc_auto
// 如果提供了 initValue，生成的代码会将变量初始化为该值
func (a *SORCodeGenAdapter) GenerateSmartVarAlloc(cType, varName, indent string, initValue string) string {
	if !a.IsActive {
		return a.defaultVarDecl(cType, varName, indent, initValue)
	}

	decision := a.GetVarDecision(varName)
	if decision == nil || !decision.IsSOR {
		return a.defaultVarDecl(cType, varName, indent, initValue)
	}

	isPointer := strings.HasSuffix(cType, "*")
	if !isPointer {
		return a.stackVarDecl(cType, varName, indent, initValue, decision.AllocKind)
	}

	baseType := strings.TrimRight(cType, "*")
	// 修复 #15：所有非栈分配统一收敛到 BumpPool/kmm_v4_alloc_auto
	if decision.AllocKind == "BumpPool" {
		if initValue != "" {
			return fmt.Sprintf("%s%s %s = (%s)kmm_v4_alloc_auto(sizeof(%s)); /* sor: %s */\n%s*%s = %s;\n",
				indent, cType, varName, cType, baseType, decision.AllocKind,
				indent, varName, initValue)
		}
		return fmt.Sprintf("%s%s %s = (%s)kmm_v4_alloc_auto(sizeof(%s)); /* sor: %s */\n",
			indent, cType, varName, cType, baseType, decision.AllocKind)
	}
	return a.stackVarDecl(cType, varName, indent, initValue, decision.AllocKind)
}

// defaultVarDecl 生成不带 SOR 决策的默认变量声明
func (a *SORCodeGenAdapter) defaultVarDecl(cType, varName, indent, initValue string) string {
	if initValue != "" {
		return fmt.Sprintf("%s%s %s = %s;\n", indent, cType, varName, initValue)
	}
	return fmt.Sprintf("%s%s %s;\n", indent, cType, varName)
}

// stackVarDecl 生成栈分配变量声明（含 SOR 标注）
func (a *SORCodeGenAdapter) stackVarDecl(cType, varName, indent, initValue, allocKind string) string {
	if initValue != "" {
		return fmt.Sprintf("%s%s %s = %s; /* sor: %s */\n", indent, cType, varName, initValue, allocKind)
	}
	return fmt.Sprintf("%s%s %s = {0}; /* sor: %s */\n", indent, cType, varName, allocKind)
}

// String 返回适配器的摘要信息（用于调试）
func (a *SORCodeGenAdapter) String() string {
	if a == nil || !a.IsActive {
		return "SORCodeGenAdapter(inactive)"
	}
	return fmt.Sprintf("SORCodeGenAdapter(active: %d vars, %d scopes, %d liveness)",
		len(a.VarDecisions), len(a.ScopeDecisions), len(a.lastUses))
}

// ============================================================================
// 作用域 ID 映射（修复 #18）
// ============================================================================

// RegisterScope 注册 CodeGen 作用域名并关联到 SOR scopeID
// 修复 #18：接受外部传入的 sorScopeID，建立 name→sorScopeID 映射
func (a *SORCodeGenAdapter) RegisterScope(scopeName string) int {
	if a == nil || !a.IsActive {
		return 0
	}
	// 如果已注册则返回已有 ID
	if id, ok := a.scopeNameToID[scopeName]; ok {
		return id
	}
	// 默认分配新 ID（与 SOR 无关的场景）
	id := len(a.scopeNameToID) + 1
	a.scopeNameToID[scopeName] = id
	return id
}

// MapScope 建立 CodeGen scopeName 与 SOR scopeID 的映射（修复 #18）
func (a *SORCodeGenAdapter) MapScope(scopeName string, sorScopeID int) {
	if a == nil || !a.IsActive {
		return
	}
	a.scopeNameToID[scopeName] = sorScopeID
}

// GetScopeIDByName 通过 CodeGen 作用域名获取 SOR scopeID
func (a *SORCodeGenAdapter) GetScopeIDByName(scopeName string) int {
	if a == nil || !a.IsActive {
		return 0
	}
	if id, ok := a.scopeNameToID[scopeName]; ok {
		return id
	}
	return 0
}
