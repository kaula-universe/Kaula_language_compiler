package codegen

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"kaula/internal/ast"
	"kaula/internal/astutil"
	"kaula/internal/codegen/conc"
	"kaula/internal/config"
	"kaula/internal/core"
	"kaula/internal/sema"
	"kaula/internal/stdlib"
	"kaula/internal/symbol"
	"kaula/internal/version"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// GenericInstanceCache 泛型实例缓存
type GenericInstanceCache struct {
	OriginalName   string
	TypeArguments  []string
	GeneratedCode  string
	InstantiatedAt int
}

type CodeGenerator struct {
	output          string
	indent          int
	program         *ast.Program
	templateManager *TemplateManager
	config          *config.Config
	pluginManager   *PluginManager
	stdlibConfig    *stdlib.StdlibConfig
	treeManager     *core.TreeManager
	prefixManager   *core.PrefixManager
	symbolTable     *symbol.SymbolTable
	currentScope    *symbol.SymbolTable
	// survivorStopScope：当前函数 return 路径的 per-object 回收作用域链边界。
	// = 进入当前函数前的外层作用域；generateSurvivorFreeForReturn 遍历到此停，
	// 防止嵌套函数回收外层函数存活变量（use-after-free）。
	survivorStopScope *symbol.SymbolTable
	errors            []string
	usedModules       []string
	trackedModules    map[string]bool

	// prefixBindings: 前缀展开期的 $参数绑定栈（内层前缀优先），
	// 由 generatePrefixCallBody 推入/弹出
	prefixBindings []map[string]PrefixParamBinding

	typeGenerator       *TypeGenerator
	functionGenerator   *FunctionGenerator
	expressionGenerator *ExpressionGenerator
	statementGenerator  *StatementGenerator

	// 当前正在生成的函数的静态分析结果（含别名组），
	// 供 liveness 提前释放/返回回收路径做别名感知，防止
	// 别名变量的 use-after-free 与 double-free
	currentFuncAnalysis *StaticAnalysisResult

	usedThirdPartyLibs map[string]bool
	localImportFuncs   map[string]bool
	// 本地 import 的 export 函数名（C 侧保持原名，不加 K_ 前缀）。
	// export 含 pub 语义（跨文件可见 + C 级导出），定义端以原名输出，
	// 调用端若按普通用户函数加 K_ 前缀会导致跨文件链接失败。
	localImportExportFuncs map[string]bool
	// 本地 import 的 pub/export 函数返回类型（函数名 -> Kaula 返回类型），
	// 供 inferType 对跨文件调用做类型推断（println 格式选择、concat 重载等）
	localImportReturns map[string]string
	// 本地 import 的类型定义（跨文件 class/struct 等），用于 C 类型映射与成员访问解析
	localPubTypes map[string]*sema.LocalPubTypeInfo

	genericCache        map[string]*GenericInstanceCache
	genericInstantiated map[string]bool
	genericFuncCode     strings.Builder // 泛型实例化函数代码
	genericInstDepth    int             // 当前实例化深度（防止无限递归）

	classImplCode strings.Builder // class 构造器/方法体（延迟到 forwardDecls 与全局变量之后输出）

	genericTypeCache     map[string]string // 泛型类型实例缓存：Box<int> → K_Box_int
	genericTypeCode      strings.Builder   // 泛型类型实例化代码（typedef 定义）
	genericTypeInstDepth int               // 泛型类型实例化深度（防止无限递归）

	currentFunctionName       string
	currentFunctionReturnType string

	// pendingEnumCtxType：正在生成初始化表达式的声明类型（Kaula 类型名）。
	// 供泛型 enum 构造（Result.Ok(42)）从声明上下文拿到完整类型参数：
	// `Result r = Result.Ok(42)` 里 Ok 构造应实例化为 Result<i64,i64>，
	// 而不是从字面量 42 推断出 i32（丢 Err 参数 → 生成坏 C）。
	// 声明处设置，表达式生成后清除；return 场景直接用 currentFunctionReturnType。
	pendingEnumCtxType string

	/*
	 * pendingArrayElemType：正在生成初始化表达式的数组字面量元素类型（Kaula 类型名）。
	 *
	 * 数组字面量 `[]i32 arr = [1,3,1990]` 的复合字面量元素类型必须与声明
	 * 的元素类型一致。此前 generateArrayLiteral 硬编码 `(int64_t[]){...}`，
	 * 于是 `int32_t* arr = ((int64_t[]){...})` —— 指针步长错位，
	 * arr[1] 读到的其实是第 0 个 8 字节槽的高 4 字节，静默给出错误结果。
	 *
	 * 声明处（generateRuntimeSizedArrayAlloc / 定长数组路径）按元素类型设置，
	 * 表达式生成后清除。空串表示无上下文，此时回退到 int64_t（保持旧行为）。
	 */
	pendingArrayElemType string

	/*
	 * inConcResume 表示正在生成 conc fn 的 resume 函数体。
	 *
	 * 此时 `return X` 不能生成裸的 C return —— resume 的返回类型是
	 * KaulaResumeResult，不是用户函数的返回类型。必须改写成
	 * "把 X 存进 Frame 的 result 字段，再返回 DONE"。
	 *
	 * concFrameVar 是 resume 里那个 Frame 指针的变量名（"f"）。
	 */
	inConcResume  bool
	concFrameVar  string
	concHasResult bool

	/*
	 * concIoRewrite 决定可插桩调用在 resume 里被生成成什么（I4）。
	 *
	 * 一次挂起要把调用劈成两半：
	 *   submit 段：算出实参 → 填 KaulaIoRequest → 交 IoDriver → 归还 Worker
	 *   result 段：恢复后从 kaula_conc_tls_io_result 取回结果
	 * 所以同一条语句要按两种模式各生成一次。
	 *
	 * 早期版本没做这个拆分，生成的是"原地阻塞调用 + 空转挂起"——
	 * 状态机形状对，但 Worker 真的卡在 IO 上，直接违反 I4。
	 */
	concIoRewrite concIoRewriteMode
	concIoCounter int

	// lastIoVar 是本次 lowering 里最近生成的 IO 请求变量名，
	// 供 concSuspendReturn 填进 KaulaResumeResult.io_request
	lastIoVar string

	/*
	 * ---- 复合语句内的恢复（守卫模式）----
	 *
	 * pc 只能表示「顶层语句切到哪儿」。循环体/分支体里的挂起点落在
	 * 顶层切分之外，只能整条语句重新生成一遍 —— 恢复后整个循环体
	 * 再跑一次，副作用翻倍，且不报错。
	 *
	 * 守卫模式把「体内部含挂起点」的复合语句整条只生成一次，改成给每层
	 * 开一个槽位计数器：语句逐条包进 `if (f->sub[d] == k)`，恢复时按槽位
	 * 跳过本轮已执行的语句。
	 *
	 * concGuardDepth：当前复合语句体的守卫层号；-1 = 不在守卫模式。
	 * concGuardSeq  ：本层下一个可用槽位号。
	 * concSegmentPC ：本层所在段的 pc，内联挂起要写回它。
	 */
	concGuardDepth int
	concGuardSeq   int
	concSegmentPC  int

	// concFirstRun：enter 首跑直通模式。true 时守卫生成省略 sub 槽判断
	// （spawn 时 sub 槽清零，判断必然命中），只保留进度写入与 branch 判断。
	concFirstRun bool

	// concSlotAwaits 是当前语句里已生成的可插桩调用数。
	// 一条语句含多个挂起点时，一个槽位放不下两段结果 ——
	// 那时必须报错，不能让第二个挂起点覆盖第一个的恢复位置。
	concSlotAwaits int

	// concAnalysis / concParams 供挂起返回计算「此刻活跃的变量」。
	concAnalysis *conc.BodyAnalysis
	concParams   []string

	// handleResultTypes：Handle **变量**名 → 其任务结果的 C 类型。
	// 由 `Handle h = spawn f()` 的声明处登记，表达式位置的 await 查它来解引用。
	//
	// 只登记标量句柄：isHandleVar 据此判断"这个局部量本身是 Handle"，
	// 从而在作用域出口发 kaula_handle_release(name)。容器（数组/字段）
	// 登记到这里会让它被当成标量句柄，生成 kaula_handle_release(hs) ——
	// 而 hs 是数组，类型不符。
	handleResultTypes map[string]string

	// handleContainerResultTypes：Handle **容器**名（数组变量 / 结构体字段）
	// → 其元素的 spawn 结果 C 类型。
	//
	// 与 handleResultTypes 分开是为了不混淆"标量句柄"与"装着句柄的容器"：
	// 前者要发 handle_release，后者不发，但两者的 await 都要能推出结果类型。
	handleContainerResultTypes map[string]string

	// concFuncMap：conc fn 的 C 名 → 其 C 侧签名。
	// 随 CodeGenerator 走，不做包级全局 —— 一次编译有多个 CodeGenerator
	// （每个 local import 文件一个），全局表会让文件之间互相看见对方的
	// conc fn，spawn 于是查到错误的 Frame 字段名（形参名），往错字段塞实参。
	concFuncMap map[string]*conc.Emitter

	callStack map[string]bool

	// 当前类上下文：在生成构造函数/方法体期间设置，供 exprgen 识别成员字段
	currentClassName string

	sorAdapter *SORCodeGenAdapter

	// promoteSkipNotes: "拷贝大小不可安全确定 → 跳过 promote"的诊断（非致命）
	promoteSkipNotes []string

	// tco: 尾调用改写状态（受 KMM 作用域托管的自递归函数），nil=未启用
	// 背景：clang -O2 已经会对**不涉及 KMM 作用域**的递归做 TCO/TRE，但带
	// 作用域托管的函数每层都要 scope_pop/回收，C 后端无法消除帧——递归深度
	// 受 KMM_V4_MAX_SCOPE_DEPTH 约束。这里在 Kaula 层把
	// "return f(...)" 改写成"回收本层 → 实参写回形参 → 跳回循环顶部"，
	// 复用同一个 C 栈帧，深度不再随递归增长。
	tco *tcoState
	// tcoTempSeq: 尾调用改写实参临时变量的序号（保证同一函数内多次改写不重名）
	tcoTempSeq int

	// 调用图与递归环节点集合（惰性构建一次，见 callgraph.go）：
	// 判断"函数是否处于递归环"（含互递归），供 recursion_scope_depth 诊断用
	callGraph      map[string][]string
	cycleNodes     map[string]bool
	callGraphReady bool

	// recursionScopeNotes: "递归 + 每层 KMM 作用域"的函数名（非致命诊断）
	recursionScopeNotes []string

	// 默认编译流程（未开启 --sor）的轻量逃逸分析结果（SOR EscapeResult 序列化）。
	// 键：变量名 → 逃逸级别字符串（"None"/"Arg"/"Return"/"CrossScope"/"Global"/"Heap"）。
	// 用途：结构化所有权的分配路径路由——"只在本作用域内使用"走 alloc_auto 的
	// TLAB 快速路径，逃逸对象走 alloc_global 的 survivor 段（scope_pop 不回卷）。
	escapeLevels map[string]string
	// escapeAvailable: 是否已注入逃逸分析结果（默认流程由 AnalyzeEscapeFromAST 提供）
	escapeAvailable bool

	// 修复 #25：变量声明时的堆分配常量大小登记（varName → 字节数）
	// 供跨作用域 promote 在决策无 SizeBytes、且类型为 void* 时回退使用
	sorInitSizes map[string]int

	kmmScopeDepth    int
	offsetScopeDepth int // 跟踪 offset_save/restore scope 嵌套深度，用于相邻 scope 合并优化
	tryAssignCounter int // := 错误传播赋值的临时变量计数器

	// kmmUseMark：当前函数用 base 感知的 kmm_v4_mark_save/restore 替代
	// kmm_v4_scope_push/pop。mark 落在 C 栈局部变量里，不占用
	// g_kmm_v4_scope_stack 的定长槽位，因此递归/深层嵌套不再受
	// KMM_V4_MAX_SCOPE_DEPTH（默认 1024）约束，深度只受 C 栈限制。
	// 仅对"处于递归环 + 函数级启用 KMM"的普通函数开启，语义与 scope 等价
	// （退出区间即回卷 thread heap offset 回收区间内分配）。
	kmmUseMark bool
	// kmmMarkSeq：mark 变量名序号（per-codegen 单调递增，保证嵌套/多函数不重名）
	kmmMarkSeq int
	// kmmMarkStack：当前未闭合的 KMM 区间 mark 变量名栈（函数级在最底）
	kmmMarkStack []string
	// kmmFnMark：当前函数的函数级 mark 变量名，供 return/TCO 提前退出时
	// 一次性回卷到函数入口（等价于由内向外逐层 pop 的净效果）
	kmmFnMark string

	// 方案 C：survivor 段 checkpoint/rewind 追踪
	// funcEmitsPromote: 当前函数是否发射了「向外」的 kmm_v4_scope_promote_p
	//   —— 即 promote 出的幸存副本的所有权已移交给调用方（return promote）
	//     或被调方/线程（arg/thread promote）。
	// 若为 true，则函数出口/return 不发射 survivor_rewind——
	// 否则 rewind 会把已移交出去的副本连同 survivor 段一起回卷，造成悬垂指针。
	funcEmitsPromote bool
	// funcEmitsInternalPromote: 当前函数发射的 promote 全部是「内部」promote
	//   —— promote 目标（或其容器）是本函数体内声明的局部变量、且不是形参，
	//      函数退出时这些幸存副本连同持有者一起成为垃圾。
	// 内部 promote **不**抑制 survivor_rewind：否则每轮循环 promote 一份、
	// 函数出口又不回卷，survivor 段单调增长（等同于泄漏）。
	// 与 funcEmitsPromote 分工：前者=所有权出函数（必须保活），后者=留在函数内（可回收）。
	funcEmitsInternalPromote bool
	// funcHasKMMCheckpoint: 当前函数入口是否生成了 __surv_cp checkpoint 声明
	// (仅函数级 useKMM 时生成; return 里的 survivor_rewind(__surv_cp) 引用它, 无声明时不能发射)
	funcHasKMMCheckpoint bool
	// needsStringConcat: 代码中使用了字符串拼接("a" + x), 注入内联 string 运行时
	needsStringConcat bool

	// 缺口4：整数溢出检查开关（debug 模式默认启用，release 模式禁用）
	// 启用时 +、-、* 运算用 kaula_add/sub/mul_i64/u64 内联函数包装，
	// 使用 __builtin_*_overflow 零开销检查，溢出时 abort。
	enableOverflowCheck bool

	// 修复 #25：符号声明时的 codegen 作用域栈深度（函数体=1，if/while/block=2+）
	// SOR 分析器不把 if/while 块计为作用域（scopeID 恒为 0），
	// 跨作用域 promote 判定需靠 codegen 自己的作用域栈
	scopeDepth     int
	declScopeDepth map[string]int

	// 赋值逃逸登记（KMM 回卷悬垂修复）：通过普通赋值 dst = src 把 src 所指对象
	// 的引用传给更长生命周期持有者（外层局部/全局）的源变量名集合。
	// 已逃逸变量：
	//  1. 在赋值点被 promote 到幸存段（副本长期有效）；
	//  2. 其块尾 per-object survivor_free 必须抑制——否则会释放幸存副本，
	//     使新持有者悬垂（声明还在、内容丢失）。
	assignEscapedVars map[string]bool

	// promoteHolders: **持有** promote 出来的幸存副本的变量名集合（赋值/提取的目标）。
	// 与 assignEscapedVars（标记**源**变量、用于抑制源自身的 free）互补：
	// 这里标记的是目标变量，用于 return 判定——若返回值正是某个幸存副本的持有者，
	// rewind 会回卷掉该副本，返回的指针随即悬垂，故必须抑制 rewind。
	promoteHolders map[string]bool

	// paramNames: 当前函数形参名集合。形参指向的内存属于调用方，
	// 写穿形参（obj.field = p / arr[i] = p）等于把对象交回调用方，
	// 因此形参**不是**"函数局部"——用于区分内部 promote 与向外 promote。
	paramNames map[string]bool

	// safeLocalContainers: 当前函数的「安全局部容器」集合（方案 A'）。
	//
	// 安全局部容器 = 在本函数内分配的局部变量，且**从不逃逸**：
	//   - 不作为 return 值出现；
	//   - 不作为调用实参出现；
	//   - 不作为赋值右值出现（g = cur / other.field = cur）；
	//   - 不作为 yield 值出现。
	//
	// 对这类容器，`cur.next = nd` 这类**写穿**不 promote 源，且该写穿所在的
	// 循环体放弃 mark_restore（循环级回卷）——对象留在 bump 区连续排布，
	// 函数出口 scope_pop 一锅端回收。这同时消除 survivor 段单调泄漏（写穿不再
	// 每轮 promote 一份副本）与链损坏（容器/节点不再被 bump 复用覆盖）。
	//
	// 安全性：集合外的容器一律走旧路径（promote + 回卷，靠泄漏换正确性），
	// 所以判错的最坏后果只是"该回收的没回收"，不会引入 use-after-free。
	// 逐函数重建（ResetFunctionLocalState），与 paramNames 同生命周期。
	safeLocalContainers map[string]bool

	// suppressLoopRewind: 当前正在生成的循环体内，是否出现过「写穿安全局部容器
	// 而放弃 promote」的点（方案 A'）。true 表示该循环必须放弃 mark_restore ——
	// 否则回卷后的复用分配会覆盖刚写穿的链节点。
	// 由 generateAssignEscapeStatement 打点，generateWhileStatement 消费；
	// 处理完一个循环后由调用方 ResetSuppressLoopRewind 清除。
	suppressLoopRewind bool

	// writtenThroughContainers: 当前函数内被**写穿**过的「安全局部容器」名集合
	// （obj.field = p / arr[i] = p 的目标容器，方案 A'）。
	//
	// 用途：区分「写穿 + 游标推进」与「纯游标推进」两种形态。
	//   - 纯游标推进（keep = p，无写穿）→ 走旧路径（promote + 回卷），
	//     这是 c983678 已修好的形态，必须保持；
	//   - 写穿 + 游标推进（cur.next = nd; cur = nd）→ 游标推进也**不 promote**，
	//     整条链留在 bump 区（方案 A' 的核心）。
	// 由预扫描（ScanSafeLocalContainers 的伴生输出）在函数入口一次性填好。
	writtenThroughContainers map[string]bool

	// chainLinkedVars: 被**写穿进安全局部容器**的源对象名集合（方案 A'，`cur.next = nd` 里的 nd）。
	//
	// 这些对象已经挂在链上（由容器持有），游标变量随后不再引用它们；
	// liveness 分析若按"游标的最后使用点"判定，会误以为对象已死而发射 early free
	// → 直接 use-after-free。故 generateEarlyFreeForStmtIndex 必须豁免它们。
	chainLinkedVars map[string]bool

	// chainEscapingVars: 「写穿容器的链首已随返回值离开函数」的容器名集合（方案 A'）。
	//
	// 典型 `i64 head = as<i64>(cur); ...; return head`：head 是地址值、与 cur
	// 之间没有可识别的指针别名对，逃逸传播不会把 cur 标为 assign-escaped；
	// 但 cur 指向的正是链首节点。此时若照常对 cur 发射 early free，释放的就是
	// 链首节点 → 下一轮分配回收该槽位 → 链首被覆写（n 大时才暴露）。
	// 与 chainLinkedVars 分工：那个管"挂进去的 nd"，这个管"容器 cur"。
	chainEscapingVars map[string]bool

	// escapedVars: 当前函数内逃逸过的变量名集合（含别名闭包传播）。
	//
	// 用途：写穿 promote 的外向/内部分类必须看**容器是否逃逸**，
	// 而不是只看容器是否函数局部（`Node* head = cur; return head` 中
	// cur 是函数局部但已通过别名逃逸）。见 markCrossScopePromote。
	escapedVars map[string]bool

	// 结构化所有权基础层扩展（从 SOR 下移的能力 + 多分配器 origin 追踪）：
	// externFuncNames: 程序中声明的 extern 函数名集合
	// externOriginVars: 初始值来自 extern 函数返回的变量集合（向后兼容）
	// varOrigins: 变量的分配器来源（kmm/libc_malloc/extern/unknown），用于按来源释放
	// promotedVars: 已被 promote 的变量集合
	// earlyFreedVars: 已被 early free 的变量集合
	// exportedFuncNames: export 函数名集合
	externFuncNames  map[string]bool
	externOriginVars map[string]bool
	varOrigins       map[string]string
	promotedVars     map[string]bool
	// ownedObjVars: 本作用域内由类构造函数新建的对象（`Elem e = Elem()`）。
	// 这类对象归当前作用域所有，一旦把指针交给可能长期持有的被调方
	// （vector_push_back 等 C 侧容器）就必须提升；而 elem_at()/vector_get()
	// 取回的借用指针不在此列——拷贝它们会让容器本体与副本失联。
	ownedObjVars   map[string]bool
	earlyFreedVars map[string]bool
	userFreedVars  map[string]bool // 用户显式调用 *_free 释放的指针
	// initializerSuppliedVars：由 conc 守卫降级路径登记——该变量的初始化式
	// 已单独生成（放入恢复槽位），声明本体不得再发射缺省堆分配。
	// 见 KBUG_KMM_FREE_NOOP_SURVIVOR_LEAK / conc_guard.go:concGuardedDecl。
	initializerSuppliedVars map[string]bool
	exportedFuncNames       map[string]bool

	sourceMap  *SourceMap
	sourceFile string

	retHoistSeq       int // F17: return 表达式求值提升的临时变量序号
	lambdaCounter     int
	lambdaDefinitions []string

	// 编译期常量表：const 变量名 → 求值后的字面量
	// 用于在 codegen 阶段将 const 引用内联为字面量，实现编译期常量求值
	constTable map[string]string

	// 数组变量名 → 元素个数（spend 强制消费流的代码生成依据）
	arrayLens map[string]int

	// spend 语句包装器命名计数器
	spendCounter int

	// 动态对象运行时（std/obj/dynobj）是否需要注入头文件和 std 模块
	needsObjRuntime bool

	// 动态对象字面量计数器（独立于 lambda 计数器，避免变量名冲突）
	objectLiteralCounter int

	// 对象字面量静态变量声明（文件作用域）
	objectDeclCode strings.Builder

	// 对象字面量初始化代码（注入 main 函数体内）
	preludeInitCode strings.Builder

	// 嵌套函数捕获信息：函数名 → 捕获列表（生成函数体与调用点共用）
	nestedFuncCaptures map[string]nestedFuncInfo

	// 当前正在生成函数体的捕获上下文（identifier 重写为 (*_cap_N)）
	currentCaptures   []string
	currentCaptureSet map[string]int

	// 嵌套函数延迟定义队列：函数体生成时收集，定义提升到所在外层函数结束处输出
	// （C 语言不允许函数内嵌定义，clang 拒绝 GNU nested functions）
	pendingNestedFuncs   []string
	nestedFuncPrototypes strings.Builder
	inFunctionBody       bool

	// userFuncNames: 本文件全部用户自定义 fn 的名字集合（含函数体内嵌套 fn）。
	// 这些符号在 C 侧统一加 K_ 前缀，彻底避开 C 保留字（int/double/...）与 C 库符号冲突。
	// main 是 C 入口、extern/stdlib/第三方函数不在此集合，均保持原名。
	userFuncNames map[string]bool

	// 任务②：深提升回调（kmm_v4_scope_promote_p_deep）的合成与去重
	// deepPromoteCBEmitted: kaula 类型名 -> true（已经合成过，避免重复）
	// deepPromoteCBCode: 合成的 static C 函数体（文件作用域，在 typedef 之后、函数代码之前输出）
	deepPromoteCBEmitted map[string]struct{}
	deepPromoteCBCode    strings.Builder
	// deepPromoteCBBlock: 组装好的 cb 代码块（注释 + 全部 static cb 定义），
	// 由 Generate 尾部在 classImplCode 之前输出（修复先使用后定义问题）
	deepPromoteCBBlock string

	// 高级属性支持：#[test]、#[derive]、#[command]
	TestMode         bool // 测试模式：生成测试 runner 而非普通 main
	testRunnerActive bool // 测试模式且确有 #[test] 函数（runner 已生成）
	attributeFeat    *AttributeFeatures

	// TU 拆分模式（见 config.Config.TUMode）。空串 = 单 TU，行为与历史完全一致。
	//
	// 本文件是"本 TU 自己那句"的生成器；跨 TU 可见性（前置段、泛型实例归属）
	// 由入口侧统一决策后经 config 注入，codegen 只负责按决策输出。
	tuMode string
	// tuPreludeOrigin 本 TU 贡献给前置段的 type_code（仅定义者 TU 非空）
	tuPreludeOrigin string
	// tuGenericsDeferred 本 TU 实例化、但归属别的 TU 的泛型实例（只出原型，由归属者出定义）
	tuGenericsDeferred []deferredGeneric
	// tuGenericsLocalDefined 本 TU 实例化且由本 TU 定义的泛型实例名（供入口侧汇总归属）
	tuGenericsLocalDefined []string
	// tuClassStatics 本 TU 输出过的文件作用域 static 函数名（.h 中据此补原型）
	tuClassStatics []string
	tuSeenStatics  map[string]bool
	// tuExportVars export 全局变量的 C 声明文本（.h 中供其它 TU extern 引用）
	tuExportVars []string
	// tuExportFuncProtos 对外可见函数（pub / export）的 C 原型，进 header
	// 供其它 TU 调用。
	//
	// 单 TU 时代这份原型就在主 C 的前置声明段里，跨文件调用天然可见；
	// 拆 TU 后调用方看不到定义方的前置声明，C99 起又不允许隐式声明，
	// 所以必须经由 header 传出去。裸本地函数（非 pub/export）不放进来：
	// 它们跨文件本就不可调用，多放只会让 header 越来越像第二个全局命名空间。
	tuExportFuncProtos []string
}

