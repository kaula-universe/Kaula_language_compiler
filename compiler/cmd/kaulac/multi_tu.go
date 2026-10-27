package main

// 多翻译单元（multi-TU）构建：把每个被 import 的 .kl 拆成独立 C TU 并行编译。
//
// # 背景
//
// 历史上的做法是"一个大 C 文件"：主程序生成一份 C，每个被 import 的 .kl
// 各生成一份 C，然后用 extractFunctionDefs（按大括号计数）把"看起来像函数"
// 的片段抠出来拼进主 C。这条路径有两个结构性问题：
//
//  1. 抠取是启发式的。类型定义、extern 声明、文件作用域 static 函数、
//     static 全局变量、泛型实例、「不形似函数但合法的声明」统统被丢掉，
//     只能靠一条条注释打补丁（_fltused 重复定义、_kaula_entry 重复定义、
//     跨文件 typedef 不可见 ...）。每加一个语言特性就要再补一次。
//  2. C 编译器只能看到一个巨大的 TU，无从并行；任何一个导入文件的改动
//     都会让整份 C 重编。
//
// # 现在的做法
//
// 每个 TU 都是一份"完整合法的 C 源"（自己的 include / 类型 / 原型 / 函数体），
// 由 C 编译器独立编译成 .o，最后一次性链接。跨 TU 的可见性由三样东西保证：
//
//   - prelude：跨文件类型定义前置到每个 TU（typedef 允许重定义的前提是
//     定义完全一致，而它们确实来自同一份生成结果）；
//   - header ：每个定义 TU 的接口（extern 变量 / 泛型实例原型 / static
//     函数原型），带内容哈希的 include guard；
//   - 泛型实例归属：同一个实例只允许一个 TU 出定义（见 genericOwnerIndex）。
//
// # 与单 TU 路径共用参数
//
// 这里不自己拼 clang 参数，而是复用单 TU 路径的拼装结果（compileCCode 以
// "dry-run" 模式产出）：-I/-D/-O/-target/库列表逐字相同。手抄一份 flags
// 迟早会漂移，而 KMM 池大小、目标三元组这类宏一旦两边不一致，报错会晚到
// 链接期、且信息与真实原因毫不相干。
//
// # 并行度与确定性
//
// 并行度沿用既有约定：std 模块预编译用 runtime.NumCPU()，这里同样按 CPU
// 数开 worker（上限 16，可用 KAULAC_TU_JOBS 覆盖便于复现）。确定性由
// "不共享可变状态 + 结果集合排序"保证：每个 TU 写自己的 .c/.o（文件名 =
// 源名 + 路径哈希，互不重叠），没有任何全局计数器；失败信息收集后按文件名
// 排序输出，与哪个 goroutine 先失败无关。

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

const (
	// envTUJobs 并行度覆盖（测试与压测用）
	envTUJobs = "KAULAC_TU_JOBS"
	// entryTUBasename 入口 TU 的基名（与 prepareMultiTU 的命名保持一致）
	entryTUBasename = "main.kl.tu"
	// tuMaxJobs 并行上限：CPU 再多，收益也被 clang 的启动开销与峰值内存吃掉
	tuMaxJobs = 16
)

// tuUnit 一个待编译的翻译单元。Src 为空表示源已在 tuDir/<Name>.c 中。
type tuUnit struct {
	Name string
	Src  string
}

// tuFailure 单个 TU 的失败信息
type tuFailure struct {
	Name   string
	Detail string
}

// materializeMultiTU 把多 TU 的全部 C 源落盘到一个独立目录，返回该目录。
//
// 目录名按"全部源内容 + 编译器构建标识"哈希：同一份输入反复构建复用同一
// 目录（writeTUSources 内容未变则跳过写，避免 mtime 抖动），不同输入互不
// 干扰，也不会出现两个并发 kaulac 互相覆盖对方的 TU。
func materializeMultiTU(b *multiTUBuild) (string, error) {
	names := make([]string, 0, len(b.Sources))
	for name := range b.Sources {
		names = append(names, name)
	}
	sort.Strings(names)
	h := sha256.New()
	h.Write([]byte(compilerBuildStamp()))
	h.Write([]byte{0})
	b.WriteFingerprint(h)
	dir := filepath.Join(compilerTempDir(), "kaula-tu", hex.EncodeToString(h.Sum(nil))[:16])

	units := make([]tuUnit, 0, len(names))
	for _, name := range names {
		units = append(units, tuUnit{Name: strings.TrimSuffix(name, ".c"), Src: b.Sources[name]})
	}
	if err := writeTUSources(dir, units); err != nil {
		return "", err
	}
	return dir, nil
}

