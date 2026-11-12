package driver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// A backend stream compiles modules while the front end is still
// generating the rest: marmot-backend reads job lines from its stdin
// (--batch=-) and starts each as it arrives. Only tier0 batches can stream;
// the LLVM batch needs every module up front for its cross-module phases.
type backendStream struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	in     io.WriteCloser
	out    bytes.Buffer
	mu     sync.Mutex
	err    error // first write error
	objs   []string
	work   string
}

// canStream reports whether the backend can start jobs as they arrive.
func (b *build) canStream() bool {
	return b.haveBackend() && os.Getenv("MARMOT_TIER0") == "1" && !b.wpo() && b.cfg.Dialect != "go" &&
		(b.cfg.Emit == "exe" || b.cfg.Emit == "obj") && os.Getenv("MARMOT_NO_STREAM") == ""
}

// startStream starts marmot-backend for n modules.
func (b *build) startStream(n int) (*backendStream, error) {
	backend := filepath.Join(b.cfg.Root, "build", "bin", "marmot-backend")
	args := b.backendArgs("obj", "-")
	if b.cfg.PrintCmd {
		fmt.Fprintln(b.cfg.Stderr, backend+" "+strings.Join(args, " "))
	}
	s := &backendStream{objs: make([]string, n), work: b.work}
	for i := range s.objs {
		s.objs[i] = filepath.Join(b.work, fmt.Sprintf("m%03d.o", i))
	}
	ctx, cancel := context.WithTimeout(context.Background(), toolTimeout())
	s.cancel = cancel
	s.cmd = exec.CommandContext(ctx, backend, args...)
	s.cmd.Stdout = &s.out
	s.cmd.Stderr = &s.out
	in, err := s.cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	s.in = in
	startGroup(s.cmd)
	if err := s.cmd.Start(); err != nil {
		return nil, err
	}
	children.add(s.cmd.Process.Pid)
	return s, nil
}

// send hands module i, written to path, to the backend.
func (s *backendStream) send(i int, path, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.objs) <= i {
		s.objs = append(s.objs, filepath.Join(s.work, fmt.Sprintf("m%03d.o", len(s.objs))))
	}
	line := path + " " + s.objs[i]
	if key != "" {
		line += " " + key
	}
	if s.err == nil {
		_, s.err = io.WriteString(s.in, line+"\n")
	}
}

// finish ends the batch and waits for the backend.
func (s *backendStream) finish(b *build) ([]string, error) {
	s.in.Close()
	err := s.cmd.Wait()
	children.remove(s.cmd.Process.Pid)
	killGroup(s.cmd.Process.Pid)
	s.cancel()
	b.lastOutput = s.out.String()
	if err != nil || s.err != nil {
		if err == nil {
			err = s.err
		}
		msg := strings.TrimSpace(b.lastOutput)
		if len(msg) > 4000 {
			msg = msg[:4000] + "\n..."
		}
		return nil, fmt.Errorf("marmot-backend failed: %v\n%s", err, msg)
	}
	b.logBackendTimes()
	return s.objs, nil
}

// abort stops the backend after a front end error.
func (s *backendStream) abort() {
	s.in.Close()
	killGroup(s.cmd.Process.Pid)
	s.cmd.Wait()
	children.remove(s.cmd.Process.Pid)
	s.cancel()
}