// deferredGeneric 一个"在别处定义"的泛型实例：只需要原型。
type deferredGeneric struct {
	CName string // C 函数名（MangleGenericName 结果）
	Prot  string // 原型文本（含换行）
}

// TUArtifacts 单个 TU 的产物：C 源码 + 供其它 TU 共享的接口信息。
//
// TL 拆分后，定义者 TU 需要把"别的 TU 可能引用的东西"暴露出去：
//   - Prelude：跨文件类型定义（typedef/struct/enum），必须对所有 TU 可见；
//   - Header：非 static 函数原型、class 构造函数/方法原型、deferred 泛型原型，
//     以名字集合的 include guard 包裹，供调用方 TU 显式包含；
//   - GenericDefs：本 TU 实际定义的泛型实例名（入口据此做唯一归属）。
type TUArtifacts struct {
	Source      string
	Prelude     string
	Header      string
	GenericDefs []string
	UsedModules []string
}

// TUArtifacts 返回本次生成在 TU 拆分模式下需要共享出去的接口信息。
func (cg *CodeGenerator) TUArtifacts() TUArtifacts {
	return TUArtifacts{
		Source:      cg.output,
		Prelude:     cg.tuPreludeOrigin,
		Header:      cg.buildTUHeader(),
		GenericDefs: append([]string(nil), cg.tuGenericsLocalDefined...),
		UsedModules: append([]string(nil), cg.usedModules...),
	}
}

// TUHeaderGuard 生成确定性的 include guard（同名 .h 只被包含一次），
// 同一组名字在任意进程/任意顺序下都必须得到同一个 guard。
func TUHeaderGuard(key string) string {
	h := sha256.Sum256([]byte(key))
	return "KAULA_TU_" + hex.EncodeToString(h[:8])
}

// registerTUStatic 登记本 TU 输出的文件作用域 static 函数的签名（不含右括号与函数体），
// 由 buildTUHeader 补成完整原型，供其它 TU 在**自己的 TU 内**补一份声明。
//
// 为什么需要它：类方法/构造器是 `static inline`（内部链接），符号不会出现在
// 定义者 .o 的外部符号表里。调用方 TU 若用到了某个库类的方法，就必须在
// 自己的 TU 内看到该 static 函数的原型（C99 起没有隐式声明），而单 TU 时代
// 这是天然成立的（同一个文件里定义在前、调用在后）。
func (cg *CodeGenerator) registerTUStatic(sig string) {
	if cg.tuMode == "" || sig == "" {
		return
	}
	if cg.tuSeenStatics == nil {
		cg.tuSeenStatics = make(map[string]bool)
	}
	if cg.tuSeenStatics[sig] {
		return
	}
	cg.tuSeenStatics[sig] = true
	cg.tuClassStatics = append(cg.tuClassStatics, sig+");")
}

// collectTUPrelude 把本 TU 的类型定义登记为"跨 TU 前置段"。
//
// 单 TU 时代所有类型定义在同一份 C 里，顺序是"类型 → 原型 → 函数体"；
// 拆成多 TU 后，主 TU 可能引用库 TU 里定义的 class/struct（跨文件 pub 类型），
// 此时主 TU 自己也要看到该类型定义。做法：定义者把这些定义同时输出一份到
// prelude，入口把所有 prelude 前置到每个 TU 的 includes 之后。
//
// 幂等性由每段代码自己的 include guard 保证：同一个类型的定义会在多处出现
// （定义者 TU 自己的 typeCode、所有 TU 共享的 prelude），C 不允许 typedef
// 重定义 —— 哪怕两份内容逐字相同。这里给每段加一个按内容哈希命名的 guard，
// 使"包含两次"退化为空操作。
//
// 注意 guard 必须基于**内容**而不是类型名：同名类型在两个 TU 里可能是两个
// 不同的类型（两个库各自定义 struct A），按名字去重会把它们错误地合并。
func (cg *CodeGenerator) collectTUPrelude(code string) {
	if cg.tuMode == "" || code == "" {
		return
	}
	cg.tuPreludeOrigin += cg.guardTUType(code)
}

// guardTUType 给一段类型定义套上按内容哈希命名的 include guard（单 TU 模式下原样返回）。
//
// 同一个类型会同时出现在两处：定义者 TU 的 typeCode，以及所有 TU 共享的
// prelude。C 不允许 typedef 重定义（即使内容逐字相同），而 prelude 是前置给
// **每个** TU 的 —— 包括定义者自己。所以定义者的 typeCode 也必须带同一个
// guard，两者才能在同一次预处理里只留一份。
func (cg *CodeGenerator) guardTUType(code string) string {
	if cg.tuMode == "" || code == "" {
		return code
	}
	g := TUHeaderGuard(code)
	return "#ifndef " + g + "\n#define " + g + "\n" + code + "#endif\n"
}

// buildTUHeader 生成供其它 TU 包含的接口头：
//
//	fn 原型 + class 构造/方法原型 + 变量 extern 声明 + 泛型实例原型
//
// 为什么需要原型而不是"包含整个 C 文件"：C99 起不再允许隐式函数声明，
// 调用出现在定义之前的 TU 必须能看到原型；而类方法/构造器是 static inline，
// 原型与定义同 TU 即可（定义者 TU 已经在 classImplCode 之前输出原型），
// 这里只为"外部调用"的场景补上。
func (cg *CodeGenerator) buildTUHeader() string {
	var b strings.Builder
	seen := make(map[string]bool)
	write := func(line string) {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			return
		}
		seen[line] = true
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for _, f := range cg.tuExportFuncProtos {
		write(f)
	}
	for _, v := range cg.tuExportVars {
		write(v)
	}
	for _, g := range cg.tuGenericsDeferred {
		write(g.Prot)
	}
	for _, s := range cg.tuClassStatics {
		write(s)
	}
	return b.String()
}

// EnsureDeepPromoteCB 为给定 kaula class/struct 类型合成类型化深提升回调。
// 仅对需要 promote 的聚合类型（class 声明中可能含 * 字段）合成；每个类型只合成一次。
// 返回 C 回调名（如 __kaula_deeppromote_Node），未实现的聚合类型 fallback 返回 ""（仍走浅 promote）。
//
// Bug 2 正解（任务②）：SOR 的跨作用域 promote 对含指针字段的对象需要把每个 * 字段
// 都递归提升并改写为 survivor 段新地址。此函数为每个类生成静态 C 函数：
//
//	static int __kaula_deeppromote_<T>(void* promoted_obj) {
//	    K_<T>* self = (K_<T>*)promoted_obj;
//	    self->ptr_field = kmm_v4_promote_deep(self->ptr_field, sizeof(*self->ptr_field), cb);
//	    ... （每个指针字段/字符串字段递归提升）
//	    return 0;
//	}
func (cg *CodeGenerator) EnsureDeepPromoteCB(kaulaType string) string {
	if kaulaType == "" {
		return ""
	}
	if cg.typeGenerator == nil || !cg.typeGenerator.classTypes[kaulaType] {
		return "" // 非 class/struct：不需要 cb
	}
	if _, ok := cg.deepPromoteCBEmitted[kaulaType]; ok {
		return "__kaula_deeppromote_" + kaulaType // 已合成过，直接复用
	}
	// 修复：递归类型（A 含字段 *B，B 含字段 *A / 自引用）在字段处理中会重入
	// EnsureDeepPromoteCB，如果等到 body 合成完再登记 deepPromoteCBEmitted，就会
	// 无限递归挂死。这里先"占位"登记，使重入路径能立即返回同名 cb（C 静态函数
	// 允许前向声明/先使用后定义——但当前实现是直接写函数体，若子引用字段在 body
	// 合成时需要父 cb 名，返回相同字符串只是在 C 表达式里被当作函数指针，没有
	// 依赖字段信息，所以是安全的）。
	cg.deepPromoteCBEmitted[kaulaType] = struct{}{}
	fields := cg.typeGenerator.getClassFields(kaulaType)
	if len(fields) == 0 {
		// 无字段的空结构体：cb 直接返回 0（浅拷贝完全等价于深拷贝）
		cb := fmt.Sprintf(
			"/* SOR deep-promote callback for '%s' (no pointer fields, no-op) */\n"+
				"static int __kaula_deeppromote_%s(void* promoted_obj) {\n"+
				"    (void)promoted_obj;\n"+
				"    return 0;\n"+
				"}\n\n",
			kaulaType, kaulaType)
		cg.deepPromoteCBCode.WriteString(cb)
		cg.deepPromoteCBEmitted[kaulaType] = struct{}{}
		return "__kaula_deeppromote_" + kaulaType
	}

	cTagName := kaulaType // class 在 C 中 typedef 为 struct <Name>（typedef 名 <Name>）

	var body strings.Builder
	body.WriteString(fmt.Sprintf(
		"/* SOR deep-promote callback for '%s' (Bug 2 fix):\n"+
			"   对每个指针字段递归提升，把内部指针改写为 survivor 段地址。 */\n"+
			"static int __kaula_deeppromote_%s(void* promoted_obj) {\n"+
			"    %s* self = (%s*)promoted_obj;\n",
		kaulaType, kaulaType, cTagName, cTagName))

	hasPtrField := false
	for _, f := range fields {
		if f == nil || f.Name == "" {
			continue
		}
		fcType := cg.typeGenerator.convertType(f.Type, f.Nullable)
		// Bug 2 隐患②修复：指针字段不再一律传 NULL cb。
		// 如果 pointeeType 本身就是 class/聚合类型，递归 EnsureDeepPromoteCB
		// 合成 pointee 的深提升 cb 传进去，确保多级嵌套指针递归提升。
		// POD pointee（基础类型、非 class）保持 NULL，避免无意义回调调用。
		if fcType != "" && strings.HasSuffix(fcType, "*") && fcType != "void*" {
			hasPtrField = true
			pointeeType := strings.TrimRight(fcType, "*")
			fieldCB := "NULL"
			if cg.typeGenerator != nil && cg.typeGenerator.classTypes[f.Type] {
				// f.Type 是原始 Kaula 类型名（class 注册名）；
				// 如果它是 class，则 pointeeType 对应的就是该 class 的 C 结构。
				if subCB := cg.EnsureDeepPromoteCB(f.Type); subCB != "" {
					fieldCB = subCB
				} else {
					// 理论上不可达：classTypes[f.Type] 为 true 的 EnsureDeepPromoteCB
					// 一定返回非空（fields==0 也返回 no-op cb）。但仍硬 guard：
					// 强制在 C 编译期报错，避免 NULL cb 退化为浅提升漏过 Bug 2。
					msg := fmt.Sprintf(
						"字段 %q 类型 %q 是 class 但无法合成子 cb（多级嵌套指针将悬垂）",
						f.Name, f.Type)
					cg.error("[SOR deep-promote] " + msg)
					body.WriteString(fmt.Sprintf(
						"    #error SOR_SUB_CB_MISSING: owner=%s field=%s pointee_type=%s /* audit: Bug 2 nested */\n",
						kaulaType, f.Name, f.Type))
				}
			}
			// 字段本身是子指针：提升 pointee，把字段值改写为 survivor 段新地址。
			// 说明：原生指针字段（如 f64* / []f64，fieldCB == NULL）的 pointee 字节数
			// SOR 无法静态推断（可能指向 nin*nout 个元素的数组），按 sizeof(元素)
			// 提升会把数组截断为单元素（提升后越界）。这是类型系统缺少长度信息的
			// 已知限制：定长数组字段请用 [N]T 声明（值语义 memcpy 完整包含），
			// 不定长指针字段若作为逃逸对象的内部数组，需要库侧用全局分配管理。
			// 保持提升（保证指向作用域内存的字段不悬空），并对不确定大小的
			// 原生指针字段输出编译警告，提示潜在截断。
			body.WriteString(fmt.Sprintf(
				"    if (self->%s != NULL) {\n"+
					"        void* _field_new = kmm_v4_promote_deep(self->%s, sizeof(%s), %s);\n"+
					"        if (_field_new == NULL) return -1;\n"+
					"        self->%s = (%s)_field_new;\n"+
					"    }\n",
				f.Name, f.Name, pointeeType, fieldCB, f.Name, fcType))
			if fieldCB == "NULL" {
				fmt.Printf("[kaulac] WARN: deep-promote of native pointer field '%s.%s' (%s) uses sizeof(element); arrays will be truncated - declare as fixed-size [N]T or allocate globally\n",
					kaulaType, f.Name, f.Type)
			}
		} else if f.Type == "string" || f.Type == "str" || f.Type == "String" || fcType == "String" {
			// 任务①联动：String 字段的载荷单独分配，浅拷贝提升后 ptr 仍指向作用域段。
			// 需要把 String.ptr 重新提升并改写字段的 {len, ptr}。
			hasPtrField = true
			body.WriteString(fmt.Sprintf(
				"    /* String field '%s': re-clone payload into survivor via global allocator */\n"+
					"    {\n"+
					"        String _sclone = string_clone_global(self->%s);\n"+
					"        if (_sclone.ptr == NULL && self->%s.len != 0) return -1;\n"+
					"        self->%s = _sclone;\n"+
					"    }\n",
				f.Name, f.Name, f.Name, f.Name))
		} else if fcType == "void*" {
			// opaque void* 字段：只提升外壳（sizeof(void*) 实际是指针本身，
			// 指向内容 SOR 不可知——保守按指向的字节无法提升，这里仅记 no-op）
			// 注：如果 void* 实际是已知类型，调用方应该显式 cast；
			// SOR 对 opaque 提升保持浅拷贝，与现有口径一致。
			hasPtrField = true
			body.WriteString(fmt.Sprintf(
				"    /* void* field '%s' is opaque to SOR; leaving shallow-copy */\n",
				f.Name))
		} else if cg.typeGenerator != nil && cg.typeGenerator.classTypes[f.Type] {
			// 内嵌聚合类型（非指针 class 字段，值语义）：外层 promote_deep 已经 memcpy
			// 了整个结构体（包括内嵌值字段的字节内容），因此**不能**再调用
			// kmm_v4_scope_promote_p_deep：后者对 &(self->f) 转 (void**) 会把值字段
			// 的起始若干字节（刚好等于 sizeof(void*)）当成指针再分配一遍，会把
			// 第一个指针字段错误地覆盖为新的 dangling 地址。
			// 正确语义：直接对子结构体（已在 survivor 段）执行其深提升 cb，
			// 仅递归处理其内部指针字段。
			hasPtrField = true
			innerCB := cg.EnsureDeepPromoteCB(f.Type)
			if innerCB == "" {
				// 与隐患①保持一致：classTypes[f.Type] 为真却无 cb → 内部不一致，
				// 绝不静默漏过。
				msg := fmt.Sprintf(
					"内嵌 class 字段 %q 类型 %q 的深提升子 cb 合成失败", f.Name, f.Type)
				cg.error("[SOR deep-promote] " + msg)
				body.WriteString(fmt.Sprintf(
					"    #error SOR_INNER_CB_MISSING: owner=%s field=%s type=%s /* audit: Bug 2 inline */\n",
					kaulaType, f.Name, f.Type))
				body.WriteString("    return -1;\n")
				continue
			}
			body.WriteString(fmt.Sprintf(
				"    /* inline class field '%s' (%s): already memcpy'd on promote; only re-run its cb on the address */\n"+
					"    if (%s(&(self->%s)) != 0) return -1;\n",
				f.Name, f.Type, innerCB, f.Name))
		}
	}

	if !hasPtrField {
		// 字段全部是 POD：无递归提升，cb 为 no-op（浅拷贝 == 深拷贝）
		body.WriteString("    /* all POD fields; no recursive promotion needed */\n")
	}
	body.WriteString("    return 0;\n}\n\n")

	cg.deepPromoteCBCode.WriteString(body.String())
	cg.deepPromoteCBEmitted[kaulaType] = struct{}{}
	return "__kaula_deeppromote_" + kaulaType
}

// AddObjectDecl 添加动态对象字面量的静态变量声明（文件作用域）
func (cg *CodeGenerator) AddObjectDecl(code string) {
	cg.objectDeclCode.WriteString(code)
}

// AddPreludeInit 添加动态对象字面量的初始化代码（注入 main 函数体内）
func (cg *CodeGenerator) AddPreludeInit(code string) {
	cg.preludeInitCode.WriteString(code)
}

func (cg *CodeGenerator) error(message string) {
	cg.errors = append(cg.errors, message)
}

// AddPromoteSkipNote 记录一条"因拷贝大小不可安全确定而跳过 promote"的诊断。
// 非致命（不阻断编译）：跳过优于拷贝错误长度。供报告层输出，
// 避免用户面对静默的借用语义一头雾水。
func (cg *CodeGenerator) AddPromoteSkipNote(message string) {
	cg.promoteSkipNotes = append(cg.promoteSkipNotes, message)
}

// PromoteSkipNotes 返回 promote 跳过诊断列表
func (cg *CodeGenerator) PromoteSkipNotes() []string {
	return cg.promoteSkipNotes
}

// HandleResultType 查询 Handle 变量的结果 C 类型（登记表读）。
// 未登记返回 ""：该变量不被识别为 Handle（不 release、await 走错误路径）。
func (cg *CodeGenerator) HandleResultType(varName string) string {
	if cg.handleResultTypes == nil {
		return ""
	}
	return cg.handleResultTypes[varName]
}

// RegisterHandleResultType 登记 Handle 变量的结果 C 类型（登记表写）。
// 除 `Handle x = spawn f()` 的声明处（noteHandleResultType）外，
// Handle 复制路径（`Handle y = x`）也调用：传播源句柄的结果类型，
// 否则 y 不被识别为 Handle —— 作用域出口漏发 release（pin 晚还），
// 表达式位置 y.await 还会走 generateAwaitExpression 的错误分支
// （生成悬垂读，答案错但不崩）。
func (cg *CodeGenerator) RegisterHandleResultType(varName, cType string) {
	if cg.handleResultTypes == nil {
		cg.handleResultTypes = make(map[string]string)
	}
	cg.handleResultTypes[varName] = cType
}

// tcoState 是"每函数"的尾调用改写状态：循环标签、形参名（实参回写用）、
// 以及登记为可改写的 self-call 节点集合。
type tcoState struct {
	label  string
	params []string
	sites  map[*ast.CallExpression]bool
	// converted: 实际完成改写的尾调用点数（有些候选点会在生成期因实参
	// 引用本帧会被回收的对象而退回真实调用，用于判断是否还有深度受限的递归）
	converted int
}

// MarkTcoConverted 记录一次实际完成的尾调用改写
func (t *tcoState) MarkTcoConverted() {
	if t != nil {
		t.converted++
	}
}

// SetTCO 启用当前函数的尾调用改写（受 KMM 作用域托管的自递归函数）。
// label 是函数内循环顶部的标签名，params 是形参名，sites 是可改写的 self-call。
// 生成嵌套函数前应先 TakeTCO 取出并清空，避免内层函数误用外层状态。
func (cg *CodeGenerator) SetTCO(label string, params []string, sites map[*ast.CallExpression]bool) {
	cg.tco = &tcoState{label: label, params: params, sites: sites}
}

// TakeTCO 取出并清空当前尾调用改写状态（用于保存/恢复，避免嵌套函数继承）
func (cg *CodeGenerator) TakeTCO() *tcoState {
	prev := cg.tco
	cg.tco = nil
	return prev
}

// RestoreTCO 恢复先前取出的尾调用改写状态
func (cg *CodeGenerator) RestoreTCO(prev *tcoState) {
	cg.tco = prev
}

// TcoState 返回当前函数的尾调用改写状态（nil=未启用）
func (cg *CodeGenerator) TcoState() *tcoState { return cg.tco }

// IsTcoTailSite 判断该 self-call 是否应改写为尾跳转
func (cg *CodeGenerator) IsTcoTailSite(call *ast.CallExpression) bool {
	return cg.tco != nil && call != nil && cg.tco.sites[call]
}

