package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Bash is a tool for executing shell commands.
type Bash struct {
	// Cwd is the working directory for commands.
	Cwd string
	// Timeout for command execution.
	Timeout time.Duration
}

func (Bash) Name() string { return "bash" }

func (Bash) Description() string {
	return "Execute a shell command and return its output, exit code, and any errors."
}

func (Bash) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {
				"type": "string",
				"description": "The shell command to execute"
			},
			"cwd": {
				"type": "string",
				"description": "Working directory for the command. Defaults to current directory."
			},
			"timeout": {
				"type": "integer",
				"description": "Timeout in seconds. Defaults to 30."
			}
		},
		"required": ["command"]
	}`)
}

func (t Bash) Execute(ctx context.Context, args json.RawMessage, update ToolUpdate) (*ToolResult, error) {
	var params struct {
		Command string `json:"command"`
		Cwd     string `json:"cwd"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Command == "" {
		return nil, fmt.Errorf("command is required")
	}

	cwd := params.Cwd
	if cwd == "" {
		cwd = t.Cwd
	} else {
		cwd = NormalizePath(cwd)
	}
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get working directory: %w", err)
		}
	}

	timeout := params.Timeout
	if timeout <= 0 {
		timeout = 30
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// Run command
	cmd := exec.CommandContext(ctx, "bash", "-c", params.Command)
	cmd.Dir = cwd

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	output := stdout.String()
	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			return nil, fmt.Errorf("run command: %w", err)
		}
	}

	result := &ToolResult{
		Content: output,
		Metadata: map[string]any{
			"exitCode": exitCode,
			"cwd":      cwd,
			"elapsed":  elapsed.String(),
		},
	}

	if stderr.Len() > 0 {
		result.Metadata["stderr"] = stderr.String()
	}

	if exitCode != 0 {
		result.IsError = true
		result.Content = strings.TrimSpace(output) + "\n" + stderr.String()
	}

	if update != nil {
		update(result)
	}

	return result, nil
}

func (Bash) IsReadOnly() bool { return false }




