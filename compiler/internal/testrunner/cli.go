package testrunner

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// Parse accepts flags before or after the optional path, like the compiler CLI.
func Parse(args []string, output io.Writer) (Options, []string, error) {
	opts := Options{Timeout: DefaultTimeout, Output: output}
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&opts.Filter, "filter", "", "regular expression matching relative fixture paths")
	flags.BoolVar(&opts.Update, "update", false, "create/update <file>.kl.stdout snapshots")
	flags.DurationVar(&opts.Timeout, "timeout", DefaultTimeout, "timeout per compile and run (e.g. 30s, 2m)")
	flags.StringVar(&opts.Include, "include", "", "standard C headers directory")
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: kaulac test [file.kl|directory] [--filter regexp] [--update] [--timeout 30s] [--include path]")
		flags.PrintDefaults()
	}
	var positional, options []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}
		options = append(options, arg)
		name := strings.TrimLeft(arg, "-")
		if name == "filter" || name == "timeout" || name == "include" {
			if i+1 < len(args) {
				i++
				options = append(options, args[i])
			}
		}
	}
	if err := flags.Parse(options); err != nil {
		return opts, nil, err
	}
	if opts.Timeout <= 0 {
		return opts, nil, fmt.Errorf("timeout must be positive")
	}
	if len(positional) > 1 {
		return opts, nil, fmt.Errorf("test accepts at most one file or directory")
	}
	return opts, positional, nil
}
