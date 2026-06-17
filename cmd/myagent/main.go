package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"myagent/internal/auto"
	"myagent/internal/llm"
	"myagent/internal/loop"
	"myagent/internal/tools"
)

func main() {
	maxTurns := flag.Int("max-turns", 20, "maximum model turns before giving up")
	verbose := flag.Bool("v", true, "verbose: print turns, tool calls, and results")
	autonomous := flag.Bool("auto", false, "autonomous mode: self-drive until STATE.md reports complete")
	maxIters := flag.Int("max-iterations", auto.DefaultMaxIterations, "autonomous hard cap on iterations (non-negotiable)")
	yes := flag.Bool("yes", false, "allow run_bash to run destructive commands (rm/mv/dd/git push/...) without confirmation")
	flag.Parse()

	task := strings.TrimSpace(strings.Join(flag.Args(), " "))
	if task == "" {
		fmt.Fprintln(os.Stderr, `usage: myagent [-max-turns N] [-v] [-auto [-max-iterations N]] [-yes] "your task here"`)
		fmt.Fprintln(os.Stderr, "\nrequires env: AGENT_API_KEY (and optionally AGENT_BASE_URL, AGENT_MODEL)")
		os.Exit(2)
	}

	// Wire the destructive-action gate: --yes allows freely, otherwise prompt.
	if *yes {
		tools.Gate.Allow = true
	} else {
		tools.Gate.Confirm = confirmStdin
	}

	client, err := llm.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("backend: %s | model: %s\n", client.BaseURL, client.Model)

	if *autonomous {
		wd, _ := os.Getwd()
		stepper := auto.LLMStepper{Client: client, MaxTurns: *maxTurns, Verbose: *verbose}
		err = auto.Run(task, stepper, auto.Options{Dir: wd, MaxIterations: *maxIters})
	} else {
		err = loop.Run(client, task, *maxTurns, *verbose)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nfailed: %v\n", err)
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