// AddRecursionScopeNote 记录"递归 + 每层 KMM 作用域"的诊断信息。
// 这类函数每层 push 一个 KMM 作用域，嵌套深度受 KMM_V4_MAX_SCOPE_DEPTH
// 约束（默认 1024），超出即运行期 FATAL abort 且不可捕获——属于设计约束，
// 提前告知用户可选方案（#[no_kmm] / 放宽上限 / 改写成循环）。
func (cg *CodeGenerator) AddRecursionScopeNote(funcName string) {
	for _, n := range cg.recursionScopeNotes {
		if n == funcName {
			return // 去重
		}
	}
	cg.recursionScopeNotes = append(cg.recursionScopeNotes, funcName)
}

// RecursionScopeNotes 返回"递归 + KMM 作用域"的函数名列表
func (cg *CodeGenerator) RecursionScopeNotes() []string {
	return cg.recursionScopeNotes
}

func (cg *CodeGenerator) Errors() []string {
	return cg.errors
}

func (cg *CodeGenerator) HasErrors() bool {
	return len(cg.errors) > 0
}

func (cg *CodeGenerator) SetStdlibConfig(cfg *stdlib.StdlibConfig) {
	cg.stdlibConfig = cfg
}

// SetSORResult 设置 SOR 分析结果，供代码生成阶段使用
func (cg *CodeGenerator) SetSORResult(result map[string]interface{}) {
	cg.sorAdapter = NewSORCodeGenAdapter(result)
}

// GetSORAdapter 获取 SOR CodeGen 适配器
func (cg *CodeGenerator) GetSORAdapter() *SORCodeGenAdapter {
	return cg.sorAdapter
}

// SetEscapeAnalysis 注入轻量逃逸分析结果（默认编译流程：未开启 --sor 时使用）。
// result 键位与 sor.SerializeEscapeResult 一致：escape / extern_origins。
// 注意：这只提供逃逸信息，不激活 SOR 决策（所有权检查/内存决策等仍仅在 --sor 下运行）。
func (cg *CodeGenerator) SetEscapeAnalysis(result map[string]interface{}) {
	if result == nil {
		return
	}
	escape, ok := result["escape"].(map[string]interface{})
	if !ok {
		return
	}
	if cg.escapeLevels == nil {
		cg.escapeLevels = make(map[string]string, len(escape))
	}
	for name, levelVal := range escape {
		if level, ok := levelVal.(string); ok {
			cg.escapeLevels[name] = level
		}
	}
	cg.escapeAvailable = len(cg.escapeLevels) > 0
}

// HasEscapeAnalysis 报告是否已注入逃逸分析结果
func (cg *CodeGenerator) HasEscapeAnalysis() bool {
	return cg.escapeAvailable
}

// GetEscapeLevel 查询变量的逃逸级别。第二个返回值 ok=false 表示逃逸分析
// 未覆盖该变量（未知），调用方应按原路径处理，不得据此改写分配策略。
func (cg *CodeGenerator) GetEscapeLevel(varName string) (level string, ok bool) {
	if cg == nil || varName == "" || !cg.escapeAvailable {
		return "", false
	}
	level, ok = cg.escapeLevels[varName]
	return level, ok
}

// IsVarScopeLocal 判断变量是否"只在本作用域内使用"（逃逸级别 None）。
// ok=false 表示逃逸分析未覆盖该变量——保守起见调用方应视为可能逃逸。
func (cg *CodeGenerator) IsVarScopeLocal(varName string) (scopeLocal bool, ok bool) {
	level, ok := cg.GetEscapeLevel(varName)
	if !ok {
		return false, false
	}
	return level == "None", true
}

// IsInKMMScope 当前是否在 KMM 作用域内
func (cg *CodeGenerator) IsInKMMScope() bool {
	return cg.kmmScopeDepth > 0
}

// GetSourceMap 获取源代码映射
func (cg *CodeGenerator) GetSourceMap() *SourceMap {
	return cg.sourceMap
}

// generateGlobalVarDeclString 生成 C 变量声明（"类型 名字"），
// 固定大小数组类型 [N]elem → "elem name[N]"
func (cg *CodeGenerator) generateGlobalVarDeclString(cType, varName string) string {
	openBracket := strings.Index(cType, "[")
	if openBracket > 0 && strings.HasSuffix(cType, "]") {
		baseType := cType[:openBracket]
		arrayPart := cType[openBracket:]
		return baseType + " " + varName + arrayPart
	}
	return cType + " " + varName
}

// SetSourceFile 设置源文件名
func (cg *CodeGenerator) SetSourceFile(filename string) {
	cg.sourceFile = filename
}

// EnterKMMScope 进入 KMM 作用域
func (cg *CodeGenerator) EnterKMMScope() {
	cg.kmmScopeDepth++
}

// ExitKMMScope 退出 KMM 作用域
func (cg *CodeGenerator) ExitKMMScope() {
	if cg.kmmScopeDepth > 0 {
		cg.kmmScopeDepth--
	}
}

// SetKMMUseMark 设置当前函数是否使用 mark 回收替代 scope push/pop。
// 每个函数生成入口都必须显式设置（true/false），避免嵌套函数继承外层模式。
func (cg *CodeGenerator) SetKMMUseMark(v bool) {
	cg.kmmUseMark = v
	cg.kmmMarkStack = nil
	// 函数级 mark 用固定名：函数体（含 return / 尾调用）是在函数级 start
	// 发射**之前**生成的，提前退出路径必须此刻就能拿到这个名字，否则会
	// 生成不回卷的 return（每层泄漏一层分配）。C 函数作用域互相独立，
	// 固定名不会冲突。
	if v {
		cg.kmmFnMark = "__kmm_fnmark"
	} else {
		cg.kmmFnMark = ""
	}
}

// KMMUseMark 当前函数是否处于 mark 回收模式
func (cg *CodeGenerator) KMMUseMark() bool {
	return cg.kmmUseMark
}

// KMMMarkName 分配一个新的 mark 变量名并压栈（mark 模式下使用）
func (cg *CodeGenerator) KMMMarkName(prefix string) string {
	cg.kmmMarkSeq++
	name := fmt.Sprintf("__kmm_%s%d", prefix, cg.kmmMarkSeq)
	cg.kmmMarkStack = append(cg.kmmMarkStack, name)
	return name
}

// KMMStart 生成 KMM 区间起始代码（mark 模式产出 mark save + 裸块，
// 否则产出 KMM_V4_SCOPE_START/NAMED + do 块）。owner 非空表示函数级命名作用域。
func (cg *CodeGenerator) KMMStart(owner string) string {
	// 块级（owner == ""）只有在函数级已经开出 mark（kmmFnMark 非空）时才跟随
	// mark 模式。否则（如闭包/嵌套函数体这类没有函数级包裹的路径）退回
	// scope 模式，避免出现"块级 mark 却没有函数级 mark"的半吊子状态。
	if cg.kmmUseMark && (owner != "" || cg.kmmFnMark != "") {
		var name string
		if owner != "" {
			// 复用 SetKMMUseMark 预置的固定名（return/TCO 路径已引用它）
			if cg.kmmFnMark == "" {
				cg.kmmFnMark = "__kmm_fnmark"
			}
			name = cg.kmmFnMark
			cg.kmmMarkStack = append(cg.kmmMarkStack, name)
		} else {
			name = cg.KMMMarkName("mark")
		}
		return "kmm_v4_mark_t " + name + " = kmm_v4_mark_save(); {\n"
	}
	if owner != "" {
		return "KMM_V4_SCOPE_NAMED(\"" + owner + "\") {\n"
	}
	return "KMM_V4_SCOPE_START {\n"
}

// KMMEnd 生成 KMM 区间结束代码（与 KMMStart 配对，mark 模式回卷到栈顶 mark）
func (cg *CodeGenerator) KMMEnd() string {
	if cg.kmmUseMark && len(cg.kmmMarkStack) > 0 {
		name := cg.kmmMarkStack[len(cg.kmmMarkStack)-1]
		cg.kmmMarkStack = cg.kmmMarkStack[:len(cg.kmmMarkStack)-1]
		return "} kmm_v4_mark_restore(" + name + ");\n"
	}
	return "} KMM_V4_SCOPE_END;\n"
}

// KMMExitToFunctionMark return / 尾调用等提前退出路径的区间回卷代码。
// 非 mark 模式沿用"按 kmmScopeDepth 逐层 pop"；mark 模式只需一次回卷到
// 函数入口 mark——由内向外逐层回卷的净效果就是把 offset 恢复到函数入口。
func (cg *CodeGenerator) KMMExitToFunctionMark() string {
	if cg.kmmUseMark {
		if cg.kmmFnMark != "" {
			return "kmm_v4_mark_restore(" + cg.kmmFnMark + ");\n"
		}
		return ""
	}
	var b strings.Builder
	for i := 0; i < cg.kmmScopeDepth; i++ {
		b.WriteString("kmm_v4_scope_pop();\n")
	}
	return b.String()
}

// KMMNeedsExitPop 提前退出路径是否需要发射区间回卷代码
func (cg *CodeGenerator) KMMNeedsExitPop() bool {
	if cg.kmmUseMark {
		return cg.kmmFnMark != ""
	}
	return cg.kmmScopeDepth > 0
}

