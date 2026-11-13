package driver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/loader"
)

// Linking through the clang driver costs ~80 ms of process start and SDK
// lookup on every build. The linker command clang would run is computed
// once per (clang binary, link flags) with clang -### and cached; links
// then run the linker directly.

const (
	objSentinel = "marmot-objects-sentinel.o"
	outSentinel = "marmot-output-sentinel"
)

// ldCommand returns the linker command for clang args (whose object files
// are objs and output is out), from the cache or by asking clang once.
func (b *build) ldCommand(args, objs []string, out string) ([]string, bool) {
	isObj := map[string]bool{}
	for _, o := range objs {
		isObj[o] = true
	}
	var key []string
	dir := filepath.Join(b.cfg.Root, "build", "cache")
	sentinel := filepath.Join(dir, objSentinel)
	tmplArgs := []string{}
	placed := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-o" && i+1 < len(args) {
			tmplArgs = append(tmplArgs, "-o", outSentinel)
			i++
			continue
		}
		if isObj[a] {
			if !placed {
				tmplArgs = append(tmplArgs, sentinel)
				placed = true
			}
			continue
		}
		if b.work != "" {
			a = strings.ReplaceAll(a, b.work, workSentinel)
		}
		tmplArgs = append(tmplArgs, a)
		key = append(key, a)
	}
	clang := llvmTool("clang")
	h := sha256.New()
	fileSig(h, clang)
	fmt.Fprintf(h, "%q\n", key)
	cacheFile := filepath.Join(dir, "ld-"+hex.EncodeToString(h.Sum(nil))[:24]+".json")
	var tmpl []string
	if data, err := os.ReadFile(cacheFile); err == nil && json.Unmarshal(data, &tmpl) == nil && len(tmpl) > 0 {
		touch(cacheFile)
		return expandLd(tmpl, sentinel, objs, out, b.work), true
	}
	if !fileExists(sentinel) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, false
		}
		if err := os.WriteFile(sentinel, nil, 0o644); err != nil {
			return nil, false
		}
	}
	outb, err := exec.Command(clang, append([]string{"-###"}, tmplArgs...)...).CombinedOutput()
	if err != nil {
		return nil, false
	}
	lines := strings.Split(strings.TrimSpace(string(outb)), "\n")
	tmpl = parseQuoted(lines[len(lines)-1])
	if len(tmpl) == 0 || !filepath.IsAbs(tmpl[0]) {
		return nil, false
	}
	hasObj, hasOut := false, false
	for _, a := range tmpl {
		hasObj = hasObj || a == sentinel
		hasOut = hasOut || a == outSentinel
	}
	if !hasObj || !hasOut {
		return nil, false
	}
	if data, err := json.Marshal(tmpl); err == nil {
		if err := loader.WriteAtomic(cacheFile, data); err != nil {
			b.logf("link template cache: %v", err)
		}
	}
	return expandLd(tmpl, sentinel, objs, out, b.work), true
}

func expandLd(tmpl []string, sentinel string, objs []string, out, work string) []string {
	var cmd []string
	for i := 0; i < len(tmpl); i++ {
		a := tmpl[i]
		// No object is LLVM bitcode: without -lto_library ld does not load
		// libLTO (16 ms of a 100 ms link of cmd/marmot), and the -mllvm
		// options clang adds are for LTO code generation.
		if (a == "-lto_library" || a == "-mllvm") && i+1 < len(tmpl) {
			i++
			continue
		}
		switch a {
		case sentinel:
			cmd = append(cmd, objs...)
		case outSentinel:
			cmd = append(cmd, out)
		default:
			cmd = append(cmd, strings.ReplaceAll(a, workSentinel, work))
		}
	}
	return cmd
}

// workSentinel stands for the build's work directory in ld templates.
const workSentinel = "/marmot-work-sentinel"

// parseQuoted splits a clang -### command line (each argument in double
// quotes, backslash escapes).
func parseQuoted(line string) []string {
	var out []string
	var cur strings.Builder
	in := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case !in && c == '"':
			in = true
			cur.Reset()
		case in && c == '\\' && i+1 < len(line):
			i++
			cur.WriteByte(line[i])
		case in && c == '"':
			in = false
			out = append(out, cur.String())
		case in:
			cur.WriteByte(c)
		}
	}
	return out
}

func (b *build) dropLdCache() {
	matches, _ := filepath.Glob(filepath.Join(b.cfg.Root, "build", "cache", "ld-*.json"))
	for _, m := range matches {
		if err := os.Remove(m); err != nil {
			b.logf("link template cache: %v", err)
		}
	}
}