// WriteFingerprint 把构建输入（全部 C 源）写入 h，供调用方派生子目录名。
// 内容而非路径入哈希：换了目录但内容相同的构建可以复用同一份 TU 目录。
func (b *multiTUBuild) WriteFingerprint(h interface{ Write([]byte) (int, error) }) {
	names := make([]string, 0, len(b.Sources))
	for name := range b.Sources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write([]byte(b.Sources[name]))
		h.Write([]byte{0})
	}
}

// tuWorkers 返回本次并行编译的 worker 数。
func tuWorkers(n int) int {
	if raw := os.Getenv(envTUJobs); raw != "" {
		if v, err := parsePositiveInt(raw); err == nil && v > 0 {
			return v
		}
	}
	cpu := runtime.NumCPU()
	if cpu < 1 {
		cpu = 1
	}
	if cpu > tuMaxJobs {
		cpu = tuMaxJobs
	}
	if n > 0 && n < cpu {
		return n
	}
	return cpu
}

// parsePositiveInt 只接受纯十进制正整数（避免 "4x" 之类被静默接受）。
func parsePositiveInt(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not a number: %q", s)
		}
		n = n*10 + int(r-'0')
		if n > 1<<20 {
			return 0, fmt.Errorf("too large: %q", s)
		}
	}
	return n, nil
}

// splitLinkInputs 把单 TU 的完整 clang 参数拆成三段：
//
//	compileFlags：编译每个 TU 需要的 flags（-O/-g/-I/-D/-fwrapv ...）
//	entryInput  ：入口源的输入描述（`-x c -` 或 `-x c <path>`）
//	linkInputs  ：链接期专属输入（`-x none` 之后的 .o、-l、-L、库文件等）
//
// 划分依据是 `-x none`：单 TU 路径用它"重置 clang 对后续输入的语言判定"，
// 因此它之前是编译器侧（flags + 入口输入）、之后是链接器侧。
//
// **入口输入必须单独返回**：早先的实现把 `-x none` 之前的一切都当成
// compileFlags，入口的 `-x c -` 混在里面，进了链接命令行就成了裸 `-`，
// 链接报 "clang: error: -E or -x required when input is from standard input"。
// 只靠 linkFlagsForLink 事后过滤是脆弱的——它得猜哪个 `-` 是输入、哪个是
// 选项，判错一次就整条链接失败。这里一次性切开，链接侧拿到的东西里
// 根本不可能出现输入描述。
func splitLinkInputs(args []string) (linkInputs, entryInput, compileFlags []string) {
	var rest []string
	for i, a := range args {
		if a == "-x" && i+1 < len(args) && args[i+1] == "none" {
			linkInputs = append([]string(nil), args[i+2:]...)
			rest = append([]string(nil), args[:i]...)
			break
		}
	}
	if rest == nil {
		// 没有 `-x none`：整条都是编译器侧，链接只加对象文件
		rest = append([]string(nil), args...)
	}
	from, to := entryInputIndex(rest)
	if from < 0 {
		return linkInputs, nil, rest
	}
	compileFlags = append(append([]string(nil), rest[:from]...), rest[to:]...)
	entryInput = append([]string(nil), rest[from:to]...)
	return linkInputs, entryInput, compileFlags
}

// entryInputIndex 在编译 flags 里定位"入口源"占位符的位置。
//
// 单 TU 路径用 `-x c -` 从 stdin 读源，多 TU 要把它换成每个单元自己的
// 文件路径。这里返回 `-`（或它前面的 `-x c`）中需要被替换的下标区间，
// 其余 flags 原样保留——包括 `-o <exe>`（链接阶段才用）。
func entryInputIndex(flags []string) (from, to int) {
	for i, a := range flags {
		if a != "-" {
			continue
		}
		// 形如 -x c -：连 -x c 一起换掉，避免残留 -x c 把 .c 输入判错语言
		if i >= 2 && flags[i-2] == "-x" {
			return i - 2, i + 1
		}
		return i, i + 1
	}
	return -1, -1
}

