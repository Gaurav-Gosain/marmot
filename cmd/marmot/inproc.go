package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
)

// inProcess runs one marmot command inside this process, for the compile service code when
// there are no processes (wasip1: the browser playground's front end). One at a time: it
// changes the working directory, the environment and standard output and error.
var inProcessMu sync.Mutex

func inProcess(_ context.Context, dir string, args, env []string) ([]byte, error) {
	inProcessMu.Lock()
	defer inProcessMu.Unlock()
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		return nil, err
	}
	defer func() { _ = os.Chdir(wd) }() // back to where the caller was
	var restore []func()
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		old, had := os.LookupEnv(k)
		_ = os.Setenv(k, v)
		restore = append(restore, func() {
			if had {
				_ = os.Setenv(k, old)
			} else {
				_ = os.Unsetenv(k)
			}
		})
	}
	defer func() {
		for _, r := range restore {
			r()
		}
	}()
	// The command's output goes to a file (no pipes here), read back afterwards.
	out, err := os.CreateTemp("", "marmot-out-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(out.Name()) }() // the output was read back
	stdout, stderr, eo := os.Stdout, os.Stderr, errOut
	os.Stdout, os.Stderr, errOut = out, out, out
	code := run(args)
	os.Stdout, os.Stderr, errOut = stdout, stderr, eo
	_ = out.Close()
	data, rerr := os.ReadFile(out.Name())
	if rerr != nil {
		return nil, rerr
	}
	if code != 0 {
		return data, exitError(code)
	}
	return data, nil
}

type exitError int

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", int(e)) }
