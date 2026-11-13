package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// marmot mcp serves the Model Context Protocol on stdin and stdout
// (JSON-RPC 2.0, one message per line), so coding agents call marmot as
// tools and get the JSON diagnostics directly. Each tool runs marmot as a
// child process in JSON mode in the given directory.

type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	args        func(map[string]any) ([]string, error)
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
func listProp(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}

func schema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

var dirProp = strProp("directory to run in (a Go module or below one); default the server's directory")

func mcpTools() []mcpTool {
	strs := func(m map[string]any, k string) []string {
		var out []string
		if v, ok := m[k].([]any); ok {
			for _, x := range v {
				if s, ok := x.(string); ok {
					out = append(out, s)
				}
			}
		}
		return out
	}
	str := func(m map[string]any, k string) string { s, _ := m[k].(string); return s }
	return []mcpTool{
		{Name: "build", Description: "Compile a Go main package with marmot. Returns JSON Lines: one diagnostic per error (code, message, span, label, notes, help, fixes with exact edits, source context, rendered text) and a final result line {ok, errors, output}.",
			InputSchema: schema(map[string]any{"dir": dirProp, "packages": listProp("package patterns or .go files (default .)"),
				"flags": listProp("extra build flags, Go style, e.g. [\"-release\", \"-o\", \"app\"]")}),
			args: func(m map[string]any) ([]string, error) {
				return append(append([]string{"build"}, strs(m, "flags")...), strs(m, "packages")...), nil
			}},
		{Name: "run", Description: "Build and run a Go program. Returns the build diagnostics (JSON Lines) when it does not compile; otherwise the program's stdout and stderr (a panic is one JSON object: message, frames with file and line) and its exit code.",
			InputSchema: schema(map[string]any{"dir": dirProp, "package": strProp("package or .go file to run (default .)"),
				"args": listProp("program arguments"), "flags": listProp("extra build flags")}),
			args: func(m map[string]any) ([]string, error) {
				pkg := str(m, "package")
				if pkg == "" {
					pkg = "."
				}
				a := append(append([]string{"run"}, strs(m, "flags")...), pkg)
				return append(a, strs(m, "args")...), nil
			}},
		{Name: "test", Description: "Run package tests. Returns one JSON line per test (name, status, durationMs, file, line, message), a diagnostic per failure with source context, and a result line.",
			InputSchema: schema(map[string]any{"dir": dirProp, "packages": listProp("packages (default .)"),
				"run": strProp("regexp selecting tests, as -run"), "flags": listProp("extra build or test flags")}),
			args: func(m map[string]any) ([]string, error) {
				a := append([]string{"test"}, strs(m, "flags")...)
				if r := str(m, "run"); r != "" {
					a = append(a, "-run", r)
				}
				return append(a, strs(m, "packages")...), nil
			}},
		{Name: "check", Description: "Run the code checks in one pass: fmt, vet, lint (golangci-lint standard set, reads .golangci.yml), modernize, gopls, nil, vuln, tidy, cross, deadcode (exhaustive on request). Returns one JSON diagnostic per finding (check, analyzer, span, message, fixes), one {kind:check, check, status, findings} line per check and a result line. Repeated calls are fast: results are cached per package.",
			InputSchema: schema(map[string]any{"dir": dirProp, "packages": listProp("package patterns (default: every module of the workspace)"),
				"only":    listProp("only these checks, e.g. [\"vet\", \"lint\"]"),
				"changed": strProp("check only what changed against this git ref (\"default\": origin/main, else main)"),
				"staged":  boolProp("check what git has staged instead of the working tree"),
				"offline": boolProp("no network: skip the vulnerability check")}),
			args: func(m map[string]any) ([]string, error) {
				a := []string{"check"}
				if o := strs(m, "only"); len(o) > 0 {
					a = append(a, "-only", strings.Join(o, ","))
				}
				switch c := str(m, "changed"); c {
				case "":
				case "default":
					a = append(a, "-changed")
				default:
					a = append(a, "-changed="+c)
				}
				for _, f := range []string{"staged", "offline"} {
					if v, _ := m[f].(bool); v {
						a = append(a, "-"+f)
					}
				}
				return append(a, strs(m, "packages")...), nil
			}},
		{Name: "fix", Description: "Apply the machine applicable fixes of the build errors (imports, unused names, := to =, ...) and build again; then, once the build is clean, the fixes of check findings (fmt, modernize; vet and lint suggestions with all), keeping a fix only if the packages still type check. Reports each applied fix and the errors left.",
			InputSchema: schema(map[string]any{"dir": dirProp, "packages": listProp("packages (default .)"),
				"codes": listProp("only these error codes, e.g. [\"M0005\"]"), "all": boolProp("also apply fixes that may be incorrect (typo corrections, vet and lint suggestions)"),
				"only": listProp("only the fixes of these checks, e.g. [\"modernize\"]"),
				"diff": boolProp("print the check fixes as patches and change nothing"),
				"test": boolProp("keep a check fix only if the tests of its package pass")}),
			args: func(m map[string]any) ([]string, error) {
				a := []string{"fix"}
				if c := strs(m, "codes"); len(c) > 0 {
					a = append(a, "-code", strings.Join(c, ","))
				}
				if o := strs(m, "only"); len(o) > 0 {
					a = append(a, "-only", strings.Join(o, ","))
				}
				for _, f := range []string{"all", "diff", "test"} {
					if v, _ := m[f].(bool); v {
						a = append(a, "-"+f)
					}
				}
				return append(a, strs(m, "packages")...), nil
			}},
		{Name: "explain", Description: "Explain a marmot error code (M0002, ...): description, wrong and right example, usual fix. With no code, list all codes.",
			InputSchema: schema(map[string]any{"code": strProp("error code, e.g. M0014")}),
			args: func(m map[string]any) ([]string, error) {
				a := []string{"explain", "-json"}
				if c := str(m, "code"); c != "" {
					a = append(a, c)
				}
				return a, nil
			}},
	}
}

