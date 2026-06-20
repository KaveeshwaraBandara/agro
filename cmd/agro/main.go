package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"agro/internal/auto"
	"agro/internal/config"
	"agro/internal/llm"
	"agro/internal/loop"
	"agro/internal/tools"
)

func main() {
	// Subcommands for key/provider management run before the task flow.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "login":
			exitOnErr(runLogin())
			return
		case "keys":
			exitOnErr(runKeys(os.Args[2:]))
			return
		}
	}

	maxTurns := flag.Int("max-turns", 20, "maximum model turns before giving up")
	verbose := flag.Bool("v", true, "verbose: print turns, tool calls, and results")
	autonomous := flag.Bool("auto", false, "autonomous mode: self-drive until STATE.md reports complete")
	maxIters := flag.Int("max-iterations", auto.DefaultMaxIterations, "autonomous hard cap on iterations (non-negotiable)")
	resume := flag.Bool("resume", false, "autonomous mode: continue from an existing STATE.md instead of starting fresh")
	yes := flag.Bool("yes", false, "allow run_bash to run destructive commands (rm/mv/dd/git push/...) without confirmation")
	flag.Parse()

	task := strings.TrimSpace(strings.Join(flag.Args(), " "))
	if task == "" {
		fmt.Fprintln(os.Stderr, `usage: agro [-max-turns N] [-v] [-auto [-max-iterations N] [-resume]] [-yes] "your task here"`)
		fmt.Fprintln(os.Stderr, "       agro login | agro keys <list|use|rm> [provider]")
		fmt.Fprintln(os.Stderr, "\nconfig: env AGENT_* overrides; otherwise the active profile from `agro login`")
		os.Exit(2)
	}

	// Wire the destructive-action gate: --yes allows freely, otherwise prompt.
	if *yes {
		tools.Gate.Allow = true
	} else {
		tools.Gate.Confirm = confirmStdin
	}

	// Resolve the backend: env (AGENT_*) first, then the stored active profile.
	client, err := resolveClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf("backend: %s | model: %s\n", client.BaseURL, client.Model)

	if *autonomous {
		wd, _ := os.Getwd()
		stepper := auto.LLMStepper{Client: client, MaxTurns: *maxTurns, Verbose: *verbose}
		err = auto.Run(task, stepper, auto.Options{Dir: wd, MaxIterations: *maxIters, Resume: *resume})
	} else {
		err = loop.Run(client, task, *maxTurns, *verbose)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nfailed: %v\n", err)
		os.Exit(1)
	}
}

// resolveClient builds the LLM client the way the CLI task path does: resolve
// the backend (env AGENT_* first, then the active stored profile) and construct
// it via llm.NewWith. Throttling (AGENT_MIN_REQUEST_INTERVAL) is applied inside
// NewWith, so it holds for every construction path — env or stored profile.
func resolveClient() (*llm.Client, error) {
	cfgPath, err := config.Path()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	resolved, err := config.Resolve(os.Getenv, cfg)
	if err != nil {
		return nil, err
	}
	return llm.NewWith(resolved.BaseURL, resolved.Model, resolved.Key)
}

// exitOnErr prints the error and exits non-zero; used by subcommand dispatch.
func exitOnErr(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// confirmStdin asks the operator to approve a destructive command interactively.
func confirmStdin(cmd string) bool {
	fmt.Fprintf(os.Stderr, "\n⚠️  destructive command requested:\n    %s\nallow? [y/N] ", cmd)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.TrimSpace(strings.ToLower(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
