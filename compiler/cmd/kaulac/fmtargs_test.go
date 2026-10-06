package main

import "testing"

// isFmtSubcommand 必须只认 fmt 的四个子命令关键字，
// 否则参数边界会被算错、把文件名当成子命令吃掉。
func TestIsFmtSubcommand(t *testing.T) {
	for _, tok := range []string{"file", "stdin", "check"} {
		if !isFmtSubcommand(tok) {
			t.Errorf("%q 应是 fmt 子命令", tok)
		}
	}
	for _, tok := range []string{"--write", "-w", "a.kl", "", "help", "-"} {
		if isFmtSubcommand(tok) {
			t.Errorf("%q 不应被当作 fmt 子命令", tok)
		}
	}
}

// 模拟预扫描的参数切分：`fmt` 之后的参数必须一直收到最后一个，
// 不能「遇到 - 开头就停」——否则排在文件名之后的 --write/-w 会被漏掉，
// 使 `kaulac fmt file x.kl --write` 静默退化成只打印、不落盘。
func TestCollectFmtArgsKeepsFlagsAfterFile(t *testing.T) {
	collect := func(args []string, i int) ([]string, int) {
		var fmtArgs []string
		for i+1 < len(args) && !isFmtSubcommand(args[i+1]) {
			fmtArgs = append(fmtArgs, args[i+1])
			i++
		}
		return fmtArgs, i
	}

	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"旗标在文件后", []string{"fmt", "file", "x.kl", "--write"}, []string{"x.kl", "--write"}},
		{"旗标在文件前", []string{"fmt", "file", "--write", "x.kl"}, []string{"--write", "x.kl"}},
		{"短旗标", []string{"fmt", "file", "x.kl", "-w"}, []string{"x.kl", "-w"}},
		{"多文件带旗标", []string{"fmt", "file", "a.kl", "b.kl", "--write"}, []string{"a.kl", "b.kl", "--write"}},
		{"遇下一个子命令停止", []string{"fmt", "file", "x.kl", "check", "y.kl"}, []string{"x.kl"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := collect(c.args, 1) // args[1] == "file" 是 fmtCmd
			if len(got) != len(c.want) {
				t.Fatalf("实得 %v，期望 %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("实得 %v，期望 %v", got, c.want)
				}
			}
		})
	}
}
