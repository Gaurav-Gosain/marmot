package check

import (
	"bytes"
	"encoding/gob"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

// A run over a tree that has not changed since the last run with the same
// options answers with that run's result. The key covers every file go
// list reads in the module trees (by size and time), the lint and
// platform configuration, the go command, the environment that changes a
// build, the checks asked for and the day (the vulnerability database
// changes daily). Changed mode depends on git state as well and is not
// memoized.

func runKey(sc *scope, opts Options, lc *lintConfig, checks []string) string {
	h := newHasher("run-1")
	h.str(toolID(), sc.dir, time.Now().UTC().Format("2006-01-02"))
	h.str(checks...)
	h.str(sc.patterns...)
	h.str(opts.Tags...)
	h.str(opts.Targets...)
	if opts.Offline {
		h.str("offline")
	}
	h.str(envKey(opts.Tags, hostGOOS(), hostGOARCH()))
	for _, k := range []string{"GOFLAGS", "GOOS", "GOARCH", "CGO_ENABLED", "GOEXPERIMENT", "GOTOOLCHAIN", "GOWORK", "GOAMD64", "GOARM64", "GOPROXY", "GONOSUMDB", "GOPRIVATE"} {
		h.str(k + "=" + os.Getenv(k))
	}
	if p, err := exec.LookPath("go"); err == nil {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			p = r
		}
		h.str(p, statOf(p))
	}
	extra := []string{}
	if lc != nil {
		extra = append(extra, lc.path)
	}
	for _, d := range []string{sc.dir, workRoot(sc.dir)} {
		if d != "" {
			extra = append(extra, filepath.Join(d, "scripts", "platforms.txt"), filepath.Join(d, "platforms.txt"), filepath.Join(d, ".golangci.yml"), filepath.Join(d, ".golangci.yaml"))
		}
	}
	for _, p := range extra {
		h.str(p, statOf(p))
	}
	files := sc.treeFiles()
	names := make([]string, 0, len(files))
	for p := range files {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		h.str(p, files[p])
	}
	return h.sum()
}

func statOf(p string) string {
	info, err := os.Stat(p)
	if err != nil {
		return "-"
	}
	return statKey(info)
}

// memoizable reports whether a result can stand for the next run: no
// check failed to run (a network error, a tool error), only on findings.
func memoizable(res *Result, opts Options) bool {
	for _, c := range res.Checks {
		if c.Status == Skipped && !opts.Offline {
			return false
		}
		if c.Status == Failed && len(c.Findings) == 0 {
			return false // the check could not run (load errors, a tool error)
		}
	}
	return true
}

func loadRun(st *store, key string) *Result {
	data := st.get(key)
	if data == nil {
		return nil
	}
	var res Result
	if gob.NewDecoder(bytes.NewReader(data)).Decode(&res) != nil {
		return nil
	}
	for _, c := range res.Checks {
		c.Cached = true
		c.Duration = 0
	}
	res.Stats = Stats{}
	return &res
}

func saveRun(st *store, key string, res *Result) {
	var buf bytes.Buffer
	if gob.NewEncoder(&buf).Encode(res) == nil {
		st.put(key, buf.Bytes())
	}
}