// KMMExitPopLines 提前退出（return / 尾调用）路径的区间回卷代码，每行已带缩进。
func (cg *CodeGenerator) KMMExitPopLines(indent string) string {
	if !cg.KMMNeedsExitPop() {
		return ""
	}
	code := strings.TrimRight(cg.KMMExitToFunctionMark(), "\n")
	if code == "" {
		return ""
	}
	var b strings.Builder
	for _, line := range strings.Split(code, "\n") {
		b.WriteString(indent)
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// PropagateInitSize 在 yield/extract 转移时把源对象的大小登记传播给目标变量
// （修复 #25：目标变量自身初始化可能是 null，但转移来的对象大小来自源声明）
func (cg *CodeGenerator) PropagateInitSize(dst, src string) {
	if dst == "" || src == "" {
		return
	}
	if n, ok := cg.sorInitSizes[src]; ok {
		cg.sorInitSizes[dst] = n
	}
}

func (cg *CodeGenerator) ExtractMallocSizeBytesFromAST(call *ast.CallExpression) int {
	if call == nil || len(call.Args) == 0 {
		return 0
	}
	arg := call.Args[0]
	if lit, ok := arg.(*ast.IntegerLiteral); ok {
		return int(lit.Value)
	}
	if sizeOf, ok := arg.(*ast.SizeOfExpression); ok {
		if size, ok := cg.typeGenerator.GetTypeSize(sizeOf.TargetType); ok {
			return size
		}
	}
	return 0
}

// RecordDeclScopeDepth 在变量声明时记录其作用域栈深度（修复 #25）
func (cg *CodeGenerator) RecordDeclScopeDepth(name string) {
	if name != "" {
		if _, ok := cg.declScopeDepth[name]; !ok {
			cg.declScopeDepth[name] = cg.scopeDepth
		}
	}
}

// GetDeclScopeDepth 返回变量声明时的作用域栈深度
func (cg *CodeGenerator) GetDeclScopeDepth(name string) int {
	if d, ok := cg.declScopeDepth[name]; ok {
		return d
	}
	return 0
}

// MarkAssignEscaped 登记通过普通赋值逃逸到更长生命周期持有者的源变量。
// 已逃逸变量的块尾 per-object survivor_free 必须抑制（对象已 promote，
// 所有权由新持有者延续，提前 free 会造成新持有者悬垂）。
func (cg *CodeGenerator) MarkAssignEscaped(name string) {
	if name != "" {
		cg.assignEscapedVars[name] = true
	}
}

// IsAssignEscaped 查询变量是否已通过普通赋值逃逸
func (cg *CodeGenerator) IsAssignEscaped(name string) bool {
	return cg.assignEscapedVars[name]
}

// === 结构化所有权基础层扩展（从 SOR 下移的能力）===

// IsExternFunc 查询函数名是否为 extern 声明的函数
func (cg *CodeGenerator) IsExternFunc(name string) bool {
	return cg.externFuncNames[name]
}

// 变量分配器来源常量（多分配器 origin 追踪）
// 用于编译期确定每个指针变量该用哪个释放函数，零运行时开销
const (
	OriginKmm        = "kmm"         // KMM 分配（kmm_v4_alloc_auto 等），用 survivor_free
	OriginLibcMalloc = "libc_malloc" // libc 分配（malloc/calloc/realloc），用 free
	OriginExtern     = "extern"      // extern 函数返回，不释放（所有权不透明）
	OriginUnknown    = "unknown"     // 未知来源，保守不释放
)

// kmmAllocFuncs KMM 分配函数名集合
var kmmAllocFuncs = map[string]bool{
	"kmm_v4_alloc_auto": true,
	"kmm_v4_alloc":      true,
	"kmm_v4_malloc":     true,
	"kmm_v4_calloc":     true,
	"kmm_v4_bump":       true,
}

// libcAllocFuncs libc 分配函数名集合
var libcAllocFuncs = map[string]bool{
	"malloc":  true,
	"calloc":  true,
	"realloc": true,
}

// RecordExternOriginFromAST 追踪变量初始化是否来自 extern 函数返回
// 用于 return promote 时警告：extern 指针对 SOR 不透明，promote 语义可能不正确
func (cg *CodeGenerator) RecordExternOriginFromAST(name string, expr ast.Expression) {
	if name == "" || expr == nil {
		return
	}
	var call *ast.CallExpression
	switch e := expr.(type) {
	case *ast.CallExpression:
		call = e
	case *ast.TypeCastExpression:
		if c, ok := e.Expression.(*ast.CallExpression); ok {
			call = c
		}
	}
	if call == nil || call.Function == nil {
		return
	}
	if fnIdent, ok := call.Function.(*ast.Identifier); ok {
		if cg.IsExternFunc(fnIdent.Name) {
			cg.externOriginVars[name] = true
			cg.varOrigins[name] = OriginExtern
		}
	}
}

// RecordVarOriginFromAST 多分配器 origin 追踪：识别变量初始化的分配函数
// 识别顺序：KMM 分配函数 > libc 分配函数 > extern 函数 > 未知
// freestanding 模式下 std_malloc 被重写为 kmm_v4_alloc_auto，会被识别为 KMM
func (cg *CodeGenerator) RecordVarOriginFromAST(name string, expr ast.Expression) {
	if name == "" || expr == nil {
		return
	}
	var call *ast.CallExpression
	switch e := expr.(type) {
	case *ast.CallExpression:
		call = e
	case *ast.TypeCastExpression:
		if c, ok := e.Expression.(*ast.CallExpression); ok {
			call = c
		}
	}
	if call == nil || call.Function == nil {
		return
	}
	fnName := ""
	if os.Getenv("KAULA_DEBUG_ORIGIN") != "" {
		fmt.Fprintf(os.Stderr, "[ORIGIN-AST] var=%s exprType=%T funcType=%T\n",
			name, expr, call.Function)
	}
	if fnIdent, ok := call.Function.(*ast.Identifier); ok {
		fnName = fnIdent.Name
	} else if member, ok := call.Function.(*ast.MemberExpression); ok {
		// std.malloc 形式（旧 AST 节点）
		fnName = member.Member
	} else if member, ok := call.Function.(*ast.MemberAccessExpression); ok {
		// std.memory.kmm_v4_alloc 形式（当前解析器产出的节点）。
		//
		// KBUG_KMM_FREE_NOOP_SURVIVOR_LEAK：此前只识别 *ast.MemberExpression，
		// 而实际解析器产出的是 *ast.MemberAccessExpression，导致
		// `u8* r = std.memory.kmm_v4_alloc(64)` 的 origin 恒为 unknown。
		// origin=unknown 又使「用户显式 free」分支误判为可跳过回收，
		// 最终 survivor 槽位永久泄漏（实测 ~194 B/请求）。
		fnName = member.Member
	}
	if fnName == "" {
		return
	}
	// 类实例构造（`Elem e = Elem()`）：本作用域新建的堆对象，所有权在当前
	// 作用域，登记为 owned obj——实参交给可能持有它的被调方前需要 promote。
	if cg.IsClassType(fnName) {
		cg.ownedObjVars[name] = true
		return
	}
	// KMM 分配函数
	if kmmAllocFuncs[fnName] {
		cg.varOrigins[name] = OriginKmm
		return
	}
	// libc 分配函数（std_malloc 在 hosted 模式下是系统 malloc）
	if libcAllocFuncs[fnName] || fnName == "std_malloc" {
		// freestanding 模式下 std_malloc 被重写为 kmm_v4_alloc_auto
		// 这里保守标记为 libc_malloc，表达式生成阶段的重写不影响 origin 判断
		// 实际释放时，如果代码中是 kmm_v4_alloc_auto，会被 KMM scope 管理
		cg.varOrigins[name] = OriginLibcMalloc
		return
	}
	// extern 函数返回
	if cg.IsExternFunc(fnName) {
		cg.externOriginVars[name] = true
		cg.varOrigins[name] = OriginExtern
		return
	}
}

// GetVarOrigin 查询变量的分配器来源
func (cg *CodeGenerator) GetVarOrigin(name string) string {
	if origin, ok := cg.varOrigins[name]; ok {
		return origin
	}
	// 向后兼容：externOriginVars 中有记录的视为 extern
	if cg.externOriginVars[name] {
		return OriginExtern
	}
	return OriginUnknown
}

// SetVarOrigin 手动设置变量来源（用于赋值传播等场景）
func (cg *CodeGenerator) SetVarOrigin(name, origin string) {
	if name != "" && origin != "" {
		cg.varOrigins[name] = origin
		if origin == OriginExtern {
			cg.externOriginVars[name] = true
		}
	}
}

// IsExternOrigin 查询变量是否来自 extern 函数返回
func (cg *CodeGenerator) IsExternOrigin(name string) bool {
	return cg.externOriginVars[name] || cg.varOrigins[name] == OriginExtern
}

// MarkPromoted 标记变量已被 promote（reaching definition，防止重复 promote）
func (cg *CodeGenerator) MarkPromoted(name string) {
	if name != "" {
		cg.promotedVars[name] = true
	}
}

// IsPromoted 查询变量是否已被 promote
func (cg *CodeGenerator) IsPromoted(name string) bool {
	return cg.promotedVars[name]
}

// ResetLifetimeFlags 重置变量生命周期标志。同名变量重新声明（循环体内
// 每次迭代声明一次）即新实例：上一实例的 promoted/assign-escaped 标志
// 不适用于新实例，否则 thread_create 实参提升在第二轮迭代起被误跳过。
func (cg *CodeGenerator) ResetLifetimeFlags(name string) {
	delete(cg.promotedVars, name)
	delete(cg.assignEscapedVars, name)
	delete(cg.earlyFreedVars, name)
	delete(cg.ownedObjVars, name)
	// 注意：**不**清除 initializerSuppliedVars。
	// 该标志由 concGuardedDecl 在调用 generateVariableDeclaration 之前精确
	// 置位，而后者内部会先 AddSymbol → ResetLifetimeFlags。若在此清除，
	// 「初始化式已单独生成」的信号会被自己抹掉，声明路径重新发射死分配
	// （KBUG_KMM_FREE_NOOP_SURVIVOR_LEAK 的复发点）。
	// 该标志的语义是「本条声明不需要缺省分配」，按声明一次性消费即可；
	// 同名变量重新声明时由 concGuardedDecl 重新置位，无需在此复位。
}

// MarkOwnedObj 标记变量是本作用域新建的类实例（所有权在当前作用域）
func (cg *CodeGenerator) MarkOwnedObj(name string) {
	if name != "" {
		cg.ownedObjVars[name] = true
	}
}

// IsOwnedObj 查询变量是否是本作用域新建的类实例
func (cg *CodeGenerator) IsOwnedObj(name string) bool {
	return cg.ownedObjVars[name]
}

// ResetUserFreedVars 清空"用户已释放"集合（函数入口调用）。
// 不挂在 ResetLifetimeFlags 上：同名变量重新声明属于新实例，但"用户显式释放"
// 是整个函数体的属性，循环体内每次迭代都应跳过回收。
func (cg *CodeGenerator) ResetUserFreedVars() {
	cg.userFreedVars = make(map[string]bool)
}

// MarkUserFreed 标记变量已被用户代码显式释放（std.memory.kmm_v4_free /
// kmm_v4_global_free / string_free / libc free 等 *_free 调用）。
// 所有权已转移给用户代码，所有权��的回收路径（作用域末尾 per-object reclaim、
// return 前回收、liveness 提前释放）都必须跳过，否则同一指针被释放两次。
func (cg *CodeGenerator) MarkUserFreed(name string) {
	if name != "" {
		cg.userFreedVars[name] = true
	}
}

// IsUserFreed 查询变量是否已被用户显式释放
func (cg *CodeGenerator) IsUserFreed(name string) bool {
	return cg.userFreedVars[name]
}

// SetInitializerSupplied 登记该变量的初始化式已由调用方单独生成
// （conc 守卫降级路径：声明被降级为赋值 + 恢复副本）。
// 声明本体据此跳过「无初始化式 → 缺省堆分配」，避免发射一个结果立刻
// 被覆盖的死分配（每次 resume 泄漏一个 survivor 槽位）。
func (cg *CodeGenerator) SetInitializerSupplied(name string) {
	if name == "" || cg.initializerSuppliedVars == nil {
		return
	}
	cg.initializerSuppliedVars[name] = true
}

// IsInitializerSupplied 查询变量初始化式是否已单独生成
func (cg *CodeGenerator) IsInitializerSupplied(name string) bool {
	return cg.initializerSuppliedVars[name]
}

// MarkEarlyFreed 标记变量已被 early free（防止 return/块尾重复释放）
func (cg *CodeGenerator) MarkEarlyFreed(name string) {
	if name != "" {
		cg.earlyFreedVars[name] = true
	}
}

// IsEarlyFreed 查询变量是否已被 early free
func (cg *CodeGenerator) IsEarlyFreed(name string) bool {
	return cg.earlyFreedVars[name]
}

// === 条件分支路径敏感（从 SOR 下移的能力⑤）===

// SnapshotPromotedVars 快照当前已 promote 变量集合，用于条件分支的路径敏感分析。
// 在生成 if/else 分支前调用，生成后用 RestorePromotedVars 恢复。
func (cg *CodeGenerator) SnapshotPromotedVars() map[string]bool {
	snapshot := make(map[string]bool, len(cg.promotedVars))
	for k, v := range cg.promotedVars {
		snapshot[k] = v
	}
	return snapshot
}

// RestorePromotedVars 恢复已 promote 变量集合到快照状态。
func (cg *CodeGenerator) RestorePromotedVars(snapshot map[string]bool) {
	cg.promotedVars = make(map[string]bool, len(snapshot))
	for k, v := range snapshot {
		cg.promotedVars[k] = v
	}
}

// MergePromotedFromBranches 合并两个分支的 promote 状态。
// 只有在两个分支中都被 promote 的变量，才在合并点标记为已 promote（路径敏感交集）。
// 只在一个分支中 promote 的变量，保守地不标记（因为另一个分支可能没 promote）。
func (cg *CodeGenerator) MergePromotedFromBranches(thenPromoted, elsePromoted map[string]bool) {
	for name := range thenPromoted {
		if elsePromoted[name] {
			cg.promotedVars[name] = true
		}
	}
}

// DiffPromotedVars 计算当前 promotedVars 与快照的差异（新增的已 promote 变量）。
// 用于记录单个分支中新 promote 的变量。
func (cg *CodeGenerator) DiffPromotedVars(snapshot map[string]bool) map[string]bool {
	diff := make(map[string]bool)
	for name := range cg.promotedVars {
		if !snapshot[name] {
			diff[name] = true
		}
	}
	return diff
}

// IsMallocLikeCall 检查是否为 malloc 类调用（包含 hosted std_malloc，不区分 freestanding）
// 用于编译期大小登记
func (cg *CodeGenerator) IsMallocLikeCall(call *ast.CallExpression) bool {
	if call == nil || call.Function == nil {
		return false
	}
	switch fn := call.Function.(type) {
	case *ast.Identifier:
		switch fn.Name {
		case "std_malloc", "kmm_v4_malloc", "kmm_v4_alloc", "kmm_v4_alloc_auto", "kmm_v4_calloc", "kmm_v4_bump":
			return true
		}
	case *ast.MemberAccessExpression:
		switch fn.Member {
		case "std_malloc", "kmm_v4_malloc", "kmm_v4_alloc", "kmm_v4_alloc_auto", "kmm_v4_calloc", "kmm_v4_bump":
			return true
		}
	}
	return false
}

// RecordAllocSizeFromAST 从 malloc 类调用的 AST 直接提取常量大小并登记（修复 #25）
// 解决 hosted 模式下 std_malloc 不被重写、字符串登记失效的问题
func (cg *CodeGenerator) RecordAllocSizeFromAST(name string, expr ast.Expression) {
	if name == "" || expr == nil {
		return
	}
	var call *ast.CallExpression
	switch e := expr.(type) {
	case *ast.CallExpression:
		if cg.IsMallocLikeCall(e) {
			call = e
		}
	case *ast.TypeCastExpression:
		if c, ok := e.Expression.(*ast.CallExpression); ok && cg.IsMallocLikeCall(c) {
			call = c
		}
	}
	if call != nil {
		if n := cg.ExtractMallocSizeBytesFromAST(call); n > 0 {
			cg.sorInitSizes[name] = n
		}
	}
}

// EnterOffsetScope 进入 offset_save/restore 作用域
// 用于相邻 scope 合并优化：当外层已有 offset scope 时，内层 BlockStatement 跳过重复包裹
func (cg *CodeGenerator) EnterOffsetScope() {
	cg.offsetScopeDepth++
}

// ExitOffsetScope 退出 offset_save/restore 作用域
func (cg *CodeGenerator) ExitOffsetScope() {
	if cg.offsetScopeDepth > 0 {
		cg.offsetScopeDepth--
	}
}

// IsInOffsetScope 当前是否在 offset_save/restore 作用域内
func (cg *CodeGenerator) IsInOffsetScope() bool {
	return cg.offsetScopeDepth > 0
}

// IsFreestanding 判断是否为 freestanding（裸机）编译模式。
// 该模式下没有 libc，std_malloc 会被重写为 kmm_v4_alloc_auto；
// hosted 模式下 std_malloc 是系统 malloc 包装，与 kmm_v4 系列严格区分。
func (cg *CodeGenerator) IsFreestanding() bool {
	return cg.config != nil && cg.config.Freestanding
}

func (cg *CodeGenerator) GetStdlibConfig() *stdlib.StdlibConfig {
	return cg.stdlibConfig
}

func (cg *CodeGenerator) IsGenericInstantiated(name string) bool {
	return cg.genericInstantiated[name]
}

func (cg *CodeGenerator) MarkGenericInstantiated(name string) {
	if cg.genericInstantiated == nil {
		cg.genericInstantiated = make(map[string]bool)
	}
	cg.genericInstantiated[name] = true
}

func (cg *CodeGenerator) GetUsedModules() []string {
	return cg.usedModules
}

// trackModuleUsage 自动追踪模块使用（无需显式 import）
func (cg *CodeGenerator) trackModuleUsage(moduleName string) {
	if cg.trackedModules == nil {
		cg.trackedModules = make(map[string]bool)
	}
	if !cg.trackedModules[moduleName] {
		cg.trackedModules[moduleName] = true
		cg.usedModules = append(cg.usedModules, moduleName)
	}
}

// SetLocalImportFuncs 注册本地导入的 pub 函数名
func (cg *CodeGenerator) SetLocalImportFuncs(funcs map[string]bool) {
	cg.localImportFuncs = funcs
}

// SetLocalImportExportFuncs 注册本地导入的 export 函数名。
// export 函数在 C 侧保持原名（FFI 边界），跨文件调用端必须与定义端命名一致。
func (cg *CodeGenerator) SetLocalImportExportFuncs(funcs map[string]bool) {
	cg.localImportExportFuncs = funcs
}

// SetLocalPubTypes 注册本地导入的类型定义（跨文件 class/struct 等）。
// 同时登记到 typeGenerator 的 struct/class 类型表，使 convertType / IsClassType
// 能正确生成指针语义（class）或值语义（struct）的 C 类型。
func (cg *CodeGenerator) SetLocalPubTypes(types map[string]*sema.LocalPubTypeInfo) {
	cg.localPubTypes = types
	if cg.typeGenerator == nil {
		return
	}
	for name, info := range types {
		if info == nil {
			continue
		}
		switch info.Kind {
		case "class":
			cg.typeGenerator.structTypes[name] = true
			cg.typeGenerator.classTypes[name] = true
		case "struct":
			cg.typeGenerator.structTypes[name] = true
		}
	}
}

// SetLocalImportReturnTypes 注册本地 import 的 pub/export 函数返回类型映射。
// inferType 对跨文件函数调用查 program.FindFunction 必然失败（定义在别的
// 文件），此处提供 O(1) 回查，保证 println 格式/concat 重载等推断正确。
func (cg *CodeGenerator) SetLocalImportReturnTypes(m map[string]string) {
	cg.localImportReturns = m
	if len(m) > 0 && cg.typeGenerator != nil {
		// 顺带把跨文件函数名注册为已知符号，便于其他按名查询的路径兜底
		if cg.localImportFuncs == nil {
			cg.localImportFuncs = make(map[string]bool)
		}
		for name := range m {
			cg.localImportFuncs[name] = true
		}
	}
}

func NewCodeGenerator(cfg *config.Config) *CodeGenerator {
	tm := NewTemplateManager()
	templatePath := filepath.Join(cfg.TemplatePath, "main.c.tmpl")
	tm.LoadTemplate("main", templatePath)
	// 加载 freestanding 裸机入口模板（用于 --freestanding 模式）
	freestandingPath := filepath.Join(cfg.TemplatePath, "freestanding.c.tmpl")
	tm.LoadTemplate("freestanding", freestandingPath)
	// 加载用户态入口模板（用于 --boot user 模式）
	if cfg.Boot == "user" {
		userPath := filepath.Join(cfg.TemplatePath, "user.c.tmpl")
		tm.LoadTemplate("user", userPath)
	}

	pm := NewPluginManager()

	stdlibPath := cfg.StdlibPath
	if stdlibPath == "" {
		// 与 kaulac.findStdlib 保持一致：先找可执行文件同级的 stdlib.json
		// （build/bin 布局），再退回 cwd 相对路径。只查 cwd 会漏掉
		// 从仓库根目录调用 build/bin/kaulac.exe 的场景。
		if exePath, err := os.Executable(); err == nil {
			exeDir := filepath.Dir(filepath.Clean(exePath))
			for _, cand := range []string{
				filepath.Join(exeDir, "stdlib.json"),
				filepath.Join(exeDir, "compiler", "stdlib.json"),
				filepath.Join(exeDir, "..", "compiler", "stdlib.json"),
			} {
				if _, err := os.Stat(cand); err == nil {
					stdlibPath = cand
					break
				}
			}
		}
	}
	if stdlibPath == "" {
		// cwd 相对路径
		for _, cand := range []string{"stdlib.json", "kaula/stdlib.json", "../stdlib.json", "../../stdlib.json"} {
			if _, err := os.Stat(cand); err == nil {
				stdlibPath = cand
				break
			}
		}
	}
	if stdlibPath == "" {
		stdlibPath = "stdlib.json" // 保底: 让 LoadStdlibConfig 报真实错误
	}
	// 加载 stdlib 配置（失败时警告并置 nil，后续代码均有 nil 检查）
	stdlibConfig, err := stdlib.LoadStdlibConfig(stdlibPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[WARN] 加载 stdlib 配置失败 (%s): %v; 标准库函数签名将不可用\n", stdlibPath, err)
		stdlibConfig = nil
	}

	treeManager := core.NewTreeManager()
	prefixManager := core.NewPrefixManager()

	symbolTable := symbol.NewSymbolTable(nil, "global")

	cg := &CodeGenerator{
		output: "",
		indent: 0,
		/*
		 * 守卫模式默认关闭。
		 *
		 * 零值 0 会被读成「层 0 的复合语句体」，于是**所有**代码生成都
		 * 被卷进守卫槽位：普通函数的语句被包进 `if (f->sub[0] == k)`，
		 * 而 f 根本不存在。必须显式置 -1（= 不在守卫模式）。
		 */
		concGuardDepth:           -1,
		templateManager:          tm,
		config:                   cfg,
		pluginManager:            pm,
		stdlibConfig:             stdlibConfig,
		treeManager:              treeManager,
		prefixManager:            prefixManager,
		symbolTable:              symbolTable,
		currentScope:             symbolTable,
		errors:                   []string{},
		userFreedVars:            make(map[string]bool),
		initializerSuppliedVars:  make(map[string]bool),
		usedThirdPartyLibs:       make(map[string]bool),
		localImportFuncs:         make(map[string]bool),
		genericCache:             make(map[string]*GenericInstanceCache),
		genericInstantiated:      make(map[string]bool),
		genericTypeCache:         make(map[string]string),
		sourceMap:                NewSourceMap("", ""),
		constTable:               make(map[string]string),
		arrayLens:                make(map[string]int),
		sorInitSizes:             make(map[string]int),
		declScopeDepth:           make(map[string]int),
		assignEscapedVars:        make(map[string]bool),
		promoteHolders:           make(map[string]bool),
		paramNames:               make(map[string]bool),
		safeLocalContainers:      make(map[string]bool),
		writtenThroughContainers: make(map[string]bool),
		chainLinkedVars:          make(map[string]bool),
		chainEscapingVars:        make(map[string]bool),
		escapedVars:              make(map[string]bool),
		externFuncNames:          make(map[string]bool),
		externOriginVars:         make(map[string]bool),
		varOrigins:               make(map[string]string),
		promotedVars:             make(map[string]bool),
		ownedObjVars:             make(map[string]bool),
		earlyFreedVars:           make(map[string]bool),
		exportedFuncNames:        make(map[string]bool),
		deepPromoteCBEmitted:     make(map[string]struct{}),
		// 缺口4：整数溢出检查。debug 模式默认启用（零开销 __builtin_*_overflow），
		// release 模式禁用（wrapping 行为，与 C 一致，零开销）。
		enableOverflowCheck: !cfg.Release,
	}

	cg.typeGenerator = NewTypeGenerator(cg)
	cg.functionGenerator = NewFunctionGenerator(cg)
	cg.expressionGenerator = NewExpressionGenerator(cg)
	cg.statementGenerator = NewStatementGenerator(cg)

	return cg
}

func (cg *CodeGenerator) Generate(program *ast.Program) string {
	/*
	 * 复位「本 TU 是否用到并发内核」。
	 *
	 * codeUsesConcurrency 是包级全局，供 Generate 末尾决定要不要
	 * #include "concurrency/kaula_conc.h"。它是跨调用的，不复位就会
	 * 泄漏：同一个 kaulac 进程里先编一个带 conc fn 的文件，之后每个
	 * 文件都会被塞进内核头 —— 包括一行 conc 都没有的裸机目标。
	 * 那正是这条注释原本要防的事，所以复位必须发生在这里。
	 */
	codeUsesConcurrency = false

	// TU 拆分模式：每次 Generate 都从 config 重新读取（同一个 CodeGenerator
	// 不会跨文件复用，但显式复位可以避免上一次生成的残留状态影响归属决策）。
	cg.tuMode = ""
	cg.tuPreludeOrigin = ""
	cg.tuGenericsDeferred = nil
	cg.tuGenericsLocalDefined = nil
	cg.tuClassStatics = nil
	cg.tuExportVars = nil
	cg.tuExportFuncProtos = nil
	if cg.config != nil {
		cg.tuMode = cg.config.TUMode
		cg.tuSeenStatics = make(map[string]bool)
	}

	// 嵌套 conc fn 调用：AST 层内联展开（Future 扁平化，见 conc_inline.go）。
	// 必须在 FlattenDeclarations 之前：内联产生的新声明需要统一展平。
	cg.inlineConcFns(program)

	// spawn 实参常量特化（A）：字面量实参 → 克隆特化版 conc fn，
	// 去参数化 + 重分析。也必须在 FlattenDeclarations 之前
	// （特化 fn 的新声明需要统一展平）。
	cg.specializeSpawnConsts(program)

	// 预注册全部 conc fn 的 emitter：顺序调用（A 调 B、B 定义在后）
	// 与特化 fn（插入头部）生成时能查到 shim，避免生成裸 C 调用。
	cg.preRegisterConcFns(program)

	// 单语句多变量声明（i64 a = 1, b = 2）展平为独立声明（幂等；
	// 未经过语义阶段的直连代码生成路径也能正确输出全部声明符）
	astutil.FlattenDeclarations(program)

	cg.program = program
	cg.usedThirdPartyLibs = make(map[string]bool)
	cg.lambdaCounter = 0
	cg.objectLiteralCounter = 0
	cg.lambdaDefinitions = nil
	cg.objectDeclCode.Reset()
	cg.preludeInitCode.Reset()

	// 初始化高级属性处理器
	cg.attributeFeat = NewAttributeFeatures(cg)

	// 收集本文件全部用户自定义 fn（含函数体内嵌套 fn），用于统一 K_ 函数前缀。
	// main、extern fn（ExternStatement）、stdlib/第三方 C 函数都不在其中，保持原名。
	cg.userFuncNames = make(map[string]bool)
	// 收集 export 函数名（FFI 边界，保持原名不加 K_ 前缀）
	cg.exportedFuncNames = make(map[string]bool)
	program.Traverse(func(n ast.Node) {
		if fn, ok := n.(*ast.FunctionStatement); ok && fn.Name != "" {
			cg.userFuncNames[fn.Name] = true
			if fn.IsExported {
				cg.exportedFuncNames[fn.Name] = true
			}
		}
		// F15：预收集 extern 函数名，保证声明之前的调用点也能命中原名规则
		// （输出阶段的增量收集在语句生成时才填充，晚于调用点查询）
		if es, ok := n.(*ast.ExternStatement); ok && es.Name != "" {
			cg.externFuncNames[es.Name] = true
		}
	})

	type rawEntry struct {
		section string
		relLine int
		srcLine int
		srcCol  int
		kind    string
		symbol  string
	}
	var rawEntries []rawEntry
	var typeLine, globalLine, funcLine, mainLine int

	addEntry := func(section string, srcLine, srcCol int, kind, symbol string, lineCount int) {
		if srcLine > 0 {
			var baseLine *int
			switch section {
			case "type":
				baseLine = &typeLine
			case "global":
				baseLine = &globalLine
			case "func":
				baseLine = &funcLine
			case "main":
				baseLine = &mainLine
			}
			if baseLine != nil {
				rawEntries = append(rawEntries, rawEntry{
					section: section,
					relLine: *baseLine + 1,
					srcLine: srcLine,
					srcCol:  srcCol,
					kind:    kind,
					symbol:  symbol,
				})
			}
			*baseLine += lineCount
		} else {
			var baseLine *int
			switch section {
			case "type":
				baseLine = &typeLine
			case "global":
				baseLine = &globalLine
			case "func":
				baseLine = &funcLine
			case "main":
				baseLine = &mainLine
			}
			if baseLine != nil {
				*baseLine += lineCount
			}
		}
	}

	var typeCode strings.Builder
	var globalVars strings.Builder
	var classGlobalVars strings.Builder // class 类型全局变量: 需在 typedef 之后生成
	var classInitCode strings.Builder   // class 全局变量的初始化: 注入 main 开头
	var functionCode strings.Builder
	var mainCode strings.Builder
	typeCode.Grow(4096)
	globalVars.Grow(1024)
	functionCode.Grow(8192)
	mainCode.Grow(4096)

	hasMain := false

	// 收集高级属性：#[test]、#[derive]、#[command]
	testFunctions := cg.attributeFeat.CollectTestFunctions(program)
	deriveTypes := cg.attributeFeat.CollectDeriveTypes(program)
	commands := cg.attributeFeat.CollectCommands(program)

	// 测试模式下，忽略用户定义的 main，使用自动生成的测试 runner
	if cg.TestMode && len(testFunctions) > 0 {
		hasMain = false
		cg.testRunnerActive = true
	}

	importedModules := make(map[string]bool)

	for _, stmt := range program.Statements {
		if stmt == nil {
			continue
		}
		if importStmt, ok := stmt.(*ast.ImportStatement); ok {
			/*
			 * TU 拆分下的两种 import 处理：
			 *
			 *   - 本地 .kl import（路径/库）：它对应的 C 代码在**另一个 TU** 里。
			 *     单 TU 时代这些 import 只为"语义上可见性"存在，生成侧本来也不
			 *     产出任何代码（对照下面的 default 分支），这里显式跳过即可；
			 *     但**不能**把它的 Module 计进 importedModules —— 那会输出
			 *     `#include "<路径>.kl"` 或把本地库名当成 std 模块处理。
			 *   - std / freestanding / pkglib 模块：每个 TU 都要自己的头，
			 *     照旧计入。
			 */
			if !importStmt.IsLocal {
				importedModules[importStmt.Module] = true
			}
			continue
		}
		if _, ok := stmt.(*ast.PackageStatement); ok {
			continue
		}
		if _, ok := stmt.(*ast.ExportStatement); ok {
			continue
		}

		if fnStmt, ok := stmt.(*ast.FunctionStatement); ok {
			if fnStmt.Name == "main" {
				// 测试模式下跳过用户定义的 main，使用测试 runner
				if cg.TestMode && len(testFunctions) > 0 {
					continue
				}
				hasMain = true
			}
			code := cg.generateStatement(stmt) + "\n"
			lines := strings.Count(code, "\n")
			addEntry("func", fnStmt.Pos.Line, fnStmt.Pos.Column, "function", fnStmt.Name, lines)
			functionCode.WriteString(code)
		} else if classStmt, ok := stmt.(*ast.ClassStatement); ok {
			code := cg.generateStatement(stmt) + "\n"
			lines := strings.Count(code, "\n")
			addEntry("type", classStmt.Pos.Line, classStmt.Pos.Column, "class", classStmt.Name, lines)
			typeCode.WriteString(cg.guardTUType(code))
			cg.collectTUPrelude(code)
		} else if ifaceStmt, ok := stmt.(*ast.InterfaceStatement); ok {
			code := cg.generateStatement(stmt) + "\n"
			lines := strings.Count(code, "\n")
			addEntry("type", ifaceStmt.Pos.Line, ifaceStmt.Pos.Column, "interface", ifaceStmt.Name, lines)
			typeCode.WriteString(cg.guardTUType(code))
			cg.collectTUPrelude(code)
		} else if structStmt, ok := stmt.(*ast.StructStatement); ok {
			code := cg.generateStatement(stmt) + "\n"
			lines := strings.Count(code, "\n")
			addEntry("type", structStmt.Pos.Line, structStmt.Pos.Column, "struct", structStmt.Name, lines)
			typeCode.WriteString(cg.guardTUType(code))
			cg.collectTUPrelude(code)
		} else if enumStmt, ok := stmt.(*ast.EnumStatement); ok {
			// 注册枚举变体到符号表
			for _, variant := range enumStmt.Variants {
				cg.symbolTable.AddSymbol(variant.Name, "enum_variant:"+enumStmt.Name, false, "global", enumStmt.Pos.Line, enumStmt.Pos.Column)
			}
			code := cg.generateStatement(stmt) + "\n"
			lines := strings.Count(code, "\n")
			addEntry("type", enumStmt.Pos.Line, enumStmt.Pos.Column, "enum", enumStmt.Name, lines)
			typeCode.WriteString(cg.guardTUType(code))
			cg.collectTUPrelude(code)
		} else if typeStmt, ok := stmt.(*ast.TypeAliasStatement); ok {
			code := cg.generateStatement(stmt) + "\n"
			lines := strings.Count(code, "\n")
			addEntry("type", typeStmt.Pos.Line, typeStmt.Pos.Column, "type", typeStmt.Name, lines)
			typeCode.WriteString(cg.guardTUType(code))
			cg.collectTUPrelude(code)
		} else if varDecl, ok := stmt.(*ast.VariableDeclaration); ok {
			if varDecl == nil {
				continue
			}
			// const 变量：可编译期求值的存入常量表（不生成 C 代码）；
			// 显式类型且无法求值的（如 const char* s = fn()）按普通 C const 变量生成
			// export 导出的 const 额外生成真实 C 常量定义（跨文件/外部链接可见）
			if varDecl.IsConst {
				if evaluated := cg.tryEvalConstExpr(varDecl.Value); evaluated != "" {
					cg.constTable[varDecl.Name] = evaluated
					if !varDecl.IsExported {
						continue
					}
				}
				if varDecl.Type == "" {
					cg.constTable[varDecl.Name] = cg.expressionGenerator.GenerateExpression(varDecl.Value)
					if !varDecl.IsExported {
						continue
					}
				}
				// 导出常量需要真实 C 定义，无类型时从字面量推导
				if varDecl.IsExported && varDecl.Type == "" {
					varDecl.Type = inferExportedType(varDecl.Value)
				}
			}
			cType := cg.typeGenerator.convertType(varDecl.Type, varDecl.Nullable)
			// class 类型变量: 引用语义, C 类型为指针 (K_A*)
			if cg.typeGenerator != nil && cg.typeGenerator.classTypes[varDecl.Type] && !strings.HasSuffix(cType, "*") {
				cType += "*"
			}
			// 注册全局变量到 codegen 符号表 (成员访问/方法调用需要)
			cg.AddSymbol(varDecl.Name, varDecl.Type, varDecl.Nullable, "global", varDecl.Pos.Line, varDecl.Pos.Column)
			initValue := cg.generateExpression(varDecl.Value)
			// char g[16] = "global";
			// generateExpression 对字符串字面量一律生成 String 结构体字面量，而定长
			// char 数组的初始化器必须是 C 字符串字面量。原先只有下面那句
			//     if isArray && !strings.HasPrefix(initValue, "\"") { initValue = "{0}" }
			// 兜底，它假设 initValue 已经是 C 字面量——实际永远不是，于是全局 char
			// 数组一律被降级成 {0}，字符全为 0。
			if strLit, ok := varDecl.Value.(*ast.StringLiteral); ok && isCharArrayDecl(cType) {
				initValue = "\"" + escapeCString(strLit.Value) + "\""
			}
			if varDecl.IsAuto && varDecl.Type == "" {
				// 顶层 auto 无法由语义阶段推导类型时回退：若已有推导类型则直接使用
				cType = "auto"
			}
			// class 类型全局变量: 延迟到 typedef 之后生成 (K_Student 等)
			isClassGlobal := cg.typeGenerator != nil && cg.typeGenerator.classTypes[varDecl.Type]
			// 全局变量：支持属性、static
			var varPrefix strings.Builder
			if len(varDecl.Attributes) > 0 {
				varPrefix.WriteString(generateVarAttributes(varDecl.Attributes))
			}
			// export 修饰符：C 级导出声明（置于存储/类型限定符之前，MSVC/GCC 均合法）
			if varDecl.IsExported {
				varPrefix.WriteString("KAULA_EXPORT ")
			}
			if varDecl.IsStatic {
				varPrefix.WriteString("static ")
			}
			if varDecl.IsConst {
				varPrefix.WriteString("const ")
			}
			prefix := varPrefix.String()
			// 固定大小数组类型 [N]elem → C 声明 "elem name[N]"
			decl := cg.generateGlobalVarDeclString(cType, varDecl.Name)
			isArray := strings.Contains(cType, "[") && strings.HasSuffix(cType, "]")
			isStruct := cg.typeGenerator != nil && cg.typeGenerator.structTypes[varDecl.Type]
			// 数组全局变量：标量初始化值转换为 {0}（字符串字面量初始化除外）
			if isArray && !strings.HasPrefix(initValue, "\"") {
				initValue = "{0}"
			}
			// 无初始化器的结构体全局变量：{0} 零初始化（C 不允许 struct = 0）
			if isStruct && varDecl.Value == nil {
				initValue = "{0}"
			}
			var classGlobalAbsent bool
			if prefix != "" {
				code := fmt.Sprintf("%s%s = %s;\n", prefix, decl, initValue)
				lines := strings.Count(code, "\n")
				addEntry("global", varDecl.Pos.Line, varDecl.Pos.Column, "variable", varDecl.Name, lines)
				if isClassGlobal {
					// class 全局变量: 声明为 NULL, main 开头用赋值初始化 (避免局部遮蔽)
					classGlobalVars.WriteString(fmt.Sprintf("%s%s = NULL;\n", prefix, decl))
					classInitCode.WriteString(varDecl.Name + " = " + initValue + ";\n")
					classGlobalAbsent = true
				} else {
					globalVars.WriteString(code)
				}
			} else {
				code := fmt.Sprintf("%s = %s;\n", decl, initValue)
				lines := strings.Count(code, "\n")
				addEntry("global", varDecl.Pos.Line, varDecl.Pos.Column, "variable", varDecl.Name, lines)
				if isClassGlobal {
					// class 全局变量: 声明为 NULL, main 开头用赋值初始化 (避免局部遮蔽)
					classGlobalVars.WriteString(decl + " = NULL;\n")
					classInitCode.WriteString(varDecl.Name + " = " + initValue + ";\n")
					classGlobalAbsent = true
				} else {
					globalVars.WriteString(code)
				}
			}
			/*
			 * TU 拆分：export 全局变量在定义者 TU 里是普通定义，调用方 TU 必须能
			 * `extern` 引用。单 TU 时代所有 TU 本就是一份，无须声明；拆开后
			 * 接口头需要给出 extern 形态。
			 *
			 * 必须复用同一条 decl（含 const 等限定符）：`export const x = 1`
			 * 定义处是 `const uint8_t x`，若 header 只写 `extern uint8_t x`，
			 * clang 会判 "redefinition of 'x' with a different type"。
			 * 前缀只保留类型限定符，丢弃 KAULA_EXPORT / static —— 前者已在
			 * 定义处，后者语义上不该跨 TU。
			 */
			if cg.tuMode != "" && varDecl.IsExported && !classGlobalAbsent {
				var qual strings.Builder
				if varDecl.IsConst {
					qual.WriteString("const ")
				}
				cg.tuExportVars = append(cg.tuExportVars, "extern "+qual.String()+decl+";")
			}
		} else if externStmt, ok := stmt.(*ast.ExternStatement); ok {
			if externStmt == nil {
				continue
			}
			// 结构化所有权基础层：登记 extern 函数名，用于追踪变量来源
			if externStmt.IsFunction {
				cg.externFuncNames[externStmt.Name] = true
			}
			var code string
			if externStmt.IsFunction {
				// extern 函数声明：生成完整原型
				returnType := cg.typeGenerator.convertType(externStmt.ReturnType, false)
				if returnType == "" {
					returnType = "void"
				}
				var params strings.Builder
				if len(externStmt.ParamTypes) == 0 {
					params.WriteString("void")
				} else {
					for i, pType := range externStmt.ParamTypes {
						if i > 0 {
							params.WriteString(", ")
						}
						cType := cg.typeGenerator.convertType(pType, false)
						if cType == "" {
							cType = "void*"
						}
						params.WriteString(cType)
					}
				}
				code = fmt.Sprintf("extern %s %s(%s);\n", returnType, externStmt.Name, params.String())
			} else {
				cType := cg.typeGenerator.convertType(externStmt.Type, externStmt.Nullable)
				code = fmt.Sprintf("extern %s;\n", cg.generateGlobalVarDeclString(cType, externStmt.Name))
			}
			lines := strings.Count(code, "\n")
			addEntry("global", externStmt.Pos.Line, externStmt.Pos.Column, "extern", externStmt.Name, lines)
			globalVars.WriteString(code)
		} else {
			code := cg.indentString() + cg.generateStatement(stmt)
			lines := strings.Count(code, "\n")
			if pos := getStmtPos(stmt); pos != nil {
				addEntry("main", pos.Line, pos.Column, "statement", "", lines)
			} else {
				addEntry("main", 0, 0, "statement", "", lines)
			}
			mainCode.WriteString(code)
		}
	}

	// 注入对象字面量声明（文件作用域）到 functionCode 开头
	if cg.objectDeclCode.Len() > 0 {
		var newFuncCode strings.Builder
		newFuncCode.Grow(functionCode.Len() + cg.objectDeclCode.Len() + 64)
		newFuncCode.WriteString("// --- dynobj object declarations ---\n")
		newFuncCode.WriteString(cg.objectDeclCode.String())
		newFuncCode.WriteString("// --- end declarations ---\n\n")
		newFuncCode.WriteString(functionCode.String())
		functionCode.Reset()
		functionCode.WriteString(newFuncCode.String())
	}

	// 注意：preludeInitCode 已在 generateMainFunction 中注入到 main 函数体中
	// （不再在此处重复注入，以免重复代码）

	cg.usedModules = make([]string, 0, len(importedModules)+1)
	for moduleName := range importedModules {
		cg.usedModules = append(cg.usedModules, moduleName)
	}
	// 将 class 全局变量的初始化注入 main 开头 (运行时构造)
	if classInitCode.Len() > 0 {
		originalMain := mainCode.String()
		mainCode.Reset()
		mainCode.WriteString(classInitCode.String())
		mainCode.WriteString(originalMain)
	}

	// 动态对象运行时依赖 std/obj 模块（dynobj.c），需要预编译链接
	if cg.needsObjRuntime {
		cg.usedModules = append(cg.usedModules, "std.obj")
		// std.string 仅使用头文件声明，不链接实现（避免 Windows C 运行时兼容性问题）
	}
	// 将使用的 pkglib 第三方库添加到 usedModules，使链接阶段能正确处理其 libraries/include_path
	for libName := range cg.usedThirdPartyLibs {
		already := false
		for _, m := range cg.usedModules {
			if m == libName {
				already = true
				break
			}
		}
		if !already {
			cg.usedModules = append(cg.usedModules, libName)
		}
	}

	// 泛型类型实例化代码保留在 genericTypeCode 中，
	// 在最终组装时注入到函数原型（forwardDecls）之前，
	// 确保实例化类型定义（如 K_Option_int 的 typedef）先于引用它的原型。

	// 将 lambda 定义插入到 functionCode 之前
	var lambdaCode strings.Builder
	for _, def := range cg.lambdaDefinitions {
		lambdaCode.WriteString(def)
	}
	// 将 lambda 定义合并到 functionCode 前面
	if lambdaCode.Len() > 0 {
		combined := lambdaCode.String() + functionCode.String()
		functionCode.Reset()
		functionCode.WriteString(combined)
	}

	// 将泛型实例化代码注入到 functionCode 之前，
	// 确保定义在 main 中调用之前
	if cg.genericFuncCode.Len() > 0 {
		combined := cg.genericFuncCode.String() + functionCode.String()
		functionCode.Reset()
		functionCode.WriteString(combined)
	}

	// 任务② Bug 2 正解：深提升合成回调。
	// cb 是 static C 函数（引用 K_<Class> 类型和 string_clone_global/kmm_v4_promote_deep）。
	// 注意：不能拼进 functionCode 后输出——class 方法体（classImplCode，如 MLP_new
	// 里的 kmm_v4_scope_promote_p_deep 调用）在最终组装时位于 functionCode 之前，
	// 若 cb 定义在 functionCode 中会导致"先使用后定义"（static 函数 undeclared）。
	// 因此 cb 单独保存，由 Generate 尾部在 classImplCode 之前输出。
	if cg.deepPromoteCBCode.Len() > 0 {
		comment := "\n// --- SOR synthesized deep-promote callbacks (Bug 2 fix) ---\n\n"
		cg.deepPromoteCBBlock = comment + cg.deepPromoteCBCode.String()
	}

	var allIncludes strings.Builder
	allIncludes.Grow(2048)
	if cg.config != nil && cg.config.Freestanding {
		// freestanding 模式：只包含 freestanding 安全的标准头文件
		// 不包含 kaula.h（标准库运行时头，裸机环境不可用）
		// 不包含 <stdio.h>/<stdlib.h>/<string.h>，它们在裸机下不存在
		// memset/memcpy 等由 kaula_freestanding_runtime.c 提供
		allIncludes.WriteString("#include <stdint.h>\n#include <stdbool.h>\n#include <stddef.h>\n")
		// 裸机下不能包含 kaula.h（它带 <string.h>），但代码生成器在几乎所有
		// 非平凡函数里都会用到 KMM 分配器宏/函数（作用域回收、survivor、
		// 字符串字面量提升）与 debug 溢出检查助手，因此这两个头无条件包含:
		//   - kmm_scoped_allocator_v4.h 自包含（KMM_V4_* 宏 + kmm_v4_* 声明），
		//     内部用 #ifndef KAULA_FREESTANDING 屏蔽 libc 头
		//   - kaula_runtime.h 提供 kaula_add_i64 等，且已做成双环境可用
		// 早前这里用"代码里是否出现 kmm_v4_/KMM_V4_"来条件包含，但 includes 在
		// 函数体生成之前拼装，扫到的代码不全，导致 KMM_V4_SCOPE_END /
		// kmm_v4_scope_pop / kmm_v4_survivor_* / kaula_add_i64 未声明直接编译失败。
		allIncludes.WriteString("#include \"kmm_scoped_allocator_v4.h\"\n")
		allIncludes.WriteString("#include \"kaula_runtime.h\"\n")
		// 并发内核：conc fn 的 Frame / resume / coercion shim 依赖
		// KaulaResumeResult、kaula_spawn、kaula_handle_await 等。
		// 仅当程序里真的出现 conc fn 时才包含 —— 不用 spawn 的程序
		// 链接器会把这整个归档丢弃（设计说明书 §2.7 推论二）。
		if UsesConcurrency() {
			allIncludes.WriteString("#include \"concurrency/kaula_conc.h\"\n")
		}
		// Windows (MSVC ABI)：使用浮点的 TU 会引用 _fltused（CRT 提供）。
		// --freestanding 在 Windows 宿主上链接时同样不链 CRT（-nostdlib），
		// 必须自备 stub，否则链接期 undefined symbol: _fltused。
		// 守卫条件用 _WIN32/_WIN64 判定：真正的裸机目标（*-none-elf）不受影响。
		allIncludes.WriteString("#if defined(_WIN32) || defined(_WIN64)\n")
		allIncludes.WriteString("#ifndef _KAULA_FLTUSED\n")
		allIncludes.WriteString("#define _KAULA_FLTUSED\n")
		allIncludes.WriteString("int _fltused = 0;\n")
		allIncludes.WriteString("#endif\n")
		allIncludes.WriteString("#endif\n")
	} else {
		allIncludes.WriteString("#include <stdint.h>\n#include <stdbool.h>\n#include <stddef.h>\n#include <stdio.h>\n#include <stdlib.h>\n#include <string.h>\n#include \"kaula.h\"\n#include \"kaula_runtime.h\"\n")
		// 并发内核（见上面的同款注释）
		if UsesConcurrency() {
			allIncludes.WriteString("#include \"concurrency/kaula_conc.h\"\n")
		}
		// Windows (MSVC ABI / UCRT)：编译器对使用浮点的 TU 生成未定义引用 _fltused，
		// 该符号由 CRT 提供；本工具链不链接 CRT，必须自备定义（标准 _fltused stub）
		// 幂等守卫：多文件编译（injectLocalCode）会把各本地导入 .kl 各自生成
		// 的函数定义连同其前导 _fltused 段一起注入同一 TU；带初始化的文件作用域
		// 定义在 C 中重复即重定义错误，用 #ifndef 守卫保证整个 TU 只定义一次。
		if runtime.GOOS == "windows" {
			allIncludes.WriteString("#if defined(_WIN32) || defined(_WIN64)\n")
			allIncludes.WriteString("#define itoa K_itoa\n")
			allIncludes.WriteString("#ifndef _KAULA_FLTUSED\n")
			allIncludes.WriteString("#define _KAULA_FLTUSED\n")
			allIncludes.WriteString("int _fltused = 0;\n")
			allIncludes.WriteString("#endif\n")
			allIncludes.WriteString("#endif\n")
		}
		// string_cdata 兜底宏：kaula.h（std/base/types.h）在 SSO 环境中已定义
		// （len<=15 读 sso 槽位）；在无 SSO 的最小 String 环境（如 codegen
		// 单元测试）下未定义，回退到 .ptr。字符串 match 生成的比较代码依赖
		// 该宏做 SSO 感知读取，否则 SSO 短字符串会读取未初始化的 ptr 槽位。
		allIncludes.WriteString("#ifndef string_cdata\n")
		allIncludes.WriteString("#define string_cdata(s) ((s).ptr)\n")
		allIncludes.WriteString("#endif\n")
	}

	if cg.stdlibConfig != nil {
		// 只为显式导入的模块生成 #include
		for moduleName := range importedModules {
			module, ok := cg.stdlibConfig.Modules[moduleName]
			if ok {
				if module.Header != "" {
					header := module.Header
					if len(header) >= 4 && header[0] == 's' && header[1] == 't' && header[2] == 'd' && header[3] == '/' {
						header = header[4:]
					}
					// freestanding 模块头保留完整前缀 freestanding/xxx/xxx.h，
					// 由 -I <kaula-root> 解析。不能去掉前缀：去掉后会与
					// std 目录下同名头文件（string/string.h 等）冲突，
					// 且 std 路径在前时会被错误解析到 std 版本
					allIncludes.WriteString("#include \"")
					allIncludes.WriteString(header)
					allIncludes.WriteString("\"\n")
				}
			} else {
				for _, lib := range cg.stdlibConfig.ThirdParty {
					if lib.Name == moduleName {
						if lib.Type == "single_header" && lib.ImplementMacro != "" {
							allIncludes.WriteString("#define ")
							allIncludes.WriteString(lib.ImplementMacro)
							allIncludes.WriteByte('\n')
						}
						for _, header := range lib.Headers {
							allIncludes.WriteString("#include ")
							allIncludes.WriteString(header)
							allIncludes.WriteByte('\n')
						}
						break
					}
				}
			}
		}
	}

	// 动态对象运行时头文件（仅在代码中使用了 object 功能时注入）
	// 注意: 字符串拼接("a" + x)不需要注入——std.h 默认链入 string/string.h,
	// string_concat/string_concat_int/string_concat_float 声明与实现(kaula_std.lib)均可用
	if cg.needsObjRuntime {
		allIncludes.WriteString("#include \"obj/obj.h\"\n")
		// 注入字符串运行时的最小内联实现（仅依赖 KMM 分配器，避免链接整个 std.string 模块）
		allIncludes.WriteString(`
// --- inline string minimal runtime ---
#ifndef KAULA_INLINE_STRING_RUNTIME
#define KAULA_INLINE_STRING_RUNTIME
#include "base/types.h"
#include "memory/memory.h"
#include <string.h>
#include <stdio.h>

static inline String string_create(const char* str) {
    if (!str) { String r = {0, NULL}; return r; }
    size_t len = strlen(str);
    char* data = (char*)kmm_v4_malloc(len + 1);
    if (data) { data[len] = '\0'; if (len > 0) memcpy(data, str, len); }
    String r = {len, data};
    return r;
}

static inline String string_concat(String str1, String str2) {
    size_t total = str1.len + str2.len;
    char* data = (char*)kmm_v4_malloc(total + 1);
    if (data) {
        data[total] = '\0';
        if (str1.len > 0) memcpy(data, str1.ptr, str1.len);
        if (str2.len > 0) memcpy(data + str1.len, str2.ptr, str2.len);
    }
    String r = {total, data};
    return r;
}

static inline String string_concat_int(String s, int64_t v) {
    char buf[32];
    snprintf(buf, sizeof(buf), "%lld", (long long)v);
    return string_concat(s, string_create(buf));
}

static inline String string_concat_float(String s, double v) {
    char buf[64];
    snprintf(buf, sizeof(buf), "%f", v);
    return string_concat(s, string_create(buf));
}
#endif
// --- end inline string runtime ---
`)
	}

	// export 导出宏（供 KAULA_EXPORT 声明使用）
	allIncludes.WriteString(`
#ifndef KAULA_EXPORT
#if defined(_WIN32)
#define KAULA_EXPORT __declspec(dllexport)
#else
#define KAULA_EXPORT __attribute__((visibility("default")))
#endif
#endif
`)

	/*
	 * TU 拆分：把"别的 TU 需要看见的东西"插到模板的固定占位点上。
	 *
	 * 位置很关键 —— 必须在 {{includes}} 之后、{{type_code}}/{{function_code}} 之前：
	 *   - 前置段（跨文件类型）在最前：本 TU 的原型和函数体都可能引用它；
	 *   - 本 TU 的接口头（extern 变量 / 泛型实例原型 / static 函数原型）紧随其后。
	 *
	 * 只对本 TU 已经 include 过的头文件补原型：头文件是纯声明，不引入任何
	 * 符号依赖，因此对所有模板（main/freestanding/user）都成立。
	 */
	if cg.tuMode != "" && cg.config != nil {
		var tuBlock strings.Builder
		if cg.config.TUPrelude != "" {
			tuBlock.WriteString("/* --- kaula TU prelude: cross-TU type definitions --- */\n")
			tuBlock.WriteString(cg.config.TUPrelude)
			tuBlock.WriteString("/* --- end kaula TU prelude --- */\n")
		}
		if hdr := cg.buildTUHeader(); hdr != "" {
			// guard 按**内容**计算而不是按 TU 名：内容相同的头天然等价，
			// 用内容哈希也顺带避免"同名 TU 不同内容"被错误去重。
			guard := TUHeaderGuard(hdr)
			tuBlock.WriteString("#ifndef " + guard + "\n#define " + guard + "\n")
			tuBlock.WriteString(hdr)
			tuBlock.WriteString("#endif\n")
		}
		if tuBlock.Len() > 0 {
			allIncludes.WriteString("\n")
			allIncludes.WriteString(tuBlock.String())
		}
	}

	var forwardDecls strings.Builder
	forwardDecls.Grow(1024)

	// 测试模式（F12）：断言辅助函数定义在文件尾部的 runner 块中，
	// 但 #[test] 函数体的调用在其之前，需前向声明
	if cg.TestMode && len(testFunctions) > 0 {
		forwardDecls.WriteString("void __kaula_test_assert(int cond, const char* expr, const char* file, int line);\n")
		forwardDecls.WriteString("void __kaula_test_assert_eq_i64(long long a, long long b, const char* file, int line);\n")
		forwardDecls.WriteString("void __kaula_test_assert_str_eq(const char* a, const char* b, const char* file, int line);\n")
	}
	// conc fn 的 coercion shim 前置声明：顺序调用方（或 spawn 常量特化
	// 插到头部、先于目标 fn 生成的函数）调用 shim 时需要原型，
	// 否则 C 隐式声明（int()）与后面定义冲突。
	for _, em := range cg.concFuncs() {
		if em.CReturnType == "" {
			continue
		}
		// 空参必须写成 `(void)`。
		//
		// C 与 C++ 不同：`f()` 是「参数未指定」，不是「无参数」。
		// 生成 `int64_t K_x_as_fn();` 时 clang 对 `K_x_as_fn(5)` 不作检查，
		// 于是**定义**成为唯一判据 —— 一旦定义与调用点的实参个数不一致，
		// 报错就落到调用行上（too many arguments），而不是声明行，
		// 排查时很容易被引偏。写成 `(void)` 让声明本身就把约束立起来，
		// 有参则逐个写出类型，两种情况都不能省。
		forwardDecls.WriteString(em.CReturnType)
		forwardDecls.WriteString(em.ShimFn())
		forwardDecls.WriteString("(")
		if len(em.Params) == 0 {
			forwardDecls.WriteString("void")
		}
		for i := range em.Params {
			if i > 0 {
				forwardDecls.WriteString(", ")
			}
			if i < len(em.ParamCTypes) {
				forwardDecls.WriteString(em.ParamCTypes[i])
			} else {
				forwardDecls.WriteString("int64_t")
			}
		}
		forwardDecls.WriteString(");\n")
	}
	for _, stmt := range program.Statements {
		if fnStmt, ok := stmt.(*ast.FunctionStatement); ok && fnStmt.Name != "main" {
			// 泛型函数跳过前置声明：原始签名的类型参数 T 无法映射为合法 C 类型，
			// 生成原型只会产生 "unknown type name 'T'"；实际调用点经
			// InstantiateGeneric 单态化为 identity_int 等实例（定义自带原型语义，
			// 位于 genericFuncCode），无需也不应为此处生成原始签名原型。
			if fnStmt.Generic || len(fnStmt.TypeParams) > 0 {
				continue
			}
			// 参数/返回值含自定义类型 (struct/class/enum/type alias/跨文件类型) 的函数
			// 跳过前置声明：这些类型的 typedef 定义在 forwardDecls 之后才输出，
			// 若先生成原型会导致 C 编译报 "unknown type name"。
			needsTypeBeforeDecl := false
			// isUserDefinedType 判断 kaulaType 是否是需要 typedef 在前的自定义类型
			isUserDefinedType := func(kaulaType string) bool {
				base := strings.TrimSuffix(strings.TrimPrefix(kaulaType, "*"), "*")
				tg := cg.typeGenerator
				if tg != nil {
					if tg.structTypes[base] || tg.classTypes[base] ||
						tg.enumTypes[base] || tg.typeAliasTypes[base] {
						return true
					}
				}
				// 跨文件 import 的公共类型：struct/class/enum/type alias 均在
				// 另一个 .kl 生成的 typeCode 中定义，若当前文件 forwardDecls 先生成
				// 就会引用未定义类型，同样需要跳过前置声明。
				if cg.localPubTypes != nil {
					if _, ok := cg.localPubTypes[base]; ok {
						return true
					}
				}
				return false
			}
			for _, pType := range fnStmt.ParamTypes {
				if isUserDefinedType(pType) {
					needsTypeBeforeDecl = true
					break
				}
			}
			if !needsTypeBeforeDecl && fnStmt.ReturnType != "" && fnStmt.ReturnType != "void" {
				if isUserDefinedType(fnStmt.ReturnType) {
					needsTypeBeforeDecl = true
				}
			}
			if needsTypeBeforeDecl {
				continue
			}
			returnType := tgReturnTypeToC(cg.typeGenerator, fnStmt.ReturnType)
			// export 函数的定义带 KAULA_EXPORT，前置声明必须同样携带，
			// 否则 clang 报 "redeclaration cannot add dllexport attribute"
			if fnStmt.IsExported {
				forwardDecls.WriteString("KAULA_EXPORT ")
			}
			forwardDecls.WriteString(returnType)
			forwardDecls.WriteByte(' ')
			forwardDecls.WriteString(cg.UserFuncCName(fnStmt.Name))
			forwardDecls.WriteByte('(')
			for i, pType := range fnStmt.ParamTypes {
				if i > 0 {
					forwardDecls.WriteString(", ")
				}
				isNullable := false
				if i < len(fnStmt.ParamNullables) {
					isNullable = fnStmt.ParamNullables[i]
				}
				cType := cg.typeGenerator.convertType(pType, isNullable)
				if cType == "" {
					cType = "void*"
				}
				// 函数指针类型参数：R (*)(args) name → R (*name)(args)
				// 数组类型参数：T[N] name → T name[N]（C 要求声明符在后）
				forwardDecls.WriteString(formatCVarDecl(cType, fnStmt.Params[i]))
			}
			forwardDecls.WriteString(");\n")
			// TU 拆分：export 函数要被别的 TU 调用，原型必须进 header。
			//
			// 这里刻意复述上面那个"类型未定义则跳过"的判断：命中该分支时
			// forwardDecls 里没有这条原型，header 也不能凭空造一个——真正需要
			// 跨 TU 可见时，类型定义已经在 prelude 里（见 collectTUPrelude），
			// 那时 needsTypeBeforeDecl 为 false，原型会正常生成。
			if cg.tuMode != "" && (fnStmt.IsExported || fnStmt.IsPublic) && !needsTypeBeforeDecl {
				var proto strings.Builder
				if fnStmt.IsExported {
					// export 与 pub 的 C 名相同，但 export 的定义带 KAULA_EXPORT，
					// 原型不带就会被 clang 判为"重声明不能添加 dllexport"。
					proto.WriteString("KAULA_EXPORT ")
				}
				proto.WriteString(returnType)
				proto.WriteByte(' ')
				proto.WriteString(cg.UserFuncCName(fnStmt.Name))
				proto.WriteByte('(')
				for i, pType := range fnStmt.ParamTypes {
					if i > 0 {
						proto.WriteString(", ")
					}
					isNullable := false
					if i < len(fnStmt.ParamNullables) {
						isNullable = fnStmt.ParamNullables[i]
					}
					cType := cg.typeGenerator.convertType(pType, isNullable)
					if cType == "" {
						cType = "void*"
					}
					proto.WriteString(formatCVarDecl(cType, fnStmt.Params[i]))
				}
				proto.WriteString(");")
				cg.tuExportFuncProtos = append(cg.tuExportFuncProtos, proto.String())
			}
		}
	}

	forwardDecls.WriteString(cg.nestedFuncPrototypes.String())

	// #[derive] 派生函数前向声明
	for _, d := range deriveTypes {
		cName := d.TypeName
		for _, trait := range d.Traits {
			trait = strings.TrimSpace(trait)
			switch strings.ToLower(trait) {
			case "debug":
				forwardDecls.WriteString(fmt.Sprintf("void %s_debug(%s* self);\n", d.TypeName, cName))
			case "clone":
				forwardDecls.WriteString(fmt.Sprintf("%s* %s_clone(%s* self);\n", cName, d.TypeName, cName))
			case "eq", "equals":
				forwardDecls.WriteString(fmt.Sprintf("int %s_equals(%s* a, %s* b);\n", d.TypeName, cName, cName))
			}
		}
	}

	// #[command] 解析函数前向声明
	for _, cmd := range commands {
		cName := cmd.StructName
		forwardDecls.WriteString(fmt.Sprintf("%s* %s_parse(int argc, char* argv[]);\n", cName, cmd.StructName))
	}

	cacheDir := "cache"
	if err := os.MkdirAll(cacheDir, 0755); err == nil {
		os.WriteFile(filepath.Join(cacheDir, "all_includes.txt"), []byte(allIncludes.String()), 0644)
	}

	var result string
	var typeOffset, globalOffset, funcOffset, mainOffset int

	// freestanding 模式：始终使用裸机入口模板，无论是否存在 main 函数
	useFreestanding := cg.config != nil && cg.config.Freestanding

	// --boot user：用户态程序入口模板（user_start → kaula_main → sys_exit）
	isUserMode := cg.config != nil && cg.config.Boot == "user"

	/*
	 * TU 拆分：入口归属。
	 *
	 * 被 import 的文件只有函数定义，没有 main —— 若不干预，它们会走下面的
	 * `!hasMain` 模板分支，被套上 `int main() { ... }` 壳（那个壳里的 mainCode
	 * 是空的，会得到一个"空 main"）。拆开后每个定义 TU 都有自己的 main 符号，
	 * 链接期直接撞车；更糟的是 freestanding 模板会生成 _kaula_entry。
	 *
	 * 定义 TU 选择"无入口"：让生成结果停留在 includes + 类型 + 原型 + 函数体。
	 * 入口 TU 则按正常规则（有 main 走模板、无 main 走 main 壳）。
	 */
	emitEntry := !(cg.tuMode == "definition" && cg.config != nil && !cg.config.Freestanding)

	if !emitEntry {
		// 定义 TU：不输出任何入口壳，但保留完整声明/定义节。
		var rb strings.Builder
		rb.WriteString(allIncludes.String())
		rb.WriteString("\n\n")
		rb.WriteString(cg.genericTypeCode.String())
		rb.WriteString(typeCode.String())
		rb.WriteByte('\n')
		rb.WriteString(forwardDecls.String())
		rb.WriteByte('\n')
		rb.WriteString(globalVars.String())
		rb.WriteByte('\n')
		rb.WriteString(classGlobalVars.String())
		rb.WriteByte('\n')
		if cg.deepPromoteCBBlock != "" {
			rb.WriteString(cg.deepPromoteCBBlock)
			rb.WriteByte('\n')
		}
		rb.WriteString(cg.classImplCode.String())
		rb.WriteByte('\n')
		rb.WriteString(functionCode.String())
		if len(deriveTypes) > 0 {
			rb.WriteString(cg.attributeFeat.GenerateDeriveImpls(deriveTypes, program))
		}
		if len(commands) > 0 {
			rb.WriteString(cg.attributeFeat.GenerateCommandParser(commands))
		}
		// 顶层语句（函数外的调用等）：单 TU 时代它们会被注入 main 开头，
		// 定义 TU 没有 main，无法承载 —— 语义阶段本就不允许被 import 的文件
		// 含可执行顶层语句之外的副作用，这里保持"不输出"与旧行为一致
		// （旧行为里 extractFunctionDefs 同样丢弃了这些语句）。
		cg.output = rb.String()
		return cg.output
	}

	if useFreestanding || !hasMain {
		templateName := "main"
		if useFreestanding {
			templateName = "freestanding"
		}
		if isUserMode {
			templateName = "user"
		}
		template, ok := cg.templateManager.GetTemplate(templateName)
		if !ok {
			var resultBuilder strings.Builder
			resultBuilder.Grow(allIncludes.Len() + forwardDecls.Len() + globalVars.Len() + typeCode.Len() + functionCode.Len() + mainCode.Len() + 256)
			resultBuilder.WriteString(allIncludes.String())
			resultBuilder.WriteString("\n\n")
			resultBuilder.WriteString(cg.genericTypeCode.String())
			// 修复: typedef/struct/enum/type alias (typeCode) 必须在函数前置声明
			// (forwardDecls) 之前输出，否则当导出函数签名中含有简单枚举/type alias/
			// 跨文件 struct 等类型时，C 编译器报 "unknown type name"。
			resultBuilder.WriteString(typeCode.String())
			resultBuilder.WriteByte('\n')

			typeOffset = strings.Count(allIncludes.String(), "\n") + 3 + strings.Count(cg.genericTypeCode.String(), "\n") + 1

			resultBuilder.WriteString(forwardDecls.String())
			resultBuilder.WriteByte('\n')

			globalOffset = typeOffset + strings.Count(typeCode.String(), "\n") + 1 + strings.Count(forwardDecls.String(), "\n") + 1

			resultBuilder.WriteString(globalVars.String())
			resultBuilder.WriteByte('\n')
			resultBuilder.WriteString(classGlobalVars.String())
			resultBuilder.WriteByte('\n')

			// 深提升回调（cb）必须在 classImplCode 之前输出：
			// class 方法体（如 MLP_new 的 kmm_v4_scope_promote_p_deep）会使用 cb，
			// 若 cb 定义在后（functionCode 中）将产生 static 先使用后定义错误。
			if cg.deepPromoteCBBlock != "" {
				resultBuilder.WriteString(cg.deepPromoteCBBlock)
				resultBuilder.WriteByte('\n')
			}

			// class 构造器/方法体：依赖 forwardDecls（顶层函数原型）与全局变量，
			// 故置于两者之后、普通函数体之前
			funcOffset = globalOffset + strings.Count(globalVars.String(), "\n") + 1 +
				strings.Count(cg.classImplCode.String(), "\n") + 1
			resultBuilder.WriteString(cg.classImplCode.String())
			resultBuilder.WriteByte('\n')
			resultBuilder.WriteString(functionCode.String())

			// 生成 #[derive] 派生实现（Debug/Clone/Eq）
			if len(deriveTypes) > 0 {
				resultBuilder.WriteString(cg.attributeFeat.GenerateDeriveImpls(deriveTypes, program))
			}

			// 生成 #[command] 命令行参数解析器
			if len(commands) > 0 {
				resultBuilder.WriteString(cg.attributeFeat.GenerateCommandParser(commands))
			}

			// 内联 pkglib 第三方库的 C 源码（Sources 字段指定的文件）
			if cg.stdlibConfig != nil && len(cg.usedThirdPartyLibs) > 0 {
				for _, lib := range cg.stdlibConfig.ThirdParty {
					if !cg.usedThirdPartyLibs[lib.Name] {
						continue
					}
					if lib.SourceDir == "" || len(lib.Sources) == 0 {
						continue
					}
					for _, srcFile := range lib.Sources {
						srcPath := srcFile
						if !filepath.IsAbs(srcPath) {
							srcPath = filepath.Join(lib.SourceDir, srcFile)
						}
						if data, err := os.ReadFile(srcPath); err == nil {
							resultBuilder.WriteString("\n\n/* === pkglib ")
							resultBuilder.WriteString(lib.Name)
							resultBuilder.WriteString(": ")
							resultBuilder.WriteString(srcFile)
							resultBuilder.WriteString(" === */\n")
							resultBuilder.Write(data)
						}
					}
				}
			}

			if useFreestanding {
				// freestanding 模式：不生成 main 函数，入口由 _start 或自定义 entry 提供
				result = resultBuilder.String()
			} else if cg.TestMode && len(testFunctions) > 0 {
				// 测试模式：生成自动测试 runner
				resultBuilder.WriteString(cg.attributeFeat.GenerateTestRunner(testFunctions))
				result = resultBuilder.String()
			} else {
				mainHeader := "\n\nint main() {\n    "
				mainOffset = funcOffset + strings.Count(functionCode.String(), "\n") + strings.Count(mainHeader, "\n")
				resultBuilder.WriteString(mainHeader)
				resultBuilder.WriteString(mainCode.String())
				resultBuilder.WriteString("\n    return 0;\n}\n")
				result = resultBuilder.String()
			}
		} else {
			result = template
			result = strings.ReplaceAll(result, "{{includes}}", allIncludes.String())
			// 修复: 将类型定义 (genericTypeCode + typeCode) 全部放在 forward_decls 之前。
			// 把 forward_decls 占位符内容置空，类型与前置声明合并输出到 type_code 占位符中，
			// 从而保证 typedef/enum/struct/type alias 定义先于引用它们的函数原型。
			result = strings.ReplaceAll(result, "{{forward_decls}}", "")
			result = strings.ReplaceAll(result, "{{global_vars}}", globalVars.String())
			typeCodeWithClassGlobals := typeCode.String()
			if classGlobalVars.Len() > 0 {
				typeCodeWithClassGlobals += "\n" + classGlobalVars.String()
			}
			// 合并顺序: generic 实例化类型 → 本地 typedef → 函数前置声明
			typeAndForward := cg.genericTypeCode.String() +
				typeCodeWithClassGlobals + "\n\n" +
				forwardDecls.String()
			result = strings.ReplaceAll(result, "{{type_code}}", typeAndForward)
			// class 构造器/方法体置于函数体区段最前（已在 forwardDecls/全局变量之后）；
			// 深提升 cb 必须先于 classImplCode 输出（class 方法内会使用 cb，如 MLP_new 的 promote）
			funcAndImpl := cg.deepPromoteCBBlock
			if funcAndImpl != "" {
				funcAndImpl += "\n"
			}
			funcAndImpl += cg.classImplCode.String() + "\n" + functionCode.String()
			// 生成 #[derive] 派生实现
			if len(deriveTypes) > 0 {
				funcAndImpl += cg.attributeFeat.GenerateDeriveImpls(deriveTypes, program)
			}
			// 生成 #[command] 命令行参数解析器
			if len(commands) > 0 {
				funcAndImpl += cg.attributeFeat.GenerateCommandParser(commands)
			}
			result = strings.ReplaceAll(result, "{{function_code}}", funcAndImpl)
			// 测试模式：测试 runner 自带 main。整体替换模板的 main 壳，
			// 避免 runner 的 int main 嵌套进模板 int main() 产生双重 main
			// （C 报 invalid storage class）。
			if cg.TestMode && len(testFunctions) > 0 {
				runner := cg.attributeFeat.GenerateTestRunner(testFunctions)
				if replaced, ok := replaceMainShellWithRunner(result, runner); ok {
					result = replaced
				} else {
					// 自定义模板没有标准 main 壳：去掉占位符后把 runner 追加到文件末尾，
					// 保证它位于文件作用域，而不是被嵌进某个函数体。
					result = strings.ReplaceAll(result, "{{main_code}}", "") + runner
				}
			} else {
				result = strings.ReplaceAll(result, "{{main_code}}", mainCode.String())
			}
			result = strings.ReplaceAll(result, "{{code}}", "")

			idxIncludes := strings.Index(result, allIncludes.String())
			// typeOffset 现在以 cg.genericTypeCode (作为 type_code 最前缀) 定位
			idxType := strings.Index(result, cg.genericTypeCode.String())
			if idxType < 0 {
				idxType = strings.Index(result, typeCode.String())
			}
			idxGlobal := strings.Index(result, globalVars.String())
			idxFunc := strings.Index(result, functionCode.String())
			idxMain := strings.Index(result, mainCode.String())

			typeOffset = strings.Count(result[:idxType], "\n") + 1
			globalOffset = strings.Count(result[:idxGlobal], "\n") + 1
			funcOffset = strings.Count(result[:idxFunc], "\n") + 1
			// freestanding 模板无 {{main_code}} 占位符，idxMain 可能为 -1
			if idxMain >= 0 {
				mainOffset = strings.Count(result[:idxMain], "\n") + 1
			} else {
				mainOffset = funcOffset + strings.Count(functionCode.String(), "\n")
			}
			_ = idxIncludes
		}
	} else {
		var resultBuilder strings.Builder
		resultBuilder.Grow(allIncludes.Len() + cg.genericTypeCode.Len() + forwardDecls.Len() + globalVars.Len() + typeCode.Len() + functionCode.Len() + 16)
		resultBuilder.WriteString(allIncludes.String())
		resultBuilder.WriteString("\n")
		resultBuilder.WriteString(cg.genericTypeCode.String())
		// 修复: typedef/struct/enum/type alias (typeCode) 必须在函数前置声明
		// (forwardDecls) 之前输出，避免导出函数签名含简单枚举/type alias/
		// 跨文件类型时出现 "unknown type name" 编译错误。
		// 同时 typeCode 仍然在 globalVars 之前 (extern 声明同样需要类型先定义)
		typeOffset = strings.Count(allIncludes.String(), "\n") + 2 + strings.Count(cg.genericTypeCode.String(), "\n") + 1
		resultBuilder.WriteString(typeCode.String())
		resultBuilder.WriteString("\n\n")

		resultBuilder.WriteString(forwardDecls.String())
		resultBuilder.WriteString("\n")

		globalOffset = typeOffset + strings.Count(typeCode.String(), "\n") + 1 + strings.Count(forwardDecls.String(), "\n") + 2
		resultBuilder.WriteString(globalVars.String())
		resultBuilder.WriteString("\n")

		// class 类型全局变量: 需在 typedef 之后生成
		if classGlobalVars.Len() > 0 {
			resultBuilder.WriteString("\n")
			resultBuilder.WriteString(classGlobalVars.String())
		}

		// 深提升回调（cb）必须在 classImplCode 之前输出：
		// class 方法体（如 MLP_new 的 kmm_v4_scope_promote_p_deep）会使用 cb，
		// 若 cb 定义在后（在 functionCode 中）将产生 static 先使用后定义错误。
		if cg.deepPromoteCBBlock != "" {
			resultBuilder.WriteString(cg.deepPromoteCBBlock)
			resultBuilder.WriteString("\n")
		}

		// class 构造器/方法体：依赖 forwardDecls 与全局变量，先于普通函数体输出
		if cg.classImplCode.Len() > 0 {
			resultBuilder.WriteString("\n")
			resultBuilder.WriteString(cg.classImplCode.String())
			resultBuilder.WriteString("\n")
		}

		funcOffset = globalOffset + strings.Count(globalVars.String(), "\n") + 1
		// class 全局变量初始化 + 顶层可执行语句注入 main 函数体开头 (运行时构造)
		//
		// 顶层语句（tree/prefix 语句、函数外的普通调用等）在"无 fn main"的分支里由
		// 模板壳承载（{{main_code}} / mainHeader）。本分支用户已定义 fn main，没有
		// 该出口，mainCode 此前被整段静默丢弃——写在函数外的语句"编译成功但永不执行"。
		// 这里注入到 main 体开头：class 全局先初始化，随后顶层语句，最后才是用户 main
		// 的函数体，与"无 fn main 时顶层语句即 main 体"的语义保持一致。
		mainFirstLine := 0 // 首条顶层语句在 functionCode 中的行号（源码映射用）
		if classInitCode.Len() > 0 || mainCode.Len() > 0 {
			payload := classInitCode.String()
			if mainCode.Len() > 0 {
				payload += mainCode.String()
			}
			mainFunc := functionCode.String()
			idx := strings.Index(mainFunc, "int main() {")
			if idx < 0 {
				idx = strings.Index(mainFunc, "void main() {")
			}
			if idx >= 0 {
				braceIdx := strings.Index(mainFunc[idx:], "{")
				if braceIdx >= 0 {
					insertAt := idx + braceIdx + 1
					if mainCode.Len() > 0 {
						// 首条顶层语句位于 functionCode 第 (C + nci + 2) 行：
						// "{"行 = C+1，class 初始化占 nci 行，之后才是首条顶层语句；
						// 源码映射约定 functionCode 第 1 行 = funcOffset，故减 1
						linesBeforeBrace := strings.Count(mainFunc[:insertAt], "\n")
						mainFirstLine = funcOffset + linesBeforeBrace +
							strings.Count(classInitCode.String(), "\n") + 1
					}
					mainFunc = mainFunc[:insertAt] + "\n" + payload + mainFunc[insertAt:]
				}
			}
			functionCode.Reset()
			functionCode.WriteString(mainFunc)
		}
		if mainFirstLine > 0 {
			mainOffset = mainFirstLine
		}
		resultBuilder.WriteString(functionCode.String())

		// 生成 #[derive] 派生实现（Debug/Clone/Eq）
		if len(deriveTypes) > 0 {
			resultBuilder.WriteString(cg.attributeFeat.GenerateDeriveImpls(deriveTypes, program))
		}

		// 生成 #[command] 命令行参数解析器
		if len(commands) > 0 {
			resultBuilder.WriteString(cg.attributeFeat.GenerateCommandParser(commands))
		}

		result = resultBuilder.String()
	}

	cg.sourceMap = NewSourceMap(cg.sourceFile, "")
	for _, e := range rawEntries {
		var genLine int
		switch e.section {
		case "type":
			genLine = typeOffset + e.relLine - 1
		case "global":
			genLine = globalOffset + e.relLine - 1
		case "func":
			genLine = funcOffset + e.relLine - 1
		case "main":
			genLine = mainOffset + e.relLine - 1
		}
		if genLine > 0 {
			cg.sourceMap.AddEntry(genLine, cg.sourceFile, e.srcLine, e.srcCol, e.kind, e.symbol)
		}
	}

	// 版本头注释：编译器版本 (version.json) + 项目版本 (kaula.json "version" 字段)。
	// 产物溯源用；两者均缺失时也输出生成时间。
	result = prependVersionBanner(result, cg.config)

	if os.Getenv("KAULAC_DUMP") != "" && cg.sourceFile != "" {
		base := strings.ReplaceAll(filepath.Base(cg.sourceFile), ".kl", "")
		os.WriteFile(filepath.Join(os.TempDir(), "klc_"+base+".out.c"), []byte(result), 0644)
	}

	return result
}

// prependVersionBanner 在生成的 C 代码头部插入版本注释块。
// 版本来源：
//   - 编译器版本：compiler/version.json（经 internal/version 读取）
//   - 项目版本：kaula.json 的 "version" 字段（经 config.Version 传入）
//
// 无论是否命中版本号都输出生成时间，保证产物可溯源。
func prependVersionBanner(code string, cfg *config.Config) string {
	var b strings.Builder
	b.Grow(len(code) + 256)
	b.WriteString("/*\n")
	b.WriteString(" * Generated by kaulac " + version.GetVersion() + "\n")
	if cfg != nil && cfg.Version != "" {
		b.WriteString(" * Project version: " + cfg.Version + "\n")
	}
	b.WriteString(" * Generated at: " + time.Now().Format("2006-01-02 15:04:05") + "\n")
	b.WriteString(" * Do not edit this file directly.\n")
	b.WriteString(" */\n")
	b.WriteString(code)
	return b.String()
}

func (cg *CodeGenerator) generateStatement(stmt ast.Statement) string {
	return cg.statementGenerator.GenerateStatement(stmt)
}

// generateNestedFunctionStatement 生成函数定义语句：位于函数体**内部**的嵌套函数
// 必须提升为文件级定义（C 不允许函数内嵌定义），排队在外层函数定义之后输出；
// 调用点先于定义出现的问题由文件头部的原型声明（nestedFuncPrototypes）解决。
// 位于顶层的函数定义照常就地生成。
//
// pendingNestedFuncs 按"层"隔离：生成本层嵌套函数时收集它内部更深层的嵌套
// 函数，避免内层把外层已排队的定义连同自己的一起清空/错位输出。
func (cg *CodeGenerator) generateNestedFunctionStatement(fn *ast.FunctionStatement) string {
	if fn == nil {
		return ""
	}
	if !cg.inFunctionBody {
		return cg.functionGenerator.GenerateFunctionStatement(fn)
	}
	saved := cg.pendingNestedFuncs
	cg.pendingNestedFuncs = nil
	code := cg.functionGenerator.GenerateFunctionStatement(fn)
	hoisted := append([]string{code}, cg.pendingNestedFuncs...)
	cg.pendingNestedFuncs = append(saved, hoisted...)
	return ""
}

// generateStatementFromStmts 从多个语句生成代码（类构造函数/方法体使用）
func (cg *CodeGenerator) generateStatementFromStmts(stmts []ast.Statement) string {
	var b strings.Builder
	for _, s := range stmts {
		code := cg.generateStatement(s)
		if code != "" {
			b.WriteString(code)
		}
	}
	return b.String()
}

func (cg *CodeGenerator) generateExpression(expr ast.Expression) string {
	return cg.expressionGenerator.GenerateExpression(expr)
}

var indentCache = []string{
	"",
	"    ",
	"        ",
	"            ",
	"                ",
	"                    ",
	"                        ",
	"                            ",
	"                                ",
	"                                    ",
}

func (cg *CodeGenerator) indentString() string {
	if cg.indent < len(indentCache) {
		return indentCache[cg.indent]
	}
	// 超出缓存范围，动态生成
	indent := ""
	for i := 0; i < cg.indent; i++ {
		indent += "    "
	}
	return indent
}

// RegisterPlugin 注册插件
func (cg *CodeGenerator) RegisterPlugin(plugin Plugin) {
	cg.pluginManager.RegisterPlugin(plugin)
}

// EnterScope 进入一个新的作用域
// 如果 SOR 适配器启用，同时注册作用域 ID 映射
func (cg *CodeGenerator) EnterScope(scopeName string) {
	newScope := symbol.NewSymbolTable(cg.currentScope, scopeName)
	cg.currentScope = newScope
	cg.scopeDepth++
	// 注册 SOR 作用域 ID 映射
	if cg.sorAdapter != nil && cg.sorAdapter.IsActive {
		cg.sorAdapter.RegisterScope(scopeName)
	}
}

// ExitScope 退出当前作用域
func (cg *CodeGenerator) ExitScope() {
	if cg.currentScope != cg.symbolTable {
		cg.currentScope = cg.currentScope.GetParent()
	}
	if cg.scopeDepth > 0 {
		cg.scopeDepth--
	}
}

// GetCurrentScope 获取当前作用域
func (cg *CodeGenerator) GetCurrentScope() *symbol.SymbolTable {
	return cg.currentScope
}

// AddSymbol 添加一个符号到当前作用域
func (cg *CodeGenerator) AddSymbol(name, symbolType string, nullable bool, scope string, line, column int) {
	// 修复 #25：在首次添加符号时记录其声明时的作用域深度
	if _, ok := cg.declScopeDepth[name]; !ok {
		cg.declScopeDepth[name] = cg.scopeDepth
	}
	// 形参登记：形参指向的内存属于调用方，写穿形参即把对象交回调用方，
	// 因此形参不能算"函数局部"（区分内部 promote / 向外 promote 的判据之一）。
	if scope == "parameter" {
		cg.paramNames[name] = true
	}
	cg.currentScope.AddSymbol(name, symbolType, nullable, scope, line, column)
}

// IsFunctionLocalVar 报告变量是否确定为本函数体内声明的局部变量。
// 判据：本函数内声明过（declScopeDepth 有记录）**且**不是形参。
// 全局、形参、外部符号一律返回 false——它们指向的内存生命周期跨越函数边界，
// 写到那里的对象在函数返回后仍然存活，不能随 survivor 段一起回卷。
func (cg *CodeGenerator) IsFunctionLocalVar(name string) bool {
	if name == "" {
		return false
	}
	if cg.paramNames[name] {
		return false
	}
	_, ok := cg.declScopeDepth[name]
	return ok
}

// MarkPromoteHolder 登记持有 promote 幸存副本的变量（赋值的目标侧）。
func (cg *CodeGenerator) MarkPromoteHolder(name string) {
	if name != "" {
		cg.promoteHolders[name] = true
	}
}

// IsPromoteHolder 查询变量是否持有 promote 出来的幸存副本。
// return 判定用它：返回值若是幸存副本的持有者，rewind 会回卷掉该副本 → 悬垂。
func (cg *CodeGenerator) IsPromoteHolder(name string) bool {
	if name == "" {
		return false
	}
	return cg.promoteHolders[name]
}

// ResetFunctionLocalState 清空 per-function 的形参与 promote 持有者登记。
// 两者都是纯粹的 per-function 语义，函数切换时必须清掉，否则上一个函数的
// 形参名会被下一个函数误判为"非局部"（漏掉内部 promote 回收），或把幸存副本
// 持有者的身份带到无关函数上（无谓地抑制 rewind）。
// 注意：declScopeDepth **不**清理——它是既有的跨阶段结构，清理会改变
// 深度判定既有行为；同一个名字在新函数里若重新声明，AddSymbol 的
// "首次才记录"语义已保证不会把形参误记为局部。
func (cg *CodeGenerator) ResetFunctionLocalState() {
	cg.paramNames = make(map[string]bool)
	cg.promoteHolders = make(map[string]bool)
	cg.safeLocalContainers = make(map[string]bool)
	cg.writtenThroughContainers = make(map[string]bool)
	cg.chainLinkedVars = make(map[string]bool)
	cg.chainEscapingVars = make(map[string]bool)
	cg.escapedVars = make(map[string]bool)
}

// SetSafeLocalContainers 以预扫描结果替换当前函数的「安全局部容器」集合（方案 A'）。
// 由 GenerateFunctionStatement 在生成函数体之前调用（此时形参尚未登记，
// 集合来自纯词法 AST 遍历，与符号表无关）。
func (cg *CodeGenerator) SetSafeLocalContainers(names map[string]bool) {
	if names == nil {
		cg.safeLocalContainers = make(map[string]bool)
		return
	}
	cg.safeLocalContainers = names
}

// SetWrittenThroughContainers 记录当前函数内被写穿过的「安全局部容器」名集合。
// 与 SetSafeLocalContainers 一同由函数入口的预扫描结果填好。
func (cg *CodeGenerator) SetWrittenThroughContainers(names map[string]bool) {
	if names == nil {
		cg.writtenThroughContainers = make(map[string]bool)
		return
	}
	cg.writtenThroughContainers = names
}

// IsSafeLocalContainer 报告 name 是否为「安全局部容器」（方案 A'）。
// 只有集合内的容器，其写穿点才允许"不 promote + 抑制循环回卷"。
func (cg *CodeGenerator) IsSafeLocalContainer(name string) bool {
	if name == "" {
		return false
	}
	return cg.safeLocalContainers[name]
}

// IsWrittenThroughContainer 报告 name 是否为「被写穿过的安全局部容器」（方案 A'）。
// 游标推进（cur = nd）据此判断：只有该游标所在的链曾发生写穿，才随链一起
// 放弃 promote，避免把"纯游标推进"（keep = p，已由 c983678 修好）误伤。
func (cg *CodeGenerator) IsWrittenThroughContainer(name string) bool {
	if name == "" {
		return false
	}
	return cg.writtenThroughContainers[name]
}

// SetChainLinkedVars 记录被写穿进安全局部容器的源对象名集合。
func (cg *CodeGenerator) SetChainLinkedVars(names map[string]bool) {
	if names == nil {
		cg.chainLinkedVars = make(map[string]bool)
		return
	}
	cg.chainLinkedVars = names
}

// IsChainLinked 报告 name 是否为「已挂在链上的对象」（方案 A'）。
// 这类对象的 early free 必须豁免：对象由容器持有，游标不再引用并不代表它死了。
func (cg *CodeGenerator) IsChainLinked(name string) bool {
	if name == "" {
		return false
	}
	return cg.chainLinkedVars[name]
}

// SetChainEscapingVars 记录「写穿容器的链首被带出函数」的容器名集合。
//
// 与 ChainLinked 的区别：ChainLinked 说的是**挂进去的对象**（nd）不能提前释放；
// ChainEscaping 说的是**容器本身**（cur）不能提前释放——因为 cur 指向的
// 正是链首节点，而 `i64 head = as<i64>(cur); ...; return head` 把链首
// 交了出去。见 SafeContainerScan.ChainEscaping 的注释。
func (cg *CodeGenerator) SetChainEscapingVars(names map[string]bool) {
	if names == nil {
		cg.chainEscapingVars = make(map[string]bool)
		return
	}
	cg.chainEscapingVars = names
}

// IsChainEscaping 报告 name 是否为「链首已随返回值离开函数」的写穿容器。
//
// 这类容器的 early free 必须豁免：释放它就等于释放链首节点，
// 调用方/后续读取会踩空（n 小时 slab 未耗尽所以不暴露，n 大时必崩）。
func (cg *CodeGenerator) IsChainEscaping(name string) bool {
	if name == "" {
		return false
	}
	return cg.chainEscapingVars[name]
}

// SetEscapedVars 记录当前函数内逃逸过的变量名集合（含别名闭包传播）。
func (cg *CodeGenerator) SetEscapedVars(names map[string]bool) {
	if names == nil {
		cg.escapedVars = make(map[string]bool)
		return
	}
	cg.escapedVars = names
}

// IsEscapedVar 报告 name 是否在本函数内逃逸（return / 实参 / 别名传播等）。
// 写穿 promote 的外向/内部分类用它：容器逃逸 → 副本必须活过函数返回。
func (cg *CodeGenerator) IsEscapedVar(name string) bool {
	if name == "" {
		return false
	}
	return cg.escapedVars[name]
}

// MarkSuppressLoopRewind 打点：当前循环体内出现了「写穿安全局部容器」的点，
// 该循环必须放弃 mark_restore（方案 A'）。
func (cg *CodeGenerator) MarkSuppressLoopRewind() {
	cg.suppressLoopRewind = true
}

// ResetSuppressLoopRewind 清除打点。进入一个循环体前调用，避免上一个循环的
// 打点污染下一个循环。
func (cg *CodeGenerator) ResetSuppressLoopRewind() {
	cg.suppressLoopRewind = false
}

// hasSuppressLoopRewind 查询当前循环体的打点状态。
func (cg *CodeGenerator) hasSuppressLoopRewind() bool {
	return cg.suppressLoopRewind
}

// GetSymbol 获取一个符号
func (cg *CodeGenerator) GetSymbol(name string) *symbol.Symbol {
	return cg.currentScope.GetSymbol(name)
}

// HasSymbol 检查是否存在符号
func (cg *CodeGenerator) HasSymbol(name string) bool {
	return cg.currentScope.HasSymbol(name)
}

// GetSymbolType 获取符号的 Kaula 类型字符串（修复 #25：promote 判定用）
func (cg *CodeGenerator) GetSymbolType(name string) string {
	if s := cg.currentScope.GetSymbol(name); s != nil {
		return s.Type
	}
	return ""
}

// GetVarDeclaredScopeDepth 查找变量声明所在的作用域深度（结构化所有权基础层）。
// 从当前作用域向上遍历，找到第一个 HasLocalSymbol(name) 的作用域，返回其 depth。
// 未找到返回 -1（全局/外部符号）。这是纯词法分析，不依赖 SOR 数据流。
func (cg *CodeGenerator) GetVarDeclaredScopeDepth(name string) int {
	if name == "" {
		return -1
	}
	for scope := cg.currentScope; scope != nil; scope = scope.GetParent() {
		if scope.HasLocalSymbol(name) {
			return scope.GetScopeDepth()
		}
	}
	return -1
}

// IsVarDeclaredInCurrentScope 判断变量是否在当前词法作用域声明（结构化所有权基础层）。
// 用于 return promote 判定：只有当前块声明的指针变量返回时才需要 promote。
func (cg *CodeGenerator) IsVarDeclaredInCurrentScope(name string) bool {
	if name == "" || cg.currentScope == nil {
		return false
	}
	return cg.currentScope.HasLocalSymbol(name)
}

// recordAllocSizeHint 从初始化表达式登记编译期可知的堆分配常量大小（修复 #25）
// 匹配 kmm_v4_alloc_auto(<常量>) 模式（std_malloc 已在表达式生成阶段重写为它）
func (cg *CodeGenerator) recordAllocSizeHint(varName, initValue string) {
	if varName == "" || initValue == "" {
		return
	}
	idx := strings.Index(initValue, "kmm_v4_alloc_auto(")
	if idx < 0 {
		return
	}
	digits := make([]byte, 0, 16)
	for i := idx + len("kmm_v4_alloc_auto("); i < len(initValue); i++ {
		c := initValue[i]
		if c >= '0' && c <= '9' {
			digits = append(digits, c)
			continue
		}
		break
	}
	if len(digits) == 0 {
		return
	}
	n := 0
	for _, d := range digits {
		n = n*10 + int(d-'0')
	}
	if n > 0 {
		cg.sorInitSizes[varName] = n
	}
}

// GetLocalSymbol 获取当前作用域中的符号
func (cg *CodeGenerator) GetLocalSymbol(name string) *symbol.Symbol {
	return cg.currentScope.GetLocalSymbol(name)
}

// HasLocalSymbol 检查当前作用域是否存在符号
func (cg *CodeGenerator) HasLocalSymbol(name string) bool {
	return cg.currentScope.HasLocalSymbol(name)
}

// MangleGenericName 生成泛型实例化后的 C 函数名。
// 规则：kaula_<funcName>_<mangled_typeargs>，类型参数中的非字母数字字符
// 转义为 _<codepoint>_ 以保证生成合法的 C 标识符。
// 例：identity<int>      → kaula_identity_int
//
//	identity<[]int>    → kaula_identity__91__93_int
func MangleGenericName(funcName string, typeArgs []string) string {
	name := "kaula_" + funcName + "_"
	for i, arg := range typeArgs {
		if i > 0 {
			name += "_"
		}
		for _, ch := range arg {
			if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
				name += string(ch)
			} else {
				name += fmt.Sprintf("_%d_", ch)
			}
		}
	}
	return name
}

// MaxGenericInstantiationDepth 泛型实例化深度上限，防止无限递归实例化
// （如 fn f<T>(T x) { f<[]T>([x]) } 会无限展开）
const MaxGenericInstantiationDepth = 32

// InstantiateGeneric 实例化泛型函数
func (cg *CodeGenerator) InstantiateGeneric(funcName string, typeArgs []string, line int) (string, error) {
	// 深度限制：防止无限递归实例化
	if cg.genericInstDepth >= MaxGenericInstantiationDepth {
		return "", fmt.Errorf("generic instantiation depth limit (%d) exceeded at %s<%s> (line %d): possible infinite recursion",
			MaxGenericInstantiationDepth, funcName, strings.Join(typeArgs, ","), line)
	}

	// 生成缓存键
	cacheKey := funcName + "<"
	for i, arg := range typeArgs {
		if i > 0 {
			cacheKey += ","
		}
		cacheKey += arg
	}
	cacheKey += ">"

	// 检查缓存
	if cached, ok := cg.genericCache[cacheKey]; ok {
		return cached.GeneratedCode, nil
	}

	// 生成实例化后的函数名: kaula_max_int64 (添加 kaula_ 前缀避免与 C 宏冲突)
	instName := MangleGenericName(funcName, typeArgs)

	if cg.genericInstantiated[instName] {
		return "", nil // 已经实例化过
	}

	// 获取原始函数
	program := cg.getProgram()
	if program == nil {
		return "", fmt.Errorf("cannot find program for generic instantiation")
	}

	fnStmt := program.FindFunction(funcName)
	if fnStmt == nil || !fnStmt.IsGeneric() {
		return "", fmt.Errorf("function %s is not generic", funcName)
	}

	// 创建实例化后的函数（复制并替换类型参数）
	instFunc, typeMap := cg.instantiateGenericFunction(fnStmt, typeArgs, instName)

	// 设置活跃类型映射，使函数体内的类型参数引用（T、[]T、*T 等）
	// 在 convertType 时被替换为具体类型。生成后恢复，避免污染其他函数。
	// 同时递增实例化深度，防止无限递归实例化。
	// 状态隔离：实例化可能发生在任意外层函数体生成中途（调用点触发），
	// 此时 kmmScopeDepth/indent 等 per-function 易变状态反映的是外层函数；
	// 若不归零，generateReturnStatement 会按外层 depth 补发孤立的
	// kmm_v4_scope_pop()，运行期 KMM 作用域栈下溢直接 abort。
	savedScopeDepth := cg.kmmScopeDepth
	savedEmitsPromote := cg.funcEmitsPromote
	savedHasCheckpoint := cg.funcHasKMMCheckpoint
	savedIndent := cg.indent
	cg.kmmScopeDepth = 0
	cg.funcEmitsPromote = false
	cg.funcHasKMMCheckpoint = false
	cg.indent = 0

	oldTypeMap := cg.typeGenerator.PushActiveTypeMap(typeMap)
	cg.genericInstDepth++

	/*
	 * TU 拆分：泛型实例只能有一个定义。
	 *
	 * 同一实例可能被多个 TU 同时实例化（A 的 fn 调 identity<i32>、main 也调），
	 * 每个 TU 各写一份函数体 → 重复定义；只让"用到它的那个 TU"定义，
	 * 又会让别的 TU 缺原型。归属由入口侧统一决策（TUGenericOwner），
	 * 非归属方只输出原型、不生成函数体。
	 *
	 * 这里**仍然**做一次 GenerateFunctionStatement 来拿到原型之外的类型推导
	 * 副作用是多余的，所以归属判定放在生成之前；非归属方走原型分支时
	 * 必须保持 genericCache/genericInstantiated 的语义（后续调用直接命中缓存）。
	 */
	ownsInstance := cg.tuMode == "" || cg.config == nil || cg.config.TUGenericOwner == nil ||
		cg.config.TUGenericOwner(instName)

	var code string
	if ownsInstance {
		code = cg.functionGenerator.GenerateFunctionStatement(instFunc)
	} else {
		code = ""
	}
	cg.genericInstDepth--
	cg.typeGenerator.PopActiveTypeMap(oldTypeMap)

	cg.kmmScopeDepth = savedScopeDepth
	cg.funcEmitsPromote = savedEmitsPromote
	cg.funcHasKMMCheckpoint = savedHasCheckpoint
	cg.indent = savedIndent

	// 写入泛型实例化代码缓冲区，稍后插入到 functionCode 之前
	if code != "" {
		cg.genericFuncCode.WriteString(code)
		cg.genericFuncCode.WriteByte('\n')
	}
	// 归属记录：定义者登记到"本 TU 定义的实例"，非归属方登记为 deferred 原型。
	// 非归属方仍保留 cache（避免重复实例化），但 GeneratedCode 为空 ——
	// 调用方 TU 的原型由入口写入的 prelude/header 提供。
	if cg.tuMode != "" {
		if ownsInstance {
			cg.tuGenericsLocalDefined = append(cg.tuGenericsLocalDefined, instName)
		} else {
			cg.tuGenericsDeferred = append(cg.tuGenericsDeferred, deferredGeneric{
				CName: instName,
				Prot:  genericPrototype(instName, instFunc, cg.typeGenerator),
			})
		}
	}

	// 添加到缓存
	cg.genericCache[cacheKey] = &GenericInstanceCache{
		OriginalName:   funcName,
		TypeArguments:  typeArgs,
		GeneratedCode:  code,
		InstantiatedAt: line,
	}
	cg.genericInstantiated[instName] = true

	return code, nil
}

// instantiateGenericFunction 创建泛型函数的实例化版本，返回实例化函数及类型参数映射
func (cg *CodeGenerator) instantiateGenericFunction(fnStmt *ast.FunctionStatement, typeArgs []string, instName string) (*ast.FunctionStatement, map[string]string) {
	// 创建类型参数映射：T -> int（Kaula 类型名，由 convertType 进一步映射为 C 类型）
	typeMap := make(map[string]string)
	for i, tp := range fnStmt.TypeParams {
		if i < len(typeArgs) {
			typeMap[tp.Name] = typeArgs[i]
		}
	}

	// 实例化返回类型
	returnType := fnStmt.ReturnType
	if mappedType, ok := typeMap[returnType]; ok {
		returnType = mappedType
	}

	// 创建新的函数语句
	instFunc := &ast.FunctionStatement{
		Name:       instName,
		Params:     make([]string, len(fnStmt.Params)),
		Body:       fnStmt.Body,
		ReturnType: returnType,
		Generic:    false,
		NoKMM:      fnStmt.NoKMM,
		Inline:     fnStmt.Inline,
		Annotation: fnStmt.Annotation,
	}

	// 复制参数名
	copy(instFunc.Params, fnStmt.Params)

	// 复制参数可空标记
	instFunc.ParamNullables = make([]bool, len(fnStmt.ParamNullables))
	copy(instFunc.ParamNullables, fnStmt.ParamNullables)

	// 映射参数类型
	instFunc.ParamTypes = make([]string, len(fnStmt.ParamTypes))
	for i, pt := range fnStmt.ParamTypes {
		instFunc.ParamTypes[i] = pt
		if mappedType, ok := typeMap[pt]; ok {
			instFunc.ParamTypes[i] = mappedType
		}
	}

	return instFunc, typeMap
}

// getProgram 获取程序 AST（简化实现，实际需要从编译器获取）
func (cg *CodeGenerator) getProgram() *ast.Program {
	return cg.program
}

// MangleGenericTypeName 生成泛型类型实例化后的 C 类型名（不含 K_ 前缀）。
// 规则：baseName_<mangled_typeargs>，类型参数中的非字母数字字符
// 转义为 _<codepoint>_ 以保证生成合法的 C 标识符。
// 例：Box<int>           → Box_int
//
//	Result<int, string> → Result_int_string
//	Pair<[]int, *T>     → Pair__91__93_int__42_T
func MangleGenericTypeName(baseName string, typeArgs []string) string {
	name := baseName
	for _, arg := range typeArgs {
		name += "_"
		for _, ch := range arg {
			if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
				name += string(ch)
			} else {
				name += fmt.Sprintf("_%d_", ch)
			}
		}
	}
	return name
}

