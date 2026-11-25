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
	"github.com/WywySenarios/wc-harness/src/events"
	"github.com/WywySenarios/wc-harness/src/llm"
	"github.com/WywySenarios/wc-harness/src/tools"
)

func main() {
	os.Exit(run())
}

// stderr writes a diagnostic to the process error stream.
func stderr(format string, args ...any) {
	if _, err := fmt.Fprintf(os.Stderr, format, args...); err != nil {
		panic(fmt.Sprintf("write stderr: %v", err))
	}
}

func run() int {
	envPath := flag.String("env", ".env", "dotenv file with WC_LLM_* settings")
	toolsDir := flag.String("tools", "tools", "staged tool tree (binaries + manifests/)")
	maxTurns := flag.Int("max-turns", 10, "maximum model turns before aborting")
	fuseSocket := flag.String("fuse-socket", "", "wc-fuse control socket; enables a FUSE workspace when set")
	fuseProject := flag.String("fuse-project", "", "host project root to mount through wc-fuse")
	sessionID := flag.String("session", "", "session id reported to wc-fuse (default: hostname)")
	eventsMode := flag.String("events", "text", "event output: text or jsonl")
	flag.Usage = func() {
		stderr("usage: %s [flags] [workspace]\n\n", filepath.Base(os.Args[0]))
		stderr("Runs the task in <workspace>/PROMPT.md. The workspace defaults to the current directory.\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() > 1 {
		stderr("wc-harness: expected at most one workspace argument\n")
		flag.Usage()
		return 2
	}
	workspace := "."
	if flag.NArg() == 1 {
		workspace = flag.Arg(0)
	}
	if *maxTurns < 1 {
		stderr("wc-harness: -max-turns must be at least 1\n")
		return 2
	}
	if *fuseSocket != "" && *fuseProject == "" {
		stderr("wc-harness: -fuse-project is required with -fuse-socket\n")
		return 2
	}
	if *eventsMode != "text" && *eventsMode != "jsonl" {
		stderr("wc-harness: -events must be text or jsonl\n")
		return 2
	}

	session := *sessionID
	if session == "" {
		hostname, err := os.Hostname()
		if err == nil {
			session = hostname
		}
		if session == "" {
			session = "wc-harness"
		}
	}

	emitter := events.New(*eventsMode, os.Stdout, os.Stderr)

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		emitter.Error(fmt.Sprintf("resolve workspace: %v", err))
		emitter.SessionEnd(false)
		return 1
	}

	promptData, err := os.ReadFile(filepath.Join(absWorkspace, "PROMPT.md"))
	if err != nil {
		emitter.Error(fmt.Sprintf("read PROMPT.md: %v", err))
		emitter.SessionEnd(false)
		return 1
	}
	prompt := strings.TrimSpace(string(promptData))
	if prompt == "" {
		emitter.Error("PROMPT.md is empty")
		emitter.SessionEnd(false)
		return 1
	}

	if *fuseSocket != "" {
		fuse := client.New(*fuseSocket, nil)
		mountPoint, err := fuse.Mount(session, *fuseProject)
		if err != nil {
			emitter.Error(fmt.Sprintf("mount workspace: %v", err))
			emitter.SessionEnd(false)
			return 1
		}
		defer func() {
			if err := fuse.Unmount(*fuseProject); err != nil {
				stderr("wc-harness: unmount workspace: %v\n", err)
			}
		}()
		absWorkspace = mountPoint
		stderr("==> fuse mount=%s project=%s session=%s\n", mountPoint, *fuseProject, session)
	}

	cfg, err := config.Load(*envPath)
	if err != nil {
		emitter.Error(err.Error())
		emitter.SessionEnd(false)
		return 1
	}
	emitter.SessionStart(session, cfg.Model)

	registry, err := tools.Load(*toolsDir)
	if err != nil {
		emitter.Error(err.Error())
		emitter.SessionEnd(false)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	stderr("==> model=%s workspace=%s tools=%d\n", cfg.Model, absWorkspace, registry.Len())

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
		Events:    emitter,
	}
	if err := runner.Run(ctx, prompt); err != nil {
		if errors.Is(err, context.Canceled) {
			emitter.Error("interrupted")
			emitter.SessionEnd(false)
			return 130
		}
		emitter.Error(err.Error())
		emitter.SessionEnd(false)
		return 1
	}
	emitter.SessionEnd(true)
	return 0
}