// compileArgsForTU 由"入口编译 flags"生成某个 TU 的编译参数：
// 去掉 -o 及其值、把入口源占位符换成该 TU 的源路径。
func compileArgsForTU(flags []string, src, obj string) []string {
	out := make([]string, 0, len(flags)+5)
	for i := 0; i < len(flags); i++ {
		a := flags[i]
		if a == "-o" {
			i++ // 丢弃 -o 与其值：编译期输出由 -o <obj> 单独给
			continue
		}
		out = append(out, a)
	}
	// 输入描述统一放到末尾：-x c 位置敏感，紧跟它的必须是本 TU 的源。
	// 放在末尾而不是原位置，是为了不依赖 flags 里恰好有一个入口占位符。
	out = append(out, "-x", "c", src, "-c", "-o", obj)
	return out
}

// execFn 执行一个编译器命令（测试可替换）
type execFn func(dir string, args []string) (string, error)

// execCmd 生产环境执行器：与单 TU 路径共用 compilerCommand（工具链环境一致）。
func execCmd(dir string, args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("empty command")
	}
	cmd := compilerCommand(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// compileTUsParallel 并行编译所有 TU，返回对象文件列表（已排序）。
//
// 语义保证：
//   - 每个 TU 只写自己的 .o（名字由 Name 决定），互不干扰；
//   - 全部 TU 都会被尝试（不因第一个失败就提前返回），错误一次报全；
//   - 返回的 objs 顺序与输入顺序无关（排序后），链接命令行因此完全确定。
func compileTUsParallel(units []tuUnit, flags []string, tuDir, objDir string, cmdPrefix []string, jobs int, exec execFn) (objs []string, failures []tuFailure) {
	if jobs < 1 {
		jobs = 1
	}
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, jobs)
	)
	for _, u := range units {
		u := u
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			// 源统一在 writeTUSources 之后从磁盘读：Src 非空时它已经被写进
			// tuDir/<Name>.c（内容相同则跳过写），两种来源指向同一个文件。
			// 之前这里按 Src 是否为空二选一，入口 TU 带着源码进来却去读一个
			// 尚未落盘的路径，报错信息是 clang 把整段源码当成文件名。
			src := filepath.Join(tuDir, u.Name+".c")
			obj := filepath.Join(objDir, u.Name+".o")
			args := append([]string(nil), cmdPrefix...)
			args = append(args, compileArgsForTU(flags, src, obj)...)
			out, err := exec(tuDir, args)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				detail := out
				if strings.TrimSpace(detail) == "" {
					detail = err.Error()
				}
				failures = append(failures, tuFailure{Name: u.Name, Detail: detail})
				return
			}
			objs = append(objs, obj)
		}()
	}
	wg.Wait()
	sort.Slice(failures, func(i, j int) bool { return failures[i].Name < failures[j].Name })
	sort.Strings(objs)
	return objs, failures
}

// writeTUSources 把内存中的 TU 源落盘。
// 内容未变则跳过写：避免频繁重编时 mtime 抖动引发下游（缓存/增量）误判。
func writeTUSources(tuDir string, units []tuUnit) error {
	if err := os.MkdirAll(tuDir, 0755); err != nil {
		return err
	}
	for _, u := range units {
		if u.Src == "" {
			continue
		}
		path := filepath.Join(tuDir, u.Name+".c")
		if old, err := os.ReadFile(path); err == nil && string(old) == u.Src {
			continue
		}
		if err := os.WriteFile(path, []byte(u.Src), 0644); err != nil {
			return fmt.Errorf("写入 TU 源失败 (%s): %w", path, err)
		}
	}
	return nil
}

