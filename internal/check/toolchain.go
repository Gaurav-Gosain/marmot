package check

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"
)

var reAnalyzerName = regexp.MustCompile(`Name:\s+"([a-z0-9_]+)"`)

var toolchainNames struct {
	once  sync.Once
	names map[string]bool
}

// toolchainAnalyzers returns the names of the analyzers that the go
// command's own vet and fix know: those of the x/tools copy vendored in
// GOROOT. marmot links a newer x/tools; restricting go vet and go fix
// to these names makes marmot check report what the installed go vet and
// go fix report. It returns nil when GOROOT has no vendored copy.
func toolchainAnalyzers(root string) map[string]bool {
	toolchainNames.once.Do(func() {
		if root == "" {
			return
		}
		passes := filepath.Join(root, "src", "cmd", "vendor", "golang.org", "x", "tools", "go", "analysis", "passes")
		names := map[string]bool{}
		// The callback skips what it cannot read, so the walk cannot fail.
		_ = filepath.WalkDir(passes, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			for _, m := range reAnalyzerName.FindAllSubmatch(data, -1) {
				names[string(m[1])] = true
			}
			return nil
		})
		if len(names) > 0 {
			toolchainNames.names = names
		}
	})
	return toolchainNames.names
}

// restrict keeps the analyzers the toolchain knows and returns the names
// of the others.
func (f *family) restrict(names map[string]bool) []string {
	if names == nil {
		return nil
	}
	var kept []*analysis.Analyzer
	var dropped []string
	for _, a := range f.analyzers {
		if names[a.Name] {
			kept = append(kept, a)
		} else {
			dropped = append(dropped, a.Name)
			delete(f.linter, a)
		}
	}
	f.analyzers = kept
	return dropped
}
