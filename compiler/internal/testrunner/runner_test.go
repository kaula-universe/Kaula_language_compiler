package testrunner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, dir, name, source string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	a := fixture(t, dir, "a.kl", "fn main(): i32 { return 0 }")
	b := fixture(t, dir, "nested/b.kl", "fn main(): i32 { let n: f64 = 1.5; return 0 }")
	fixture(t, dir, "library.kl", "// fn main() {}\nfn helper(): i32 { return 0 }")
	fixture(t, dir, "strings.kl", `let text = "fn main() {}"`)
	fixture(t, dir, "method.kl", "class A { fn main(): i32 { return 0 } }")
	fixture(t, dir, "block.kl", "/* fn main() {} */")
	files, err := Discover(dir, "")
	if err != nil || len(files) != 2 || files[0] != a || files[1] != b {
		t.Fatalf("files=%v err=%v", files, err)
	}
	files, err = Discover(dir, "nested/b")
	if err != nil || len(files) != 1 || files[0] != b {
		t.Fatalf("filter: %v %v", files, err)
	}
	files, err = Discover(a, "a")
	if err != nil || len(files) != 1 {
		t.Fatalf("file: %v %v", files, err)
	}
	if _, err := Discover(dir, "["); err == nil {
		t.Fatal("invalid regex accepted")
	}
	if _, err := Discover(filepath.Join(dir, "missing"), ""); err == nil {
		t.Fatal("missing path accepted")
	}
}

func TestRunSnapshotsAndCommand(t *testing.T) {
	dir := t.TempDir()
	file := fixture(t, dir, "space dir/pass.kl", "fn main(): i32 { return 0 }")
	var out bytes.Buffer
	var outputDir string
	calls := 0
	runner := func(ctx context.Context, cwd, name string, args ...string) commandResult {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Error("missing timeout")
		}
		if cwd != filepath.Dir(file) || !filepath.IsAbs(name) {
			t.Errorf("cwd=%s name=%s", cwd, name)
		}
		if len(args) > 0 {
			if args[0] != "--no-cache" || args[len(args)-1] != file {
				t.Errorf("args=%v", args)
			}
			outputDir = strings.TrimPrefix(args[1], "--output-dir=")
			if !filepath.IsAbs(outputDir) || outputDir == dir {
				t.Errorf("output=%s", outputDir)
			}
			if args[2] != "--source-dir="+filepath.Dir(file) {
				t.Errorf("args=%v", args)
			}
			return commandResult{}
		}
		if filepath.Dir(name) != outputDir {
			t.Errorf("run outside temp output: %s", name)
		}
		return commandResult{stdout: []byte("42\r\n"), stderr: []byte("not a snapshot")}
	}
	opts := Options{Update: true, Output: &out}
	summary, err := run([]string{file, file}, opts, runner)
	if err != nil || summary.Passed != 1 || calls != 2 {
		t.Fatalf("%+v %v calls=%d", summary, err, calls)
	}
	data, err := os.ReadFile(SnapshotPath(file))
	if err != nil || string(data) != "42\n" {
		t.Fatalf("snapshot=%q err=%v", data, err)
	}
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Fatalf("temporary output retained: %v", err)
	}
	opts.Update = false
	if _, err := run([]string{file}, opts, runner); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(SnapshotPath(file), []byte("wrong\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if _, err := run([]string{file}, opts, runner); err == nil {
		t.Fatal("mismatch passed")
	}
	for _, text := range []string{"Compiler command:", "42", "wrong", "not a snapshot", "stdout snapshot mismatch"} {
		if !strings.Contains(out.String(), text) {
			t.Errorf("missing %q in %s", text, &out)
		}
	}
}

func TestFailuresAndContinue(t *testing.T) {
	for _, stage := range []string{"compile", "run", "timeout"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			fixture(t, dir, "a.kl", "fn main(): i32 { return 0 }")
			fixture(t, dir, "b.kl", "fn main(): i32 { return 0 }")
			var out bytes.Buffer
			failed := false
			runner := func(ctx context.Context, cwd, name string, args ...string) commandResult {
				isCompile := len(args) > 0
				if !failed && ((stage == "compile" && isCompile) || (stage != "compile" && !isCompile)) {
					failed = true
					err := errors.New("exit status 7")
					if stage == "timeout" {
						<-ctx.Done()
						err = ctx.Err()
					}
					return commandResult{stdout: []byte(strings.Repeat("X", 10000)), stderr: []byte("diagnostic-end"), err: err}
				}
				return commandResult{}
			}
			summary, err := run([]string{dir}, Options{Output: &out, Timeout: time.Millisecond}, runner)
			if err == nil || summary.Failed != 1 || summary.Passed != 1 {
				t.Fatalf("%+v %v", summary, err)
			}
			for _, text := range []string{"Compiler command:", strings.Repeat("X", 10000), "diagnostic-end"} {
				if !strings.Contains(out.String(), text) {
					t.Errorf("failure output truncated: %q", text[:min(len(text), 20)])
				}
			}
		})
	}
}

func TestUpdateDoesNotAcceptRunFailure(t *testing.T) {
	file := fixture(t, t.TempDir(), "fail.kl", "fn main(): i32 { return 1 }")
	runner := func(ctx context.Context, cwd, name string, args ...string) commandResult {
		if len(args) > 0 {
			return commandResult{}
		}
		return commandResult{stdout: []byte("bad"), err: errors.New("exit status 1")}
	}
	if _, err := run([]string{file}, Options{Update: true, Output: io.Discard}, runner); err == nil {
		t.Fatal("failure accepted")
	}
	if _, err := os.Stat(SnapshotPath(file)); !os.IsNotExist(err) {
		t.Fatalf("snapshot written: %v", err)
	}
}