// InstantiateGenericType 实例化泛型类型（类/结构体/枚举），返回 C 类型名（含 K_ 前缀）。
// 如 Box<int> → K_Box_int，同时生成对应的 C 类型定义写入 genericTypeCode 缓冲区。
// 机制：找到泛型类型定义，构建类型参数映射（T→具体类型），复制 AST 节点并清除 Generic
// 标记，设置 activeTypeMap 后调用常规 Generate*Statement 路径生成实例化代码。
func (cg *CodeGenerator) InstantiateGenericType(typeName string, typeArgs []string, line int) (string, error) {
	// 深度限制：防止无限递归实例化（如 struct Node<T> { Node<T>* next; }）
	if cg.genericTypeInstDepth >= MaxGenericInstantiationDepth {
		return "", fmt.Errorf("generic type instantiation depth limit (%d) exceeded at %s<%s> (line %d): possible infinite recursion",
			MaxGenericInstantiationDepth, typeName, strings.Join(typeArgs, ","), line)
	}

	// 生成缓存键
	cacheKey := typeName + "<"
	for i, arg := range typeArgs {
		if i > 0 {
			cacheKey += ","
		}
		cacheKey += arg
	}
	cacheKey += ">"

	// 检查缓存
	if cached, ok := cg.genericTypeCache[cacheKey]; ok {
		return cached, nil
	}

	if cg.program == nil {
		return "", fmt.Errorf("program not set for generic type instantiation")
	}

	// 查找泛型类型定义（类/结构体/枚举），构建类型参数映射和生成器
	var typeParams []*ast.TypeParameter
	var fields []*ast.FieldDeclaration
	needSynthCtor := false
	var generate func(instName string) string
	isClassInst := false

	if classStmt := cg.program.FindClass(typeName); classStmt != nil && classStmt.Generic {
		isClassInst = true
		typeParams = classStmt.TypeParams
		fields = classStmt.Fields
		// 无显式构造器的泛型类：同样需要合成位置构造器
		needSynthCtor = len(classStmt.Constructors) == 0
		generate = func(instName string) string {
			inst := *classStmt // 浅拷贝：Fields/Methods/Constructors 共享（只读）
			inst.Name = instName
			inst.Generic = false
			inst.TypeParams = nil
			return cg.typeGenerator.GenerateClassStatement(&inst)
		}
	} else if structStmt := cg.program.FindStruct(typeName); structStmt != nil && structStmt.Generic {
		typeParams = structStmt.TypeParams
		fields = structStmt.Fields
		// struct 从不生成用户构造器，一律合成位置构造器，
		// 使 Box<int>(42) 之类构造调用有定义可链
		needSynthCtor = true
		generate = func(instName string) string {
			inst := *structStmt
			inst.Name = instName
			inst.Generic = false
			inst.TypeParams = nil
			return cg.typeGenerator.GenerateStructStatement(&inst)
		}
	} else if enumStmt := cg.program.FindEnum(typeName); enumStmt != nil && enumStmt.Generic {
		typeParams = enumStmt.TypeParams
		generate = func(instName string) string {
			inst := *enumStmt
			inst.Name = instName
			inst.Generic = false
			inst.TypeParams = nil
			return cg.typeGenerator.GenerateEnumStatement(&inst)
		}
	} else {
		return "", fmt.Errorf("generic type %s not found or not generic", typeName)
	}

	// 构建类型参数映射：T -> 具体类型（Kaula 类型名，由 MapKaulaTypeToC 进一步映射为 C 类型）
	typeMap := make(map[string]string)
	for i, tp := range typeParams {
		if i < len(typeArgs) {
			typeMap[tp.Name] = typeArgs[i]
		}
	}

	instName := MangleGenericTypeName(typeName, typeArgs)
	cName := kaulaStructTag(instName)

	// 递归环保护：必须在生成**之前**登记缓存。
	// 单态化过程中处理字段/方法/构造器时，可能再次遇到同一实例的请求
	// （例如方法签名里残留的类型参数被解析回 `Box<T>`）。若在生成结束后才写缓存，
	// 嵌套请求会查不到命中而再次展开同一实例——表现为同一个
	// `typedef struct K_Box_i32` 被重复输出几十次，clang 报 redefinition。
	cg.genericTypeCache[cacheKey] = cName

	// 设置活跃类型映射，使字段/方法/构造函数中的类型参数引用被替换为具体类型。
	// 生成后恢复，避免污染其他类型。同时递增实例化深度，防止无限递归实例化。
	// 状态隔离同 InstantiateGeneric：实例化可能在外层函数/类型的生成中途触发，
	// 需归零 per-function 状态（kmmScopeDepth 等）防止孤立 scope_pop 注入。
	// currentFunctionReturnType 同样必须隔离：实例化过程中会生成构造器/方法体，
	// 它们各自设置并在结束时复位为 ""。若不保存，外层函数（触发实例化的那个）
	// 在实例化之后遇到的 `return <expr>` 会因返回类型丢失退化成裸 `return;`，
	// 生成物里表现为 clang 报 non-void function 缺返回值。
	savedScopeDepth := cg.kmmScopeDepth
	savedEmitsPromote := cg.funcEmitsPromote
	savedHasCheckpoint := cg.funcHasKMMCheckpoint
	savedIndent := cg.indent
	savedReturnType := cg.currentFunctionReturnType
	cg.kmmScopeDepth = 0
	cg.funcEmitsPromote = false
	cg.funcHasKMMCheckpoint = false
	cg.indent = 0

	oldTypeMap := cg.typeGenerator.PushActiveTypeMap(typeMap)
	cg.typeGenerator.structTypes[instName] = true
	if isClassInst {
		// class 是引用语义：变量声明要靠 classTypes 命中才退化为指针（`K_Box_i32*`）。
		// instName（“Box_i32”）由 GenerateClassStatement 自行登记；这里额外登记
		// 用户在源码里书写的形态（“Box<i32>”）——变量声明节点的 Type 未经改写，
		// 只认这一种拼写。
		cg.typeGenerator.classTypes[instName] = true
		cg.typeGenerator.classTypes[typeName+"<"+strings.Join(typeArgs, ",")+">"] = true
	}
	cg.genericTypeInstDepth++

	code := generate(instName)

	// 合成位置构造器 instName_new(fields...)：按字段顺序初始化，
	// 未提供实参的尾部字段保持零值。必须在 activeTypeMap 窗口内完成
	// 字段类型的映射。函数体自包含，写入 classImplCode 延迟段。
	synthCtor := ""
	if needSynthCtor && len(fields) > 0 {
		var b strings.Builder
		b.WriteString(fmt.Sprintf("static inline %s* %s_new(", cName, instName))
		for i, f := range fields {
			if i > 0 {
				b.WriteString(", ")
			}
			ft := cg.typeGenerator.convertType(f.Type, f.Nullable)
			if ft == "" {
				ft = "void*"
			}
			b.WriteString(fmt.Sprintf("%s %s", ft, f.Name))
		}
		b.WriteString(") {\n")
		b.WriteString(fmt.Sprintf("    %s* self = KMM_V4_ALLOC_ZERO(%s);\n", cName, cName))
		b.WriteString("    if (self == NULL) { return NULL; }\n")
		n := len(typeArgs)
		if n > len(fields) {
			n = len(fields)
		}
		for i := 0; i < n; i++ {
			b.WriteString(fmt.Sprintf("    self->%s = %s;\n", fields[i].Name, fields[i].Name))
		}
		b.WriteString("    return self;\n")
		b.WriteString("}\n")
		synthCtor = b.String()
	}

	cg.genericTypeInstDepth--
	cg.typeGenerator.PopActiveTypeMap(oldTypeMap)

	cg.kmmScopeDepth = savedScopeDepth
	cg.funcEmitsPromote = savedEmitsPromote
	cg.funcHasKMMCheckpoint = savedHasCheckpoint
	cg.indent = savedIndent
	cg.currentFunctionReturnType = savedReturnType

	// 写入泛型类型实例化代码缓冲区，稍后前置注入到 typeCode 之前
	if code != "" {
		cg.genericTypeCode.WriteString(code)
		if !strings.HasSuffix(code, "\n") {
			cg.genericTypeCode.WriteByte('\n')
		}
	}
	if synthCtor != "" {
		cg.classImplCode.WriteString(synthCtor)
		if !strings.HasSuffix(synthCtor, "\n") {
			cg.classImplCode.WriteByte('\n')
		}
	}

	return cName, nil
}

