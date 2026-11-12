package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// The server answers initialize and tools/list, and runs a tool through
// the marmot binary (here a stand-in script that echoes its arguments).
func TestMCPServer(t *testing.T) {
	dir := t.TempDir()
	fake := dir + "/marmot"
	os.WriteFile(fake, []byte("#!/bin/sh\necho \"$@\" >&2\nexit 1\n"), 0o755)
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"fix","arguments":{"dir":"` + dir + `","codes":["M0005"],"all":true}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"nope"}`,
	}, "\n")
	var out bytes.Buffer
	serveMCP(strings.NewReader(in), &out, fake, time.Minute)
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("%d replies, want 4:\n%s", len(lines), out.String())
	}
	var r []map[string]any
	for _, l := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatal(err)
		}
		r = append(r, m)
	}
	if v := r[0]["result"].(map[string]any)["protocolVersion"]; v != "2025-06-18" {
		t.Errorf("protocol %v", v)
	}
	tools := r[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 5 {
		t.Errorf("%d tools", len(tools))
	}
	res := r[2]["result"].(map[string]any)
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if res["isError"] != true || strings.TrimSpace(text) != "fix -code M0005 -all" {
		t.Errorf("tool call: %v %q", res["isError"], text)
	}
	if r[3]["error"].(map[string]any)["code"].(float64) != -32601 {
		t.Errorf("unknown method: %v", r[3])
	}
}
