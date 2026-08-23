package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"compiler/internal/config"
)

// Workspace 表示一个多包工作空间
type Workspace struct {
	Root    string                  // 工作空间根目录
	Config  *config.WorkspaceConfig // 工作空间配置
	Members []*Member               // 成员包列表（按依赖拓扑排序）
}

// Member 工作空间中的一个成员包
type Member struct {
	Name   string          // 成员名称（目录名）
	Dir    string          // 成员目录的绝对路径
	Config *config.Config  // 成员的编译配置
	Order  int             // 拓扑排序后的编译顺序
}

// LoadWorkspace 从指定目录加载工作空间
func LoadWorkspace(rootDir string) (*Workspace, error) {
	kaulaJSON := filepath.Join(rootDir, "kaula.json")
	data, err := os.ReadFile(kaulaJSON)
	if err != nil {
		return nil, fmt.Errorf("read kaula.json: %w", err)
	}

	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse kaula.json: %w", err)
	}

	if cfg.Workspace == nil || len(cfg.Workspace.Members) == 0 {
		return nil, fmt.Errorf("no workspace members defined in kaula.json")
	}

	ws := &Workspace{
		Root: rootDir,
		Config: &config.WorkspaceConfig{
			Members:    cfg.Workspace.Members,
			SharedDeps: cfg.Workspace.SharedDeps,
			Exclude:    cfg.Workspace.Exclude,
		},
	}

	// 加载所有成员
	for _, memberPath := range cfg.Workspace.Members {
		absPath := filepath.Join(rootDir, memberPath)
		if _, err := os.Stat(absPath); err != nil {
			return nil, fmt.Errorf("workspace member not found: %s", memberPath)
		}

		memberCfg, err := config.LoadConfigAt(absPath)
		if err != nil {
			// 成员没有 kaula.json 也可以，使用默认配置
			memberCfg = config.DefaultConfig()
			memberCfg.BasePath = absPath
		}

		// 合并共享依赖
		if cfg.Workspace.SharedDeps != nil {
			if memberCfg.Dependencies == nil {
				memberCfg.Dependencies = map[string]string{}
			}
			for name, ver := range cfg.Workspace.SharedDeps {
				if _, exists := memberCfg.Dependencies[name]; !exists {
					memberCfg.Dependencies[name] = ver
				}
			}
		}

		name := filepath.Base(memberPath)
		ws.Members = append(ws.Members, &Member{
			Name:   name,
			Dir:    absPath,
			Config: memberCfg,
		})
	}

	// 拓扑排序
	if err := ws.topoSort(); err != nil {
		return nil, fmt.Errorf("workspace dependency cycle: %w", err)
	}

	return ws, nil
}

// InitWorkspace 初始化一个新的工作空间
func InitWorkspace(rootDir string, memberDirs []string) error {
	kaulaJSON := filepath.Join(rootDir, "kaula.json")

	// 检查是否已存在
	if _, err := os.Stat(kaulaJSON); err == nil {
		return fmt.Errorf("kaula.json already exists in %s", rootDir)
	}

	wsCfg := &config.WorkspaceConfig{
		Members: memberDirs,
	}

	cfg := &config.Config{
		BasePath:       rootDir,
		TemplatePath:   "templates",
		IncludePath:    "../std",
		TargetLanguage: "c",
		QueueSize:      100,
		SpendableSize:  10,
		MemoryLimitMB:  4096,
		TimeoutSec:     120,
		Workspace:      wsCfg,
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(kaulaJSON, data, 0644); err != nil {
		return err
	}

	// 创建成员目录和默认 kaula.json
	for _, memberDir := range memberDirs {
		absMember := filepath.Join(rootDir, memberDir)
		if err := os.MkdirAll(absMember, 0755); err != nil {
			return fmt.Errorf("create member dir %s: %w", memberDir, err)
		}

		memberConfig := config.DefaultConfig()
		memberConfig.BasePath = absMember
		if err := config.SaveConfig(memberConfig, filepath.Join(absMember, "kaula.json")); err != nil {
			return fmt.Errorf("create member kaula.json: %w", err)
		}
	}

	return nil
}

// topoSort 对成员包进行拓扑排序（基于 import 关系）
func (ws *Workspace) topoSort() error {
	// 构建邻接表
	graph := map[string][]string{} // member -> 依赖的其他 member
	for _, m := range ws.Members {
		graph[m.Name] = []string{}
	}

	// 分析每个成员的 import 语句，找出对其他成员的依赖
	for _, m := range ws.Members {
		deps := ws.findMemberDeps(m)
		graph[m.Name] = deps
	}

	// Kahn 算法拓扑排序
	inDegree := map[string]int{}
	for _, m := range ws.Members {
		inDegree[m.Name] = 0
	}
	for _, deps := range graph {
		for _, d := range deps {
			if _, exists := inDegree[d]; exists {
				inDegree[d]++
			}
		}
	}

	var queue []string
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}
	sort.Strings(queue) // 确定性

	var sorted []string
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		sorted = append(sorted, node)

		for _, dep := range graph[node] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
				sort.Strings(queue)
			}
		}
	}

	if len(sorted) != len(ws.Members) {
		return fmt.Errorf("cycle detected: sorted %d of %d members", len(sorted), len(ws.Members))
	}

	// 应用排序
	orderMap := map[string]int{}
	for i, name := range sorted {
		orderMap[name] = i
	}
	for _, m := range ws.Members {
		m.Order = orderMap[m.Name]
	}
	sort.Slice(ws.Members, func(i, j int) bool {
		return ws.Members[i].Order < ws.Members[j].Order
	})

	return nil
}

