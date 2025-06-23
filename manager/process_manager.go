package manager

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

type InvokeResult struct {
	Stdout     string
	Stderr     string
	ExitCode   int
	DurationMs int64
}

func InvokeFunction(id, input string) (*InvokeResult, error) {
	fn, err := GetFunction(id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, fn.BinPath)

	if input != "" {
		cmd.Stdin = bytes.NewBufferString(input)
	}
	var out bytes.Buffer
	var errBuf bytes.Buffer

	cmd.Stdout = &out
	cmd.Stderr = &errBuf

	start := time.Now()

	err = cmd.Run()
	duration := time.Since(start).Milliseconds()

	exitCode := 0

	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		errBuf.WriteString(err.Error())
		exitCode = -1
	}

	return &InvokeResult{
		Stdout:     out.String(),
		Stderr:     errBuf.String(),
		ExitCode:   exitCode,
		DurationMs: duration,
	}, nil
}