// formatTUFailures 把并发失败汇总成一条稳定的错误（已按文件名排序）。
func formatTUFailures(total int, failures []tuFailure) error {
	if len(failures) == 0 {
		return nil
	}
	// 自行排序：调用方（compileTUsParallel）虽然也排序，但"报错顺序与
	// goroutine 完成顺序无关"是这个函数对外承诺的一部分，不能靠上游的
	// 顺手排好。少一个依赖，就少一处会随重构静默退化的地方。
	sorted := append([]tuFailure(nil), failures...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	var b strings.Builder
	fmt.Fprintf(&b, "multi-TU: %d/%d 个翻译单元编译失败", len(failures), total)
	for _, f := range sorted {
		fmt.Fprintf(&b, "\n--- %s ---\n%s", f.Name, strings.TrimRight(f.Detail, "\n"))
	}
	return fmt.Errorf("%s", b.String())
}

// loadTUUnits 从工作目录列出已落盘的定义 TU，并登记入口 TU（源码从缓存文件读）。
func loadTUUnits(tuDir, entryCFile string) ([]tuUnit, error) {
	entries, err := os.ReadDir(tuDir)
	if err != nil {
		return nil, err
	}
	var units []tuUnit
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tu.c") {
			continue
		}
		units = append(units, tuUnit{Name: strings.TrimSuffix(e.Name(), ".c")})
	}
	if entryCFile != "" {
		data, err := os.ReadFile(entryCFile)
		if err != nil {
			return nil, err
		}
		units = append(units, tuUnit{Name: entryTUBasename, Src: string(data)})
	}
	sort.Slice(units, func(i, j int) bool { return units[i].Name < units[j].Name })
	return units, nil
}

// runMultiTU 多 TU 构建的执行核心：写源 → 并行编译 → 链接。
//
// entryArgs 是单 TU 路径拼好的完整 clang 参数（`-x c - -o <exe> ... [-x none <链接输入>]`）：
//   - `-x none` 之前：编译 flags，每个 TU 复用；
//   - `-x none` 之后：链接输入（std/.o/库），链接时追加；
//   - 其中的 `-o <exe>` 在多 TU 下只对链接有意义，编译期被丢弃。
//
// cmdPrefix 是 ccache 之类的前缀（命令名必须在最前），cc 是前缀之后的真实编译器。
func runMultiTU(entryArgs []string, cc string, cmdPrefix []string, cacheFile, outputFile, workDir, tuDir string) error {
	linkInputs, _, compileFlags := splitLinkInputs(entryArgs)

	// 链接参数由"编译 flags 去掉输入描述"得到：
	//   - 去掉 `-o <exe>`：输出路径在链接命令行末尾单独给定；
	//   - 去掉 `-x c <src>`：`-x c` 会让链接器把 `.o` 当成 C 源码解析，
	//     报 "source file is not valid UTF-8"（二进制当然不是 UTF-8）。
	// 保留 -O/-g/-I/-D/-target/-fuse-ld 等对链接同样有效的 flags。
	linkFlags := linkFlagsForLink(compileFlags)

	units, err := loadTUUnits(tuDir, cacheFile)
	if err != nil {
		return fmt.Errorf("multi-TU: 读取 TU 失败: %w", err)
	}
	if len(units) == 0 {
		return fmt.Errorf("multi-TU: %s 下没有可编译的 TU", tuDir)
	}
	objDir := filepath.Join(tuDir, "obj")
	// 清理上一轮对象：目录名由源内容哈希决定，内容变了就是新目录，本来
	// 不会有残留；但同一目录在"内容相同、只改了 -D"时会复用，旧的 .o 与
	// 本次 flags 不匹配。全清最省心，代价只是重编（本来也要重编）。
	if err := os.RemoveAll(objDir); err != nil {
		return fmt.Errorf("multi-TU: 清理对象目录失败 (%s): %w", objDir, err)
	}
	if err := os.MkdirAll(objDir, 0755); err != nil {
		return fmt.Errorf("multi-TU: 创建对象目录失败 (%s): %w", objDir, err)
	}
	// 先落盘再编译：入口 TU 的源在这里才写进 tuDir（cacheFile 是单 TU 时代的
	// "主 C 缓存文件"，多 TU 下它就是入口 TU 的落盘位置），不先写就会出现
	// "编译一个还不存在/还是旧内容的文件"。
	if err := writeTUSources(tuDir, units); err != nil {
		return err
	}

	prefix := append([]string(nil), cmdPrefix...)
	prefix = append(prefix, cc)
	if verboseMode {
		fmt.Printf("[Multi-TU] %d 个 TU，并行度 %d\n", len(units), tuWorkers(len(units)))
	}
	objs, failures := compileTUsParallel(units, compileFlags, tuDir, objDir, prefix, tuWorkers(len(units)), execCmd)
	if err := formatTUFailures(len(units), failures); err != nil {
		return err
	}

	entryObj := filepath.Join(objDir, entryTUBasename+".o")
	if _, statErr := os.Stat(entryObj); statErr != nil {
		return fmt.Errorf("multi-TU: 入口对象缺失 (%s): %w", entryObj, statErr)
	}
	rest := make([]string, 0, len(objs))
	for _, o := range objs {
		if o != entryObj {
			rest = append(rest, o)
		}
	}

	linkArgs := append([]string(nil), prefix...)
	linkArgs = append(linkArgs, linkFlags...)
	linkArgs = append(linkArgs, "-o", outputFile)
	linkArgs = append(linkArgs, entryObj)
	linkArgs = append(linkArgs, rest...)
	linkArgs = append(linkArgs, linkInputs...)

	out, err := execCmd(workDir, linkArgs)
	if err != nil {
		return fmt.Errorf("multi-TU: 链接失败: %v\n%s", err, strings.TrimRight(out, "\n"))
	}
	if verboseMode {
		fmt.Printf("[Multi-TU] 链接完成: %s\n", outputFile)
	}
	return nil
}

