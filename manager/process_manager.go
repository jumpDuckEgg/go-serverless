//go:build darwin
// +build darwin

package manager

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
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

	if fn.WasmPath != "" {
		if _, err := os.Stat(fn.WasmPath); err == nil {
			return invokeWasm(fn.WasmPath, input)
		}
	}

	return invokeBin(fn.BinPath, input)
}

func invokeWasm(wasmPath, input string) (*InvokeResult, error) {
	ctx := context.Background()
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, err
	}

	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)

	if _, err = wasi_snapshot_preview1.Instantiate(ctx, r); err != nil {
		return nil, err
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	config := wazero.NewModuleConfig().
		WithStdout(stdout).
		WithStderr(stderr)
	if input != "" {
		config = config.WithStdin(bytes.NewBufferString(input))
	}

	compiled, err := r.CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	mod, err := r.InstantiateModule(ctx, compiled, config)
	duration := time.Since(start).Milliseconds()
	if err != nil {
		return &InvokeResult{
			Stdout:     stdout.String(),
			Stderr:     stderr.String() + err.Error(),
			ExitCode:   -1,
			DurationMs: duration,
		}, nil
	}
	defer mod.Close(ctx)

	exitCode := 0
	entry := mod.ExportedFunction("_start")
	if entry != nil {
		_, callErr := entry.Call(ctx)
		if callErr != nil {
			var exitErr *sys.ExitError
			if errors.As(callErr, &exitErr) {
				exitCode = int(exitErr.ExitCode())
			} else {
				exitCode = -1
				stderr.WriteString(callErr.Error())
			}
		}
	} else {
		exitCode = -1
		stderr.WriteString("no _start entry in wasm module")
	}

	fmt.Println("invoke wasm")

	return &InvokeResult{
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		ExitCode:   exitCode,
		DurationMs: duration,
	}, nil
}

func invokeBin(binPath, input string) (*InvokeResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath)

	if input != "" {
		cmd.Stdin = bytes.NewBufferString(input)
	}
	var out bytes.Buffer
	var errBuf bytes.Buffer

	cmd.Stdout = &out
	cmd.Stderr = &errBuf

	start := time.Now()

	err := cmd.Run()
	duration := time.Since(start).Milliseconds()

	exitCode := 0

	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		errBuf.WriteString(err.Error())
		exitCode = -1
	}

	fmt.Println("invoke bin")

	return &InvokeResult{
		Stdout:     out.String(),
		Stderr:     errBuf.String(),
		ExitCode:   exitCode,
		DurationMs: duration,
	}, nil
}
