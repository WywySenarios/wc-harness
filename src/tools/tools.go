// Package tools loads declared tools from a staged tool tree and executes
// them over their JSON argument contract.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/WywySenarios/wc-harness/src/llm"
	"gopkg.in/yaml.v3"
)

const maxCapture = 64 << 10

var (
	ErrInvalidManifest  = errors.New("invalid tool manifest")
	ErrNoTools          = errors.New("no tool manifests found")
	ErrInvalidArguments = errors.New("tool arguments must be a JSON object")
)

// Tool is one manifest-backed executable.
type Tool struct {
	Name        string
	Description string
	Parameters  json.RawMessage
	Path        string
}

// Registry is the set of tools available to the agent.
type Registry struct {
	tools map[string]Tool
	order []string
}

// Result is the captured outcome of one tool execution.
type Result struct {
	ExitCode  int
	Stdout    string
	Stderr    string
	Duration  time.Duration
	Truncated bool
}

type manifest struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	ArgvSchema  map[string]any `yaml:"argv_schema"`
}

// Load reads <dir>/manifests/*.yaml and resolves each executable from <dir>.
func Load(dir string) (*Registry, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve tool dir: %w", err)
	}
	manifestDir := filepath.Join(dir, "manifests")
	entries, err := os.ReadDir(manifestDir)
	if err != nil {
		return nil, fmt.Errorf("read tool manifests: %w", err)
	}

	registry := &Registry{tools: make(map[string]Tool)}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(manifestDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}

		var spec manifest
		if err := yaml.Unmarshal(data, &spec); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}

		name := strings.TrimSuffix(entry.Name(), ".yaml")
		if spec.Name != name {
			return nil, fmt.Errorf("%w: %s: manifest name is %q, expected %q", ErrInvalidManifest, path, spec.Name, name)
		}
		if strings.ContainsAny(name, `/\`) || name == ".." || name == "." {
			return nil, fmt.Errorf("%w: %s: invalid tool name %q", ErrInvalidManifest, path, name)
		}
		if spec.ArgvSchema == nil {
			return nil, fmt.Errorf("%w: %s: argv_schema is required", ErrInvalidManifest, path)
		}
		if kind, _ := spec.ArgvSchema["type"].(string); kind != "object" {
			return nil, fmt.Errorf("%w: %s: argv_schema.type must be 'object'", ErrInvalidManifest, path)
		}
		parameters, err := json.Marshal(spec.ArgvSchema)
		if err != nil {
			return nil, fmt.Errorf("%s: encode argv_schema: %w", path, err)
		}

		executable := filepath.Join(dir, name)
		info, err := os.Stat(executable)
		if err != nil {
			return nil, fmt.Errorf("%s declares %s, which is not staged: %w", path, name, err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return nil, fmt.Errorf("%w: %s is not an executable file", ErrInvalidManifest, executable)
		}

		description := spec.Description
		if description == "" {
			description = name
		}
		registry.tools[name] = Tool{
			Name:        name,
			Description: description,
			Parameters:  parameters,
			Path:        executable,
		}
		registry.order = append(registry.order, name)
	}

	if len(registry.tools) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoTools, manifestDir)
	}
	return registry, nil
}

// Len reports how many tools are registered.
func (r *Registry) Len() int { return len(r.tools) }

// Lookup returns the tool with the given name.
func (r *Registry) Lookup(name string) (Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

// Schemas returns the OpenAI function definitions advertised to the model.
func (r *Registry) Schemas() []llm.Tool {
	schemas := make([]llm.Tool, 0, len(r.order))
	for _, name := range r.order {
		tool := r.tools[name]
		schemas = append(schemas, llm.Tool{
			Type: "function",
			Function: llm.FunctionDef{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			},
		})
	}
	return schemas
}

// Run executes the tool with dir as its working directory and one JSON argv
// element. A non-zero tool exit code is a result, not an error.
func (t Tool) Run(ctx context.Context, dir, argsJSON string) (Result, error) {
	trimmed := strings.TrimSpace(argsJSON)
	if trimmed == "" || trimmed[0] != '{' || !json.Valid([]byte(trimmed)) {
		return Result{}, ErrInvalidArguments
	}

	cmd := exec.CommandContext(ctx, t.Path, trimmed)
	cmd.Dir = dir
	stdout := &capBuffer{limit: maxCapture}
	stderr := &capBuffer{limit: maxCapture}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = time.Second

	start := time.Now()
	err := cmd.Run()
	result := Result{
		Stdout:    stdout.buf.String(),
		Stderr:    stderr.buf.String(),
		Duration:  time.Since(start),
		Truncated: stdout.truncated || stderr.truncated,
	}

	var exitErr *exec.ExitError
	switch {
	case err == nil:
		result.ExitCode = 0
	case errors.As(err, &exitErr):
		result.ExitCode = exitErr.ExitCode()
		if result.ExitCode < 0 {
			result.ExitCode = 1
		}
	default:
		return result, fmt.Errorf("run %s: %w", t.Name, err)
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	return result, nil
}

type capBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

// Write to an upper-bounded buffer.
func (c *capBuffer) Write(p []byte) (int, error) {
	remaining := c.limit - c.buf.Len()
	if remaining <= 0 {
		c.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		c.buf.Write(p[:remaining])
		c.truncated = true
		return len(p), nil
	}
	return c.buf.Write(p)
}
