package codegen

import (
	"fmt"
	"strconv"
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

	// 修复 #25：跨函数所有权签名（funcSigs 消费方恢复）
	funcSigs map[string]*SORFuncSig // funcName -> 参数所有权签名

	// 方案 A：scope drop points（scopeID → 需在作用域退出时释放的变量名列表）
	// 供 codegen 在作用域退出点生成 per-object survivor_free 代码
	scopeDropPoints map[int][]string

	// 任务③：extern 逃逸来源（变量名 → extern 函数名）。
	// 来自 escape.go analyzeExternBind。任何来自 extern 返回值的对象对 SOR
	// 都是 opaque，跨作用域 promote 会产生 Bug 2 式悬垂（内部指针未被追踪），
	// 必须拒绝/分流而非静默浅提升。
	externOrigins map[string]string

	// 根治 #1：决策按 (函数, 变量名) 建索引。
	// 变量名只在词法作用域内唯一，跨函数同名是常态；若只按变量名索引，
	// 被调函数的形参会命中调用方同名局部变量的决策——历史上导致
	// "promote arg into callee" 在形参上就地改写并生成 sizeof(*p) 截断副本，
	// 递归调用直接越界读（btree_bench --sor 段错误）。
	funcVarDecisions map[string]*SORVarDecision // funcName\x00varName -> 决策
	globalDecisions  map[string]*SORVarDecision // 顶层声明（funcName == ""）varName -> 决策
	// currentFunc 当前正在生成的函数名（""=顶层/main 之外的全局上下文）
	currentFunc string
}

// SORFuncSig 函数参数的所有权签名（修复 #25）
// 参数模式: 0=ModeOwned(消费所有权/move in), 1=ModeReleased(只读借用), 2=ModeUnrestricted(值语义)
type SORFuncSig struct {
	Params     map[string]int // paramName -> OwnershipMode（按名查询）
	ParamModes []int          // 按位置查询的参数模式（实参按位置匹配形参）
}

// SORVarDecision 单个变量的 CodeGen 决策
type SORVarDecision struct {
	VarName      string // 变量名
	ObjID        string // SOR 对象 ID
	FuncName     string // 声明所在函数（""=顶层声明）；根治 #1 的索引键之一
	CTypeName    string // C 类型名（如 "int64_t*", "MyStruct*"）
	AllocKind    string // "stack", "bmppool"（arena 已收敛到 bmppool）
	AllocKindID  int    // 修复 #23：int 值，0=Stack, 1=BumpPool, 2-4=Arena(收敛到BumpPool)
	DropAction   string // "none", "scope_end", "hollow"
	DropActionID int    // 修复 #23：int 值，0=None, 1=ScopeEnd, 2=Hollow
	EscapeLevel  string // "none", "arg", "return", "cross_scope", "heap"
	SizeBytes    int    // 估算的字节大小
	IsSOR        bool   // 是否由 SOR 追踪
	ScopeID      int    // 修复 #18：直接存储 SOR scopeID
}

// SORScopeDecision 作用域级别的 CodeGen 决策
type SORScopeDecision struct {
	ScopeID      int  // 作用域 ID
	UsesBumpPool bool // 是否使用 bump pool
}

