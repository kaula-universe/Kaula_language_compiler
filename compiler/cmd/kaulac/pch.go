package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ============================================================================
// PCH（预编译头）的编译器家族适配
//
// 背景：kaulac 把 kaula.h 预编译成 kaula.h.gch 供生成 TU 复用，省掉每个 TU
// 各解析一遍重量级运行时头（KMM V4 / 并发内核）的开销。这套机制原先只按
// clang 的语法实现：
//
//	生成：clang -x c-header -c kaula.h -o kaula.h.gch <flags>
//	消费：-include-pch kaula.h.gch   （显式点名 .gch，插在输入之前）
//
// GCC 完全不认这两个形态：
//  1) -include-pch 不是 GCC 的选项。GCC 会把它拆成 -include + "-pch"，
//     报 "<command-line>: fatal error: -pch: No such file or directory"。
//  2) GCC 的 PCH 查找是**随 #include 的搜索路径**逐目录进行的：每个目录里
//     先找 <name>.h.gch，命中且有效才用它，否则按普通头文件继续找。
//     没有"直接点名一个 .gch"的用法。
//
// 因此 GCC 侧的适配是"搜索路径"形态：把 .gch 所在目录用 -I 插到**最前**，
// 让 #include "kaula.h" 先命中缓存目录里的 kaula.h.gch。
//
// 这个形态还有一个意外之喜：**失效时不会把构建打挂**。实测（GCC 13）：
// PCH 与当前命令行语义不匹配（缺 -D、宏值不同）时，GCC 只是跳过该 .gch、
// 按普通头文件继续编译（-Winvalid-pch 下给一条 warning）；而 clang 遇到
// 语义不符是硬错误（"signed integer overflow handling differs in PCH file
// vs. current file"）。所以 GCC 侧可以安全地把缓存目录排在最前，判活失效
// 的代价只是一次普通头解析，不是构建失败。
//
// 但"跳过无效 .gch"只覆盖**语义无效**；格式不兼容（拿 clang 的 .gch 喂
// gcc，或反过来）时 clang 是致命错误、gcc 是静默跳过。所以 .gch 的判活
// 必须记录**生成它的编译器身份**（路径 + --version 指纹），不能只比 mtime。
// ============================================================================

// ccFamily C 编译器家族。决定 PCH 的生成/消费语法。
type ccFamily int

const (
	// ccFamilyClang：-include-pch <file> 显式消费 .gch。
	// 同时是**默认值**：历史上只有 clang 路径，未知编译器保持原行为，
	// 不被换到未验证的语法上。
	ccFamilyClang ccFamily = iota
	// ccFamilyGCC：无 -include-pch；依赖 #include 搜索路径中的 .gch。
	ccFamilyGCC
)

func (f ccFamily) String() string {
	if f == ccFamilyGCC {
		return "gcc"
	}
	return "clang"
}

// ccInfo 一次编译器探测的结论。
type ccInfo struct {
	family ccFamily
	// versionKey 是 --version 输出的摘要。进 PCH 判活指纹：同一路径下的
	// 编译器升级（clang 18 → 19）会改 .gch 的 AST 格式，旧 .gch 对 clang
	// 是致命错误，必须靠它失配重编。探测失败时为空串（见 detectCCInfo）。
	versionKey string
}

var (
	ccFamilyCacheMu sync.Mutex
	ccFamilyCache   = map[string]ccInfo{}
)

// detectCCInfo 探测 ccPath 的家族与版本指纹（带进程内缓存）。
//
// 判定顺序刻意为之：
//  1. 先跑 --version。名字不体现家族的 cc / /usr/bin/cc 只能问编译器本人。
//  2. 名字含 "clang" → clang，优先于探测输出。覆盖 clang / clang-18 /
//     clang-cl，以及 clang2gcc 这类"吃 clang 命令行、翻译给 gcc"的翻译器
//     ——它们的 --version 输出带 gcc，但参数语法是 clang 侧的。
//  3. --version 失败（不存在的编译器、不支持该选项）时按名字兜底。
//  4. 全部落空 → clang（默认，见 ccFamilyClang 注释）。
func detectCCInfo(ccPath string) ccInfo {
	ccFamilyCacheMu.Lock()
	defer ccFamilyCacheMu.Unlock()
	if info, ok := ccFamilyCache[ccPath]; ok {
		return info
	}
	info := probeCCInfo(ccPath)
	ccFamilyCache[ccPath] = info
	return info
}

func probeCCInfo(ccPath string) ccInfo {
	base := strings.ToLower(filepath.Base(ccPath))
	nameSaysClang := strings.Contains(base, "clang")
	// 走 compilerCommand 是为了与其它 C 工具链子进程一样注入可写临时目录。
	if out, err := compilerCommand(ccPath, "--version").CombinedOutput(); err == nil {
		versionKey := hex.EncodeToString(sha256Sum(out))[:12]
		if nameSaysClang {
			// 名字优先：clang2gcc 等翻译器按 clang 判，版本指纹仍记录。
			return ccInfo{family: ccFamilyClang, versionKey: versionKey}
		}
		if f, ok := classifyCCVersionOutput(string(out)); ok {
			return ccInfo{family: f, versionKey: versionKey}
		}
		return ccInfo{family: ccFamilyClang, versionKey: versionKey}
	}
	// 探测失败：按名字兜底（versionKey 留空，指纹退化为不含版本）。
	if nameSaysClang {
		return ccInfo{family: ccFamilyClang}
	}
	if strings.Contains(base, "gcc") || base == "cc" || base == "c99" || base == "c89" {
		return ccInfo{family: ccFamilyGCC}
	}
	return ccInfo{family: ccFamilyClang}
}