func TestNoTests(t *testing.T) {
	if _, err := Run([]string{t.TempDir()}, Options{Output: io.Discard}); err == nil {
		t.Fatal("empty suite passed")
	}
}

func TestParse(t *testing.T) {
	opts, paths, err := Parse([]string{"fixtures", "--filter", "numeric/", "--update", "--timeout=2s", "--include", "space path"}, io.Discard)
	if err != nil || len(paths) != 1 || paths[0] != "fixtures" || opts.Filter != "numeric/" || !opts.Update || opts.Timeout != 2*time.Second || opts.Include != "space path" {
		t.Fatalf("%+v %v %v", opts, paths, err)
	}
	for _, args := range [][]string{{"--timeout=0s"}, {"--timeout=-1s"}, {"--filter"}, {"--unknown"}, {"a", "b"}} {
		if _, _, err := Parse(args, io.Discard); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}

func TestExecuteRealProcess(t *testing.T) {
	if os.Getenv("KAULA_TEST_HELPER") == "1" {
		if os.Getenv("KAULA_TEST_SLEEP") == "1" {
			time.Sleep(10 * time.Second)
		}
		os.Stdout.WriteString("real stdout\n")
		os.Stderr.WriteString("real stderr\n")
		os.Exit(7)
	}
	t.Setenv("KAULA_TEST_HELPER", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := execute(ctx, t.TempDir(), exe, "-test.run=^TestExecuteRealProcess$")
	if result.err == nil || string(result.stdout) != "real stdout\n" || string(result.stderr) != "real stderr\n" {
		t.Fatalf("result=%+v", result)
	}
	t.Setenv("KAULA_TEST_SLEEP", "1")
	ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result = execute(ctx, t.TempDir(), exe, "-test.run=^TestExecuteRealProcess$")
	if !errors.Is(result.err, context.DeadlineExceeded) {
		t.Fatalf("timeout=%v", result.err)
	}
}


// expectFailDirective 解析与负向语料执行行为。
// 标注 `// kaulac:expect-fail [子串]` 的 fixture：编译失败 = PASS（可选要求
// 输出含子串）；编译成功 = FAIL。未标注文件行为不变（编译失败 = FAIL）。
func TestExpectFailDirectiveParse(t *testing.T) {
	dir := t.TempDir()
	marked, want := expectFailDirective(fixture(t, dir, "plain.kl", "fn main() {}\n"))
	if marked || want != "" {
		t.Fatalf("plain: marked=%v want=%q", marked, want)
	}
	marked, want = expectFailDirective(fixture(t, dir, "bare.kl", "// kaulac:expect-fail\nfn main() {}\n"))
	if !marked || want != "" {
		t.Fatalf("bare: marked=%v want=%q", marked, want)
	}
	marked, want = expectFailDirective(fixture(t, dir, "code.kl", "import std.io\n// kaulac:expect-fail wrong_arg_count_local —— 说明\nfn main() {}\n"))
	if !marked || want != "wrong_arg_count_local" {
		t.Fatalf("code: marked=%v want=%q", marked, want)
	}
	marked, want = expectFailDirective(fixture(t, dir, "colon.kl", "// kaulac:expect-fail:missing_return\nfn main() {}\n"))
	if !marked || want != "missing_return" {
		t.Fatalf("colon: marked=%v want=%q", marked, want)
	}
}

func TestExpectFailRunner(t *testing.T) {
	compileFail := commandResult{
		stdout: []byte("Semantic Error: wrong_arg_count_local\n"),
		err:    context.DeadlineExceeded, // 任意非 nil 即"编译失败"
	}
	compileOK := commandResult{}

	cases := []struct {
		name     string
		source   string
		compile  commandResult
		wantPass bool
		wantText string
	}{
		{
			name:     "失败且含错误码 -> PASS",
			source:   "// kaulac:expect-fail wrong_arg_count_local\nfn main() {}\n",
			compile:  compileFail,
			wantPass: true,
		},
		{
			name:     "编译成功 -> FAIL",
			source:   "// kaulac:expect-fail wrong_arg_count_local\nfn main() {}\n",
			compile:  compileOK,
			wantPass: false,
			wantText: "标注了 expect-fail 但编译成功",
		},
		{
			name:     "失败但错误码不符 -> FAIL",
			source:   "// kaulac:expect-fail missing_return\nfn main() {}\n",
			compile:  compileFail,
			wantPass: false,
			wantText: "缺少期望子串",
		},
		{
			name:     "未标注且失败 -> FAIL（原行为）",
			source:   "fn main() {}\n",
			compile:  compileFail,
			wantPass: false,
			wantText: "compile:",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			file := fixture(t, dir, "neg.kl", c.source)
			calls := 0
			runner := func(ctx context.Context, cwd, name string, args ...string) commandResult {
				calls++
				return c.compile
			}
			var out bytes.Buffer
			summary, err := run([]string{file}, Options{Output: &out}, runner)
			if c.wantPass != (err == nil && summary.Passed == 1) {
				t.Fatalf("summary=%+v err=%v out=%s", summary, err, out.String())
			}
			// PASS 时不得执行运行阶段（负向语料没有可执行文件）
			if c.wantPass && calls != 1 {
				t.Fatalf("PASS 路径不应执行 fixture, calls=%d", calls)
			}
			if c.wantText != "" && !strings.Contains(out.String(), c.wantText) {
				t.Fatalf("missing %q in %s", c.wantText, out.String())
			}
		})
	}
}