func newMCPFlags() (*flag.FlagSet, *time.Duration) {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	return fs, fs.Duration("timeout", 5*time.Minute, "time limit of one tool call")
}

func cmdMCP(args []string) int {
	fs, timeout := newMCPFlags()
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return usageError("mcp", err)
	}
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "marmot mcp:", err)
		return 1
	}
	return serveMCP(os.Stdin, os.Stdout, self, *timeout)
}

func serveMCP(in io.Reader, out io.Writer, self string, timeout time.Duration) int {
	tools := mcpTools()
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	reply := func(id json.RawMessage, result any, e *rpcError) {
		_ = enc.Encode(rpcMsg{JSONRPC: "2.0", ID: id, Result: result, Error: e})
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var m rpcMsg
		if err := json.Unmarshal(line, &m); err != nil {
			reply(json.RawMessage("null"), nil, &rpcError{-32700, "parse error: " + err.Error()})
			continue
		}
		if len(m.ID) == 0 {
			continue // a notification (initialized, cancelled)
		}
		switch m.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(m.Params, &p) // optional: without it the default version answers
			v := p.ProtocolVersion
			if v == "" {
				v = "2025-06-18"
			}
			reply(m.ID, map[string]any{
				"protocolVersion": v,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "marmot", "version": version},
				"instructions":    "marmot compiles Go programs. Tools return JSON Lines diagnostics with exact spans, labels and machine applicable fixes; use build to check code, fix to apply safe fixes, explain for an error code.",
			}, nil)
		case "ping":
			reply(m.ID, map[string]any{}, nil)
		case "tools/list":
			reply(m.ID, map[string]any{"tools": tools}, nil)
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if err := json.Unmarshal(m.Params, &p); err != nil {
				reply(m.ID, nil, &rpcError{-32602, "bad params: " + err.Error()})
				continue
			}
			var tool *mcpTool
			for i := range tools {
				if tools[i].Name == p.Name {
					tool = &tools[i]
				}
			}
			if tool == nil {
				reply(m.ID, nil, &rpcError{-32602, "unknown tool " + p.Name})
				continue
			}
			if p.Arguments == nil {
				p.Arguments = map[string]any{}
			}
			text, isErr := callTool(self, tool, p.Arguments, timeout)
			reply(m.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": isErr}, nil)
		default:
			reply(m.ID, nil, &rpcError{-32601, "method not found: " + m.Method})
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "marmot mcp:", err)
		return 1
	}
	return 0
}

// callTool runs marmot for a tool call and returns its output.
func callTool(self string, t *mcpTool, args map[string]any, timeout time.Duration) (string, bool) {
	argv, err := t.args(args)
	if err != nil {
		return err.Error(), true
	}
	dir, _ := args["dir"].(string)
	if dir == "" {
		dir, _ = os.Getwd()
	}
	if !filepath.IsAbs(dir) {
		wd, _ := os.Getwd()
		dir = filepath.Join(wd, dir)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, self, argv...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "MARMOT_OUTPUT=json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.Stdin = strings.NewReader("")
	err = cmd.Run()
	var b strings.Builder
	if t.Name == "run" && stdout.Len() > 0 {
		b.WriteString("stdout:\n")
		b.Write(stdout.Bytes())
		if !strings.HasSuffix(stdout.String(), "\n") {
			b.WriteByte('\n')
		}
		b.WriteString("stderr:\n")
	} else {
		b.Write(stdout.Bytes())
	}
	b.Write(stderr.Bytes())
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		return err.Error(), true
	}
	if ctx.Err() != nil {
		fmt.Fprintf(&b, "marmot: timed out after %v\n", timeout)
	}
	if t.Name == "run" {
		fmt.Fprintf(&b, "exit code: %d\n", code)
	}
	return b.String(), code != 0
}