// detectCCFamily 只要家族结论（探测/缓存与 detectCCInfo 同源）。
func detectCCFamily(ccPath string) ccFamily {
	return detectCCInfo(ccPath).family
}

// classifyCCVersionOutput 从编译器 --version 的输出判定家族。
// 第二个返回值 false 表示"认不出来"（调用方按默认 clang 处理）。
//
// 判定用输出里的小写子串：clang 的版本串一定带 "clang"（含 Apple clang）；
// gcc 的版本串带 "gcc"（"gcc (Ubuntu 13.3.0-...) 13.3.0"），旧版或某些
// 移植只带 "Free Software Foundation" / "(GCC)"，一并覆盖。
func classifyCCVersionOutput(out string) (ccFamily, bool) {
	s := strings.ToLower(out)
	switch {
	case strings.Contains(s, "clang"):
		return ccFamilyClang, true
	case strings.Contains(s, "gcc"),
		strings.Contains(s, "free software foundation"),
		strings.Contains(s, "(gcc)"):
		return ccFamilyGCC, true
	}
	return ccFamilyClang, false
}

// pchConsumeFlags 返回 PCH 消费侧要插在输入描述（-x c <输入> -o <输出>）
// **之前**的参数。两侧都是 2 个参数，位置语义一致：
//
//	clang: -include-pch <gch 路径>   直接点名，与 -x/输入之间夹选项合法
//	gcc:   -I <gch 所在目录>         必须排在其它 -I 之前才命得中
//
// gcc 侧的失效是安全方向：语义不符 → 跳过 .gch 按普通头文件继续编译。
func pchConsumeFlags(fam ccFamily, pchPath string) []string {
	if fam == ccFamilyGCC {
		return []string{"-I", filepath.Dir(pchPath)}
	}
	return []string{"-include-pch", pchPath}
}

// ============================================================================
// PCH 判活指纹（stamp）
//
// .gch 是"宏与编译语义的冻结体"：预处理之后的 token 流，生成时生效的每个
// 宏取值都被固化。旧判据是 mtime（.gch 不比 kaula.h 旧、src/ 顶层 .h 没有
// 比 .gch 新的），有三个洞，都会以"旧声明被静默复用"或"构建硬失败"的形式
// 出事：
//
//  1. **只扫 src/ 顶层**。kaula.h 里有一句 #include "../std/io/io.h"，
//     PCH 会把 std 侧头一起冻结；改 std/io/io.h（乃至 std/string/string.h）
//     不会判死，.gch 带着旧 token 流继续被消费。std/freestanding 还是嵌套
//     目录，非递归扫描同样漏。
//  2. **不含编译器身份**。缓存目录按目标指纹划分，不按编译器划分：一次
//     clang 构建留下的 .gch，会被后续 -cc gcc 的构建按 mtime 放行。
//     clang 拿到 gcc 格式的 .gch 是**硬错误**（"file is not a valid
//     precompiled PCH file"），直接把构建打挂；同一路径下升级编译器版本
//     同理（AST 格式不兼容）。
//  3. **clang 的 mtime 硬检查**。clang 用 -include-pch 时，若 kaula.h 的
//     mtime 与 .gch 里记录的不一致（哪怕内容没变——bare touch、git
//     checkout、rsync 都会造成），直接
//     "fatal error: file ... has been modified since the precompiled
//     header ... was built: mtime changed"。
//     纯内容指纹对这个场景是失明的：内容没变 → 放行 → clang 硬失败。
//     （gcc 反向安全：.gch 比 .h 旧时它只是跳过，按普通头继续编。）
//
// 新判据是 .gch 旁边的一份 sidecar 指纹（与 .o 的 .stamp 同一传统），
// 输入六类：编译器路径、--version 指纹、家族、优化级别、PCH 生成参数里的
// 语义项、以及全部 .h 的**内容摘要 + mtime**。内容与 mtime 都要：
// - 内容变 → 摘要变（mtime 被 checkout 洗成同一时刻时靠它）；
// - mtime 变 → 指纹变（内容没变时靠它满足 clang 的硬检查，顺带满足 gcc
//   ".gch 不得旧于 .h" 的守卫）。
// 误判方向只有"多编一次"，不会静默复用旧产物，也不会把构建打挂。
//
// stamp 还**绑定 .gch 自身身份**（内容摘要 + size + mtime），不只是输入
// 指纹。原因是缓存目录是跨进程共享的（语料运行器多 jobs 并行构建），两个
// 进程可能交错发布：A(clang) rename .gch → B(gcc) rename .gch → B 写
// stamp(gcc 指纹) → A 写 stamp(clang 指纹)。最终磁盘上是 gcc 格式的 .gch
// 配 clang 指纹的 stamp，光比输入指纹会判活命中 → clang 拿 gcc 格式 .gch
// 硬失败。绑定身份后这种交错必然失配（stamp 记的是 A 生成的那份 .gch 的
// 摘要，磁盘上早已被 B 换掉），方向是重编一次，不是打挂。
// ============================================================================

