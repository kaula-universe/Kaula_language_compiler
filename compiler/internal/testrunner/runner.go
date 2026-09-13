// Package testrunner compiles and executes standalone Kaula fixtures.
package testrunner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"kaula/internal/lexer"
)

const DefaultTimeout = 30 * time.Second

type Options struct {
	Filter   string
	Update   bool
	Timeout  time.Duration
	Include  string
	Compiler string // Empty selects os.Executable, never a different kaulac on PATH.
	Output   io.Writer
}

type Summary struct{ Passed, Failed int }

type commandResult struct {
	stdout, stderr []byte
	err            error
}
type executor func(context.Context, string, string, ...string) commandResult

func execute(ctx context.Context, dir, name string, args ...string) commandResult {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	// Bound waiting for inherited pipes even when a fixture starts children.
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return commandResult{stdout.Bytes(), stderr.Bytes(), err}
}

// Discover returns absolute paths in lexical order. Only top-level main declarations
// count; comments, string literals, methods and library-only files do not.
func Discover(path, filter string) ([]string, error) {
	re, err := regexp.Compile(filter)
	if err != nil {
		return nil, fmt.Errorf("invalid filter: %w", err)
	}
	root, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".kl" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			rel = filepath.Base(path)
		}
		if !re.MatchString(filepath.ToSlash(rel)) {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if hasMain(string(source)) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func hasMain(source string) bool {
	l := lexer.NewLexer(stripBlockComments(source))
	depth := 0
	prev := lexer.TOKEN_EOF
	for {
		tok := l.Next()
		if tok.Type == lexer.TOKEN_EOF {
			return false
		}
		if depth == 0 && prev == lexer.TOKEN_FUNC && tok.Type == lexer.TOKEN_IDENT && tok.Value == "main" {
			return l.Next().Type == lexer.TOKEN_LPAREN
		}
		if tok.Type == lexer.TOKEN_LBRACE {
			depth++
		}
		if tok.Type == lexer.TOKEN_RBRACE {
			depth--
		}
		prev = tok.Type
	}
}

// SnapshotPath keeps the source extension so fixtures remain unambiguous.
func SnapshotPath(file string) string { return file + ".stdout" }

func Run(paths []string, opts Options) (Summary, error) { return run(paths, opts, execute) }

func run(paths []string, opts Options, execFn executor) (Summary, error) {
	var summary Summary
	if opts.Output == nil {
		opts.Output = os.Stdout
	}
	if opts.Timeout == 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.Timeout < 0 {
		return summary, fmt.Errorf("timeout must be positive")
	}
	if len(paths) == 0 {
		paths = []string{"."}
	}
	var files []string
	seen := map[string]bool{}
	for _, path := range paths {
		found, err := Discover(path, opts.Filter)
		if err != nil {
			return summary, err
		}
		for _, f := range found {
			if !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
	}
	if len(files) == 0 {
		return summary, fmt.Errorf("no runnable .kl tests matched")
	}
	compiler := opts.Compiler
	var err error
	if compiler == "" {
		compiler, err = os.Executable()
		if err != nil {
			return summary, err
		}
	}
	compiler, err = filepath.Abs(compiler)
	if err != nil {
		return summary, err
	}
	if opts.Include != "" {
		opts.Include, err = filepath.Abs(opts.Include)
		if err != nil {
			return summary, err
		}
	}
	for _, file := range files {
		if runFile(file, compiler, opts, execFn) {
			summary.Passed++
		} else {
			summary.Failed++
		}
	}
	fmt.Fprintf(opts.Output, "Tests: %d passed, %d failed\n", summary.Passed, summary.Failed)
	if summary.Failed != 0 {
		return summary, fmt.Errorf("%d test(s) failed", summary.Failed)
	}
	return summary, nil
}

func runFile(file, compiler string, opts Options, execFn executor) bool {
	dir, err := os.MkdirTemp("", "kaulac-test-")
	if err != nil {
		fmt.Fprintf(opts.Output, "FAIL %s: %v\n", file, err)
		return false
	}
	defer os.RemoveAll(dir)
	sourceDir := filepath.Dir(file)
	args := []string{"--no-cache", "--output-dir=" + dir, "--source-dir=" + sourceDir}
	if opts.Include != "" {
		args = append(args, "--include="+opts.Include)
	}
	args = append(args, file)
	command := commandString(compiler, args)
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	compiled := execFn(ctx, sourceDir, compiler, args...)
	cancel()
	fail := func(reason string, ran *commandResult, expected []byte) bool {
		fmt.Fprintf(opts.Output, "FAIL %s: %s\nCompiler command: %s\nWorking directory: %s\n", file, reason, command, sourceDir)
		fmt.Fprintf(opts.Output, "--- compiler stdout ---\n%s\n--- compiler stderr ---\n%s\n", compiled.stdout, compiled.stderr)
		if ran != nil {
			fmt.Fprintf(opts.Output, "--- stdout ---\n%s\n--- stderr ---\n%s\n", ran.stdout, ran.stderr)
		}
		if expected != nil {
			fmt.Fprintf(opts.Output, "--- expected stdout ---\n%s\n", expected)
		}
		return false
	}
	if compiled.err != nil {
		return fail("compile: "+compiled.err.Error(), nil, nil)
	}
	name := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	ctx, cancel = context.WithTimeout(context.Background(), opts.Timeout)
	ran := execFn(ctx, sourceDir, filepath.Join(dir, name))
	cancel()
	if ran.err != nil {
		return fail("run: "+ran.err.Error(), &ran, nil)
	}
	snapshot := SnapshotPath(file)
	if opts.Update {
		if err := os.WriteFile(snapshot, normalize(ran.stdout), 0644); err != nil {
			return fail("write snapshot: "+err.Error(), &ran, nil)
		}
	} else {
		expected, err := os.ReadFile(snapshot)
		if err != nil && !os.IsNotExist(err) {
			return fail("read snapshot: "+err.Error(), &ran, nil)
		}
		if err == nil && !bytes.Equal(normalize(expected), normalize(ran.stdout)) {
			return fail("stdout snapshot mismatch (use --update to accept)", &ran, append([]byte{}, expected...))
		}
	}
	fmt.Fprintf(opts.Output, "PASS %s\n", file)
	return true
}

func normalize(data []byte) []byte { return bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")) }
func commandString(name string, args []string) string {
	parts := []string{strconv.Quote(name)}
	for _, arg := range args {
		parts = append(parts, strconv.Quote(arg))
	}
	return strings.Join(parts, " ")
}
