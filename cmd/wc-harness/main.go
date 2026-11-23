// Command wc-harness runs one task from <workspace>/PROMPT.md through an
// OpenAI-compatible model and the tools staged in a wc-tools tree.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/WywySenarios/wc-fuse-client/client"
	"github.com/WywySenarios/wc-harness/src/agent"
	"github.com/WywySenarios/wc-harness/src/config"
	"github.com/WywySenarios/wc-harness/src/llm"
	"github.com/WywySenarios/wc-harness/src/tools"
)

func main() {
	os.Exit(run())
}

func run() int {
	envPath := flag.String("env", ".env", "dotenv file with WC_LLM_* settings")
	toolsDir := flag.String("tools", "tools", "staged tool tree (binaries + manifests/)")
	maxTurns := flag.Int("max-turns", 10, "maximum model turns before aborting")
	fuseSocket := flag.String("fuse-socket", "", "wc-fuse control socket; enables a FUSE workspace when set")
	fuseProject := flag.String("fuse-project", "", "host project root to mount through wc-fuse")
	sessionID := flag.String("session", "", "session id reported to wc-fuse (default: hostname)")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: %s [flags] [workspace]\n\n", filepath.Base(os.Args[0]))
		fmt.Fprintln(flag.CommandLine.Output(), "Runs the task in <workspace>/PROMPT.md. The workspace defaults to the current directory.")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "wc-harness: expected at most one workspace argument")
		flag.Usage()
		return 2
	}
	workspace := "."
	if flag.NArg() == 1 {
		workspace = flag.Arg(0)
	}
	if *maxTurns < 1 {
		fmt.Fprintln(os.Stderr, "wc-harness: -max-turns must be at least 1")
		return 2
	}
	if *fuseSocket != "" && *fuseProject == "" {
		fmt.Fprintln(os.Stderr, "wc-harness: -fuse-project is required with -fuse-socket")
		return 2
	}

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wc-harness: resolve workspace: %v\n", err)
		return 1
	}

	promptData, err := os.ReadFile(filepath.Join(absWorkspace, "PROMPT.md"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "wc-harness: read PROMPT.md: %v\n", err)
		return 1
	}
	prompt := strings.TrimSpace(string(promptData))
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "wc-harness: PROMPT.md is empty")
		return 1
	}

	if *fuseSocket != "" {
		session := *sessionID
		if session == "" {
			session, _ = os.Hostname()
			if session == "" {
				session = "wc-harness"
			}
		}
		fuse := client.New(*fuseSocket, nil)
		mountPoint, err := fuse.Mount(session, *fuseProject)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wc-harness: mount workspace: %v\n", err)
			return 1
		}
		defer func() {
			if err := fuse.Unmount(*fuseProject); err != nil {
				fmt.Fprintf(os.Stderr, "wc-harness: unmount workspace: %v\n", err)
			}
		}()
		absWorkspace = mountPoint
		fmt.Fprintf(os.Stderr, "==> fuse mount=%s project=%s session=%s\n", mountPoint, *fuseProject, session)
	}

	cfg, err := config.Load(*envPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wc-harness: %v\n", err)
		return 1
	}

	registry, err := tools.Load(*toolsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wc-harness: %v\n", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(os.Stderr, "==> model=%s workspace=%s tools=%d\n", cfg.Model, absWorkspace, registry.Len())

	runner := &agent.Agent{
		Client: &llm.Client{
			BaseURL:         cfg.BaseURL,
			APIKey:          cfg.APIKey,
			Model:           cfg.Model,
			ReasoningEffort: cfg.ReasoningEffort,
		},
		Tools:     registry,
		Workspace: absWorkspace,
		MaxTurns:  *maxTurns,
		Out:       os.Stdout,
		Log:       os.Stderr,
	}
	if err := runner.Run(ctx, prompt); err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "wc-harness: interrupted")
			return 130
		}
		fmt.Fprintf(os.Stderr, "wc-harness: %v\n", err)
		return 1
	}
	return 0
}