// pchStampInputs 聚合 PCH 判活指纹的全部输入。判活侧与生成侧必须传同一份
// （调用方构造一次、两处使用），否则会出现"判活用一套、生成用另一套"的漂移。
type pchStampInputs struct {
	cc           string   // 编译器路径（-cc 指定或 PATH 解析结果）
	versionKey   string   // --version 输出摘要（升级会变；探测失败时为空）
	family       ccFamily // 编译器家族（.gch 格式与消费语法由它决定）
	optLevel     string   // -O 级别（决定 __OPTIMIZE__ → KMM_V4_OPT_LEVEL）
	semanticArgs []string // PCH 生成参数里的语义项（-fwrapv / -D 集 / -g 等）
	headerRoots  []string // 全部 .h 源头 = 生成命令的 -I 根（src → std → free）
}

func pchStampValue(in pchStampInputs) string {
	var parts []string
	parts = append(parts,
		"cc="+in.cc,
		"ccver="+in.versionKey,
		"family="+in.family.String(),
		"opt="+in.optLevel,
	)
	for _, a := range in.semanticArgs {
		parts = append(parts, "A:"+a)
	}
	parts = append(parts, "headers="+pchHeadersFingerprint(in.headerRoots))
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:12])
}

func pchStampPath(pchPath string) string { return pchPath + ".stamp" }

// pchGchIdentity 返回 .gch 当前的身份串（内容摘要 + size + mtime）。
// 文件不存在时返回 "missing"——调用方（判活）会先 stat，这里只是兜底。
func pchGchIdentity(pchPath string) string {
	info, err := os.Stat(pchPath)
	if err != nil {
		return "missing"
	}
	data, err := os.ReadFile(pchPath)
	if err != nil {
		return "missing"
	}
	sum := sha256Sum(data)
	return fmt.Sprintf("%s:%d:%d", hex.EncodeToString(sum[:16]), info.Size(), info.ModTime().UnixNano())
}

// pchStampMatches 判活：stamp 存在、输入指纹一致，且 stamp 里绑定的 .gch
// 身份与磁盘上当前的 .gch 一致（防跨进程交错发布，见文件头注释）。
func pchStampMatches(pchPath, stamp string) bool {
	b, err := os.ReadFile(pchStampPath(pchPath))
	if err != nil {
		return false
	}
	fp, identity, ok := strings.Cut(string(b), " ")
	if !ok || fp != stamp {
		return false
	}
	return identity == pchGchIdentity(pchPath)
}

// writePchStamp 写 stamp（输入指纹 + 生成时那份 .gch 的身份）。
// 原子写（临时文件 + rename）：两个进程并发写时不会留下交错的半个文件。
// 失败不影响正确性：只是下次判活失配、重编一次 .gch。
func writePchStamp(pchPath, stamp string) {
	content := stamp + " " + pchGchIdentity(pchPath)
	tmp := pchPath + ".stamp.tmp"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		return
	}
	_ = renameWithRetry(tmp, pchStampPath(pchPath))
}

// pchHeadersFingerprint 汇总 PCH 全部源头的 .h 指纹（内容 + mtime）。
//
// roots 即 PCH 生成命令的 -I 根（顺序固定 src → std → freestanding）。
// 递归收集（std/freestanding 的模块头在嵌套目录里），按"首个根命中"
// 去重——与预处理器解析 #include 的顺序一致；最终按相对路径排序后哈希，
// 消除根路径拼写差异（workDir 相对根 vs KAULA_HOME 根）带来的噪声。
func pchHeadersFingerprint(roots []string) string {
	type header struct{ rel, mtime, size, body string }
	var headers []header
	seen := make(map[string]bool)
	for _, root := range roots {
		if root == "" {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".h") {
				return nil
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if seen[rel] {
				return nil
			}
			seen[rel] = true
			info, ierr := d.Info()
			if ierr != nil {
				return nil
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil
			}
			headers = append(headers, header{
				rel:   rel,
				mtime: fmt.Sprintf("%d", info.ModTime().UnixNano()),
				size:  fmt.Sprintf("%d", info.Size()),
				body:  string(data),
			})
			return nil
		})
	}
	sort.Slice(headers, func(i, j int) bool { return headers[i].rel < headers[j].rel })
	h := sha256.New()
	for _, e := range headers {
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00", e.rel, e.mtime, e.size, e.body)
	}
	return hex.EncodeToString(h.Sum(nil)[:12])
}

func sha256Sum(b []byte) []byte {
	s := sha256.Sum256(b)
	return s[:]
}