// linkFlagsForLink 从编译 flags 里去掉"输入描述"（-x <lang> + 输入路径 + -o <out>），
// 只留下对链接也成立的 flags。
func linkFlagsForLink(flags []string) []string {
	/*
	 * 目标：把"编译期才成立的输入描述"从链接命令行里彻底剔除。
	 *
	 * 需要剔除的形态：
	 *   1) -x c <input>      入口源（input 可能是 `-`，也可能是落盘的 .c 路径）
	 *   2) 单独的 `-`        入口源的 stdin 占位符
	 *   3) -include-pch <p>  预编译头（链接期输入是 .o，带上就报 -E/-x required）
	 *
	 * 关键点：`-` 本来就以 - 开头，**不能**用"是否以 - 开头"判断它是选项
	 * 还是输入。早先正是这么写的，裸 `-` 漏进链接命令行，报
	 * "clang: error: -E or -x required when input is from standard input"。
	 *
	 * 另一个坑：真实命令行里 `-x c` 与它的输入之间**可能夹着别的选项**
	 * （本项目的 -include-pch 就插在那里），所以不能用"下一个 token 是输入"
	 * 一次性消费；改成 pendingInput 状态：-x 之后遇到选项则继续挂着，
	 * 直到碰上第一个输入 token（`-` 或非选项）为止。
	 */
	var out []string
	pendingInput := false
	for i := 0; i < len(flags); i++ {
		a := flags[i]
		if pendingInput && isTUInputToken(a) {
			// 这就是 -x 的输入：丢掉，状态复位
			pendingInput = false
			continue
		}
		switch {
		case a == "-o":
			i++ // -o <exe>：链接输出单独给
		case a == "-x" && i+1 < len(flags) && isCLangName(flags[i+1]):
			i++ // 吃掉语言名
			pendingInput = true
		case a == "-":
			// 入口源的 stdin 占位符：无论是否还有 pendingInput 都是输入
			pendingInput = false
		case strings.HasPrefix(a, "-include-pch"):
			// -include-pch <p> / -include-pch=<p>
			if a == "-include-pch" && i+1 < len(flags) {
				i++
			}
		default:
			out = append(out, a)
		}
	}
	return out
}

// isTUInputToken 判断 token 是否是"输入文件"而不是选项。
// 单独一个 `-` 表示从 stdin 读，属于输入。
func isTUInputToken(tok string) bool {
	return tok == "-" || !strings.HasPrefix(tok, "-")
}

// isCLangName 判断 token 是否是 clang `-x` 认可的语言名。
//
// 只列本项目会出现的几种：多 TU 拆分只用 `-x c`，链接分界用 `-x none`。
// 保守起见不认未知 token —— 宁可把 `-x` 当普通选项留下，也不能把紧随的
// 参数误吞成语言名。
func isCLangName(tok string) bool {
	switch tok {
	case "c", "none", "c-header":
		return true
	}
	return false
}

// dropOutputFlags 去掉 `-o <path>` / `-o<path>`：多 TU 的编译期输出由每个
// 单元自己给，链接期输出单独追加，entryArgs 里的 -o 不能带进这两处。
func dropOutputFlags(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-o" {
			i++
			continue
		}
		if strings.HasPrefix(a, "-o") && len(a) > 2 {
			continue
		}
		out = append(out, a)
	}
	return out
}