// findMemberDeps 查找一个成员对其他成员的依赖
func (ws *Workspace) findMemberDeps(member *Member) []string {
	var deps []string

	// 遍历成员目录中的 .kl 文件，查找 import 语句
	filepath.Walk(member.Dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".kl" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		content := string(data)
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "import ") {
				imp := strings.TrimPrefix(line, "import ")
				imp = strings.TrimSpace(imp)
				imp = strings.Trim(imp, "\"")

				// 检查是否引用了其他成员
				for _, otherMember := range ws.Members {
					if otherMember.Name == member.Name {
						continue
					}
					// 简单匹配：import 路径包含其他成员名
					if strings.Contains(imp, otherMember.Name) {
						deps = append(deps, otherMember.Name)
					}
				}
			}
		}
		return nil
	})

	return deps
}

// BuildAll 构建所有成员（按拓扑顺序）
func (ws *Workspace) BuildAll(release bool, debug bool) error {
	for _, member := range ws.Members {
		fmt.Printf("[workspace] Building %s (%d/%d)...\n", member.Name, member.Order+1, len(ws.Members))

		cfg := member.Config
		if release {
			cfg.Release = true
		}
		if debug {
			cfg.Debug = true
		}

		// 这里调用编译器的核心编译逻辑
		// 实际实现需要 import compiler/internal/compiler 或类似方式
		// 暂时只做占位
		fmt.Printf("[workspace] %s: compiling in %s\n", member.Name, member.Dir)
	}

	return nil
}

// TestAll 运行所有成员的测试
func (ws *Workspace) TestAll() error {
	for _, member := range ws.Members {
		fmt.Printf("[workspace] Testing %s...\n", member.Name)
		// 占位：实际测试逻辑
	}
	return nil
}

// ListMembers 列出所有成员
func (ws *Workspace) ListMembers() {
	fmt.Printf("Workspace: %s\n", ws.Root)
	fmt.Printf("Members (%d):\n", len(ws.Members))
	for _, m := range ws.Members {
		depCount := len(ws.findMemberDeps(m))
		fmt.Printf("  [%d] %s (%s) - %d local deps\n", m.Order, m.Name, m.Dir, depCount)
	}
	if ws.Config.SharedDeps != nil && len(ws.Config.SharedDeps) > 0 {
		fmt.Printf("Shared dependencies:\n")
		for name, ver := range ws.Config.SharedDeps {
			fmt.Printf("  %s: %s\n", name, ver)
		}
	}
}

// GetMember 查找指定名称的成员
func (ws *Workspace) GetMember(name string) (*Member, bool) {
	for _, m := range ws.Members {
		if m.Name == name {
			return m, true
		}
	}
	return nil, false
}

// MemberDirs 返回所有成员目录路径
func (ws *Workspace) MemberDirs() []string {
	dirs := make([]string, len(ws.Members))
	for i, m := range ws.Members {
		dirs[i] = m.Dir
	}
	return dirs
}