// NewSORCodeGenAdapter 从分析结果的序列化形式创建 CodeGen 适配器
// input: FullAnalysisResult 的序列化形式（map[string]interface{}）
// 通过 sor.FullAnalysisResultSerialize() 生成，避免 codegen 对 sor 的直接依赖
func NewSORCodeGenAdapter(result map[string]interface{}) *SORCodeGenAdapter {
	adapter := &SORCodeGenAdapter{
		VarDecisions:       make(map[string]*SORVarDecision),
		ScopeDecisions:     make(map[int]*SORScopeDecision),
		scopeNameToID:      make(map[string]int),
		lastUses:           make(map[string]int),
		funcSigs:           make(map[string]*SORFuncSig),
		scopeDropPoints:    make(map[int][]string),
		externOrigins:      make(map[string]string),
		funcVarDecisions:   make(map[string]*SORVarDecision),
		globalDecisions:    make(map[string]*SORVarDecision),
		currentFunc:        "",
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
				funcName, _ := dm["func_name"].(string)
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
					FuncName:     funcName,
					AllocKind:    normalizedKind,
					AllocKindID:  allocKindID,
					DropAction:   dropAction,
					DropActionID: dropActionID,
					IsSOR:        true,
					ScopeID:      scopeID,
				}

				// 从逃逸信息补充
				// 逃逸表键位：sor.EscapeAnalyzer 以**变量名**为键（StmtLet.VarName），
				// 而决策同时携带 obj_id（obj_N）。两个键都试，先 obj_id 后 var_name，
				// 否则真实编译里 EscapeLevel 恒为空（快/慢路径路由拿不到逃逸信息）。
				if escape, ok := result["escape"].(map[string]interface{}); ok {
					key := objID
					if level, ok := escape[key].(string); ok {
						decision.EscapeLevel = level
					} else if varName != "" {
						if level, ok := escape[varName].(string); ok {
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

				// 根治 #1：决策按 (函数, 变量名) 建索引。
				// - funcName == ""：顶层声明，任意函数可见（globalDecisions）
				// - funcName != ""：仅该函数内部可见（funcVarDecisions）
				// 同名多函数各自独立，不再互相覆盖。
				if funcName == "" {
					if _, exists := adapter.globalDecisions[varName]; !exists {
						adapter.globalDecisions[varName] = decision
					}
				} else {
					key := funcVarKey(funcName, varName)
					if _, exists := adapter.funcVarDecisions[key]; !exists {
						adapter.funcVarDecisions[key] = decision
					}
				}

				// 兼容视图：按变量名的首个决策（仅供调试/无函数上下文的查询使用；
				// 决策消费方一律走 GetVarDecision 的函数级解析）
				if _, exists := adapter.VarDecisions[varName]; !exists {
					adapter.VarDecisions[varName] = decision
				}

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

	// 方案 A：解析 scope_drop_points（scopeID → []varNames）
	if dp, ok := result["scope_drop_points"].(map[string]interface{}); ok {
		for k, v := range dp {
			scopeID := 0
			if n, err := strconv.Atoi(k); err == nil {
				scopeID = n
			}
			if vars, ok := v.([]string); ok {
				adapter.scopeDropPoints[scopeID] = vars
			} else if varsI, ok := v.([]interface{}); ok {
				vars := make([]string, 0, len(varsI))
				for _, vi := range varsI {
					if s, ok := vi.(string); ok {
						vars = append(vars, s)
					}
				}
				if len(vars) > 0 {
					adapter.scopeDropPoints[scopeID] = vars
				}
			}
		}
	}

	// 修复 #25：恢复 funcSigs 解析（跨函数所有权传递消费方已恢复）
	if fs, ok := result["func_sigs"].(map[string]interface{}); ok {
		for fname, fv := range fs {
			fm, ok := fv.(map[string]interface{})
			if !ok {
				continue
			}
			sig := &SORFuncSig{Params: make(map[string]int)}
			if params, ok := fm["params"].([]interface{}); ok {
				for _, p := range params {
					pm, ok := p.(map[string]interface{})
					if !ok {
						continue
					}
					name, _ := pm["name"].(string)
					mode := 2 // 默认 Unrestricted
					if m, ok := pm["mode"].(int); ok {
						mode = m
					} else if m, ok := pm["mode"].(float64); ok {
						mode = int(m)
					}
					if name != "" {
						sig.Params[name] = mode
					}
					sig.ParamModes = append(sig.ParamModes, mode)
				}
			}
			adapter.funcSigs[fname] = sig
		}
	}

	// 任务③：解析 extern_origins（extern 返回值来源表，强制拒绝时要用）
	if eo, ok := result["extern_origins"].(map[string]interface{}); ok {
		for vname, fnVal := range eo {
			if fn, ok := fnVal.(string); ok && fn != "" {
				adapter.externOrigins[vname] = fn
			}
		}
	}

	return adapter
}

// IsExternOrigin 判断变量是否来自 extern 函数的返回值（opaque 外部分配对 SOR 不透明）。
// 任务③：promote 前必须先检查——extern 来源对象的跨作用域浅提升是 Bug 2 的来源，
// 必须强制拒绝（进入 codegen 错误列表 + C 生成时触发 #error）。
// 返回值：externFn = 产生该对象的 extern 函数名；ok = 是否是 extern 来源。
func (a *SORCodeGenAdapter) IsExternOrigin(varName string) (externFn string, ok bool) {
	if a == nil || !a.IsActive || varName == "" {
		return "", false
	}
	fn, exists := a.externOrigins[varName]
	return fn, exists
}

// ShouldSkipSurvivorFree 判断某 SOR 变量是否应抑制 kmm_v4_survivor_free 的 per-object 回收。
// 核查表缺口 ⑤（survivor_free 过早回收风险）：为以下场景提供 codegen 侧拦截点：
//   - FFI 回调把 user_data / 字符串指针长期持有在 C 侧（跨 Kaula 作用域）
//   - return promote 后 ownership 已转移，不应在 return 路径再回收
//   - 将来 sor 层面增加 #[no_kmm_drop] / #[ffi_callback_user_data] / DropAction=Leak
//     等属性时，在此接入即可，stmtgen 发射点无需二次改动。
//
// 当前基于既有决策字段的保守判定（任一成立则跳过 free）：
//  1. DropActionID != 1 (ScopeEnd)：SOR 明确说不按作用域释放（None/Hollow/移交）
//  2. AllocKindID != 1 (BumpPool)：非 survivor 分配，不走 survivor_free
//  3. EscapeLevel == "Heap" / "CrossReturn" 且 DropAction != "ScopeEnd"：所有权已外移
//  4. 变量在 externOrigins 表中：extern 返回值不由 KMM 分配，kmm_v4_survivor_free 对其
//     会触发 unowned-pointer abort，必须保守跳过。
//
// 调用方：stmtgen.go generateSurvivorFreeForScope / generateSurvivorFreeForReturn 两处。
func (a *SORCodeGenAdapter) ShouldSkipSurvivorFree(varName string, d *SORVarDecision) (skip bool, reason string) {
	if d == nil {
		return true, "no SOR decision"
	}
	if d.DropActionID != 1 {
		return true, "DropAction != ScopeEnd (" + d.DropAction + ")"
	}
	if d.AllocKindID != 1 {
		return true, "AllocKind != BumpPool (" + d.AllocKind + ")"
	}
	// extern 来源对象绝不能进入 survivor_free：绝大多数情况下 extern 返回的是
	// libc malloc / 其他分配器内存，kmm_v4_survivor_free 会判为"非 survivor 指针"
	// 走 safe no-op，但如果某个第三方库和 KMM slab 地址布局重叠就会误释放，这里强抑制。
	if fn, ok := a.IsExternOrigin(varName); ok {
		return true, "extern-origin from " + fn + " (not KMM-owned)"
	}
	// 核查表缺口 ⑤ 预留接入位：当 FFI / callback 场景需要"leak 到 C 侧长期持有"，
	// 只需 SOR/sema 层把新的 escape 级别（如 "FFIHeap"）或 annotation 映射到
	// decision.EscapeLevel / DropAction，这里 return true, "ffi callback hold" 即可。
	//
	// switch d.EscapeLevel {
	// case "FFIHeap":
	//     return true, "FFI user_data held across callbacks"
	// case "GlobalLeak":
	//     return true, "global handle intentionally leaked"
	// }
	return false, ""
}

// GetFuncParamMode 获取函数参数（按位置）的所有权模式（修复 #25）
// 返回值: 0=Owned(消费), 1=Released(只读借用), 2=Unrestricted(值), -1=未知
func (a *SORCodeGenAdapter) GetFuncParamMode(funcName string, paramIndex int) int {
	if a == nil || !a.IsActive {
		return -1
	}
	if sig, ok := a.funcSigs[funcName]; ok {
		if paramIndex >= 0 && paramIndex < len(sig.ParamModes) {
			return sig.ParamModes[paramIndex]
		}
	}
	return -1
}

// funcVarKey 生成 (函数, 变量名) 复合索引键
func funcVarKey(funcName, varName string) string {
	return funcName + "\x00" + varName
}

// SetCurrentFunc 设置当前正在生成的函数名（""=顶层/全局上下文）。
// 由 CodeGen 在进入/退出函数时调用，GetVarDecision 据此做函数级决策解析。
func (a *SORCodeGenAdapter) SetCurrentFunc(funcName string) {
	if a == nil {
		return
	}
	a.currentFunc = funcName
}

// CurrentFunc 返回当前正在生成的函数名
func (a *SORCodeGenAdapter) CurrentFunc() string {
	if a == nil {
		return ""
	}
	return a.currentFunc
}

// GetVarDecision 获取变量的 CodeGen 决策（按当前函数解析，根治 #1）
//
// 解析顺序：
//  1. (currentFunc, varName) —— 当前函数内的局部变量
//  2. ("", varName)          —— 顶层声明（全局变量），任意位置可见
//  3. nil                    —— 该变量不属于当前函数（形参、外来对象或
//     非 SOR 追踪变量），调用方必须走保守路径，不得借用其它函数的同名决策。
//
// 第 3 条是根治 #1 的关键：变量名只在词法作用域内唯一，跨函数同名是常态。
func (a *SORCodeGenAdapter) GetVarDecision(varName string) *SORVarDecision {
	if a == nil || !a.IsActive || varName == "" {
		return nil
	}
	if d, ok := a.funcVarDecisions[funcVarKey(a.currentFunc, varName)]; ok {
		return d
	}
	if d, ok := a.globalDecisions[varName]; ok {
		return d
	}
	return nil
}

// GetVarDecisionIn 按指定函数取决策（显式版本，供无法依赖当前函数上下文的调用方使用）
func (a *SORCodeGenAdapter) GetVarDecisionIn(funcName, varName string) *SORVarDecision {
	if a == nil || !a.IsActive || varName == "" {
		return nil
	}
	if d, ok := a.funcVarDecisions[funcVarKey(funcName, varName)]; ok {
		return d
	}
	return a.globalDecisions[varName]
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

// GetDropVarsForScope 返回指定作用域中需要在退出时释放 survivor 对象的变量名列表
// 方案 A：供 codegen 在作用域退出点生成 per-object survivor_free 代码
// 只返回 DropAction=ScopeEnd 且 AllocKind=BumpPool 的变量（即 promote 到 survivor 段的对象）
func (a *SORCodeGenAdapter) GetDropVarsForScope(scopeID int) []string {
	if a == nil || !a.IsActive || len(a.scopeDropPoints) == 0 {
		return nil
	}
	vars, ok := a.scopeDropPoints[scopeID]
	if !ok {
		return nil
	}
	// 过滤：只保留 promote 到 survivor 段的对象（DropActionID==1 && AllocKindID==1）
	result := make([]string, 0, len(vars))
	for _, name := range vars {
		if d := a.GetVarDecision(name); d != nil {
			if d.DropActionID == 1 && d.AllocKindID == 1 {
				result = append(result, name)
			}
		}
	}
	return result
}

// GetDropVarsForScopeByName 通过作用域名获取需释放的变量列表
// 方案 A：供 codegen 在 scope 退出点使用（codegen 用 scopeName 而非 scopeID）
func (a *SORCodeGenAdapter) GetDropVarsForScopeByName(scopeName string) []string {
	if a == nil || !a.IsActive {
		return nil
	}
	scopeID := a.GetScopeIDByName(scopeName)
	if scopeID < 0 {
		return nil
	}
	return a.GetDropVarsForScope(scopeID)
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

// IsScopeLocal 判断对象是否“只在本作用域内使用”（逃逸级别 None）。
// 满足该条件的对象可以走 kmm_v4_alloc_auto 的 per-thread TLAB 快速路径：
// 无原子操作/无锁，内存随作用域退出的 scope_pop 按作用域整段回卷回收。
// 注意 TLAB 耗尽时 alloc_auto 内部会回落到 kmm_v4_alloc_global（survivor 段），
// 这种回落对象需要作用域末尾的 per-object survivor_free 精确回收——
// 所以快速路径**不抑制** survivor_free（对 TLAB 指针它是 O(1) no-op）。
//
// 以下情况一律返回 false，保守走 kmm_v4_alloc_global 的 survivor 慢路径
// （scope_pop 不回卷，生命周期跨作用域）：
//   - 逃逸级别 Arg/Return/CrossScope/Global/Heap（对象会被外层持有）
//   - 序列化结果缺失逃逸信息（EscapeLevel 为空，无法判定）
//   - 对象来自 extern 函数返回值（非 KMM 分配，对 SOR 不透明）
func (a *SORCodeGenAdapter) IsScopeLocal(d *SORVarDecision) bool {
	if a == nil || d == nil || !d.IsSOR {
		return false
	}
	if _, extern := a.IsExternOrigin(d.VarName); extern {
		return false
	}
	return d.EscapeLevel == "None"
}

// GenerateSmartVarAlloc 生成智能内存分配代码
// 根据 SOR 的大小路由和逃逸分析结果，选择最合适的分配策略
// 修复 #15：Arena 分级已收敛到 BumpPool，统一生成 kmm_v4_alloc_auto
// 如果提供了 initValue，生成的代码会将变量初始化为该值
// initProvidesPointer 表示 initValue 本身已是指针值（分配调用/别名/取址/成员下标）：
// 此时对象已由初始化表达式创建，只声明不再分配——避免"先分配后立刻覆盖"的浪费
// （逃逸对象还会因此在 survivor 段留下一份额外占用，直到作用域末尾才回收）。
// 与结构化所有权基础层的处理保持一致。
// 修复：数组类型（含指针数组）使用 formatCVarDecl 做后缀搬运，避免非法 C 语法
func (a *SORCodeGenAdapter) GenerateSmartVarAlloc(cType, varName, indent string, initValue string, initProvidesPointer bool) string {
	if !a.IsActive {
		return a.defaultVarDecl(cType, varName, indent, initValue)
	}

	decision := a.GetVarDecision(varName)
	if decision == nil || !decision.IsSOR {
		return a.defaultVarDecl(cType, varName, indent, initValue)
	}

	// 数组类型（含指针数组如 int64_t*[2]）走栈声明路径，不做 alloc_auto
	// isPointer 仅对纯指针类型（后缀为 *）为 true，数组类型后缀为 ]
	isArray := strings.Contains(cType, "[") && strings.HasSuffix(cType, "]")
	isPointer := strings.HasSuffix(cType, "*")
	if !isPointer || isArray {
		return a.stackVarDecl(cType, varName, indent, initValue, decision.AllocKind)
	}

	baseType := strings.TrimRight(cType, "*")
	// 修复 #15：所有非栈分配统一收敛到 BumpPool
	if decision.AllocKind == "BumpPool" {
		// 大小表达式：
		//  - SOR 已知编译期精确大小（SizeBytes > 0）→ 直接用字节数
		//  - 否则回退 sizeof(T)（由 C 编译期求值）
		sizedExpr := "sizeof(" + baseType + ")"
		if decision.SizeBytes > 0 {
			sizedExpr = fmt.Sprintf("%d", decision.SizeBytes)
		}
		// 分配路径路由（不再“确认了对象大小就一律走 global 慢路径”）：
		//  ① 作用域内使用（IsScopeLocal：逃逸级别 None）→ kmm_v4_alloc_auto 快速路径：
		//     per-thread TLAB bump 分配，无原子操作，随 scope_pop 按作用域回卷回收。
		//     TLAB 耗尽时 alloc_auto 内部仍会回落到 kmm_v4_alloc_global（survivor 段），
		//     该回落对象在作用域末尾照常被 per-object survivor_free 精确回收，
		//     因此这里不需要（也不应）抑制 survivor_free。
		//  ② 逃逸对象（Arg/Return/CrossScope/Global/Heap，或逃逸信息缺失）→
		//     kmm_v4_alloc_global 慢路径：直接从 survivor slab 桶分配（任务④），
		//     scope_pop 不回卷，对象可被更长生命周期的持有者继续使用。
		//
		// 任务④：SOR 已知编译期精确大小（SizeBytes > 0）且对象逃逸时，直接从
		// survivor slab 桶分配，让 per-object survivor_free 一定命中对应 slab
		// bucket，避免 bump-large 区的无 header 不可精确回收。
		allocFn := "kmm_v4_alloc_auto"
		allocSizeExpr := sizedExpr
		allocKindTag := "BumpPool(auto, TLAB fast path, scope-local)"
		if a.IsScopeLocal(decision) {
			// TLAB 内没有 slab 桶那样的“向上取整”容错余量：SOR 字节数一旦
			// 少估（如结构体尾部 padding），相邻分配就会互相覆盖。因此快速
			// 路径优先用 C sizeof（与真实布局严格一致）；类型不可 sizeof
			// （void*/空类型）时才退回 SOR 的编译期字节数。
			bt := strings.TrimSpace(baseType)
			if bt != "" && bt != "void" {
				allocSizeExpr = "sizeof(" + bt + ")"
			}
		} else if decision.SizeBytes > 0 {
			allocFn = "kmm_v4_alloc_global"
			allocKindTag = "BumpPool(survivor-slab, sized)"
		} else {
			// 无编译期大小的逃逸对象：alloc_auto(sizeof(T)) 兜底
			//（TLAB 耗尽时内部回落到 alloc_global）
			allocKindTag = "BumpPool(auto, escaping, unsized)"
		}
		if initValue != "" {
			if initProvidesPointer {
				// 初始化表达式已创建对象（分配调用/别名/取址/成员下标）：
				// 只声明并用初始值初始化，不再重复分配。
				// 分配路径由该表达式自身决定（如 kmm_v4_alloc → TLAB、
				// kmm_v4_alloc_global → survivor），无需在声明处再选一次。
				return fmt.Sprintf("%s%s = %s; /* sor: %s (init provides object) */\n",
					indent, formatCVarDecl(cType, varName), initValue, decision.AllocKind)
			}
			// 修复 #25：指针变量初始化值直接赋给变量本身（而非 *var = init）——
			// 旧写法对 null / 堆指针初始值生成无效 C（void* → 标量赋值）
			return fmt.Sprintf("%s%s = (%s)%s(%s); /* sor: %s */\n%s%s = %s;\n",
				indent, formatCVarDecl(cType, varName), cType, allocFn, allocSizeExpr, allocKindTag,
				indent, varName, initValue)
		}
		return fmt.Sprintf("%s%s = (%s)%s(%s); /* sor: %s */\n",
			indent, formatCVarDecl(cType, varName), cType, allocFn, allocSizeExpr, allocKindTag)
	}
	return a.stackVarDecl(cType, varName, indent, initValue, decision.AllocKind)
}

// defaultVarDecl 生成不带 SOR 决策的默认变量声明
// 修复：数组类型使用 formatCVarDecl 做后缀搬运
func (a *SORCodeGenAdapter) defaultVarDecl(cType, varName, indent, initValue string) string {
	if initValue != "" {
		return fmt.Sprintf("%s%s = %s;\n", indent, formatCVarDecl(cType, varName), initValue)
	}
	return fmt.Sprintf("%s%s;\n", indent, formatCVarDecl(cType, varName))
}

// stackVarDecl 生成栈分配变量声明（含 SOR 标注）
// 修复：数组类型使用 formatCVarDecl 做后缀搬运
func (a *SORCodeGenAdapter) stackVarDecl(cType, varName, indent, initValue, allocKind string) string {
	if initValue != "" {
		return fmt.Sprintf("%s%s = %s; /* sor: %s */\n", indent, formatCVarDecl(cType, varName), initValue, allocKind)
	}
	return fmt.Sprintf("%s%s = {0}; /* sor: %s */\n", indent, formatCVarDecl(cType, varName), allocKind)
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