// findFunctionByName 在程序中查找函数声明
func (cg *CodeGenerator) findFunctionByName(name string) *ast.FunctionStatement {
	if cg.program == nil {
		return nil
	}
	for _, stmt := range cg.program.Statements {
		if fnStmt, ok := stmt.(*ast.FunctionStatement); ok {
			if fnStmt.Name == name {
				return fnStmt
			}
		}
	}
	return nil
}

// findPrefixStatement 在程序中查找 prefix 语句（递归搜索）
func (cg *CodeGenerator) findPrefixStatement(name string) *ast.PrefixStatement {
	if cg.program == nil {
		return nil
	}
	return cg.findPrefixInStatements(cg.program.Statements, name)
}

// PrefixParamBinding 前缀参数的一次绑定：实参生成的 C 表达式及其类型提示
type PrefixParamBinding struct {
	Code string // 实参生成的 C 表达式
	Kind string // exprgen 类型提示："s"/"d"/"f"/"cstr"/"obj"（推导失败为空）
}

// pushPrefixBindings 推入一层前缀参数绑定（generatePrefixCallBody 展开时调用）
func (cg *CodeGenerator) pushPrefixBindings(bindings map[string]PrefixParamBinding) {
	cg.prefixBindings = append(cg.prefixBindings, bindings)
}

// popPrefixBindings 弹出最内层前缀参数绑定
func (cg *CodeGenerator) popPrefixBindings() {
	if len(cg.prefixBindings) > 0 {
		cg.prefixBindings = cg.prefixBindings[:len(cg.prefixBindings)-1]
	}
}

// lookupPrefixBinding 自内向外查找 $参数 绑定
func (cg *CodeGenerator) lookupPrefixBinding(name string) (PrefixParamBinding, bool) {
	for i := len(cg.prefixBindings) - 1; i >= 0; i-- {
		if b, ok := cg.prefixBindings[i][name]; ok {
			return b, true
		}
	}
	return PrefixParamBinding{}, false
}

// findPrefixInStatements 递归搜索 statements 中的 prefix 语句
func (cg *CodeGenerator) findPrefixInStatements(stmts []ast.Statement, name string) *ast.PrefixStatement {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.PrefixStatement:
			if s.Name == name {
				return s
			}
		case *ast.FunctionStatement:
			if result := cg.findPrefixInStatements(s.Body, name); result != nil {
				return result
			}
		case *ast.IfStatement:
			if result := cg.findPrefixInStatements(s.Body, name); result != nil {
				return result
			}
			if result := cg.findPrefixInStatements(s.Else, name); result != nil {
				return result
			}
		case *ast.WhileStatement:
			if result := cg.findPrefixInStatements(s.Body, name); result != nil {
				return result
			}
		case *ast.ForStatement:
			if result := cg.findPrefixInStatements(s.Body, name); result != nil {
				return result
			}
		case *ast.ForInStatement:
			if result := cg.findPrefixInStatements(s.Body, name); result != nil {
				return result
			}
		case *ast.BlockStatement:
			if result := cg.findPrefixInStatements(s.Statements, name); result != nil {
				return result
			}
		}
	}
	return nil
}

// IsStructType 检查指定名称是否是已定义的结构体类型
func (cg *CodeGenerator) IsStructType(name string) bool {
	if cg.program == nil {
		return false
	}
	for _, stmt := range cg.program.Statements {
		if structStmt, ok := stmt.(*ast.StructStatement); ok {
			if structStmt.Name == name {
				return true
			}
		}
	}
	return false
}

// IsEnumType 检查指定名称是否是已定义的枚举类型
func (cg *CodeGenerator) IsEnumType(name string) bool {
	if cg.program == nil {
		return false
	}
	for _, stmt := range cg.program.Statements {
		if enumStmt, ok := stmt.(*ast.EnumStatement); ok {
			if enumStmt.Name == name {
				return true
			}
		}
	}
	return false
}

// findClassStmt 查找类定义：优先查主程序 AST，其次查本地 import 的类型。
// 支持跨文件类（import "ml/nn.kl"）的字段类型推断与成员访问解析。
func (cg *CodeGenerator) findClassStmt(name string) *ast.ClassStatement {
	if cg.program != nil {
		if stmt := cg.program.FindClass(name); stmt != nil {
			return stmt
		}
	}
	if info := cg.localPubTypes[name]; info != nil && info.Class != nil {
		return info.Class
	}
	return nil
}

// findInterfaceStmt 查找接口定义（主程序 AST；跨文件 import 的接口暂未映射）。
func (cg *CodeGenerator) findInterfaceStmt(name string) *ast.InterfaceStatement {
	if cg.program != nil {
		if stmt := cg.program.FindInterface(name); stmt != nil {
			return stmt
		}
	}
	return nil
}

// IsInterfaceType 检查指定名称是否是已定义的接口类型。
func (cg *CodeGenerator) IsInterfaceType(name string) bool {
	if cg.typeGenerator != nil && cg.typeGenerator.interfaceTypes[name] {
		return true
	}
	return cg.findInterfaceStmt(name) != nil
}

// IsClassType 检查指定名称是否是已定义的类类型（含已实例化的泛型类：如 Box_int）
func (cg *CodeGenerator) IsClassType(name string) bool {
	if cg.program == nil && len(cg.localPubTypes) == 0 {
		return false
	}
	// 跨文件 import 的 class(SetLocalImportTypes 注册): 类定义在库文件的 AST 中,
	// 主 program.FindClass 查不到, 直接按 classTypes 注册放行
	if cg.typeGenerator != nil && cg.typeGenerator.classTypes[name] {
		return true
	}
	// 首先尝试直接查找（非泛型或已实例化的泛型类型在 typeGenerator.structTypes 中注册）
	if cg.typeGenerator.structTypes[name] && cg.findClassStmt(name) != nil {
		return true
	}
	// 查 AST 中的类定义
	if cg.findClassStmt(name) != nil {
		return true
	}
	return false
}

// isUserKaulaFunc 判断 name 是否是本仓库用户自定义的 Kaula 函数
// （本文件 fn，含嵌套 fn；或跨文件 import 的本仓库函数）。
// extern fn、stdlib/第三方 C 函数、内置运算符均返回 false。
func (cg *CodeGenerator) isUserKaulaFunc(name string) bool {
	if name == "" || name == "main" {
		return false
	}
	if cg.userFuncNames != nil && cg.userFuncNames[name] {
		return true
	}
	if cg.localImportFuncs != nil && cg.localImportFuncs[name] {
		return true
	}
	return false
}

// UserFuncCName 返回 Kaula 用户函数在 C 侧的符号名：
//   - 用户自定义 fn 保持原名（不再加 K_ 前缀：定义端与调用端统一用原名，
//     避免 export 定义端裸名、调用端 K_ 名的跨文件不一致）；
//   - 程序中显式 extern 声明的函数保持原名（用户引入的 C 符号，F15：
//     此前 max/min/abs 重定向先于 extern 判定，extern fn abs 调用被改
//     成 kaula_abs 与声明符号不一致）；
//   - 非用户定义的 max/min/abs 重定向到 kaula 运行时实现（kaula_max 等），避开 C 宏；
//   - main、stdlib/第三方 C 函数保持原名。
//
// 函数定义、前置声明、嵌套原型、调用点必须统一经此函数命名，保证定义与引用一致。
func (cg *CodeGenerator) UserFuncCName(name string) string {
	if name == "" || name == "main" {
		return name
	}
	if cg.isUserKaulaFunc(name) {
		return name
	}
	// 显式 extern 声明优先于 max/min/abs 惯用名重定向（F15）
	if cg.externFuncNames != nil && cg.externFuncNames[name] {
		return name
	}
	if name == "max" || name == "min" || name == "abs" {
		return "kaula_" + name
	}
	return name
}

// GetGenericCachedCode 获取缓存的泛型代码
func (cg *CodeGenerator) GetGenericCachedCode(funcName string, typeArgs []string) (string, bool) {
	cacheKey := funcName + "<"
	for i, arg := range typeArgs {
		if i > 0 {
			cacheKey += ","
		}
		cacheKey += arg
	}
	cacheKey += ">"

	if cached, ok := cg.genericCache[cacheKey]; ok {
		return cached.GeneratedCode, true
	}
	return "", false
}

func getStmtPos(stmt ast.Statement) *ast.Position {
	if stmt == nil {
		return nil
	}
	v := reflect.ValueOf(stmt)
	if v.Kind() == reflect.Ptr && v.IsNil() {
		return nil
	}
	switch s := stmt.(type) {
	case *ast.IfStatement:
		return &s.Pos
	case *ast.WhileStatement:
		return &s.Pos
	case *ast.ForStatement:
		return &s.Pos
	case *ast.ForInStatement:
		return &s.Pos
	case *ast.ReturnStatement:
		return &s.Pos
	case *ast.ExpressionStatement:
		return &s.Pos
	case *ast.SpendStatement:
		return &s.Pos
	case *ast.PrefixStatement:
		return &s.Pos
	case *ast.TreeStatement:
		return &s.Pos
	case *ast.ObjectStatement:
		return &s.Pos
	case *ast.YieldStatement:
		return &s.Pos
	case *ast.ReleaseStatement:
		return &s.Pos
	case *ast.ExtractStatement:
		return &s.Pos
	case *ast.BreakStatement:
		return &s.Pos
	case *ast.ContinueStatement:
		return &s.Pos
	case *ast.VariableDeclaration:
		return &s.Pos
	case *ast.FunctionStatement:
		return &s.Pos
	case *ast.ClassStatement:
		return &s.Pos
	case *ast.InterfaceStatement:
		return &s.Pos
	case *ast.StructStatement:
		return &s.Pos
	case *ast.EnumStatement:
		return &s.Pos
	case *ast.TypeAliasStatement:
		return &s.Pos
	case *ast.CallStatement:
		return &s.Pos
	case *ast.NonLocalStatement:
		return &s.Pos
	case *ast.BlockStatement:
		return &s.Pos
	}
	return nil
}

// tryEvalConstExpr 尝试在编译期求值常量表达式
// 支持：整数/浮点字面量、其他 const 引用、基本算术运算（+ - * / % << >> & | ^）
// 返回求值后的字面量字符串，无法求值时返回空字符串
func (cg *CodeGenerator) tryEvalConstExpr(expr ast.Expression) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		return strconv.FormatUint(e.Value, 10)
	case *ast.FloatLiteral:
		return formatFloatLiteral(e.Value)
	case *ast.BooleanLiteral:
		if e.Value {
			return "1"
		}
		return "0"
	case *ast.Identifier:
		// 引用其他 const 变量
		if val, ok := cg.constTable[e.Name]; ok {
			return val
		}
		return ""
	case *ast.BinaryExpression:
		left := cg.tryEvalConstExpr(e.Left)
		right := cg.tryEvalConstExpr(e.Right)
		if left == "" || right == "" {
			return ""
		}
		return cg.evalBinaryOp(e.Operator, left, right)
	case *ast.ParenExpression:
		return cg.tryEvalConstExpr(e.Inner)
	}
	return ""
}

// evalBinaryOp 执行编译期二元运算
func (cg *CodeGenerator) evalBinaryOp(op, left, right string) string {
	// 尝试整数运算
	lval, lerr := strconv.ParseInt(left, 0, 64)
	rval, rerr := strconv.ParseInt(right, 0, 64)
	if lerr == nil && rerr == nil {
		switch op {
		case "+":
			return strconv.FormatInt(lval+rval, 10)
		case "-":
			return strconv.FormatInt(lval-rval, 10)
		case "*":
			return strconv.FormatInt(lval*rval, 10)
		case "/":
			if rval == 0 {
				return ""
			}
			return strconv.FormatInt(lval/rval, 10)
		case "%":
			if rval == 0 {
				return ""
			}
			return strconv.FormatInt(lval%rval, 10)
		case "<<":
			return strconv.FormatInt(lval<<uint(rval), 10)
		case ">>":
			return strconv.FormatInt(lval>>uint(rval), 10)
		case "&":
			return strconv.FormatInt(lval&rval, 10)
		case "|":
			return strconv.FormatInt(lval|rval, 10)
		case "^":
			return strconv.FormatInt(lval^rval, 10)
		}
	}
	// 浮点运算
	lf, lfErr := strconv.ParseFloat(left, 64)
	rf, rfErr := strconv.ParseFloat(right, 64)
	if lfErr == nil && rfErr == nil {
		switch op {
		case "+":
			return formatFloatLiteral(lf + rf)
		case "-":
			return formatFloatLiteral(lf - rf)
		case "*":
			return formatFloatLiteral(lf * rf)
		case "/":
			if rf == 0 {
				return ""
			}
			return formatFloatLiteral(lf / rf)
		}
	}
	return ""
}

// inferExportedType 根据字面量为无类型声明的导出常量/变量推断 Kaula 类型
func inferExportedType(value ast.Expression) string {
	switch v := value.(type) {
	case *ast.IntegerLiteral:
		if v == nil {
			return ""
		}
		if v.Value <= 255 {
			return "u8"
		}
		if v.Value <= 65535 {
			return "u16"
		}
		if v.Value <= 4294967295 {
			return "u32"
		}
		return "u64"
	case *ast.FloatLiteral:
		return "f64"
	case *ast.StringLiteral:
		return "string"
	case *ast.CharLiteral:
		return "char"
	case *ast.BooleanLiteral:
		return "bool"
	}
	return ""
}
